package bus

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// dmcKind は DMC DMA の種別。
type dmcKind uint8

const (
	// dmcLoad は $4015 でチャンネルを有効にした直後の読み出し。
	dmcLoad dmcKind = iota
	// dmcReload は再生中にサンプルバッファが空になったときの読み出し。
	dmcReload
)

// DMA は CPU を停止させて行う転送。
//
// CPU の外側に置く。DMA は CPU を止めてバスを奪う外部の仕組みであり、
// CPU のコードに知識を入れると命令ごとのサイクル列に分岐が混ざる。
type DMA struct {
	// getPutInvert は get/put の位相を反転するかどうか。
	//
	// get/put は APU のクロックそのものである。CPU と APU は電源投入時に
	// 2 通りのアライメントのいずれかに入るため、どちらを get と呼ぶかを
	// InitState.DMAGetPutPhase で選ぶ。
	getPutInvert bool
	// haltedAddr は CPU を止めたときに読もうとしていたアドレス。
	//
	// 停止中はこのアドレスの読み出しが繰り返される。読み出しに副作用を
	// 持つレジスタだった場合、副作用が複数回起こる。
	haltedAddr uint16

	// oamPending は $4014 への書き込みで立つ。
	oamPending bool
	// oamPage は転送元のページ。
	oamPage uint8

	// dmcPending は DMC がサンプルを必要としていることを表す。
	dmcPending bool
	// dmcKind は要求の種別。
	dmcKind dmcKind
	// dmcAddr は読み出すサンプルのアドレス。
	dmcAddr uint16

	// running は転送中であることを表す。再入を防ぐ。
	running bool

	// conflicts は停止中の再読み出しを行うかどうか。
	conflicts bool
	// conflictCount は再読み出しが起きた回数。
	conflictCount uint64
	// 計測用
	dmcCount  uint64
	oamCount  uint64
	dmaCycles uint64
}

// oamDataRegister は転送先のレジスタ。
const oamDataRegister = 0x2004

// oamTransferBytes は 1 回の OAM DMA で転送するバイト数。
const oamTransferBytes = 256

// initDMA は電源投入時の DMA の状態を作る。
//
// getPutPhase は get/put の初期位相。CPU と APU は電源投入時に 2 通りの
// アライメントのいずれかに入る。
func initDMA(getPutPhase int, conflicts bool) DMA {
	return DMA{
		getPutInvert: getPutPhase%2 != 0,
		conflicts:    conflicts,
	}
}

// SetDMCRegisterConflicts は停止中の再読み出しを行うかを設定する。
//
// 2A07（PAL）ではこの挙動が修正されている。リージョンに加えて設定でも
// 切り替えられるようにするのは、無効にしたほうが動くプログラムがある
// ときに選べるようにするためである。
func (b *Bus) SetDMCRegisterConflicts(v bool) { b.dma.conflicts = v }

// requestOAMDMA は $4014 への書き込みを受けて転送を要求する。
//
// すでに要求されているときはページを上書きする。`INC $4014` のような
// RMW 命令は同じアドレスへ 2 回書き込む。2 回目に書かれたページから
// 転送される。
func (b *Bus) requestOAMDMA(page uint8) {
	b.dma.oamPending = true
	b.dma.oamPage = page
}

// RequestDMCFetch は DMC のサンプル 1 バイトの読み出しを要求する。
//
// APU から呼ばれる。ここでは要求を立てるだけで、停止と読み出しは
// 次のバスアクセスの先頭で行う。実機でも停止は次のサイクルから始まる。
func (b *Bus) RequestDMCFetch(addr uint16, reload bool) {
	b.dma.dmcPending = true
	b.dma.dmcAddr = addr
	if reload {
		b.dma.dmcKind = dmcReload
		return
	}
	b.dma.dmcKind = dmcLoad
}

// serviceDMA は要求されている転送を実行する。
//
// CPU のバスアクセスの直前に呼ぶ。実機では DMA が CPU を停止させて
// バスを奪うため、CPU から見ると次のアクセスの前に時間が飛ぶ。
//
// addr と isRead は、これから CPU が行おうとしているアクセスである。
func (b *Bus) serviceDMA(addr uint16, isRead bool) {
	d := &b.dma
	if d.running || (!d.oamPending && !d.dmcPending) {
		return
	}
	// 停止できるのは CPU のリードサイクルだけである。ライトサイクルでは
	// 失敗し、次のサイクルで再試行する。RMW 命令の 2 連続ライトや割り込みの
	// 3 連続ライトにあたると、停止が最大 3 サイクル遅れる。
	if !isRead {
		return
	}

	d.running = true
	defer func() { d.running = false }()
	d.haltedAddr = addr

	// halt サイクル。CPU は止まるがアドレスバスは保たれ、同じ読み出しが
	// 繰り返される。
	b.dmaNoOpCycle()

	for {
		switch {
		case d.dmcPending:
			b.runDMCFetch()
		case d.oamPending:
			b.runOAMTransfer()
		default:
			return
		}
	}
}

// runDMCFetch は DMC のサンプル 1 バイト分の停止を行う。
//
// halt の後に dummy サイクルを 1 つ挟み、get サイクルへそろえてから
// 読み出す。合計 3 または 4 サイクルになる。
func (b *Bus) runDMCFetch() {
	d := &b.dma
	d.dmcPending = false
	d.dmcCount++

	// dummy サイクル
	b.dmaNoOpCycle()
	b.alignToGetCycle()

	// get サイクル。ここでサンプルを読んで APU へ渡す。
	v := b.ReadPRG(d.dmcAddr)
	b.dmaIdleCycle()
	b.apu.CompleteDMCFetch(v)
}

