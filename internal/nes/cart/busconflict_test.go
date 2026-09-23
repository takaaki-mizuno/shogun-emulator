package cart

import "testing"

// buildConflictROM はサブマッパーを指定した NES 2.0 の ROM を作る。
func buildConflictROM(t *testing.T, mapper uint8, submapper uint8, setting string) Cartridge {
	t.Helper()
	b := newROMBuilder().ines(8, 0)
	b.header[6] = (mapper & 0x0F) << 4
	// バイト 7 の bit 3-2 を 10 にして NES 2.0 とする。
	b.header[7] = mapper&0xF0 | 0x08
	b.header[8] = submapper << 4
	// CHR-RAM 8 KiB
	b.header[11] = 7
	// $8000 の内容を $03 にする。書いた値との論理積が観測できる。
	for i := range b.prg {
		b.prg[i] = 0xFF
	}
	b.prg[0] = 0x03
	rom, err := LoadROM(b.build())
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	if rom.Format != FormatNES20 {
		t.Fatalf("NES 2.0 と判定されない（%v）", rom.Format)
	}
	c, err := NewWithOptions(rom, Options{BusConflicts: setting})
	if err != nil {
		t.Fatalf("カートリッジを作れない: %v", err)
	}
	return c
}

// TestBusConflictBySubmapper はサブマッパーで競合の有無が決まることを
// 確かめる。
func TestBusConflictBySubmapper(t *testing.T) {
	// $8000 の ROM の内容は $03 である。競合を再現すると、書いた値の
	// bit 0-1 だけが残る。
	for _, tt := range []struct {
		submapper uint8
		write     uint8
		want      int
	}{
		// サブマッパー 0 は不明。競合を再現しない。
		{0, 0x05, 5},
		// サブマッパー 1 は競合なし。
		{1, 0x05, 5},
		// サブマッパー 2 は競合あり。$05 AND $03 = $01。
		{2, 0x05, 1},
		{2, 0x06, 2},
	} {
		c := buildConflictROM(t, 2, tt.submapper, BusConflictsAuto)
		c.WritePRG(0x8000, tt.write)
		got := c.Info().PRGBanks[0].BankIndex
		if got != tt.want {
			t.Errorf("サブマッパー %d に $%02X を書いてバンク %d, 期待 %d",
				tt.submapper, tt.write, got, tt.want)
		}
	}
}

// TestBusConflictSetting は設定で競合の再現を切り替えられることを
// 確かめる。
func TestBusConflictSetting(t *testing.T) {
	for _, tt := range []struct {
		setting   string
		submapper uint8
		write     uint8
		want      int
	}{
		{BusConflictsNever, 2, 0x05, 5},
		{BusConflictsAlways, 0, 0x05, 1},
		{BusConflictsAuto, 0, 0x05, 5},
	} {
		c := buildConflictROM(t, 2, tt.submapper, tt.setting)
		c.WritePRG(0x8000, tt.write)
		if got := c.Info().PRGBanks[0].BankIndex; got != tt.want {
			t.Errorf("%s / サブマッパー %d に $%02X を書いてバンク %d, 期待 %d",
				tt.setting, tt.submapper, tt.write, got, tt.want)
		}
	}
}

// TestBusConflictNotAppliedToMMC1 は専用チップのマッパーで競合を
// 再現しないことを確かめる。
//
// MMC1 はレジスタがデータバスを駆動しない。always でも競合を起こすと
// シリアルポートへ入る値が変わってしまう。
func TestBusConflictNotAppliedToMMC1(t *testing.T) {
	mode, err := busConflictModeFor(&ROM{Mapper: 1, Format: FormatNES20}, BusConflictsAlways)
	if err != nil {
		t.Fatal(err)
	}
	if mode != busConflictNone {
		t.Error("MMC1 で競合を再現する設定になった")
	}
}

// TestBusConflictUnknownSetting は知らない設定値を断ることを確かめる。
func TestBusConflictUnknownSetting(t *testing.T) {
	if _, err := busConflictModeFor(&ROM{Mapper: 2}, "sometimes"); err == nil {
		t.Error("知らない設定値を受け入れた")
	}
}
