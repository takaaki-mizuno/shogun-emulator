package bus

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/ppu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// newTestBus はマッパー 0 のカートリッジを載せたバスを返す。
func newTestBus(t *testing.T, r *region.Region) *Bus {
	t.Helper()
	rom := buildTestROM(t)
	c, err := cart.New(rom)
	if err != nil {
		t.Fatalf("カートリッジを作れない: %v", err)
	}
	b := New(r, ppu.New(r, c), apu.New(r), c)
	b.PowerOn(0)
	return b
}

// buildTestROM は 32 KiB PRG・8 KiB CHR のマッパー 0 の ROM を作る。
func buildTestROM(t *testing.T) *cart.ROM {
	t.Helper()
	data := make([]uint8, 16+32*1024+8*1024)
	copy(data, []uint8{0x4E, 0x45, 0x53, 0x1A})
	data[4] = 2 // PRG 32 KiB
	data[5] = 1 // CHR 8 KiB
	// $8000 と $FFFF に目印を置く
	data[16] = 0xA5
	data[16+32*1024-1] = 0x5A
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	return rom
}

// TestRAMMirroring は内蔵 RAM が $0800 ごとにミラーされることを確かめる。
func TestRAMMirroring(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	b.Write(0x0000, 0x42)
	for _, addr := range []uint16{0x0000, 0x0800, 0x1000, 0x1800} {
		if got := b.Peek(addr); got != 0x42 {
			t.Errorf("$%04X = $%02X, 期待 $42", addr, got)
		}
	}

	b.Write(0x1FFF, 0x99)
	if got := b.Peek(0x07FF); got != 0x99 {
		t.Errorf("$07FF = $%02X, 期待 $99", got)
	}
}

// TestPPURegisterMirroring は PPU レジスタが 8 バイトごとに
// ミラーされることを確かめる。
func TestPPURegisterMirroring(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	// この段階の PPU は書き込まれた値をラッチとして返す
	b.Write(0x2000, 0x37)
	for _, addr := range []uint16{0x2000, 0x2008, 0x3FF8} {
		if got := b.Peek(addr); got != 0x37 {
			t.Errorf("$%04X = $%02X, 期待 $37", addr, got)
		}
	}
}

// TestOpenBusReturnsLastValue はマップされていないアドレスの読み出しが
// 直前に読まれた値を返すことを確かめる。
func TestOpenBusReturnsLastValue(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	b.Write(0x0000, 0x5C)
	if got := b.Read(0x0000); got != 0x5C {
		t.Fatalf("$0000 = $%02X, 期待 $5C", got)
	}
	// $4018-$401F は何もない
	if got := b.Read(0x4018); got != 0x5C {
		t.Errorf("$4018 = $%02X, 期待 $5C（オープンバス）", got)
	}
	if got := b.OpenBus(); got != 0x5C {
		t.Errorf("OpenBus = $%02X, 期待 $5C", got)
	}
}

// TestCartridgeMapping はカートリッジの領域が読めることを確かめる。
func TestCartridgeMapping(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	if got := b.Read(0x8000); got != 0xA5 {
		t.Errorf("$8000 = $%02X, 期待 $A5", got)
	}
	if got := b.Read(0xFFFF); got != 0x5A {
		t.Errorf("$FFFF = $%02X, 期待 $5A", got)
	}
}

// TestTickAdvancesPPUThreeDots は NTSC で 1 CPU サイクルあたり
// PPU が 3 ドット進むことを確かめる。
func TestTickAdvancesPPUThreeDots(t *testing.T) {
	r := region.NTSC
	rom := buildTestROM(t)
	c, err := cart.New(rom)
	if err != nil {
		t.Fatal(err)
	}
	p := ppu.New(r, c)
	b := New(r, p, apu.New(r), c)
	b.PowerOn(0)
	p.PowerOn(false)

	b.Read(0x0000)
	if p.Dot() != 3 {
		t.Errorf("1 サイクル後の PPU ドット = %d, 期待 3", p.Dot())
	}
	for range 9 {
		b.Read(0x0000)
	}
	if p.Dot() != 30 {
		t.Errorf("10 サイクル後の PPU ドット = %d, 期待 30", p.Dot())
	}
	if b.Cycles() != 10 {
		t.Errorf("サイクル数 = %d, 期待 10", b.Cycles())
	}
}

