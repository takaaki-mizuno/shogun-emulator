// Package nes は NES 一式を組み立て、命令単位とフレーム単位の実行を提供する。
package nes

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/bus"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/ppu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// InitState は電源投入時に実機で値が定まらない状態を決める。
//
// 直列化の基盤と同じ型を使う。ステートとムービーのヘッダがこの値を
// 持ち、復元時にそのまま電源投入へ渡せるようにするためである。
type InitState = state.Init

// Deterministic は値が定まらない状態をすべて固定値にした InitState を返す。
func Deterministic() InitState {
	return InitState{RAMPattern: state.PatternZero}
}

// NES は本体一式。
type NES struct {
	Region *region.Region
	ROM    *cart.ROM

	Cart cart.Cartridge
	Bus  *bus.Bus
	CPU  *cpu.CPU
	PPU  *ppu.PPU
	APU  *apu.APU

	// Ports はコントローラポート 1 と 2 に接続しているデバイス。
	Ports [input.PortCount]input.Device

	init  InitState
	hooks Hooks

	// frames は電源投入からのフレーム数。
	frames uint64

	// Version と Commit はセーブステートのヘッダに書く識別情報。
	Version string
	Commit  string

	// Warn は互換性に関わる事象を記録する。nil のとき記録しない。
	// セーブステートのオーバーレイのハッシュの不一致に使う。
	Warn func(format string, args ...any)
}

// warn は互換性に関わる事象を記録する。
func (n *NES) warn(format string, args ...any) {
	if n.Warn != nil {
		n.Warn(format, args...)
	}
}

// Options は組み立て時に決まる設定。
//
// 実機に複数の版があり、プログラムがどちらかに依存する箇所をここに集める。
type Options struct {
	// Cart はマッパーの挙動の設定。
	Cart cart.Options
}

// DefaultOptions は既定の設定を返す。
func DefaultOptions() Options {
	return Options{Cart: cart.DefaultOptions()}
}

// New は既定の設定で ROM からエミュレータを組み立てる。
func New(rom *cart.ROM, r *region.Region) (*NES, error) {
	return NewWithOptions(rom, r, DefaultOptions())
}

// NewWithOptions は ROM からエミュレータを組み立てる。
func NewWithOptions(rom *cart.ROM, r *region.Region, o Options) (*NES, error) {
	c, err := cart.NewWithOptions(rom, o.Cart)
	if err != nil {
		return nil, err
	}
	n := &NES{
		Region:  r,
		ROM:     rom,
		Cart:    c,
		PPU:     ppu.New(r, c),
		APU:     apu.New(r),
		Version: "dev",
		Commit:  "none",
	}
	n.Bus = bus.New(r, n.PPU, n.APU, c)
	n.CPU = cpu.New(n.Bus)
	n.APU.SetBus(n.Bus)
	for i := range n.Ports {
		n.Ports[i] = input.NoDevice{}
	}
	return n, nil
}

// ConnectStandardControllers はポート 1 と 2 に標準コントローラを接続する。
//
// src はボタンの押下状態の供給元である。nil のときは何も押されていない
// ものとして扱う。
func (n *NES) ConnectStandardControllers(src input.Source) {
	for i := range n.Ports {
		d := input.NewStandardController(src, i)
		n.Ports[i] = d
		n.Bus.SetPort(i, d)
	}
}

// RegionForROM は ROM のタイミングモードからリージョンを決める。
//
// 複数リージョン対応の ROM は NTSC で動かす。どちらでも動くことを
// 示す値であり、既定を選ぶ必要があるためである。
func RegionForROM(rom *cart.ROM) *region.Region {
	switch rom.TimingMode {
	case cart.TimingPAL:
		return region.PAL
	case cart.TimingDendy:
		return region.Dendy
	}
	return region.NTSC
}

