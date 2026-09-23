package cart

import (
	"errors"
	"testing"
)

// romBuilder はテスト用の `.nes` ファイルを組み立てる。
//
// テストデータをファイルとして置かずコード内で組み立てるのは、
// どのヘッダの値を検証しているのかをテストの中で読み取れるようにするため
// である。
type romBuilder struct {
	header  [16]uint8
	trainer []uint8
	prg     []uint8
	chr     []uint8
}

func newROMBuilder() *romBuilder {
	b := &romBuilder{}
	copy(b.header[0:4], magic[:])
	return b
}

// ines は iNES 1.0 のヘッダを作る。prgUnits と chrUnits は単位数。
func (b *romBuilder) ines(prgUnits, chrUnits uint8) *romBuilder {
	b.header[4] = prgUnits
	b.header[5] = chrUnits
	b.prg = make([]uint8, int(prgUnits)*prgUnit)
	b.chr = make([]uint8, int(chrUnits)*chrUnit)
	return b
}

func (b *romBuilder) flags6(v uint8) *romBuilder {
	b.header[6] = b.header[6]&0xF0 | v&0x0F
	return b
}

func (b *romBuilder) mapperLow(n uint8) *romBuilder {
	b.header[6] = b.header[6]&0x0F | n<<4
	return b
}

func (b *romBuilder) byteAt(i int, v uint8) *romBuilder {
	b.header[i] = v
	return b
}

func (b *romBuilder) build() []uint8 {
	out := make([]uint8, 0, headerSize+len(b.prg)+len(b.chr))
	out = append(out, b.header[:]...)
	out = append(out, b.trainer...)
	out = append(out, b.prg...)
	out = append(out, b.chr...)
	return out
}

// TestLoadROMRejectsBadMagic はマジックが違うファイルを拒むことを確かめる。
func TestLoadROMRejectsBadMagic(t *testing.T) {
	data := newROMBuilder().ines(1, 1).build()
	data[0] = 'X'
	if _, err := LoadROM(data); !errors.Is(err, ErrNotINES) {
		t.Errorf("err = %v, 期待 ErrNotINES", err)
	}
}

// TestLoadROMRejectsShortFile はヘッダに届かないファイルを拒むことを確かめる。
func TestLoadROMRejectsShortFile(t *testing.T) {
	if _, err := LoadROM([]uint8{0x4E, 0x45, 0x53}); err == nil {
		t.Error("15 バイト以下のファイルを受け入れてしまった")
	}
}

// TestLoadROMRejectsTruncatedPRG は PRG がファイルの末尾を超える
// ヘッダを拒むことを確かめる。
func TestLoadROMRejectsTruncatedPRG(t *testing.T) {
	b := newROMBuilder().ines(2, 0)
	data := b.build()
	// PRG を半分だけ残す
	data = data[:headerSize+prgUnit]
	if _, err := LoadROM(data); err == nil {
		t.Error("PRG が足りないファイルを受け入れてしまった")
	}
}

// TestLoadROMRejectsTruncatedCHR は CHR がファイルの末尾を超える
// ヘッダを拒むことを確かめる。
func TestLoadROMRejectsTruncatedCHR(t *testing.T) {
	data := newROMBuilder().ines(1, 2).build()
	data = data[:headerSize+prgUnit+chrUnit]
	if _, err := LoadROM(data); err == nil {
		t.Error("CHR が足りないファイルを受け入れてしまった")
	}
}

// TestLoadROMRejectsZeroPRG は PRG-ROM が無いヘッダを拒むことを確かめる。
func TestLoadROMRejectsZeroPRG(t *testing.T) {
	if _, err := LoadROM(newROMBuilder().ines(0, 1).build()); err == nil {
		t.Error("PRG-ROM が 0 のファイルを受け入れてしまった")
	}
}

