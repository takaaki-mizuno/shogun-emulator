package ppu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// fakeCart は PPU のテスト用のカートリッジ。
type fakeCart struct {
	chr       [0x2000]uint8
	mirroring cart.Mirroring
	// addrs は NotifyPPUAddress で通知されたアドレスの並び。
	addrs []uint16
}

func (c *fakeCart) ReadCHR(addr uint16) uint8     { return c.chr[addr&0x1FFF] }
func (c *fakeCart) WriteCHR(addr uint16, v uint8) { c.chr[addr&0x1FFF] = v }
func (c *fakeCart) MapNametable(addr uint16) cart.NametableTarget {
	return cart.MapNametableWith(c.mirroring, addr)
}
func (c *fakeCart) NotifyPPUAddress(addr uint16, dot uint64) { c.addrs = append(c.addrs, addr) }

// newTestPPU は電源投入済みの PPU を返す。
//
// ウォームアップを終わらせてから返す。レジスタへの書き込みを検証する
// テストが、書き込みが無視される期間に当たらないようにするためである。
func newTestPPU(t *testing.T) (*PPU, *fakeCart) {
	t.Helper()
	c := &fakeCart{}
	p := New(region.NTSC, c)
	p.PowerOn(false)
	p.warmupDots = 0
	return p, c
}

// TestPaletteIndexFolding はパレット RAM の畳み込みを確かめる。
//
// 下位 2 bit が 0 のスプライト側エントリは背景側と同じ記憶域を指す。
func TestPaletteIndexFolding(t *testing.T) {
	pairs := [][2]uint16{
		{0x3F00, 0x3F10},
		{0x3F04, 0x3F14},
		{0x3F08, 0x3F18},
		{0x3F0C, 0x3F1C},
	}
	for _, pair := range pairs {
		if paletteIndex(pair[0]) != paletteIndex(pair[1]) {
			t.Errorf("$%04X と $%04X が同じ記憶域を指していない", pair[0], pair[1])
		}
	}
	// 下位 2 bit が 0 でないエントリは独立している
	for _, addr := range []uint16{0x3F11, 0x3F15, 0x3F19, 0x3F1D} {
		if paletteIndex(addr) == paletteIndex(addr&0x0F|0x3F00) {
			t.Errorf("$%04X が背景側と同じ記憶域を指している", addr)
		}
	}
	// $3F20 以降は $3F00 のミラー
	if paletteIndex(0x3F20) != paletteIndex(0x3F00) {
		t.Error("$3F20 が $3F00 のミラーになっていない")
	}
}

// TestPaletteWriteVisibleThroughMirror は $3F00 へ書いた値が $3F10 から
// 読めることを確かめる。
func TestPaletteWriteVisibleThroughMirror(t *testing.T) {
	p, _ := newTestPPU(t)
	p.writeVRAM(0x3F00, 0x21)
	if got := p.readVRAM(0x3F10); got != 0x21 {
		t.Errorf("$3F10 = $%02X, 期待 $21", got)
	}
	p.writeVRAM(0x3F14, 0x15)
	if got := p.readVRAM(0x3F04); got != 0x15 {
		t.Errorf("$3F04 = $%02X, 期待 $15", got)
	}
}

// TestPaletteStoresSixBits はパレット RAM が 6 bit しか保持しないことを
// 確かめる。
func TestPaletteStoresSixBits(t *testing.T) {
	p, _ := newTestPPU(t)
	p.writeVRAM(0x3F00, 0xFF)
	if got := p.readVRAM(0x3F00); got != 0x3F {
		t.Errorf("$3F00 = $%02X, 期待 $3F", got)
	}
}

// TestNametableMirrorRange は $3000-$3EFF が $2000-$2EFF のミラーで
// あることを確かめる。
func TestNametableMirrorRange(t *testing.T) {
	p, _ := newTestPPU(t)
	p.writeVRAM(0x2123, 0x5A)
	if got := p.readVRAM(0x3123); got != 0x5A {
		t.Errorf("$3123 = $%02X, 期待 $5A", got)
	}
}

