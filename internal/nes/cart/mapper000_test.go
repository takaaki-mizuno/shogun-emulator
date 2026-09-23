package cart

import "testing"

// buildNROM はマッパー 0 のカートリッジを作る。
func buildNROM(t *testing.T, prgUnits, chrUnits uint8, flags6 uint8) (Cartridge, *ROM) {
	t.Helper()
	b := newROMBuilder().ines(prgUnits, chrUnits).flags6(flags6)
	// PRG の各 16 KiB バンクの先頭と末尾に目印を置く
	for i := range int(prgUnits) {
		b.prg[i*prgUnit] = uint8(0x10 + i)
		b.prg[(i+1)*prgUnit-1] = uint8(0xF0 + i)
	}
	for i := range b.chr {
		b.chr[i] = uint8(i & 0xFF)
	}
	rom, err := LoadROM(b.build())
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	c, err := New(rom)
	if err != nil {
		t.Fatalf("カートリッジを作れない: %v", err)
	}
	return c, rom
}

// TestNROM32KiB は 32 KiB の PRG が $8000-$FFFF に並ぶことを確かめる。
func TestNROM32KiB(t *testing.T) {
	c, _ := buildNROM(t, 2, 1, 0)

	tests := []struct {
		addr uint16
		want uint8
	}{
		{0x8000, 0x10}, // バンク 0 の先頭
		{0xBFFF, 0xF0}, // バンク 0 の末尾
		{0xC000, 0x11}, // バンク 1 の先頭
		{0xFFFF, 0xF1}, // バンク 1 の末尾
	}
	for _, tt := range tests {
		got, handled := c.ReadPRG(tt.addr)
		if !handled {
			t.Errorf("$%04X が handled されていない", tt.addr)
			continue
		}
		if got != tt.want {
			t.Errorf("$%04X = $%02X, 期待 $%02X", tt.addr, got, tt.want)
		}
	}
}

// TestNROM16KiBMirrors は 16 KiB の PRG で $C000-$FFFF が
// $8000-$BFFF のミラーになることを確かめる。
func TestNROM16KiBMirrors(t *testing.T) {
	c, _ := buildNROM(t, 1, 1, 0)

	for _, offset := range []uint16{0x0000, 0x0001, 0x1234, 0x3FFF} {
		lo, _ := c.ReadPRG(0x8000 + offset)
		hi, _ := c.ReadPRG(0xC000 + offset)
		if lo != hi {
			t.Errorf("$%04X = $%02X と $%04X = $%02X が一致しない",
				0x8000+offset, lo, 0xC000+offset, hi)
		}
	}
}

// TestNROMWriteToPRGROMIgnored は PRG-ROM への書き込みが
// 無視されることを確かめる。
func TestNROMWriteToPRGROMIgnored(t *testing.T) {
	c, _ := buildNROM(t, 2, 1, 0)

	before, _ := c.ReadPRG(0x8000)
	c.WritePRG(0x8000, ^before)
	after, _ := c.ReadPRG(0x8000)
	if after != before {
		t.Errorf("$8000 が $%02X から $%02X に変わった。ROM への書き込みは無視すること",
			before, after)
	}
}

// TestNROMPRGRAM は $6000-$7FFF の PRG-RAM を読み書きできることを確かめる。
func TestNROMPRGRAM(t *testing.T) {
	c, _ := buildNROM(t, 2, 1, 0)

	c.WritePRG(0x6000, 0x42)
	got, handled := c.ReadPRG(0x6000)
	if !handled {
		t.Fatal("$6000 が handled されていない")
	}
	if got != 0x42 {
		t.Errorf("$6000 = $%02X, 期待 $42", got)
	}
}

// TestNROMCHRROMWriteIgnored は CHR-ROM への書き込みが無視されることを
// 確かめる。
func TestNROMCHRROMWriteIgnored(t *testing.T) {
	c, _ := buildNROM(t, 2, 1, 0)

	before := c.ReadCHR(0x0010)
	c.WriteCHR(0x0010, ^before)
	if after := c.ReadCHR(0x0010); after != before {
		t.Errorf("CHR-ROM が $%02X から $%02X に変わった", before, after)
	}
}