// runOAMTransfer は OAM への 256 バイトの転送を行う。
func (b *Bus) runOAMTransfer() {
	d := &b.dma
	d.oamPending = false
	d.oamCount++
	base := uint16(d.oamPage) << 8

	b.alignToGetCycle()

	for i := range oamTransferBytes {
		// DMC DMA は OAM DMA より優先される。転送の途中で要求が
		// 来たときは、そこで 1 バイト読ませてから続きに戻る。
		if d.dmcPending {
			b.runDMCFetch()
			b.alignToGetCycle()
		}
		v := b.Read(base + uint16(i))
		b.Write(oamDataRegister, v)
	}
}

// isGetCycle は次のサイクルが get サイクルかを返す。
//
// get/put は APU のクロックの前半・後半である。APU の位相をそのまま
// 使う。バスが別に数えると、同じクロックのはずの 2 つが食い違う。
func (b *Bus) isGetCycle() bool {
	// EvenCycle は直前に進めたサイクルの位相である。これから実行する
	// サイクルの位相はその反転になる。
	return b.apu.EvenCycle() == b.dma.getPutInvert
}

// alignToGetCycle は次が get サイクルになるまで 1 サイクル消費する。
func (b *Bus) alignToGetCycle() {
	if !b.isGetCycle() {
		b.dmaNoOpCycle()
	}
}

// dmaNoOpCycle は転送を行わない 1 サイクルを消費する。
//
// このサイクルでは CPU が読もうとしていたアドレスが読み直される。
func (b *Bus) dmaNoOpCycle() {
	b.repeatHaltedRead()
	b.dmaIdleCycle()
}

// repeatHaltedRead は停止時に読んでいたアドレスを読み直す。
//
// 実機で起きているのは「CPU が同じリードサイクルを繰り返す」ことである。
// これをそのまま表すことで、$2007・$2002・$4015・$4016・$4017 の
// すべてで正しい副作用が起こる。デバイス側に個別の処理を置くと、
// 対象を 1 つ見落としたときに気づけない。
func (b *Bus) repeatHaltedRead() {
	if !b.region.DMCDMARegisterConflict || !b.dma.conflicts {
		return
	}
	if !hasReadSideEffect(b.dma.haltedAddr) {
		return
	}
	// read を呼ぶ。Read ではない。サイクルを二重に数えないためである。
	_ = b.read(b.dma.haltedAddr)
	b.dma.conflictCount++
	if b.Warn != nil {
		b.Warn("dma: 停止中に $%04X を読み直した（レジスタ競合）", b.dma.haltedAddr)
	}
}

// hasReadSideEffect は読み出しに副作用を持つアドレスかを返す。
//
// 副作用の無いアドレスまで読み直すと、オープンバスの値だけが変わる。
// 実機でも同じ値が読まれるだけであり、観測できる違いは生じない。
// 対象を絞るのは、競合の記録を意味のある場面に限るためである。
func hasReadSideEffect(addr uint16) bool {
	switch {
	case addr >= 0x2000 && addr < 0x4000:
		// $2002 と $2007 に副作用がある。ミラーを畳んで判定する。
		reg := addr & 0x0007
		return reg == 2 || reg == 7
	case addr == 0x4015, addr == 0x4016, addr == 0x4017:
		return true
	}
	return false
}

// dmaIdleCycle は転送を行わない 1 サイクルを消費する。
func (b *Bus) dmaIdleCycle() {
	before, after := b.splitDots()
	b.stepPPU(before)
	b.apu.Step()
	b.stepPPU(after)
	b.finishCycle()
}

// DMAStats は転送の回数を返す。
func (b *Bus) DMAStats() (dmc, oam uint64) {
	return b.dma.dmcCount, b.dma.oamCount
}

// ConflictCount はレジスタ競合が起きた回数を返す。
func (b *Bus) ConflictCount() uint64 { return b.dma.conflictCount }

// SaveState は DMA の状態を書く。
func (d *DMA) SaveState(w *state.Writer) {
	end := w.Section("dma")
	w.Bool(d.getPutInvert)
	w.U16(d.haltedAddr)
	w.Bool(d.oamPending)
	w.U8(d.oamPage)
	w.Bool(d.dmcPending)
	w.U8(uint8(d.dmcKind))
	w.U16(d.dmcAddr)
	end()
}

// LoadState は DMA の状態を読む。
//
// conflicts は設定から来る値であり、エミュレーション状態ではないため
// 読み書きしない。
func (d *DMA) LoadState(r *state.Reader) {
	end := r.RequireSection("dma")
	d.getPutInvert = r.Bool()
	d.haltedAddr = r.U16()
	d.oamPending = r.Bool()
	d.oamPage = r.U8()
	d.dmcPending = r.Bool()
	d.dmcKind = dmcKind(r.U8())
	d.dmcAddr = r.U16()
	end()
}

// ReadPRG は DMC のサンプルを読む。
//
// カートリッジから直接読む。DMC が読むのは $8000 以降であり、
// レジスタの副作用を起こさない。
func (b *Bus) ReadPRG(addr uint16) uint8 {
	if v, ok := b.cart.ReadPRG(addr); ok {
		return v
	}
	return b.openBus
}