// TestWriteOnlyRegisterReadsReturnLatch は書き込み専用レジスタの読み出しが
// I/O ラッチを返すことを確かめる。
func TestWriteOnlyRegisterReadsReturnLatch(t *testing.T) {
	p, _ := newTestPPU(t)
	p.WriteRegister(regCtrl, 0x3C)
	for _, reg := range []uint16{regCtrl, regMask, regOAMAddr, regScroll, regAddr} {
		if got := p.ReadRegister(reg); got != 0x3C {
			t.Errorf("レジスタ %d の読み出し = $%02X, 期待 $3C", reg, got)
		}
	}
}

// TestStatusReadCombinesLatch は $2002 の読み出しが上位 3 bit を status
// から、下位 5 bit を ioLatch から合成することを確かめる。
func TestStatusReadCombinesLatch(t *testing.T) {
	p, _ := newTestPPU(t)
	p.WriteRegister(regMask, 0x1F) // ioLatch = $1F
	p.status = StatusVBlank | StatusSprite0Hit

	got := p.ReadRegister(regStatus)
	if got&0xE0 != 0xC0 {
		t.Errorf("上位 3 bit = $%02X, 期待 $C0", got&0xE0)
	}
	if got&0x1F != 0x1F {
		t.Errorf("下位 5 bit = $%02X, 期待 $1F", got&0x1F)
	}
	if p.status.VBlank() {
		t.Error("読み出しで VBlank がクリアされていない")
	}
}

// TestStatusReadClearsWriteToggle は $2002 の読み出しで w が false に
// なることを確かめる。
func TestStatusReadClearsWriteToggle(t *testing.T) {
	p, _ := newTestPPU(t)
	p.WriteRegister(regAddr, 0x20) // w = true
	if !p.w {
		t.Fatal("w が true になっていない")
	}
	p.ReadRegister(regStatus)
	if p.w {
		t.Error("$2002 の読み出しで w が false になっていない")
	}
}

// TestWarmupIgnoresWrites はウォームアップ中に一部のレジスタへの書き込みが
// 無視されることを確かめる。
func TestWarmupIgnoresWrites(t *testing.T) {
	c := &fakeCart{}
	p := New(region.NTSC, c)
	p.PowerOn(false)

	if p.warmupDots == 0 {
		t.Fatal("ウォームアップの長さが 0")
	}

	p.WriteRegister(regCtrl, 0xFF)
	p.WriteRegister(regMask, 0xFF)
	p.WriteRegister(regScroll, 0xFF)
	p.WriteRegister(regAddr, 0xFF)

	if p.ctrl != 0 {
		t.Errorf("ctrl = $%02X。ウォームアップ中の書き込みは無視すること", p.ctrl)
	}
	if p.mask != 0 {
		t.Errorf("mask = $%02X。ウォームアップ中の書き込みは無視すること", p.mask)
	}
	if p.w {
		t.Error("ウォームアップ中は w もトグルしないこと")
	}

	// $2003 と $2007 は即座に動作する
	p.WriteRegister(regOAMAddr, 0x12)
	if p.oamAddr != 0x12 {
		t.Errorf("oamAddr = $%02X, 期待 $12（$2003 は即座に動作すること）", p.oamAddr)
	}
}

// TestWarmupLengthMatchesResearch はウォームアップの長さが調査結果の
// CPU サイクル数から換算した値になることを確かめる。
func TestWarmupLengthMatchesResearch(t *testing.T) {
	// NTSC は約 29658 CPU サイクル。1 サイクルあたり 3 ドット。
	if got := warmupDotCount(region.NTSC); got != 29658*3 {
		t.Errorf("NTSC のウォームアップ = %d ドット, 期待 %d", got, 29658*3)
	}
	// PAL は約 33132 CPU サイクル。1 サイクルあたり 16/5 ドット。
	if got := warmupDotCount(region.PAL); got != 33132*16/5 {
		t.Errorf("PAL のウォームアップ = %d ドット, 期待 %d", got, 33132*16/5)
	}
}

