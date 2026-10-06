// Package ppu は PPU（RP2C02 / RP2C07）を実装する。
//
// 1 ドットを単位とするステートマシンとして実装する。Step の 1 回の呼び出しが
// 1 PPU ドットに対応する。画面途中でのスクロール変更、VBlank フラグの設定と
// 読み出しの競合、マッパーの A12 監視が、いずれも 1 ドットの精度を要する
// ためである。
package ppu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// PPU は画像処理ユニット。
type PPU struct {
	region *region.Region
	bus    Bus

	// 外部から見えるレジスタ
	ctrl    Control
	mask    Mask
	status  Status
	oamAddr uint8

	// 内部レジスタ
	v uint16 // 15 bit。現在の VRAM アドレス
	t uint16 // 15 bit。一時アドレス
	x uint8  // 3 bit。fine X
	w bool   // 書き込みトグル

	readBuffer uint8 // $2007 のリードバッファ
	ioLatch    uint8 // I/O バスの動的ラッチ
	// latchExpiry は ioLatch の各ビットが保持を失う時刻をドット数で表す。
	//
	// I/O バスは動的ラッチであり、駆動されないと電荷が抜けて 0 に戻る。
	// 減衰はビットごとに独立している。$2002 の読み出しは上位 3 bit だけを
	// バスへ駆動するため、下位 5 bit は減衰を続ける。
	latchExpiry [8]uint64
	// minLatchExpiry は latchExpiry の最小値。毎ドットの走査を避けるために持つ。
	minLatchExpiry uint64
	// dots は電源投入からの累積ドット数。減衰の時刻の基準にする。
	dots uint64

	// 走査位置
	scanline int
	dot      int
	oddFrame bool
	frames   uint64

	// busAddr はフェッチの 1 ドット目にバスへ出したアドレス。
	busAddr uint16

	// 背景のフェッチ結果とシフタ。atLatch は属性バイトからこのタイルの
	// 分として選んだ 2 bit である。
	ntLatch   uint8
	atLatch   uint8
	bgLoLatch uint8
	bgHiLatch uint8

	bgShiftLo uint16
	bgShiftHi uint16
	atShiftLo uint8
	atShiftHi uint8
	atLatchLo bool
	atLatchHi bool

	// スプライト
	secondary        [secondarySize]uint8
	sprites          [maxSpritesPerLine]spriteUnit
	spriteCount      int
	sprite0OnNext    bool
	sprite0OnCurrent bool
	eval             evalState

	// メモリ
	oam     [256]uint8
	palette [paletteSize]uint8
	ciram   [ciramSize]uint8

	// warmupDots は 0 になるまで一部のレジスタへの書き込みを無視する。
	warmupDots uint32

	// nmiLine は NMI 線をアサートしているかを表す。
	nmiLine bool

	// skipDot はプリレンダー行の末尾の 1 ドットを飛ばすかを保持する。
	skipDot bool

	// suppressVBlank は VBlank フラグのセットを 1 回だけ飛ばす。
	//
	// フラグがセットされる 1 ドット前に $2002 を読むと、そのフレームは
	// フラグが立たない。
	suppressVBlank bool

	// queue は完成したフレームを表示側へ渡す。
	queue *video.Queue
	frame *video.Frame

	// scrollV と scrollX はプリレンダー行のドット 304 で記録した v と x。
	// デバッガのスクロール枠に使う（設計書 04 編 §4.10.1）。エミュレーションの
	// 状態ではないため保存しない。
	scrollV uint16
	scrollX uint8

	// Hooks はデバッガが挿し込む観測点。
	Hooks Hooks

	// Warn は互換性に関わる事象を記録する。nil のとき記録しない。
	Warn func(format string, args ...any)
	// Compat は Warn と同じ判定の結果を構造化して知らせる（Agent Interface の
	// Diagnostic）。nil のとき知らせない。reg はレジスタのアドレス（$2000–$2007）
	// かパレットのアドレス。
	Compat func(kind Compat, reg uint16)
}

// Compat は PPU が知らせる互換性の事象の種類。
type Compat uint8

