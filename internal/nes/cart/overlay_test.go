package cart

import "testing"

// newOverlayROM は PRG 16 KiB・CHR 8 KiB の ROM を作る。
func newOverlayROM(t *testing.T) *ROM {
	t.Helper()
	prg := make([]uint8, 16*1024)
	chr := make([]uint8, 8*1024)
	for i := range chr {
		chr[i] = uint8(i)
	}
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, chr...)
	rom, err := LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	return rom
}

// TestOverlayAppliesThroughMapper はオーバーレイの変更がマッパーの読み出しに
// 現れ、無効にすると元に戻ることを確かめる（設計書 06 編 §6.8）。
func TestOverlayAppliesThroughMapper(t *testing.T) {
	rom := newOverlayROM(t)
	c, err := New(rom)
	if err != nil {
		t.Fatal(err)
	}
	o := rom.Overlay()
	if o.Hash() != ([8]uint8{}) {
		t.Error("変更の無いオーバーレイのハッシュが 0 でない")
	}
	if err := o.SetCHR(0x0010, 0xAA); err != nil {
		t.Fatal(err)
	}
	if err := o.SetPRG(0x0000, 0x55); err != nil {
		t.Fatal(err)
	}
	if got := c.ReadCHR(0x0010); got != 0xAA {
		t.Errorf("CHR $0010 = $%02X, 期待 $AA", got)
	}
	if got, _ := c.ReadPRG(0x8000); got != 0x55 {
		t.Errorf("PRG $8000 = $%02X, 期待 $55", got)
	}
	h := o.Hash()
	if h == ([8]uint8{}) {
		t.Error("変更のあるオーバーレイのハッシュが 0 になっている")
	}

	o.SetEnabled(false)
	if got := c.ReadCHR(0x0010); got != 0x10 {
		t.Errorf("無効にした後の CHR $0010 = $%02X, 期待 $10", got)
	}
	if o.Hash() != ([8]uint8{}) {
		t.Error("無効のオーバーレイのハッシュが 0 でない")
	}
	o.SetEnabled(true)
	if got := c.ReadCHR(0x0010); got != 0xAA || o.Hash() != h {
		t.Errorf("有効に戻した後の CHR $0010 = $%02X", got)
	}

	// 元の値に戻すと変更から外れる。
	if err := o.SetCHR(0x0010, 0x10); err != nil {
		t.Fatal(err)
	}
	prg, chr := o.Patches()
	if len(prg) != 1 || len(chr) != 0 {
		t.Errorf("変更の数 = PRG %d, CHR %d, 期待 1, 0", len(prg), len(chr))
	}
	o.Clear()
	if got, _ := c.ReadPRG(0x8000); got != 0 || !o.Empty() {
		t.Errorf("消去した後の PRG $8000 = $%02X", got)
	}
}

// TestOverlayPatchesSortedAndLoad は変更が昇順に並び、読み込みで再現される
// ことを確かめる。
func TestOverlayPatchesSortedAndLoad(t *testing.T) {
	rom := newOverlayROM(t)
	o := rom.Overlay()
	for _, off := range []int{0x300, 0x100, 0x200} {
		if err := o.SetCHR(off, 0xFF); err != nil {
			t.Fatal(err)
		}
	}
	_, chr := o.Patches()
	for i := 1; i < len(chr); i++ {
		if chr[i-1].Offset >= chr[i].Offset {
			t.Fatalf("昇順に並んでいない: %v", chr)
		}
	}
	h := o.Hash()

	other := newOverlayROM(t)
	if err := other.Overlay().Load(nil, chr, true); err != nil {
		t.Fatal(err)
	}
	if other.Overlay().Hash() != h || other.CHR[0x200] != 0xFF {
		t.Error("読み込んだオーバーレイが再現されない")
	}
	if err := o.SetCHR(len(rom.CHR), 0); err == nil {
		t.Error("範囲外の変更を受け付けた")
	}
}