// scrollCase は v / t / x / w の転送の検証項目。
type scrollCase struct {
	name string
	// setup は書き込みと読み出しを行う。
	setup func(p *PPU)
	wantT uint16
	wantV uint16
	wantX uint8
	wantW bool
}

// TestScrollRegisterTransfers は設計書 04 編 §4.4.2 のビット転送表を
// 表駆動で検証する。
//
// スクロール位置を scrollX / scrollY として持つ実装ではこの転送を
// 表現できない。画面途中でスクロールを変えるゲームがこれに依存する。
func TestScrollRegisterTransfers(t *testing.T) {
	cases := []scrollCase{
		{
			name:  "$2000 はネームテーブル選択を t の bit 10-11 へ入れる",
			setup: func(p *PPU) { p.t = 0xFFFF; p.WriteRegister(regCtrl, 0x02) },
			wantT: 0xFFFF&0xF3FF | 0x0800,
			wantV: 0,
			wantW: false,
		},
		{
			name: "$2005 の 1 回目は coarse X と fine X を入れる",
			setup: func(p *PPU) {
				p.t = 0
				p.WriteRegister(regScroll, 0b1010_1101) // coarse X = 0b10101, fine X = 0b101
			},
			wantT: 0b10101,
			wantX: 0b101,
			wantW: true,
		},
		{
			name: "$2005 の 2 回目は fine Y と coarse Y を入れる",
			setup: func(p *PPU) {
				p.t = 0
				p.w = true
				p.WriteRegister(regScroll, 0b1010_1101) // coarse Y = 0b10101, fine Y = 0b101
			},
			wantT: 0b101<<12 | 0b10101<<5,
			wantW: false,
		},
		{
			name: "$2006 の 1 回目は上位バイトを入れ bit 14 を 0 にする",
			setup: func(p *PPU) {
				p.t = 0x7FFF
				p.WriteRegister(regAddr, 0xFF)
			},
			wantT: 0x3FFF,
			wantW: true,
		},
		{
			name: "$2006 の 2 回目は下位バイトを入れて v へ転送する",
			setup: func(p *PPU) {
				p.t = 0x3F00
				p.w = true
				p.WriteRegister(regAddr, 0x21)
			},
			wantT: 0x3F21,
			wantV: 0x3F21,
			wantW: false,
		},
		{
			name: "$2002 の読み出しは w を false にする",
			setup: func(p *PPU) {
				p.w = true
				p.ReadRegister(regStatus)
			},
			wantW: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newTestPPU(t)
			p.v, p.t, p.x, p.w = 0, 0, 0, false
			tc.setup(p)

			if tc.wantT != 0 && p.t != tc.wantT {
				t.Errorf("t = $%04X, 期待 $%04X", p.t, tc.wantT)
			}
			if tc.wantV != 0 && p.v != tc.wantV {
				t.Errorf("v = $%04X, 期待 $%04X", p.v, tc.wantV)
			}
			if tc.wantX != 0 && p.x != tc.wantX {
				t.Errorf("x = %d, 期待 %d", p.x, tc.wantX)
			}
			if p.w != tc.wantW {
				t.Errorf("w = %v, 期待 %v", p.w, tc.wantW)
			}
		})
	}
}

// TestIncrementX は coarse X の桁上がりを確かめる。
func TestIncrementX(t *testing.T) {
	p, _ := newTestPPU(t)

	p.v = 0
	p.incrementX()
	if p.v != 1 {
		t.Errorf("v = $%04X, 期待 $0001", p.v)
	}

	// coarse X が 31 のとき 0 に戻り、ネームテーブルを切り替える
	p.v = 31
	p.incrementX()
	if p.v != 0x0400 {
		t.Errorf("v = $%04X, 期待 $0400", p.v)
	}
}