// 互換性の事象。
const (
	// CompatRenderAccess は描画中の $2006 への書き込みか $2007 の読み書き。
	CompatRenderAccess Compat = iota + 1
	// CompatWarmupWrite は起動直後の、書き込みを受け付けない期間の書き込み。
	CompatWarmupWrite
	// CompatColor0D は色 $0D のパレットへの書き込み。
	CompatColor0D
)

// compat は Compat へ知らせる。
func (p *PPU) compat(kind Compat, reg uint16) {
	if p.Compat != nil {
		p.Compat(kind, reg)
	}
}

// OAMAddr は OAMADDR（$2003）の値を返す。
func (p *PPU) OAMAddr() uint8 { return p.oamAddr }

// warn は互換性に関わる事象を記録する。
func (p *PPU) warn(format string, args ...any) {
	if p.Warn != nil {
		p.Warn(format, args...)
	}
}

// Hooks は PPU に対する観測点。nil のときは呼ばない。
type Hooks struct {
	// OnFrameComplete はフレームが完成したときに呼ばれる。
	OnFrameComplete func(f *video.Frame)
	// OnSprite0Hit はスプライト 0 ヒットのフラグが立ったときに呼ばれる。
	OnSprite0Hit func()
}

// New は PPU を作る。
func New(r *region.Region, b Bus) *PPU {
	p := &PPU{region: r, bus: b, queue: video.NewQueue()}
	p.frame = p.queue.Writing()
	return p
}

// Queue はフレームキューを返す。
func (p *PPU) Queue() *video.Queue { return p.queue }

// warmupCPUCycles はリセット後に一部のレジスタへの書き込みを無視する
// CPU サイクル数。
//
// 実機ではリセットから約 29658 CPU クロック（PAL では約 33132）より前の
// PPUCTRL・PPUMASK・PPUSCROLL・PPUADDR への書き込みが無視される。
func warmupCPUCycles(r *region.Region) uint32 {
	if r.Name == "NTSC" {
		return 29658
	}
	// PAL と Dendy は CPU クロックが同じであり、同じ値を使う。
	return 33132
}

// warmupDotCount はウォームアップの長さを PPU ドット数で返す。
func warmupDotCount(r *region.Region) uint32 {
	cycles := uint64(warmupCPUCycles(r))
	return uint32(cycles * uint64(r.PPUDotsNum) / uint64(r.PPUDotsDen))
}

// PowerOn は電源投入時の状態にする。
//
// OAM・パレット・CIRAM の内容は呼び出し側が InitState に従って埋める。
func (p *PPU) PowerOn(vblankFlag bool) {
	p.ctrl = 0
	p.mask = 0
	p.status = 0
	if vblankFlag {
		p.status |= StatusVBlank
	}
	p.oamAddr = 0
	p.v, p.t, p.x, p.w = 0, 0, 0, false
	p.readBuffer = 0
	p.ioLatch = 0
	p.scanline, p.dot = 0, 0
	p.oddFrame = false
	p.frames = 0
	p.busAddr = 0
	p.clearFetchState()
	p.warmupDots = warmupDotCount(p.region)
	p.nmiLine = false
	p.suppressVBlank = false
	p.skipDot = false
	p.dots = 0
	p.latchExpiry = [8]uint64{}
	p.minLatchExpiry = noLatchExpiry
	p.secondary = [secondarySize]uint8{}
	p.sprites = [maxSpritesPerLine]spriteUnit{}
	p.spriteCount = 0
	p.sprite0OnNext, p.sprite0OnCurrent = false, false
	p.eval = evalState{}
}

// Reset はリセットを掛ける。OAM・パレット・CIRAM と v は変更しない。
//
// 内部リセット信号が PPUCTRL・PPUMASK・PPUSCROLL・PPUADDR・w ラッチ・
// リードバッファをクリアする。v レジスタはクリアされない。
func (p *PPU) Reset() {
	p.ctrl = 0
	p.mask = 0
	p.t, p.x, p.w = 0, 0, false
	p.readBuffer = 0
	p.scanline, p.dot = 0, 0
	p.warmupDots = warmupDotCount(p.region)
	p.updateNMILine()
}