// TestLoadROMINES は iNES 1.0 のヘッダを解析できることを確かめる。
func TestLoadROMINES(t *testing.T) {
	data := newROMBuilder().
		ines(2, 1).      // PRG 32 KiB、CHR 8 KiB
		mapperLow(4).    // マッパー番号の下位ニブル
		byteAt(7, 0x30). // マッパー番号の上位ニブル
		flags6(0x01).    // 垂直ミラーリング
		build()

	rom, err := LoadROM(data)
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	if rom.Format != FormatINES {
		t.Errorf("Format = %v, 期待 iNES", rom.Format)
	}
	if rom.Mapper != 0x34 {
		t.Errorf("Mapper = %d, 期待 %d", rom.Mapper, 0x34)
	}
	if len(rom.PRG) != 2*prgUnit {
		t.Errorf("PRG = %d バイト, 期待 %d", len(rom.PRG), 2*prgUnit)
	}
	if len(rom.CHR) != chrUnit {
		t.Errorf("CHR = %d バイト, 期待 %d", len(rom.CHR), chrUnit)
	}
	if rom.Mirroring != MirrorVertical {
		t.Errorf("Mirroring = %v, 期待 vertical", rom.Mirroring)
	}
	if rom.CHRRAMSize != 0 {
		t.Errorf("CHR-ROM があるのに CHR-RAM を %d バイト割り当てた", rom.CHRRAMSize)
	}
}

// TestLoadROMArchaicIgnoresHighMapperNibble は archaic iNES で
// マッパー番号の上位 4 bit を無視することを確かめる。
//
// バイト 7 以降にリッパーの署名が入っている ROM があり、そのまま
// 解釈すると誤ったマッパーになる。
func TestLoadROMArchaicIgnoresHighMapperNibble(t *testing.T) {
	data := newROMBuilder().
		ines(1, 1).
		mapperLow(2).
		byteAt(7, 0x44). // bit 3-2 が 01 → archaic。上位ニブルは署名の一部
		build()

	rom, err := LoadROM(data)
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	if rom.Format != FormatArchaicINES {
		t.Errorf("Format = %v, 期待 archaic iNES", rom.Format)
	}
	if rom.Mapper != 2 {
		t.Errorf("Mapper = %d, 期待 2（上位ニブルを無視すること）", rom.Mapper)
	}
	if len(rom.Warnings) == 0 {
		t.Error("補正した内容が記録されていない")
	}
}

// TestLoadROMNES20Sizes は NES 2.0 のサイズ解釈を確かめる。
func TestLoadROMNES20Sizes(t *testing.T) {
	b := newROMBuilder()
	// PRG は 16 KiB 単位で 1、CHR は 8 KiB 単位で 1。MSB ニブルは 0。
	b.header[4] = 1
	b.header[5] = 1
	b.header[9] = 0x00
	// マッパー番号 $123、サブマッパー 4。
	// バイト 7 の bit 3-2 を 10 にすることで NES 2.0 と判別される。
	b.header[6] = 0x30
	b.header[7] = 0x28
	b.header[8] = 0x41
	// PRG-RAM と CHR-RAM のシフトカウント
	b.header[10] = 0x06 // 64 << 6 = 4096 バイト
	b.header[11] = 0x07 // 64 << 7 = 8192 バイト
	b.header[12] = 0x01 // PAL
	b.prg = make([]uint8, prgUnit)
	b.chr = make([]uint8, chrUnit)

	rom, err := LoadROM(b.build())
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	if rom.Format != FormatNES20 {
		t.Fatalf("Format = %v, 期待 NES 2.0", rom.Format)
	}
	if rom.Mapper != 0x123 {
		t.Errorf("Mapper = $%X, 期待 $123", rom.Mapper)
	}
	if rom.Submapper != 4 {
		t.Errorf("Submapper = %d, 期待 4", rom.Submapper)
	}
	if rom.PRGRAMSize != 4096 {
		t.Errorf("PRGRAMSize = %d, 期待 4096", rom.PRGRAMSize)
	}
	if rom.CHRRAMSize != 8192 {
		t.Errorf("CHRRAMSize = %d, 期待 8192", rom.CHRRAMSize)
	}
	if rom.TimingMode != TimingPAL {
		t.Errorf("TimingMode = %d, 期待 PAL", rom.TimingMode)
	}
}

