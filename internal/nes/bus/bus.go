// Package bus は CPU アドレス空間と、1 CPU サイクルごとの進行を担う。
package bus

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/ppu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// ramSize は内蔵 RAM の大きさ。
const ramSize = 2048

// Hooks はデバッガが挿し込む観測点。
//
// nil のときは呼ばない。OnCPURead と OnCPUWrite は毎秒 180 万回程度
// 呼ばれるため、デバッグウィンドウが開いていないときは nil にする。
type Hooks struct {
	OnCPURead  func(addr uint16, value uint8)
	OnCPUWrite func(addr uint16, value uint8, old uint8)
	// OnCycle は CPU サイクルの終わりに呼ばれる。DMA が CPU を止めて
	// いる間のサイクルでも呼ぶ。
	OnCycle func()
}

// Bus は CPU から見たアドレス空間。
type Bus struct {
	region *region.Region

	ram [ramSize]uint8

	ppu  *ppu.PPU
	apu  *apu.APU
	cart cart.Cartridge

	// ports はコントローラポート 1 と 2。
	ports [input.PortCount]input.Device
	// controllerLatch は $4016 へ最後に書かれた下位 3 bit。
	//
	// 書き込み専用のレジスタであるが、セーブステートの復元後に
	// strobe の状態を再現するために保持する。
	controllerLatch uint8

	// openBus は直前にバスから読まれた値。
	//
	// マップされていないアドレスの読み出しはこの値を返す。実機では
	// バスの容量に前の値が残るためである。
	openBus uint8

	// cycles は電源投入からの CPU サイクル数。
	cycles uint64
	// ppuDotAccum は PPU のドット比を分数で累積する。
	//
	// 浮動小数点で累積すると誤差が入り、決定論が保てない。
	ppuDotAccum int

	// accessDots は CPU のバスアクセスより前に進める PPU ドット数。
	//
	// 1 CPU サイクルの間に PPU は 3 ドット（PAL では 3 または 4）進む。
	// CPU がバスにアクセスするのはそのうちどの時点かが、実機では電源投入時の
	// クロック分周器の位相で決まる。VBlank フラグの読み出し競合と NMI の
	// タイミングはこの位相に依存する。
	accessDots int

	// dma は CPU を停止させて行う転送。
	dma DMA

	// irqSources はアサートされている IRQ 発生源のビット集合。
	irqSources IRQSource

	// Warn は互換性に関わる事象を記録する。nil のとき記録しない。
	Warn func(format string, args ...any)

	hooks Hooks
}

// New はバスを作る。
//
// コントローラポートには何も接続していない状態で始める。接続は
// SetPort で行う。テスト ROM の実行のように入力を必要としない場面で
// デバイスを用意させないためである。
func New(r *region.Region, p *ppu.PPU, a *apu.APU, c cart.Cartridge) *Bus {
	b := &Bus{region: r, ppu: p, apu: a, cart: c, accessDots: defaultAccessDots}
	// 既定では停止中の再読み出しを行う。回避策を持つプログラムが
	// 正しく動作するためである。
	b.dma.conflicts = true
	for i := range b.ports {
		b.ports[i] = input.NoDevice{}
	}
	return b
}

// defaultAccessDots は CPU と PPU の位相の既定値。
//
// 実機の位相に合わせた値である。テスト ROM（ppu_vbl_nmi の 05-08）が
// この位相に依存する。
const defaultAccessDots = 2

// SetAlignment は CPU と PPU の位相を設定する。0 から 2 の範囲に収める。
func (b *Bus) SetAlignment(alignment int) {
	if alignment < 0 {
		alignment = 0
	}
	if alignment > 2 {
		alignment = 2
	}
	b.accessDots = alignment + 1
}

// SetHooks は観測点を差し替える。
func (b *Bus) SetHooks(h Hooks) { b.hooks = h }