// TestIncrementY は fine Y と coarse Y の桁上がりを確かめる。
func TestIncrementY(t *testing.T) {
	p, _ := newTestPPU(t)

	// fine Y が 7 未満のときは fine Y だけ進む
	p.v = 0
	p.incrementY()
	if p.v != 0x1000 {
		t.Errorf("v = $%04X, 期待 $1000", p.v)
	}

	// coarse Y が 29 のときネームテーブルを切り替える
	p.v = 0x7000 | 29<<5
	p.incrementY()
	if p.v != 0x0800 {
		t.Errorf("v = $%04X, 期待 $0800（29 でネームテーブルを切り替える）", p.v)
	}

	// coarse Y が 31 のときは切り替えない
	p.v = 0x7000 | 31<<5
	p.incrementY()
	if p.v != 0 {
		t.Errorf("v = $%04X, 期待 $0000（31 では切り替えない）", p.v)
	}
}

// TestReadBufferDelay は $2007 の読み出しが 1 回遅れることを確かめる。
func TestReadBufferDelay(t *testing.T) {
	p, _ := newTestPPU(t)
	p.writeVRAM(0x2000, 0x11)
	p.writeVRAM(0x2001, 0x22)

	// $2006 で $2000 を指す
	p.WriteRegister(regAddr, 0x20)
	p.WriteRegister(regAddr, 0x00)

	// 1 回目はバッファの中身（初期値）が返る
	first := p.ReadRegister(regData)
	if first == 0x11 {
		t.Error("1 回目の読み出しで値が返っている。1 回分遅延すること")
	}
	if got := p.ReadRegister(regData); got != 0x11 {
		t.Errorf("2 回目 = $%02X, 期待 $11", got)
	}
	if got := p.ReadRegister(regData); got != 0x22 {
		t.Errorf("3 回目 = $%02X, 期待 $22", got)
	}
}

// TestPaletteReadIsImmediate はパレット領域の読み出しが遅延しないことを
// 確かめる。下敷きのネームテーブルがバッファへ入る。
func TestPaletteReadIsImmediate(t *testing.T) {
	p, _ := newTestPPU(t)
	p.writeVRAM(0x3F01, 0x2A)
	p.writeVRAM(0x2F01, 0x77) // パレットの下敷き

	p.WriteRegister(regAddr, 0x3F)
	p.WriteRegister(regAddr, 0x01)

	if got := p.ReadRegister(regData); got&0x3F != 0x2A {
		t.Errorf("パレットの読み出し = $%02X, 期待 $2A（遅延しないこと）", got&0x3F)
	}
	if p.readBuffer != 0x77 {
		t.Errorf("リードバッファ = $%02X, 期待 $77（下敷きのネームテーブル）", p.readBuffer)
	}
}

// TestVRAMIncrement は $2000 の bit 2 で増分が変わることを確かめる。
func TestVRAMIncrement(t *testing.T) {
	p, _ := newTestPPU(t)

	p.WriteRegister(regCtrl, 0x00)
	p.WriteRegister(regAddr, 0x20)
	p.WriteRegister(regAddr, 0x00)
	p.ReadRegister(regData)
	if p.v != 0x2001 {
		t.Errorf("v = $%04X, 期待 $2001（増分 1）", p.v)
	}

	p.WriteRegister(regCtrl, 0x04)
	p.WriteRegister(regAddr, 0x20)
	p.WriteRegister(regAddr, 0x00)
	p.ReadRegister(regData)
	if p.v != 0x2020 {
		t.Errorf("v = $%04X, 期待 $2020（増分 32）", p.v)
	}
}

// TestAttributeAddress は属性テーブルのアドレス計算を確かめる。
//
// ビット位置を 1 つ間違えると 32 ピクセルごとに色が崩れる。
func TestAttributeAddress(t *testing.T) {
	p, _ := newTestPPU(t)

	tests := []struct {
		v    uint16
		want uint16
	}{
		// coarse X = 0, coarse Y = 0, ネームテーブル 0
		{0x2000 & 0x0FFF, 0x23C0},
		// coarse X = 31, coarse Y = 29
		{31 | 29<<5, 0x23C0 | 29/4<<3 | 31/4},
		// ネームテーブル 3
		{0x0C00, 0x23C0 | 0x0C00},
	}
	for _, tt := range tests {
		p.v = tt.v
		if got := p.attributeAddress(); got != tt.want {
			t.Errorf("v=$%04X の属性アドレス = $%04X, 期待 $%04X", tt.v, got, tt.want)
		}
	}
}