// clearFetchState はフェッチ結果とシフタを初期化する。
func (p *PPU) clearFetchState() {
	p.ntLatch, p.atLatch, p.bgLoLatch, p.bgHiLatch = 0, 0, 0, 0
	p.bgShiftLo, p.bgShiftHi = 0, 0
	p.atShiftLo, p.atShiftHi = 0, 0
	p.atLatchLo, p.atLatchHi = false, false
}

// OAM は OAM を返す。電源投入時のパターンを書き込むために使う。
func (p *PPU) OAM() []uint8 { return p.oam[:] }

// Palette はパレット RAM を返す。
func (p *PPU) Palette() []uint8 { return p.palette[:] }

// CIRAM はネームテーブル用の記憶域を返す。
func (p *PPU) CIRAM() []uint8 { return p.ciram[:] }

// VRAMAddress は現在の v レジスタの値を返す。
//
// デバッガの表示と、$2007 の読み書きでアドレスが進んだことを
// 確かめるテストが使う。
func (p *PPU) VRAMAddress() uint16 { return p.v }

// Scanline は現在の走査線を返す。
func (p *PPU) Scanline() int { return p.scanline }

// SpriteHeight は現在のスプライトの高さを返す。8 または 16。
// デバッガの表示に使う。
func (p *PPU) SpriteHeight() int { return p.ctrl.SpriteHeight() }

// Dot は走査線内の位置を返す。
func (p *PPU) Dot() int { return p.dot }

// Frame は電源投入からのフレーム数を返す。
func (p *PPU) Frame() uint64 { return p.frames }

// OddFrame は奇数フレームかを返す。
func (p *PPU) OddFrame() bool { return p.oddFrame }

// NMILine は NMI 線の状態を返す。
//
// 負論理のため、アサートしていないとき true を返す。CPU は立ち下がりを
// 検出する。
func (p *PPU) NMILine() bool { return !p.nmiLine }

// updateNMILine は VBlank フラグと NMI 許可から NMI 線の状態を決める。
//
// 線はこの 2 つの論理積で駆動される。$2002 の読み出しで VBlank が
// クリアされると線も戻る。
func (p *PPU) updateNMILine() {
	p.nmiLine = p.status.VBlank() && p.ctrl.NMIEnabled()
}

// nmiAssertDot は NMI 線をアサートするドット。
const nmiAssertDot = 1

// latchDecayFrames は I/O ラッチが保持を失うまでのフレーム数。
//
// 実機の減衰は 1 秒より短い。ビットごとに時間差があるが、観測できる形は
// 「一定時間で 0 になる」ことである。
const latchDecayFrames = 36

// noLatchExpiry は減衰を待っているビットが無いことを表す。
const noLatchExpiry = ^uint64(0)

// latchDecayDots は ioLatch の保持時間をドット数で返す。
func (p *PPU) latchDecayDots() uint64 {
	return uint64(latchDecayFrames * p.region.TotalScanlines() * p.region.DotsPerScanline)
}

// refreshLatch は mask のビットだけを駆動する。
//
// 駆動されたビットは保持時間が更新される。駆動されないビットは減衰を
// 続ける。$2002 の読み出しのように一部のビットしか駆動しないアクセスを
// 正しく表すためである。
func (p *PPU) refreshLatch(v uint8, mask uint8) {
	p.ioLatch = p.ioLatch&^mask | v&mask
	expiry := p.dots + p.latchDecayDots()
	for i := range p.latchExpiry {
		if mask&(1<<i) != 0 {
			p.latchExpiry[i] = expiry
		}
	}
	p.updateMinLatchExpiry()
}

// decayLatch は保持時間を過ぎたビットを 0 にする。
func (p *PPU) decayLatch() {
	for i := range p.latchExpiry {
		if p.latchExpiry[i] != 0 && p.dots >= p.latchExpiry[i] {
			p.latchExpiry[i] = 0
			p.ioLatch &^= 1 << i
		}
	}
	p.updateMinLatchExpiry()
}

// updateMinLatchExpiry は次に減衰するビットの時刻を求める。
func (p *PPU) updateMinLatchExpiry() {
	p.minLatchExpiry = noLatchExpiry
	for _, e := range p.latchExpiry {
		if e != 0 && e < p.minLatchExpiry {
			p.minLatchExpiry = e
		}
	}
}

