package debug

import (
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// newTestNES は $8000 から code を置いた NROM の本体を組み立てる。
func newTestNES(t *testing.T, code []uint8) *nes.NES {
	t.Helper()
	prg := make([]uint8, 32*1024)
	copy(prg, code)
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	n.PowerOn(nes.Deterministic())
	return n
}

// anyHook はいずれかのフックが設定されているかを返す。
func anyHook(h nes.Hooks) bool {
	return h.OnCPURead != nil || h.OnCPUWrite != nil || h.OnInstructionStart != nil ||
		h.OnBeforeExec != nil || h.OnCycle != nil || h.OnInterrupt != nil ||
		h.OnSprite0Hit != nil || h.OnFrameComplete != nil
}

// TestWarnReachesLogger はコアの warn.compat の対象に到達したとき、ログへ
// 記録されることを確かめる（設計書 09 編 §9.8）。
func TestWarnReachesLogger(t *testing.T) {
	n := newTestNES(t, []uint8{
		// PPU が書き込みを受け付けるまで VBlank を 2 回待つ。
		0x2C, 0x02, 0x20, 0x10, 0xFB, // BIT $2002 / BPL $8000
		0x2C, 0x02, 0x20, 0x10, 0xFB, // BIT $2002 / BPL $8005
		0xA9, 0x3F, 0x8D, 0x06, 0x20, // LDA #$3F / STA $2006
		0xA9, 0x00, 0x8D, 0x06, 0x20, // LDA #$00 / STA $2006
		0xA9, 0x0D, 0x8D, 0x07, 0x20, // LDA #$0D / STA $2007（色 $0D）
		0x8B, 0x00, // XAA #$00（不安定な非公式命令）
		0x02, // STP
	})
	d := New(NewLogger(DefaultCategories, nil), 16, 0)
	d.Attach(n, NewSymbols())
	runUntilHalted(t, n)
	var msgs []string
	for _, e := range d.Logger().Entries(CatWarnCompat, "", 0) {
		msgs = append(msgs, e.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"色 $0D", "XAA", "STP"} {
		if !strings.Contains(all, want) {
			t.Errorf("%q の記録が無い: %q", want, all)
		}
	}

	// warn.compat を無効にすると記録しない。
	d.Logger().Clear()
	d.SetLogCategories(CatError)
	n.PowerOn(nes.Deterministic())
	runUntilHalted(t, n)
	if got := d.Logger().Entries(0, "", 0); len(got) != 0 {
		t.Errorf("無効にしたのに記録した: %v", got)
	}
}

// runUntilHalted は STP で止まるまで命令を進める。
func runUntilHalted(t *testing.T, n *nes.NES) {
	t.Helper()
	for range 1_000_000 {
		if n.CPU.Halted() {
			return
		}
		n.StepInstruction()
	}
	t.Fatal("STP に達しない")
}

// TestHooksOnlyWhenNeeded はブレークポイントや機能が無いときフックを
// 設定しないことを確かめる（設計書 09 編 §9.6）。
func TestHooksOnlyWhenNeeded(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	d := New(NewLogger(DefaultCategories, nil), 16, 0)
	d.Attach(n, NewSymbols())
	if anyHook(n.Hooks()) {
		t.Fatal("何も使っていないのにフックがある")
	}

	id := d.AddBreakpoint(Breakpoint{Kind: BreakWrite, AddrStart: 0x10, AddrEnd: 0x10, Enabled: true})
	if h := n.Hooks(); h.OnCPUWrite == nil || h.OnCPURead != nil || h.OnBeforeExec != nil {
		t.Errorf("書き込みブレークポイントのフック = %+v", h)
	}
	d.SetBreakpointEnabled(id, false)
	if anyHook(n.Hooks()) {
		t.Error("無効にしたブレークポイントのフックが残っている")
	}
	d.RemoveBreakpoint(id)

	frameEnd := d.AcquireSnapshots(-1)
	if h := n.Hooks(); h.OnFrameComplete == nil || h.OnBeforeExec != nil || h.OnCycle != nil {
		t.Errorf("フレーム末のスナップショットのフック = %+v", h)
	}
	line := d.AcquireSnapshots(100)
	if h := n.Hooks(); h.OnCycle == nil {
		t.Errorf("スキャンラインのスナップショットのフック = %+v", h)
	}
	if again := d.AcquireSnapshots(100); again != line {
		t.Error("同じ位置の購読が SnapshotSet を共有していない")
	}
	d.ReleaseSnapshots(line)
	if n.Hooks().OnCycle == nil {
		t.Error("購読が残っているのに OnCycle が外れた")
	}
	d.ReleaseSnapshots(line)
	d.ReleaseSnapshots(frameEnd)
	if anyHook(n.Hooks()) {
		t.Error("購読をやめた後もフックが残っている")
	}
}

// TestDisassemblerConfirmsExecuted は実行した位置の行が推定でなくなり、
// 名前と注記が付くことを確かめる（設計書 09 編 §9.4.6）。
func TestDisassemblerConfirmsExecuted(t *testing.T) {
	n := newTestNES(t, []uint8{
		0xA2, 0x05, // LDX #$05
		0xBD, 0x00, 0x03, // LDA $0300,X
		0x20, 0x10, 0x80, // JSR $8010
		0x4C, 0x00, 0x80, // JMP $8000
		0, 0, 0, 0, 0,
		0x60, // $8010 RTS
	})
	n.Bus.Poke(0x0305, 0x42)
	syms := NewSymbols()
	syms.SetLabel(0x8010, "sub", nil)
	d := New(NewLogger(0, nil), 16, 0)
	d.Attach(n, syms)
	d.SetFeatures(Features{CPUView: true})

	before := d.ListingAt(0x8000, 4)
	if !before[0].Estimated {
		t.Error("実行前の行が推定になっていない")
	}
	for range 3 {
		n.StepInstruction()
	}
	lines := d.ListingAt(0x8000, 4)
	for i, l := range lines[:3] {
		if l.Estimated {
			t.Errorf("行 %d（$%04X）が実行後も推定のまま", i, l.Addr)
		}
	}
	if lines[1].Comment != "= $0305 = #$42" {
		t.Errorf("注記 = %q, 期待 %q", lines[1].Comment, "= $0305 = #$42")
	}
	if lines[2].Operand != "sub" {
		t.Errorf("JSR のオペランド = %q, 期待 sub", lines[2].Operand)
	}
}

// TestSymbolsRoundTrip は名前・領域・ウォッチ・ブレークポイントを保存して
// 読み戻せることを確かめる（設計書 09 編 §9.9）。
func TestSymbolsRoundTrip(t *testing.T) {
	s := NewSymbols()
	s.SetLabel(0x0300, "score", nil)
	s.MarkRegion(0x10, 0x1F, RegionData)
	s.AddWatch(0x0300)
	c, err := ParseCondition("A == $42")
	if err != nil {
		t.Fatal(err)
	}
	s.SetBreakpoints([]Breakpoint{
		{Kind: BreakExec, AddrStart: 0xC000, AddrEnd: 0xC000, Enabled: true, Condition: c},
		{Kind: BreakPPUPosition, Scanline: 241, Dot: 1},
		{Kind: BreakEvent, Event: EventSprite0Hit, Enabled: true},
	})
	path := filepath.Join(t.TempDir(), "sym.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSymbols(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.LabelAt(0x0300, nil) != "score" || !got.IsData(0x15) || got.IsData(0x20) {
		t.Error("名前または領域が読み戻せない")
	}
	if w := got.Watch(); len(w) != 1 || w[0] != 0x0300 {
		t.Errorf("ウォッチ = %v", w)
	}
	bps := got.Breakpoints(nil)
	if len(bps) != 3 {
		t.Fatalf("ブレークポイントの数 = %d, 期待 3", len(bps))
	}
	if bps[0].Condition == nil || bps[0].Condition.Expr != "A == $42" || bps[0].AddrStart != 0xC000 {
		t.Errorf("実行ブレークポイント = %+v", bps[0])
	}
	if bps[1].Enabled || bps[1].Scanline != 241 || bps[1].Dot != 1 {
		t.Errorf("PPU 位置ブレークポイント = %+v", bps[1])
	}
	if bps[2].Event != EventSprite0Hit {
		t.Errorf("イベントブレークポイント = %+v", bps[2])
	}
}

// TestWriteMemorySpaces はメモリビューアの編集が空間ごとの記憶域へ届く
// ことを確かめる（設計書 09 編 §9.4.5）。
func TestWriteMemorySpaces(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	var b [1]uint8

	if err := WriteMemory(n, SpacePPU, 0x3F10, 0x21, false); err != nil {
		t.Fatal(err)
	}
	ReadMemory(n, SpacePPU, 0x3F00, b[:])
	if b[0] != 0x21 {
		t.Errorf("$3F10 への書き込みが $3F00 に写っていない（$%02X）", b[0])
	}
	if err := WriteMemory(n, SpaceRAM, 0x0123, 0x77, false); err != nil {
		t.Fatal(err)
	}
	ReadMemory(n, SpaceCPU, 0x0923, b[:])
	if b[0] != 0x77 {
		t.Errorf("内蔵 RAM のミラーに写っていない（$%02X）", b[0])
	}
	// PRG-ROM と CHR-ROM への書き込みはオーバーレイへ行く。
	if err := WriteMemory(n, SpacePRGROM, 1, 0x99, false); err != nil {
		t.Fatal(err)
	}
	if got := n.Bus.Peek(0x8001); got != 0x99 {
		t.Errorf("PRG-ROM の変更が $8001 に現れない（$%02X）", got)
	}
	if err := WriteMemory(n, SpacePPU, 0x0123, 0x5A, false); err != nil {
		t.Fatal(err)
	}
	if got := n.PPU.PeekVRAM(0x0123); got != 0x5A {
		t.Errorf("CHR-ROM の変更が PPU $0123 に現れない（$%02X）", got)
	}
	prg, chr := n.ROM.Overlay().Patches()
	if len(prg) != 1 || len(chr) != 1 || chr[0].Offset != 0x0123 {
		t.Errorf("オーバーレイの変更 = %v, %v", prg, chr)
	}
	n.ROM.Overlay().SetEnabled(false)
	if n.Bus.Peek(0x8001) != 0x00 || n.PPU.PeekVRAM(0x0123) != 0 {
		t.Error("オーバーレイを無効にしても元に戻らない")
	}
	if err := WriteMemory(n, SpaceOAM, 0x100, 0, false); err == nil {
		t.Error("範囲外の書き込みを受け付けた")
	}
}

// TestSnapshotCapturedAtFrameEnd はスナップショットがフレーム末に取られ、
// 内容が写されることを確かめる（設計書 09 編 §9.3）。
func TestSnapshotCapturedAtFrameEnd(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	d := New(NewLogger(0, nil), 16, 0)
	d.Attach(n, NewSymbols())
	var s Snapshot
	set := d.AcquireSnapshots(-1)
	if set.Latest(&s) {
		t.Fatal("取る前からスナップショットがある")
	}
	for i := range 64 {
		n.PPU.OAM()[i*4] = 0xFF
	}
	n.PPU.OAM()[0] = 0x10
	n.PPU.OAM()[1] = 0x01
	n.RunFrame()
	n.RunFrame()
	if !set.Latest(&s) {
		t.Fatal("スナップショットが無い")
	}
	if s.Scanline != -1 || s.Frame == 0 || s.OAM[0] != 0x10 || s.SpriteHeight != 8 {
		t.Errorf("スナップショット = 行 %d、フレーム %d、OAM[0] $%02X、高さ %d", s.Scanline, s.Frame, s.OAM[0], s.SpriteHeight)
	}
	// Y = $10 のスプライト 0 は $11 から 8 行にかかる。
	if !s.SpriteDrawn[0] || s.SpriteDrawn[1] {
		t.Errorf("描かれたスプライト = %v", s.SpriteDrawn[:4])
	}
	if s.ScanlineSpriteCount[0x10] != 0 || s.ScanlineSpriteCount[0x11] != 1 ||
		s.ScanlineSpriteCount[0x18] != 1 || s.ScanlineSpriteCount[0x19] != 0 {
		t.Errorf("行ごとのスプライト数 = %v", s.ScanlineSpriteCount[0x10:0x1A])
	}
}

// TestSpriteDrawnLimitsEightPerLine は 1 行に 9 個以上並んだとき、OAM の
// 9 番目以降を描かれないものとすることを確かめる。
func TestSpriteDrawnLimitsEightPerLine(t *testing.T) {
	var oam [256]uint8
	for i := range 64 {
		oam[i*4] = 0xFF
	}
	for i := range 10 {
		oam[i*4] = 0x20
	}
	var count [visibleScanlines]uint8
	var drawn [64]bool
	countSpritesPerLine(&oam, 8, &count, &drawn)
	if count[0x21] != 10 {
		t.Errorf("行 $21 のスプライト数 = %d, 期待 10", count[0x21])
	}
	for i := range 10 {
		if want := i < 8; drawn[i] != want {
			t.Errorf("スプライト %d の描画 = %v, 期待 %v", i, drawn[i], want)
		}
	}
}

// TestSnapshotAtScanlineSeesMidFrameBank はスキャンラインの開始時に取った
// スナップショットが、その時点のパレットを写すことを確かめる（設計書 09 編 §9.3.1）。
func TestSnapshotAtScanlineSeesMidFrameBank(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	d := New(NewLogger(0, nil), 16, 0)
	d.Attach(n, NewSymbols())
	top := d.AcquireSnapshots(10)
	end := d.AcquireSnapshots(-1)
	// 行 5 と行 100 でパレットを書き換え、同じフレームの行 10 とフレーム末で
	// 違う値が写ることを見る。
	stepTo := func(line int) {
		for n.PPU.Scanline() != line {
			n.StepInstruction()
		}
	}
	n.RunFrame()
	stepTo(5)
	n.PPU.Palette()[0] = 0x05
	stepTo(100)
	n.PPU.Palette()[0] = 0x06
	stepTo(245)
	var a, b Snapshot
	top.Latest(&a)
	end.Latest(&b)
	if a.Scanline != 10 || b.Scanline != -1 {
		t.Fatalf("取得位置 = %d, %d", a.Scanline, b.Scanline)
	}
	if a.Palette[0] != 0x05 || b.Palette[0] != 0x06 {
		t.Errorf("パレット = 行 10: $%02X, フレーム末: $%02X", a.Palette[0], b.Palette[0])
	}
}

// newMMC3NES は CHR の 1 KiB バンク i をすべて値 i で埋めた MMC3 の本体を作る。
func newMMC3NES(t *testing.T) *nes.NES {
	t.Helper()
	prg := make([]uint8, 32*1024)
	// 最後の 8 KiB（$E000）は固定。JMP $E000 で回り続ける。
	copy(prg[0x6000:], []uint8{0x4C, 0x00, 0xE0})
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0xE0
	chr := make([]uint8, 16*1024)
	for i := range chr {
		chr[i] = uint8(i / 1024)
	}
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 2, 0x40, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, chr...)
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	n.PowerOn(nes.Deterministic())
	return n
}

// TestSnapshotFollowsMMC3BankSwitch は画面の途中で CHR バンクを切り替えたとき、
// 取得位置によってパターンテーブルの内容が変わることを確かめる
// （設計書 09 編 §9.3.1）。
func TestSnapshotFollowsMMC3BankSwitch(t *testing.T) {
	n := newMMC3NES(t)
	d := New(NewLogger(0, nil), 16, 0)
	d.Attach(n, NewSymbols())
	top := d.AcquireSnapshots(10)
	end := d.AcquireSnapshots(-1)
	stepTo := func(line int) {
		for n.PPU.Scanline() != line {
			n.StepInstruction()
		}
	}
	n.RunFrame()
	stepTo(5)
	n.Bus.Write(0x8000, 0x00) // R0: $0000 の 2 KiB
	n.Bus.Write(0x8001, 0x02)
	stepTo(100)
	n.Bus.Write(0x8001, 0x08)
	stepTo(245)
	var a, b Snapshot
	top.Latest(&a)
	end.Latest(&b)
	if a.CHR[0] != 2 || b.CHR[0] != 8 {
		t.Errorf("CHR $0000 = 行 10: %d, フレーム末: %d, 期待 2, 8", a.CHR[0], b.CHR[0])
	}
	if a.BankView[0].BankIndex == b.BankView[0].BankIndex {
		t.Error("バンク構成の表示が取得位置で変わらない")
	}
}

// TestSnapshotFollowsScroll は毎フレーム変わるスクロール位置にスナップショットが
// 追従することを確かめる（設計書 09 編 §9.4.2）。
func TestSnapshotFollowsScroll(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	d := New(NewLogger(0, nil), 16, 0)
	d.Attach(n, NewSymbols())
	set := d.AcquireSnapshots(-1)
	for range 3 {
		n.RunFrame() // PPU が書き込みを受け付けるまで待つ。
	}
	n.PPU.WriteRegister(1, 0x08)
	for x := uint8(0); x < 40; x += 13 {
		n.PPU.WriteRegister(5, x)
		n.PPU.WriteRegister(5, 0)
		n.RunFrame()
		n.RunFrame()
		var s Snapshot
		set.Latest(&s)
		got := int(s.ScrollV&0x1F)*8 + int(s.FineX)
		if got != int(x) {
			t.Errorf("スクロール X = %d, 期待 %d", got, x)
		}
	}
}

// TestSnapshotCopySize はスナップショット 1 回のコピー量が 17 KiB 程度に
// 収まることを確かめる（設計書 09 編 §9.3）。
func TestSnapshotCopySize(t *testing.T) {
	size := int(unsafe.Sizeof(Snapshot{}))
	t.Logf("スナップショット 1 回 %d バイト、60 fps で毎秒 %.2f MiB", size, float64(size*60)/(1<<20))
	if size > 24*1024 {
		t.Errorf("スナップショットが %d バイトある", size)
	}
}

// TestEditsReflectOnScreen はビューアからの 5 種類の編集が、次のフレームの
// 画面に反映されることを確かめる（フェーズ 11 計画 §3.11）。
func TestEditsReflectOnScreen(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	for range 3 {
		n.RunFrame() // PPU が書き込みを受け付けるまで待つ。
	}
	write := func(s Space, addr int, v uint8) {
		t.Helper()
		if err := WriteMemory(n, s, addr, v, false); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 64 {
		write(SpaceOAM, i*4, 0xFF)
	}
	write(SpacePPU, 0x3F00, 0x0F)
	write(SpacePPU, 0x3F01, 0x30)
	write(SpacePPU, 0x3F05, 0x16)
	write(SpacePPU, 0x3F11, 0x2A)
	n.PPU.WriteRegister(1, 0x1E)
	// 有効にしたフレームはプリレンダー行のプリフェッチを経ていないため 1 フレーム捨てる。
	n.RunFrame()
	pixel := func(x, y int) uint8 {
		t.Helper()
		n.RunFrame()
		f := n.PPU.Queue().Take()
		if f == nil {
			t.Fatal("フレームが無い")
		}
		return video.PaletteIndex(f.At(x, y))
	}
	// RunFrame はフレームが変わった命令の終わりで戻るため、1 行目の先頭の
	// フェッチは編集より前に済んでいることがある。3 行目のタイル（y=16）で
	// 確かめる。
	if got := pixel(16, 16); got != 0x0F {
		t.Fatalf("編集前の (16, 16) = $%02X, 期待 $0F", got)
	}

	// タイル: タイル 0 の 1 行目を色 1 にする（CHR-ROM なのでオーバーレイへ）。
	write(SpacePPU, 0x0000, 0xFF)
	if got := pixel(16, 16); got != 0x30 {
		t.Errorf("タイルを編集した後の (16, 16) = $%02X, 期待 $30", got)
	}
	// ネームテーブル: (2, 2) をタイル 1（空）にする。
	write(SpacePPU, 0x2042, 0x01)
	if got := pixel(16, 16); got != 0x0F {
		t.Errorf("タイル番号を変えた後の (16, 16) = $%02X, 期待 $0F", got)
	}
	// 属性: (2-3, 2-3) の 16×16（属性バイトの右下の 2 bit）をパレット 1 にする。
	write(SpacePPU, 0x23C0, 0x40)
	if got := pixel(24, 16); got != 0x16 {
		t.Errorf("属性を変えた後の (24, 16) = $%02X, 期待 $16", got)
	}
	// パレット: パレット 1 の色 1 を変える。
	write(SpacePPU, 0x3F05, 0x21)
	if got := pixel(24, 16); got != 0x21 {
		t.Errorf("パレットを変えた後の (24, 16) = $%02X, 期待 $21", got)
	}
	// スプライト: スプライト 0 を (100, 50) に置き、X を 120 へ動かす。
	write(SpaceOAM, 0, 49)
	write(SpaceOAM, 1, 0)
	write(SpaceOAM, 2, 0)
	write(SpaceOAM, 3, 100)
	if got := pixel(100, 50); got != 0x2A {
		t.Errorf("スプライトの (100, 50) = $%02X, 期待 $2A", got)
	}
	write(SpaceOAM, 3, 120)
	n.RunFrame()
	f := n.PPU.Queue().Take()
	if a, b := video.PaletteIndex(f.At(120, 50)), video.PaletteIndex(f.At(100, 50)); a != 0x2A || b == 0x2A {
		t.Errorf("動かした後の (120, 50) = $%02X, (100, 50) = $%02X", a, b)
	}

	// オーバーレイを無効にすると元のタイルに戻る。
	n.ROM.Overlay().SetEnabled(false)
	if got := pixel(24, 16); got != 0x0F {
		t.Errorf("オーバーレイを無効にした後の (24, 16) = $%02X, 期待 $0F", got)
	}
}

// TestCHRRAMEditsDirectly は CHR-RAM のカートリッジではオーバーレイを使わず
// 直接書くことを確かめる（設計書 09 編 §9.4.8）。
func TestCHRRAMEditsDirectly(t *testing.T) {
	prg := make([]uint8, 32*1024)
	copy(prg, []uint8{0x4C, 0x00, 0x80})
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	n.PowerOn(nes.Deterministic())
	if err := WriteMemory(n, SpacePPU, 0x0123, 0x77, false); err != nil {
		t.Fatal(err)
	}
	if got := n.PPU.PeekVRAM(0x0123); got != 0x77 {
		t.Errorf("CHR-RAM $0123 = $%02X, 期待 $77", got)
	}
	if !n.ROM.Overlay().Empty() {
		t.Error("CHR-RAM の編集がオーバーレイへ行った")
	}
}

// TestAttributeAlignsWithTile は属性のパレット番号がタイルの絵と同じ位置に
// 描かれることを確かめる（設計書 04 編 §4.5.1）。
//
// 属性の 2 bit をシフタへ転送する時点の v で選ぶと、coarse X が次の
// タイルへ進んでいるため 8 ピクセル左へずれる。
func TestAttributeAlignsWithTile(t *testing.T) {
	n := newTestNES(t, []uint8{0x4C, 0x00, 0x80})
	for range 3 {
		n.RunFrame()
	}
	for addr, v := range map[int]uint8{0x3F00: 0x0F, 0x3F01: 0x30, 0x3F05: 0x16, 0x0000: 0xFF, 0x23C0: 0x40} {
		if err := WriteMemory(n, SpacePPU, addr, v, false); err != nil {
			t.Fatal(err)
		}
	}
	n.PPU.WriteRegister(1, 0x1E)
	n.RunFrame()
	n.RunFrame()
	f := n.PPU.Queue().Take()
	// 属性バイトの右下の 2 bit はタイル (2-3, 2-3)、つまり x=16-31・y=16-31。
	// タイル 0 は 1 行目だけを塗っているため、8 の倍数の行で確かめる。
	for _, tc := range []struct {
		x, y int
		want uint8
	}{{15, 16, 0x30}, {16, 16, 0x16}, {31, 24, 0x16}, {32, 16, 0x30}, {16, 8, 0x30}, {16, 32, 0x30}} {
		if got := video.PaletteIndex(f.At(tc.x, tc.y)); got != tc.want {
			t.Errorf("(%d, %d) = $%02X, 期待 $%02X", tc.x, tc.y, got, tc.want)
		}
	}
}