// TestNametableAddress はネームテーブルのアドレス計算を確かめる。
func TestNametableAddress(t *testing.T) {
	p, _ := newTestPPU(t)
	p.v = 0x0FFF
	if got := p.nametableAddress(); got != 0x2FFF {
		t.Errorf("ネームテーブルアドレス = $%04X, 期待 $2FFF", got)
	}
}

// TestFrameDotCount は 1 フレームのドット数を確かめる。
//
// レンダリング無効のとき 89342 ドット、有効な奇数フレームでは 1 ドット
// 短くなる。
func TestFrameDotCount(t *testing.T) {
	p, _ := newTestPPU(t)

	count := func() int {
		start := p.Frame()
		n := 0
		for p.Frame() == start {
			p.Step()
			n++
		}
		return n
	}

	// レンダリング無効。どのフレームも同じ長さ。
	p.mask = 0
	full := region.NTSC.TotalScanlines() * region.NTSC.DotsPerScanline
	for i := range 4 {
		if n := count(); n != full {
			t.Errorf("レンダリング無効のフレーム %d = %d ドット, 期待 %d", i, n, full)
		}
	}

	// レンダリング有効。奇数フレームが 1 ドット短い。
	p.mask = Mask(0x08)
	var lengths [4]int
	for i := range lengths {
		lengths[i] = count()
	}
	short, long := 0, 0
	for _, n := range lengths {
		switch n {
		case full:
			long++
		case full - 1:
			short++
		default:
			t.Errorf("フレームの長さ = %d, 期待 %d または %d", n, full, full-1)
		}
	}
	if short == 0 || long == 0 {
		t.Errorf("長さの内訳 = %v。長短が交互に現れること", lengths)
	}
}

// TestOddFrameTogglesRegardlessOfRendering は oddFrame がレンダリングの
// 有無に関係なく毎フレーム反転することを確かめる。
func TestOddFrameTogglesRegardlessOfRendering(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = 0

	start := p.OddFrame()
	target := p.Frame() + 1
	for p.Frame() != target {
		p.Step()
	}
	if p.OddFrame() == start {
		t.Error("レンダリング無効でも oddFrame は反転すること")
	}
}

// TestVBlankFlagSetAtScanline241 は VBlank フラグが立つ位置を確かめる。
func TestVBlankFlagSetAtScanline241(t *testing.T) {
	p, _ := newTestPPU(t)
	stepTo(p, region.NTSC.VBlankStartScanline(), 1)
	if p.status.VBlank() {
		t.Fatal("dot 1 の処理前に VBlank が立っている")
	}
	p.Step()
	if !p.status.VBlank() {
		t.Error("scanline 241 dot 1 で VBlank が立っていない")
	}
}

// TestFlagsClearedAtPreRender はプリレンダー行の dot 1 で 3 つのフラグが
// クリアされることを確かめる。
func TestFlagsClearedAtPreRender(t *testing.T) {
	p, _ := newTestPPU(t)
	p.status = StatusVBlank | StatusSprite0Hit | StatusSpriteOverflow

	stepTo(p, region.NTSC.PreRenderScanline(), 1)
	p.Step()

	if p.status != 0 {
		t.Errorf("status = $%02X, 期待 $00", uint8(p.status))
	}
}

// TestNMILineFollowsFlagAndEnable は NMI 線が VBlank フラグと NMI 許可の
// 論理積で駆動されることを確かめる。
func TestNMILineFollowsFlagAndEnable(t *testing.T) {
	p, _ := newTestPPU(t)

	// 負論理。アサートしていないとき true。
	if !p.NMILine() {
		t.Fatal("初期状態でアサートされている")
	}

	p.WriteRegister(regCtrl, 0x80) // NMI 許可
	if !p.NMILine() {
		t.Error("VBlank フラグが立っていないのにアサートされた")
	}

	stepTo(p, region.NTSC.VBlankStartScanline(), 1)
	p.Step()
	if p.NMILine() {
		t.Error("VBlank で NMI がアサートされていない")
	}

	p.ReadRegister(regStatus) // VBlank をクリア
	if !p.NMILine() {
		t.Error("VBlank のクリアで NMI が戻っていない")
	}
}