// PowerOn は電源投入時の状態にする。RAM の内容は呼び出し側が設定する。
//
// getPutPhase は DMA の get/put の初期位相。
func (b *Bus) PowerOn(getPutPhase int) {
	conflicts := b.dma.conflicts
	b.openBus = 0
	b.cycles = 0
	b.ppuDotAccum = 0
	b.irqSources = 0
	b.dma = initDMA(getPutPhase, conflicts)
	b.controllerLatch = 0
	for _, d := range b.ports {
		d.Strobe(0)
	}
}

// RAM は内蔵 RAM を返す。電源投入時のパターンを書き込むために使う。
func (b *Bus) RAM() []uint8 { return b.ram[:] }

// Cycles は電源投入からの CPU サイクル数を返す。
func (b *Bus) Cycles() uint64 { return b.cycles }

// pendingPPUDots はこの CPU サイクルで進める PPU のドット数を返す。
//
// 比を分数で累積する。浮動小数点で累積すると誤差が入り、決定論が保てない。
func (b *Bus) pendingPPUDots() int {
	b.ppuDotAccum += b.region.PPUDotsNum
	n := 0
	for b.ppuDotAccum >= b.region.PPUDotsDen {
		b.ppuDotAccum -= b.region.PPUDotsDen
		n++
	}
	return n
}

// stepPPU は PPU を n ドット進める。
func (b *Bus) stepPPU(n int) {
	for range n {
		b.ppu.Step()
	}
}

// finishCycle は 1 CPU サイクルの残りの処理を行う。
func (b *Bus) finishCycle() {
	b.cart.Tick(1)
	b.cycles++
	if b.hooks.OnCycle != nil {
		b.hooks.OnCycle()
	}
}

// splitDots はアクセスの前後に進めるドット数を返す。
func (b *Bus) splitDots() (before, after int) {
	n := b.pendingPPUDots()
	before = min(b.accessDots, n)
	return before, n - before
}

// Read は 1 サイクルを消費してアドレスを読む。
//
// PPU をアクセスの前後に分けて進める。アクセスがサイクルのどの時点で
// 起きるかが PPU の観測結果を変えるためである。
func (b *Bus) Read(addr uint16) uint8 {
	b.serviceDMA(addr, true)
	before, after := b.splitDots()
	b.stepPPU(before)
	b.apu.Step()
	v := b.read(addr)
	b.stepPPU(after)
	b.finishCycle()
	if b.hooks.OnCPURead != nil {
		b.hooks.OnCPURead(addr, v)
	}
	return v
}

// Write は 1 サイクルを消費してアドレスへ書く。
func (b *Bus) Write(addr uint16, v uint8) {
	b.serviceDMA(addr, false)
	before, after := b.splitDots()
	b.stepPPU(before)
	b.apu.Step()
	var old uint8
	if b.hooks.OnCPUWrite != nil {
		old = b.peek(addr)
	}
	b.write(addr, v)
	b.stepPPU(after)
	b.finishCycle()
	if b.hooks.OnCPUWrite != nil {
		b.hooks.OnCPUWrite(addr, v, old)
	}
}

// read はアドレスデコードを行う。サイクルを消費しない。
//
// オープンバスの更新をここで行う。$4015 のように CPU 内部で完結する
// 読み出しは openBus を更新しない。
func (b *Bus) read(addr uint16) uint8 {
	switch {
	case addr < 0x2000:
		v := b.ram[addr&0x07FF]
		b.openBus = v
		return v

	case addr < 0x4000:
		v := b.ppu.ReadRegister(addr & 0x0007)
		b.openBus = v
		return v

	case addr < 0x4014:
		// APU レジスタは書き込み専用。読み出しはオープンバスになる。
		return b.openBus

	case addr == 0x4014:
		// OAM DMA レジスタは書き込み専用。
		return b.openBus

	case addr == 0x4015:
		// CPU 内部で完結するため openBus を更新しない。
		return b.apu.ReadStatus()

	case addr == 0x4016:
		// オープンバスを更新しない。上位 3 bit は元の値をそのまま
		// 返すため、更新してもしなくても同じ値になる。下位 5 bit を
		// 書き戻すと、次の未接続アドレスの読み出しが変わってしまう。
		return b.readController(0)

	case addr == 0x4017:
		return b.readController(1)

	case addr < 0x4020:
		return b.openBus
	}

	if v, ok := b.cart.ReadPRG(addr); ok {
		b.openBus = v
		return v
	}
	return b.openBus
}