// TestTickAdvancesPPUWithRationalRatio は PAL の 16/5 の比が
// 分数で累積されることを確かめる。
//
// 浮動小数点で累積すると誤差が入り、決定論が保てない。
func TestTickAdvancesPPUWithRationalRatio(t *testing.T) {
	r := region.PAL
	rom := buildTestROM(t)
	c, err := cart.New(rom)
	if err != nil {
		t.Fatal(err)
	}
	p := ppu.New(r, c)
	b := New(r, p, apu.New(r), c)
	b.PowerOn(0)
	p.PowerOn(false)

	// 5 CPU サイクルで 16 ドット進む
	for range 5 {
		b.Read(0x0000)
	}
	if p.Dot() != 16 {
		t.Errorf("5 サイクル後の PPU ドット = %d, 期待 16", p.Dot())
	}

	// 誤差が累積しないことを確かめる。100 サイクルで 320 ドット。
	for range 95 {
		b.Read(0x0000)
	}
	total := p.Scanline()*r.DotsPerScanline + p.Dot()
	if total != 320 {
		t.Errorf("100 サイクル後の累積ドット = %d, 期待 320", total)
	}
}

// TestPeekDoesNotTick は Peek がサイクルを消費しないことを確かめる。
//
// デバッガがメモリを表示するためにサイクルを進めてはならない。
func TestPeekDoesNotTick(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	before := b.Cycles()
	for range 100 {
		b.Peek(0x2002)
		b.Peek(0x8000)
	}
	if b.Cycles() != before {
		t.Errorf("Peek でサイクルが %d 進んだ", b.Cycles()-before)
	}
}

// TestPokeWritesRAMWithoutTick は Poke がサイクルを消費せずに
// RAM を書き換えることを確かめる。
func TestPokeWritesRAMWithoutTick(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	before := b.Cycles()
	b.Poke(0x0123, 0x77)
	if got := b.Peek(0x0123); got != 0x77 {
		t.Errorf("$0123 = $%02X, 期待 $77", got)
	}
	if b.Cycles() != before {
		t.Error("Poke でサイクルが進んだ")
	}
}

// TestIRQSourcesAreOred は IRQ が発生源の論理和になることを確かめる。
//
// ある発生源がクリアされても他の発生源のアサートが残る必要がある。
func TestIRQSourcesAreOred(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	if b.IRQAsserted() {
		t.Error("初期状態で IRQ がアサートされている")
	}

	b.SetIRQ(IRQAPUFrame, true)
	b.SetIRQ(IRQMapper, true)
	if !b.IRQAsserted() {
		t.Error("IRQ がアサートされていない")
	}

	b.SetIRQ(IRQAPUFrame, false)
	if !b.IRQAsserted() {
		t.Error("マッパーの IRQ が残っているのにアサートが下りた")
	}

	b.SetIRQ(IRQMapper, false)
	if b.IRQAsserted() {
		t.Error("すべてクリアしたのにアサートが残っている")
	}
}

// TestHooksSeeReadsAndWrites はフックがアクセスを観測できることを確かめる。
func TestHooksSeeReadsAndWrites(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	var reads, writes int
	var lastOld uint8
	b.SetHooks(Hooks{
		OnCPURead:  func(addr uint16, v uint8) { reads++ },
		OnCPUWrite: func(addr uint16, v, old uint8) { writes++; lastOld = old },
	})

	b.Write(0x0010, 0x11)
	b.Write(0x0010, 0x22)
	b.Read(0x0010)

	if reads != 1 {
		t.Errorf("リードのフック呼び出し = %d, 期待 1", reads)
	}
	if writes != 2 {
		t.Errorf("ライトのフック呼び出し = %d, 期待 2", writes)
	}
	if lastOld != 0x11 {
		t.Errorf("2 回目のライトの old = $%02X, 期待 $11", lastOld)
	}
}

// TestStatusReadDoesNotUpdateOpenBus は $4015 の読み出しが
// オープンバスを更新しないことを確かめる。
//
// $4015 の読み出しは CPU 内部で完結する。
func TestStatusReadDoesNotUpdateOpenBus(t *testing.T) {
	b := newTestBus(t, region.NTSC)

	b.Write(0x0000, 0x3C)
	b.Read(0x0000)
	if got := b.OpenBus(); got != 0x3C {
		t.Fatalf("OpenBus = $%02X, 期待 $3C", got)
	}
	b.Read(0x4015)
	if got := b.OpenBus(); got != 0x3C {
		t.Errorf("$4015 の読み出しで OpenBus が $%02X に変わった", got)
	}
}