// TestNMIAssertedByEnablingDuringVBlank は VBlank 中に NMI を有効にすると
// 線がアサートされることを確かめる。
func TestNMIAssertedByEnablingDuringVBlank(t *testing.T) {
	p, _ := newTestPPU(t)
	stepTo(p, region.NTSC.VBlankStartScanline(), 1)
	p.Step()
	if !p.status.VBlank() {
		t.Fatal("VBlank が立っていない")
	}
	if p.NMILine() != true {
		t.Fatal("NMI 許可の前からアサートされている")
	}

	p.WriteRegister(regCtrl, 0x80)
	if p.NMILine() {
		t.Error("VBlank 中に NMI を有効にしてもアサートされていない")
	}
}

// TestVBlankReadOneDotBeforeSuppressesFlag はフラグが立つ 1 ドット前に
// $2002 を読むと、そのフレームはフラグが立たないことを確かめる。
func TestVBlankReadOneDotBeforeSuppressesFlag(t *testing.T) {
	p, _ := newTestPPU(t)
	// dot は「次に処理するドット」を指す。読み出しが起きたドットは dot-1。
	// フラグが立つのは dot 1 の処理であり、その 1 ドット前に読むのは
	// dot が 1 のときである。
	stepTo(p, region.NTSC.VBlankStartScanline(), 1)

	if got := p.ReadRegister(regStatus); got&0x80 != 0 {
		t.Errorf("読み出し = $%02X。フラグが立つ前なので 0 であること", got)
	}
	p.Step() // dot 1 の処理
	if p.status.VBlank() {
		t.Error("1 ドット前に読んだフレームでフラグが立った")
	}

	// 次のフレームでは通常どおり立つ
	target := p.Frame() + 1
	for p.Frame() != target {
		p.Step()
	}
	stepTo(p, region.NTSC.VBlankStartScanline(), 1)
	p.Step()
	if !p.status.VBlank() {
		t.Error("次のフレームでフラグが立っていない")
	}
}

// TestBackgroundFetchAddressesAppearOnBus は背景フェッチのアドレスが
// バスに現れることを確かめる。
//
// マッパーが A12 の遷移を数えるため、値を使わないアクセスでも通知する。
func TestBackgroundFetchAddressesAppearOnBus(t *testing.T) {
	p, c := newTestPPU(t)
	p.mask = Mask(0x08) // 背景を有効にする
	p.v = 0

	// 可視走査線の先頭から 8 ドット分進める
	stepTo(p, 0, 1)
	c.addrs = nil
	for range 8 {
		p.Step()
	}

	if len(c.addrs) != 4 {
		t.Fatalf("8 ドットでのアドレス通知 = %d 回, 期待 4 回（%v）", len(c.addrs), c.addrs)
	}
	// ネームテーブル、属性、パターン下位、パターン上位の順
	if c.addrs[0] != 0x2000 {
		t.Errorf("1 回目 = $%04X, 期待 $2000（ネームテーブル）", c.addrs[0])
	}
	if c.addrs[1] != 0x23C0 {
		t.Errorf("2 回目 = $%04X, 期待 $23C0（属性）", c.addrs[1])
	}
	if c.addrs[3] != c.addrs[2]+8 {
		t.Errorf("パターン上位 = $%04X, 期待 $%04X（下位 + 8）", c.addrs[3], c.addrs[2]+8)
	}
}

// TestNoFetchWhenRenderingDisabled はレンダリング無効のときフェッチを
// 行わないことを確かめる。
func TestNoFetchWhenRenderingDisabled(t *testing.T) {
	p, c := newTestPPU(t)
	p.mask = 0

	stepTo(p, 0, 1)
	c.addrs = nil
	for range 64 {
		p.Step()
	}
	if len(c.addrs) != 0 {
		t.Errorf("レンダリング無効でアドレス通知が %d 回あった", len(c.addrs))
	}
}

