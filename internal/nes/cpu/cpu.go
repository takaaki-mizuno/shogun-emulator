// Package cpu は 6502 コア（Ricoh 2A03 の CPU 部）を実装する。
//
// アドレッシングモードごとのサイクルシーケンスを手続きとして書き、バス
// アクセスのたびにバス側の tick が呼ばれる方式を採る。調査結果のサイクル
// 単位の表とコードが 1 対 1 で対応するため、nestest のログとの差分から
// 誤りの箇所を特定できる。
//
// この方式の帰結として、命令の途中では CPU の状態がホストのコールスタックに
// 存在する。スナップショットを取れるのは命令境界のみである。
package cpu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// Bus は CPU から見たバス。
//
// インタフェースで受け取るのは、CPU を bus パッケージの具体型から
// 切り離してテストできるようにするためである。
type Bus interface {
	// Read は 1 サイクルを消費して読む。
	Read(addr uint16) uint8
	// Write は 1 サイクルを消費して書く。
	Write(addr uint16, v uint8)
	// Peek は副作用を起こさずに読む。トレース出力に使う。
	Peek(addr uint16) uint8
	// NMILine は NMI 線の状態を返す。負論理。
	NMILine() bool
	// IRQAsserted はいずれかの発生源が IRQ をアサートしているかを返す。
	IRQAsserted() bool
	// Cycles は電源投入からの累積サイクル数を返す。
	//
	// 1 CPU サイクル = 1 バスアクセスであり、DMA が CPU を止めている間の
	// サイクルもバスが数える。計数の持ち主を 1 つにするため CPU は
	// 自前の計数を持たない。
	Cycles() uint64
}

// stackBase はスタックのベースアドレス。スタックは $0100-$01FF に置かれる。
const stackBase = 0x0100

// ベクタのアドレス。
const (
	vectorNMI   = 0xFFFA
	vectorReset = 0xFFFC
	vectorIRQ   = 0xFFFE
)

// CPU は 6502 コア。
type CPU struct {
	A, X, Y uint8
	S       uint8
	PC      uint16

	// ステータスフラグ。6 個の bool として持つ。
	//
	// B と bit 5 は CPU 内部に存在しない。push するときに引数で与える。
	C, Z, I, D, V, N bool

	bus Bus

	// nmiLinePrev は前サイクルの NMI 線の状態。立ち下がりの検出に使う。
	nmiLinePrev bool
	// nmiPending は立ち下がりを検出してから処理されるまで true。
	nmiPending bool

	// pollNMI と pollIRQ は現サイクルの終わりのポーリング結果。
	pollNMI bool
	pollIRQ bool
	// pollNMIPrev と pollIRQPrev は前サイクルの終わりのポーリング結果。
	//
	// 割り込みが実際に効くのは「命令の最後から 2 番目のサイクルの終わり」の
	// 割り込み線の状態である。最後のサイクルを終えた時点でこの 2 つが
	// その位置の結果を保持している。
	pollNMIPrev bool
	pollIRQPrev bool

	// overridePoll は命令側がポーリング結果を明示的に決めたことを表す。
	// 分岐命令だけが使う（§3.5.1 のポーリング点）。
	overridePoll bool
	overrideNMI  bool
	overrideIRQ  bool

	// indexBase と indexCrossed はインデックス付きアドレッシングの
	// 途中の情報。不安定な非公式命令が書き込む値とアドレスを決めるのに使う。
	indexBase    uint16
	indexCrossed bool

	// opPC は実行中の命令の opcode が置かれたアドレス。
	// 互換性の記録とデバッガの表示に使う。
	opPC uint16

	// halted は STP を実行したことを表す。
	halted bool

	// writesSuppressed はリセットシーケンス中にライトを抑止する。
	writesSuppressed bool

	// Warn は互換性に関わる事象を記録する。nil のとき記録しない。
	Warn func(format string, args ...any)

	// OnInterrupt は割り込みシーケンスを終えたときに呼ばれる。nil の
	// とき呼ばない。デバッガのイベントブレークポイントが使う。
	OnInterrupt func(k Interrupt)
}

// New は CPU を作る。
func New(b Bus) *CPU {
	return &CPU{bus: b}
}

// Cycles は電源投入からの CPU サイクル数を返す。
func (c *CPU) Cycles() uint64 { return c.bus.Cycles() }

// Halted は STP によって停止しているかを返す。
func (c *CPU) Halted() bool { return c.halted }

// read は 1 サイクル消費して読み、割り込み線を採取する。
//
// すべてのバスアクセスをここに通す。1 サイクルごとに NMI の立ち下がりを
// 検出する必要があるためである。
func (c *CPU) read(addr uint16) uint8 {
	v := c.bus.Read(addr)
	c.endCycle()
	return v
}

// write は 1 サイクル消費して書き、割り込み線を採取する。
//
// リセットシーケンス中はライトを抑止する。実機ではリセット時に 3 回の
// スタックアクセスが起きるが、メモリは変更されない。
func (c *CPU) write(addr uint16, v uint8) {
	if c.writesSuppressed {
		c.bus.Read(addr)
	} else {
		c.bus.Write(addr, v)
	}
	c.endCycle()
}

// fetch は PC から 1 バイト読み、PC を進める。
func (c *CPU) fetch() uint8 {
	v := c.read(c.PC)
	c.PC++
	return v
}