// TestNES20ExponentSize は指数・乗数表記を確かめる。
func TestNES20ExponentSize(t *testing.T) {
	// MSB ニブルが $F のとき、下位 6 bit が指数、下位 2 bit が乗数。
	// lsb = 0b000101_01 → 指数 5、乗数 1*2+1 = 3 → 32 * 3 = 96
	if got := nes20Size(0b00010101, 0x0F, prgUnit); got != 96 {
		t.Errorf("nes20Size = %d, 期待 96", got)
	}
	// 通常表記は単位を掛ける
	if got := nes20Size(2, 0, prgUnit); got != 2*prgUnit {
		t.Errorf("nes20Size = %d, 期待 %d", got, 2*prgUnit)
	}
	// 現実に存在しない巨大な指数は 0 を返す（オーバーフローを避ける）
	if got := nes20Size(0xFC, 0x0F, prgUnit); got != 0 {
		t.Errorf("nes20Size = %d, 期待 0", got)
	}
}

// TestLoadROMTrainer はトレーナーを読み飛ばすことを確かめる。
func TestLoadROMTrainer(t *testing.T) {
	b := newROMBuilder().ines(1, 0).flags6(0x04)
	b.trainer = make([]uint8, trainerSize)
	for i := range b.trainer {
		b.trainer[i] = 0xAA
	}
	for i := range b.prg {
		b.prg[i] = 0x55
	}

	rom, err := LoadROM(b.build())
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	if len(rom.Trainer) != trainerSize {
		t.Errorf("Trainer = %d バイト, 期待 %d", len(rom.Trainer), trainerSize)
	}
	if rom.PRG[0] != 0x55 {
		t.Errorf("PRG の先頭 = $%02X, 期待 $55（トレーナーを読み飛ばすこと）", rom.PRG[0])
	}
}

// TestLoadROMCHRRAMWhenNoCHRROM は CHR-ROM が無いとき CHR-RAM を
// 割り当てることを確かめる。
func TestLoadROMCHRRAMWhenNoCHRROM(t *testing.T) {
	rom, err := LoadROM(newROMBuilder().ines(1, 0).build())
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	if rom.CHRRAMSize != chrUnit {
		t.Errorf("CHRRAMSize = %d, 期待 %d", rom.CHRRAMSize, chrUnit)
	}
}

// TestLoadROMBatteryGoesToNVRAM はバッテリーフラグが立っているとき
// PRG-NVRAM を割り当てることを確かめる。
func TestLoadROMBatteryGoesToNVRAM(t *testing.T) {
	rom, err := LoadROM(newROMBuilder().ines(1, 1).flags6(0x02).build())
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	if !rom.HasBattery {
		t.Error("HasBattery が false")
	}
	if rom.PRGNVRAMSize == 0 {
		t.Error("PRG-NVRAM が割り当てられていない")
	}
	if rom.PRGRAMSize != 0 {
		t.Errorf("PRGRAMSize = %d。バッテリーがあるときは NVRAM 側に置くこと", rom.PRGRAMSize)
	}
}

// TestLoadROMHashDependsOnContentOnly はハッシュが PRG と CHR の内容のみで
// 決まることを確かめる。ヘッダだけが違う同じ ROM を同じものとして扱う。
func TestLoadROMHashDependsOnContentOnly(t *testing.T) {
	a := newROMBuilder().ines(1, 1)
	a.prg[0] = 0x42
	romA, err := LoadROM(a.build())
	if err != nil {
		t.Fatal(err)
	}

	b := newROMBuilder().ines(1, 1).flags6(0x01) // ミラーリングだけ違う
	b.prg[0] = 0x42
	romB, err := LoadROM(b.build())
	if err != nil {
		t.Fatal(err)
	}

	if romA.Hash != romB.Hash {
		t.Error("ヘッダの違いでハッシュが変わった")
	}

	c := newROMBuilder().ines(1, 1)
	c.prg[0] = 0x43 // 内容が違う
	romC, err := LoadROM(c.build())
	if err != nil {
		t.Fatal(err)
	}
	if romA.Hash == romC.Hash {
		t.Error("内容が違うのにハッシュが同じ")
	}
}

// TestShiftSize は NES 2.0 のシフトカウントの解釈を確かめる。
func TestShiftSize(t *testing.T) {
	tests := []struct {
		shift uint8
		want  int
	}{
		{0, 0}, // 0 はそのメモリを持たない
		{1, 128},
		{2, 256},
		{7, 8192},
		{8, 16384},
		{10, 65536},
	}
	for _, tt := range tests {
		if got := shiftSize(tt.shift); got != tt.want {
			t.Errorf("shiftSize(%d) = %d, 期待 %d", tt.shift, got, tt.want)
		}
	}
}