// TestOAMAttributeBitsReadAsZero は OAM の属性バイトの bit 2-4 が
// 読み出しで常に 0 になることを確かめる。
func TestOAMAttributeBitsReadAsZero(t *testing.T) {
	p, _ := newTestPPU(t)
	p.oam[2] = 0xFF
	p.oamAddr = 2
	if got := p.ReadRegister(regOAMData); got != 0xE3 {
		t.Errorf("属性バイトの読み出し = $%02X, 期待 $E3", got)
	}
}

// TestIOLatchDecays は I/O ラッチが時間で 0 に戻ることを確かめる。
func TestIOLatchDecays(t *testing.T) {
	p, _ := newTestPPU(t)
	p.WriteRegister(regMask, 0xFF)
	if got := p.ReadRegister(regCtrl); got != 0xFF {
		t.Fatalf("書き込み直後の読み出し = $%02X, 期待 $FF", got)
	}

	for range p.latchDecayDots() + 1 {
		p.Step()
	}
	if got := p.ReadRegister(regCtrl); got != 0x00 {
		t.Errorf("減衰後の読み出し = $%02X, 期待 $00", got)
	}
}

// TestStatusReadDoesNotRefreshLowBits は $2002 の読み出しが下位 5 bit の
// 保持を更新しないことを確かめる。
//
// $2002 の読み出しは上位 3 bit だけをバスへ駆動する。
func TestStatusReadDoesNotRefreshLowBits(t *testing.T) {
	p, _ := newTestPPU(t)
	p.WriteRegister(regMask, 0xFF)

	// 保持時間の半分まで進めてから $2002 を読む
	half := p.latchDecayDots() / 2
	for range half {
		p.Step()
	}
	p.ReadRegister(regStatus)

	// 残りの保持時間を過ぎると下位 5 bit は 0 になる
	for range p.latchDecayDots()/2 + 2 {
		p.Step()
	}
	if got := p.ioLatch & 0x1F; got != 0 {
		t.Errorf("下位 5 bit = $%02X, 期待 $00（$2002 は下位を駆動しない）", got)
	}
}

// stepTo は指定した走査位置まで進める。
func stepTo(p *PPU, scanline, dot int) {
	for p.scanline != scanline || p.dot != dot {
		p.Step()
	}
}