// PowerOn は電源を入れる。
func (n *NES) PowerOn(init InitState) {
	n.init = init
	n.frames = 0

	n.Bus.PowerOn(init.DMAGetPutPhase)
	state.FillPattern(n.Bus.RAM(), init.RAMPattern, init.RAMSeed, state.SaltRAM)

	n.PPU.PowerOn(init.PPUVBlankFlag)
	state.FillPattern(n.PPU.OAM(), init.RAMPattern, init.RAMSeed, state.SaltOAM)
	state.FillPattern(n.PPU.Palette(), init.RAMPattern, init.RAMSeed, state.SaltPalette)
	state.FillPattern(n.PPU.CIRAM(), init.RAMPattern, init.RAMSeed, state.SaltCIRAM)
	n.APU.PowerOn()

	// CPU のリセットシーケンスは 7 サイクルを消費し、その間 PPU と APU が
	// 進む。バスと PPU を先に初期化してから CPU を動かす。
	n.CPU.PowerOn()
}

// Reset はリセットボタンを押す。RAM の内容は変更しない。
func (n *NES) Reset() {
	n.PPU.Reset()
	n.APU.Reset()
	n.CPU.Reset()
}

// Hooks はデバッガが挿し込む観測点。
//
// nil のときは呼ばない。PPU のメモリアクセスに関わるフックは、
// デバッガを実装するフェーズで加える。
type Hooks struct {
	// OnCPURead と OnCPUWrite は毎秒 180 万回程度呼ばれる。
	// フック内で UI を操作せず、共有構造体への書き込みのみを行う。
	OnCPURead  func(addr uint16, value uint8)
	OnCPUWrite func(addr uint16, value uint8, old uint8)
	// OnInstructionStart は命令のフェッチ直前に呼ばれる。
	OnInstructionStart func(s cpu.State)
	// OnBeforeExec は命令のフェッチ前に PC を渡して呼ばれる。true を
	// 返すと、その命令を実行せずに StepInstruction から戻る。
	//
	// 実行ブレークポイントに使う。逆アセンブルを伴う OnInstructionStart
	// と分けるのは、ブレークポイントを置くだけで毎命令の逆アセンブルが
	// 走ることを避けるためである。
	OnBeforeExec func(pc uint16) bool
	// OnCycle は CPU サイクルの終わりに呼ばれる。
	OnCycle func()
	// OnInterrupt は NMI・IRQ・BRK・リセットの割り込みシーケンスを
	// 終えたときに呼ばれる。
	OnInterrupt func(k cpu.Interrupt)
	// OnSprite0Hit はスプライト 0 ヒットのフラグが立ったときに呼ばれる。
	OnSprite0Hit func()
	// OnFrameComplete はフレームが完成したときに呼ばれる。
	OnFrameComplete func(f *video.Frame)
}

// SetHooks は観測点を差し替える。
//
// デバッグウィンドウが開いていないときは空の Hooks を渡す。フックの
// 呼び出し自体が実行速度に影響するためである。
func (n *NES) SetHooks(h Hooks) {
	n.hooks = h
	n.Bus.SetHooks(bus.Hooks{
		OnCPURead:  h.OnCPURead,
		OnCPUWrite: h.OnCPUWrite,
		OnCycle:    h.OnCycle,
	})
	n.PPU.Hooks = ppu.Hooks{
		OnFrameComplete: h.OnFrameComplete,
		OnSprite0Hit:    h.OnSprite0Hit,
	}
	n.CPU.OnInterrupt = h.OnInterrupt
}

// Hooks は現在の観測点を返す。
func (n *NES) Hooks() Hooks { return n.hooks }

// StepInstruction は 1 命令を実行する。
//
// OnBeforeExec が true を返したときは命令を実行せずに false を返す。
// 実行したときは true を返す。
func (n *NES) StepInstruction() bool {
	if n.hooks.OnBeforeExec != nil && n.hooks.OnBeforeExec(n.CPU.PC) {
		return false
	}
	if n.hooks.OnInstructionStart != nil {
		n.hooks.OnInstructionStart(n.CPU.State(n.PPU.Scanline(), n.PPU.Dot()))
	}
	n.CPU.StepInstruction()
	n.frames = n.PPU.Frame()
	return true
}

// RunFrame は PPU のフレーム境界まで命令を実行する。
//
// OnBeforeExec が命令を止めたときは、そこで戻る。
func (n *NES) RunFrame() {
	start := n.PPU.Frame()
	for n.PPU.Frame() == start {
		if !n.StepInstruction() {
			return
		}
	}
}