// write はアドレスデコードを行う。サイクルを消費しない。
func (b *Bus) write(addr uint16, v uint8) {
	switch {
	case addr < 0x2000:
		b.ram[addr&0x07FF] = v
		return

	case addr < 0x4000:
		b.ppu.WriteRegister(addr&0x0007, v)
		return

	case addr < 0x4014:
		b.apu.WriteRegister(addr, v)
		return

	case addr == 0x4014:
		b.requestOAMDMA(v)
		return

	case addr == 0x4015:
		b.apu.WriteRegister(addr, v)
		return

	case addr == 0x4016:
		b.writeController(v)
		return

	case addr == 0x4017:
		b.apu.WriteRegister(addr, v)
		return

	case addr < 0x4020:
		return
	}

	b.cart.WritePRG(addr, v)
}

// Peek は副作用を起こさずに値を読む。tick もフックも呼ばない。
//
// デバッガとトレース出力はこれを使う。Read を使うと PPU レジスタや
// コントローラの状態が変化する。
func (b *Bus) Peek(addr uint16) uint8 { return b.peek(addr) }

// unreadableValue は副作用なしに読めないレジスタを Peek したときの値。
//
// $4000-$401F のレジスタは書き込み専用か、読み出しに副作用を持つ。
// 副作用なしの読み出しに定まった値がないため、固定値を返す。オープンバスの
// 値を返すと、同じアドレスがメモリダンプを更新するたびに違う値に見え、
// 表示として誤解を招く。APU とコントローラの状態は専用の表示から
// 各コンポーネントへ直接問い合わせる。
const unreadableValue = 0xFF

func (b *Bus) peek(addr uint16) uint8 {
	switch {
	case addr < 0x2000:
		return b.ram[addr&0x07FF]
	case addr < 0x4000:
		return b.ppu.PeekRegister(addr & 0x0007)
	case addr == 0x4016:
		return b.peekController(0)
	case addr == 0x4017:
		return b.peekController(1)
	case addr < 0x4020:
		return unreadableValue
	}
	if v, ok := b.cart.ReadPRG(addr); ok {
		return v
	}
	return b.openBus
}

// Poke は副作用を起こさずに値を書く。tick もフックも呼ばない。
func (b *Bus) Poke(addr uint16, v uint8) {
	switch {
	case addr < 0x2000:
		b.ram[addr&0x07FF] = v
	case addr < 0x4020:
		// レジスタへの書き込みは副作用そのものであるため何もしない。
	default:
		b.cart.WritePRG(addr, v)
	}
}

// OpenBus はオープンバスの値を返す。
func (b *Bus) OpenBus() uint8 { return b.openBus }

// NMILine は NMI 線の状態を返す。PPU の状態をそのまま渡す。
func (b *Bus) NMILine() bool { return b.ppu.NMILine() }

// SaveState は状態を書く。
func (b *Bus) SaveState(w *state.Writer) {
	end := w.Section("bus")
	w.RawBytes(b.ram[:])
	w.U8(b.openBus)
	w.U64(b.cycles)
	w.Int(b.ppuDotAccum)
	w.Int(b.accessDots)
	w.U8(uint8(b.irqSources))
	b.dma.SaveState(w)
	b.saveInput(w)
	end()
}

// LoadState は状態を読む。
func (b *Bus) LoadState(r *state.Reader) error {
	end := r.RequireSection("bus")
	r.RawBytes(b.ram[:])
	b.openBus = r.U8()
	b.cycles = r.U64()
	b.ppuDotAccum = r.Int()
	b.accessDots = r.Int()
	b.irqSources = IRQSource(r.U8())
	b.dma.LoadState(r)
	if err := b.loadInput(r); err != nil {
		return err
	}
	end()
	return r.Err()
}