// TestPPUStateRoundtrip は設計書 04 編 §4.11 の「保存する状態」が
// すべて往復することを確かめる。
//
// シフタとラッチを省くと、復元直後の 1 タイル分の描画が崩れる。
func TestPPUStateRoundtrip(t *testing.T) {
	p, c := newTestPPU(t)

	// 既定値と異なる値を全フィールドに入れる
	p.ctrl = Control(0x9E)
	p.mask = Mask(0x1E)
	p.status = StatusVBlank | StatusSprite0Hit
	p.oamAddr = 0x3C
	p.v, p.t, p.x, p.w = 0x2345, 0x1234, 5, true
	p.readBuffer = 0x77
	p.refreshLatch(0xA5, 0xFF)
	p.scanline, p.dot = 123, 45
	p.oddFrame = true
	p.frames = 9999
	p.busAddr = 0x1FF0
	p.ntLatch, p.atLatch, p.bgLoLatch, p.bgHiLatch = 1, 2, 3, 4
	p.bgShiftLo, p.bgShiftHi = 0xAAAA, 0x5555
	p.atShiftLo, p.atShiftHi = 0x0F, 0xF0
	p.atLatchLo, p.atLatchHi = true, true
	p.oam[0] = 0x11
	p.palette[1] = 0x22
	p.ciram[100] = 0x33
	p.warmupDots = 4242
	p.nmiLine = true
	p.suppressVBlank = true
	p.skipDot = true
	p.secondary[3] = 0x44
	p.spriteCount = 5
	p.sprite0OnNext = true
	p.sprite0OnCurrent = true
	p.sprites[2] = spriteUnit{patternLo: 0x12, patternHi: 0x34, attr: 0x56, xCounter: 78, active: true}
	p.eval = evalState{addr: 0x1C, step: evalOverflow, copied: 2, overflowCopied: 3,
		writeDisable: true, latch: 0x9A}

	w := state.NewWriter()
	p.SaveState(w)
	blob := w.Data()

	restored := New(region.NTSC, c)
	restored.PowerOn(false)
	if err := restored.LoadState(state.NewReader(blob)); err != nil {
		t.Fatalf("復元に失敗した: %v", err)
	}

	// 直列化した結果を比べる。直列化に含めていないフィールドを検出する
	// ためにバイト列で比べる。
	w2 := state.NewWriter()
	restored.SaveState(w2)
	if d := state.FirstDiff(blob, w2.Data()); d != "" {
		t.Errorf("往復で状態が一致しない: %s", d)
	}

	checks := []struct {
		name      string
		got, want any
	}{
		{"ctrl", restored.ctrl, p.ctrl},
		{"mask", restored.mask, p.mask},
		{"status", restored.status, p.status},
		{"oamAddr", restored.oamAddr, p.oamAddr},
		{"v", restored.v, p.v},
		{"t", restored.t, p.t},
		{"x", restored.x, p.x},
		{"w", restored.w, p.w},
		{"readBuffer", restored.readBuffer, p.readBuffer},
		{"ioLatch", restored.ioLatch, p.ioLatch},
		{"scanline", restored.scanline, p.scanline},
		{"dot", restored.dot, p.dot},
		{"oddFrame", restored.oddFrame, p.oddFrame},
		{"frames", restored.frames, p.frames},
		{"busAddr", restored.busAddr, p.busAddr},
		{"ntLatch", restored.ntLatch, p.ntLatch},
		{"atLatch", restored.atLatch, p.atLatch},
		{"bgLoLatch", restored.bgLoLatch, p.bgLoLatch},
		{"bgHiLatch", restored.bgHiLatch, p.bgHiLatch},
		{"bgShiftLo", restored.bgShiftLo, p.bgShiftLo},
		{"bgShiftHi", restored.bgShiftHi, p.bgShiftHi},
		{"atShiftLo", restored.atShiftLo, p.atShiftLo},
		{"atShiftHi", restored.atShiftHi, p.atShiftHi},
		{"atLatchLo", restored.atLatchLo, p.atLatchLo},
		{"atLatchHi", restored.atLatchHi, p.atLatchHi},
		{"warmupDots", restored.warmupDots, p.warmupDots},
		{"nmiLine", restored.nmiLine, p.nmiLine},
		{"suppressVBlank", restored.suppressVBlank, p.suppressVBlank},
		{"skipDot", restored.skipDot, p.skipDot},
		{"dots", restored.dots, p.dots},
		{"spriteCount", restored.spriteCount, p.spriteCount},
		{"sprite0OnNext", restored.sprite0OnNext, p.sprite0OnNext},
		{"sprite0OnCurrent", restored.sprite0OnCurrent, p.sprite0OnCurrent},
		{"eval.addr", restored.eval.addr, p.eval.addr},
		{"eval.step", restored.eval.step, p.eval.step},
		{"eval.copied", restored.eval.copied, p.eval.copied},
		{"eval.overflowCopied", restored.eval.overflowCopied, p.eval.overflowCopied},
		{"eval.writeDisable", restored.eval.writeDisable, p.eval.writeDisable},
		{"eval.latch", restored.eval.latch, p.eval.latch},
		{"sprites[2]", restored.sprites[2], p.sprites[2]},
		{"secondary[3]", restored.secondary[3], p.secondary[3]},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, 期待 %v", ch.name, ch.got, ch.want)
		}
	}
	if restored.oam[0] != 0x11 || restored.palette[1] != 0x22 || restored.ciram[100] != 0x33 {
		t.Error("メモリの内容が往復していない")
	}
	if restored.minLatchExpiry != p.minLatchExpiry {
		t.Errorf("minLatchExpiry = %d, 期待 %d", restored.minLatchExpiry, p.minLatchExpiry)
	}
}