// RunFrames は n フレーム進める。
func (n *NES) RunFrames(count int) {
	for range count {
		n.RunFrame()
	}
}

// Cycles は電源投入からの累積 CPU サイクル数を返す。
func (n *NES) Cycles() uint64 { return n.Bus.Cycles() }

// Frames は電源投入からの累積フレーム数を返す。
func (n *NES) Frames() uint64 { return n.frames }

// TakeFrame は完成したフレームを返す。無いとき nil を返す。
func (n *NES) TakeFrame() *video.Frame { return n.PPU.Queue().Take() }

// PeekVRAM は副作用を起こさずに PPU アドレス空間を読む。
func (n *NES) PeekVRAM(addr uint16) uint8 { return n.PPU.PeekVRAM(addr) }

// Peek は副作用を起こさずに CPU アドレス空間を読む。
func (n *NES) Peek(addr uint16) uint8 { return n.Bus.Peek(addr) }

// TraceLine は現在の命令境界のトレース行を返す。
func (n *NES) TraceLine() string {
	return n.CPU.State(n.PPU.Scanline(), n.PPU.Dot()).TraceLine()
}

// Header は現在の状態に対応するステートのヘッダを返す。
//
// スクリーンショットは含めない。保存する側が必要に応じて入れる。
func (n *NES) Header() state.Header {
	return state.Header{
		FormatVersion: state.FormatVersion,
		Version:       n.Version,
		Commit:        n.Commit,
		ROMHash:       n.ROM.Hash,
		Mapper:        n.ROM.Mapper,
		Submapper:     n.ROM.Submapper,
		Region:        n.Region.Name,
		Init:          n.init,
		Cycles:        n.Bus.Cycles(),
		Frames:        n.frames,
		OverlayHash:   n.ROM.OverlayHash(),
	}
}

// SaveState は状態を直列化する。
func (n *NES) SaveState() []byte { return n.SaveStateWithScreenshot(nil) }

// SaveStateWithScreenshot はスクリーンショットを添えて状態を直列化する。
//
// スクリーンショットはスロットの一覧表示に使う。エミュレーション状態では
// ないため、復元時には読み捨てる。
func (n *NES) SaveStateWithScreenshot(png []uint8) []byte {
	w := state.NewWriter()

	h := n.Header()
	h.Screenshot = png
	h.Write(w)

	endBody := w.Section("nes")
	n.CPU.SaveState(w)
	n.Bus.SaveState(w)
	n.PPU.SaveState(w)
	n.APU.SaveState(w)
	n.Cart.SaveState(w)
	endBody()

	return w.Data()
}

// LoadState は直列化された状態を復元する。
//
// ヘッダの不一致はエラーにする。別の ROM や別のバージョンのステートを
// 読み込んで、原因の分からない誤動作を起こすことを避けるためである。
func (n *NES) LoadState(b []uint8) error {
	r := state.NewReader(b)

	var h state.Header
	if err := h.Read(r); err != nil {
		return err
	}
	want := n.Header()
	if err := h.Verify(&want); err != nil {
		return err
	}
	if h.OverlayHash != want.OverlayHash {
		// タイルを編集した後に編集前のステートを読むことはデバッグで普通に
		// 起こる。ロードは続け、記録だけを残す（設計書 08 編 §8.2.2）。
		n.warn("セーブステートのオーバーレイのハッシュが現在と違う（保存時 %x、現在 %x）",
			h.OverlayHash, want.OverlayHash)
	}
	n.init = h.Init
	// 累積 CPU サイクル数は各コンポーネントの状態から復元される。
	n.frames = h.Frames

	endBody := r.RequireSection("nes")
	if err := n.CPU.LoadState(r); err != nil {
		return err
	}
	if err := n.Bus.LoadState(r); err != nil {
		return err
	}
	if err := n.PPU.LoadState(r); err != nil {
		return err
	}
	if err := n.APU.LoadState(r); err != nil {
		return err
	}
	if err := n.Cart.LoadState(r); err != nil {
		return err
	}
	endBody()
	return r.Err()
}