// skipDecisionDot は 1 ドットスキップの判定を行うドット。
const skipDecisionDot = 338

// Step は 1 ドット進める。
func (p *PPU) Step() {
	if p.warmupDots > 0 {
		p.warmupDots--
	}
	p.dots++
	if p.dots >= p.minLatchExpiry {
		p.decayLatch()
	}

	// 1 ドットスキップの判定は、飛ばすドットより前に確定する。
	if p.scanline == p.region.PreRenderScanline() && p.dot == skipDecisionDot {
		p.skipDot = p.oddFrame && p.mask.RenderingEnabled()
	}

	switch {
	case p.scanline < p.region.VisibleScanlines:
		p.renderScanlineDot(true)
	case p.scanline == p.region.PreRenderScanline():
		p.renderScanlineDot(false)
	case p.scanline == p.region.VBlankStartScanline():
		if p.dot == 1 {
			p.enterVBlank()
		}
		if p.dot == nmiAssertDot {
			p.updateNMILine()
		}
	}

	if p.dot == frameScrollDot && p.scanline == p.region.PreRenderScanline() {
		p.recordFrameScroll()
	}

	p.advance()
}

// frameScrollDot はスクロール位置を記録するドット。プリレンダー行の
// 垂直方向のコピー（ドット 280-304）を終えた位置である。
const frameScrollDot = 304

// recordFrameScroll はそのフレームの描画を始めるときのスクロール位置を記録する。
//
// レンダリングが無効のときは v が画面の位置を表さないため t を使う。
func (p *PPU) recordFrameScroll() {
	p.scrollV, p.scrollX = p.v, p.x
	if !p.mask.RenderingEnabled() {
		p.scrollV = p.t
	}
}

// FrameScroll はそのフレームの描画を始めたときの v と fine X を返す。
// ネームテーブルビューアのスクロール枠に使う。
func (p *PPU) FrameScroll() (v uint16, fineX uint8) { return p.scrollV, p.scrollX }

// Control は $2000 の値を返す。デバッガの表示に使う。
func (p *PPU) Control() Control { return p.ctrl }

// Mask は $2001 の値を返す。デバッガの表示に使う。
func (p *PPU) Mask() Mask { return p.mask }

// enterVBlank は VBlank 期間に入る。
func (p *PPU) enterVBlank() {
	if p.suppressVBlank {
		// フラグがセットされる 1 ドット前に $2002 が読まれた。
		// このフレームはフラグを立てない。
		p.suppressVBlank = false
		return
	}
	p.status |= StatusVBlank
}

// leaveVBlank はプリレンダー行の dot 1 で 3 つのフラグをクリアする。
func (p *PPU) leaveVBlank() {
	p.status &^= StatusVBlank | StatusSprite0Hit | StatusSpriteOverflow
	p.updateNMILine()
}

// advance はドットと走査線を進める。
func (p *PPU) advance() {
	p.dot++

	// レンダリング有効の奇数フレームでは、プリレンダー行の dot 339 の次を
	// スキャンライン 0 の dot 0 とし、1 ドットを飛ばす。
	if p.scanline == p.region.PreRenderScanline() && p.dot == p.region.DotsPerScanline-1 && p.skipDot {
		p.skipDot = false
		p.completeFrame()
		return
	}

	if p.dot < p.region.DotsPerScanline {
		return
	}
	p.dot = 0
	p.scanline++

	if p.scanline == p.region.PostRenderScanline() {
		// 可視領域を描き終えた。ここでフレームを表示側へ渡す。
		p.publishFrame()
	}
	if p.scanline > p.region.PreRenderScanline() {
		p.completeFrame()
	}
}

// completeFrame はフレームの末尾でカウンタを進める。
func (p *PPU) completeFrame() {
	p.scanline = 0
	p.dot = 0
	// oddFrame はレンダリングの有無に関係なく毎フレーム反転する。
	p.oddFrame = !p.oddFrame
	p.frames++
}

// publishFrame は描き終えたフレームを表示側へ渡す。
func (p *PPU) publishFrame() {
	if p.Hooks.OnFrameComplete != nil {
		p.Hooks.OnFrameComplete(p.frame)
	}
	p.queue.Commit()
	p.frame = p.queue.Writing()
}