// packP は push する 1 バイトを組み立てる。
//
// bit 5 は常に 1 である。bFlag は BRK と PHP で true、NMI と IRQ で false。
func (c *CPU) packP(bFlag bool) uint8 {
	var v uint8 = 0x20
	if c.C {
		v |= 0x01
	}
	if c.Z {
		v |= 0x02
	}
	if c.I {
		v |= 0x04
	}
	if c.D {
		v |= 0x08
	}
	if bFlag {
		v |= 0x10
	}
	if c.V {
		v |= 0x40
	}
	if c.N {
		v |= 0x80
	}
	return v
}

// SetP は P の値を各フラグへ展開する。デバッガのレジスタ編集に使う。
func (c *CPU) SetP(v uint8) { c.unpackP(v) }

// unpackP はスタックから取り出した値を反映する。
// bit 5 と bit 4 は CPU 内部に存在しないため無視する。
func (c *CPU) unpackP(v uint8) {
	c.C = v&0x01 != 0
	c.Z = v&0x02 != 0
	c.I = v&0x04 != 0
	c.D = v&0x08 != 0
	c.V = v&0x40 != 0
	c.N = v&0x80 != 0
}

// P はステータスレジスタの値を返す。トレース出力に使う。
func (c *CPU) P() uint8 { return c.packP(false) }

// setZN は値から Z と N を決める。
func (c *CPU) setZN(v uint8) {
	c.Z = v == 0
	c.N = v&0x80 != 0
}

// push は 1 バイトを積む。
func (c *CPU) push(v uint8) {
	c.write(stackBase|uint16(c.S), v)
	c.S--
}

// pull は 1 バイトを取り出す。
func (c *CPU) pull() uint8 {
	c.S++
	return c.read(stackBase | uint16(c.S))
}

// peekStack はスタックポインタの指す先をダミーで読む。
//
// PLA・PLP・RTS・RTI は取り出しの前に 1 サイクル分のスタックアクセスを行う。
func (c *CPU) peekStack() {
	c.read(stackBase | uint16(c.S))
}

// PowerOn は電源投入時の状態にする。
//
// A・X・Y を 0、S を 0 にした上でリセットシーケンスを実行する。
// シーケンス中に S が 3 回デクリメントされるため S は $FD になり、
// 7 サイクルを消費して PC が ($FFFC) になる。実機の電源投入後の状態を
// 別に書き下すのではなく、同じ経路で作る。
func (c *CPU) PowerOn() {
	c.A, c.X, c.Y = 0, 0, 0
	c.S = 0x00
	c.C, c.Z, c.D, c.V, c.N = false, false, false, false, false
	c.I = true
	c.nmiLinePrev = true
	c.nmiPending = false
	c.pollNMI, c.pollIRQ = false, false
	c.pollNMIPrev, c.pollIRQPrev = false, false
	c.overridePoll = false
	c.halted = false
	c.PC = 0
	c.resetSequence()
}

// Reset はリセットを掛ける。RAM とレジスタの内容は変更しない。
func (c *CPU) Reset() {
	c.halted = false
	c.nmiPending = false
	c.pollNMI, c.pollIRQ = false, false
	c.pollNMIPrev, c.pollIRQPrev = false, false
	c.overridePoll = false
	c.resetSequence()
}

// resetSequence は割り込みシーケンスと同じ流れをライトを抑止して実行する。
func (c *CPU) resetSequence() {
	c.writesSuppressed = true
	c.serviceInterrupt(interruptReset)
	c.writesSuppressed = false
}

// SaveState は状態を書く。
func (c *CPU) SaveState(w *state.Writer) {
	end := w.Section("cpu")
	w.U8(c.A)
	w.U8(c.X)
	w.U8(c.Y)
	w.U8(c.S)
	w.U16(c.PC)
	w.Bool(c.C)
	w.Bool(c.Z)
	w.Bool(c.I)
	w.Bool(c.D)
	w.Bool(c.V)
	w.Bool(c.N)
	// エッジ検出のために前サイクルの NMI 線を保存する。
	// 省くと復元直後に NMI を取りこぼすか二重に発生させる。
	w.Bool(c.nmiLinePrev)
	w.Bool(c.nmiPending)
	// ポーリング結果を 2 サイクル分保存する。命令境界でのスナップショット
	// でも、次の命令の判定に前サイクルの結果が使われる。
	w.Bool(c.pollNMI)
	w.Bool(c.pollIRQ)
	w.Bool(c.pollNMIPrev)
	w.Bool(c.pollIRQPrev)
	w.Bool(c.halted)
	end()
}

// LoadState は状態を読む。
func (c *CPU) LoadState(r *state.Reader) error {
	end := r.RequireSection("cpu")
	c.A = r.U8()
	c.X = r.U8()
	c.Y = r.U8()
	c.S = r.U8()
	c.PC = r.U16()
	c.C = r.Bool()
	c.Z = r.Bool()
	c.I = r.Bool()
	c.D = r.Bool()
	c.V = r.Bool()
	c.N = r.Bool()
	c.nmiLinePrev = r.Bool()
	c.nmiPending = r.Bool()
	c.pollNMI = r.Bool()
	c.pollIRQ = r.Bool()
	c.pollNMIPrev = r.Bool()
	c.pollIRQPrev = r.Bool()
	c.halted = r.Bool()
	end()
	return r.Err()
}

// warn は互換性に関わる事象を記録する。
func (c *CPU) warn(format string, args ...any) {
	if c.Warn != nil {
		c.Warn(format, args...)
	}
}