// TestNROMCHRRAMWritable は CHR-ROM が無いとき CHR-RAM に書けることを
// 確かめる。
func TestNROMCHRRAMWritable(t *testing.T) {
	c, _ := buildNROM(t, 2, 0, 0)

	c.WriteCHR(0x0010, 0x5A)
	if got := c.ReadCHR(0x0010); got != 0x5A {
		t.Errorf("CHR-RAM $0010 = $%02X, 期待 $5A", got)
	}
}

// TestNROMNametableMirroring はミラーリングの解決を確かめる。
func TestNROMNametableMirroring(t *testing.T) {
	// flags6 の bit 0 が 0 → 水平ミラーリング（垂直配置）
	h, _ := buildNROM(t, 1, 1, 0x00)
	// bit 0 が 1 → 垂直ミラーリング（水平配置）
	v, _ := buildNROM(t, 1, 1, 0x01)

	tests := []struct {
		addr      uint16
		wantHoriz uint32
		wantVert  uint32
	}{
		{0x2000, 0x000, 0x000},
		{0x2400, 0x000, 0x400}, // 水平: 上段の 2 面が同じ
		{0x2800, 0x400, 0x000}, // 水平: 下段へ移る
		{0x2C00, 0x400, 0x400},
		{0x23FF, 0x3FF, 0x3FF},
		{0x3000, 0x000, 0x000}, // $3000-$3EFF は $2000-$2EFF のミラー
	}
	for _, tt := range tests {
		if got := h.MapNametable(tt.addr); got.Offset != tt.wantHoriz {
			t.Errorf("水平ミラーリング $%04X → オフセット $%03X, 期待 $%03X",
				tt.addr, got.Offset, tt.wantHoriz)
		}
		if got := v.MapNametable(tt.addr); got.Offset != tt.wantVert {
			t.Errorf("垂直ミラーリング $%04X → オフセット $%03X, 期待 $%03X",
				tt.addr, got.Offset, tt.wantVert)
		}
	}
}

// TestNROMFourScreenIgnored はマッパー 0 が 4 画面構成を扱えないため
// ヘッダのビットを無視することを確かめる。
func TestNROMFourScreenIgnored(t *testing.T) {
	_, rom := buildNROM(t, 1, 1, 0x08)

	if rom.Mirroring == MirrorFourScreen {
		t.Error("マッパー 0 で 4 画面構成が採用された")
	}
	if len(rom.Warnings) == 0 {
		t.Error("4 画面ビットを無視したことが記録されていない")
	}
}

// TestUnsupportedMapperError は未対応のマッパーでエラーになり、
// 名前が含まれることを確かめる。
func TestUnsupportedMapperError(t *testing.T) {
	b := newROMBuilder().ines(1, 1).mapperLow(5)
	rom, err := LoadROM(b.build())
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(rom)
	if err == nil {
		t.Fatal("未対応のマッパーを受け入れてしまった")
	}
	if !contains(err.Error(), "MMC5") {
		t.Errorf("エラーにマッパー名が含まれない: %v", err)
	}
}

// TestBankedWrapsAroundData はバンク番号がバンク総数で剰余を
// 取られることを確かめる。ROM の末尾を超えるバンクは前のバンクのミラー。
func TestBankedWrapsAroundData(t *testing.T) {
	data := make([]uint8, 3*1024)
	for i := range data {
		data[i] = uint8(i / 1024)
	}
	b := newBanked(data, 1024, 1)

	for _, tt := range []struct {
		bank int
		want uint8
	}{
		{0, 0}, {1, 1}, {2, 2},
		{3, 0},  // 3 バンクなので折り返す
		{4, 1},  //
		{-1, 2}, // 負のバンク番号は末尾から数える
	} {
		b.setBank(0, tt.bank)
		got, ok := b.read(0, 0)
		if !ok {
			t.Errorf("バンク %d が読めない", tt.bank)
			continue
		}
		if got != tt.want {
			t.Errorf("バンク %d の先頭 = %d, 期待 %d", tt.bank, got, tt.want)
		}
	}
}

// TestBankedEmptyData は空のデータで読み書きが失敗することを確かめる。
func TestBankedEmptyData(t *testing.T) {
	b := newBanked(nil, 1024, 1)
	if _, ok := b.read(0, 0); ok {
		t.Error("空のデータから読めてしまった")
	}
	if b.write(0, 0, 1) {
		t.Error("空のデータへ書けてしまった")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
