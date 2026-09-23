package cart

import "testing"

// buildMapper は指定したマッパーのカートリッジを作る。
//
// PRG の各 16 KiB バンクの先頭にバンク番号を置く。どのバンクが見えて
// いるかを 1 バイトの読み出しで確かめられるようにする。
func buildMapper(t *testing.T, mapper uint8, prgUnits, chrUnits uint8) (Cartridge, *ROM) {
	t.Helper()
	b := newROMBuilder().ines(prgUnits, chrUnits)
	// マッパー番号の下位ニブルをバイト 6 に、上位ニブルをバイト 7 に置く。
	b.header[6] = b.header[6]&0x0F | (mapper&0x0F)<<4
	b.header[7] = mapper & 0xF0
	for i := range int(prgUnits) {
		b.prg[i*prgUnit] = uint8(i)
		b.prg[(i+1)*prgUnit-1] = uint8(0x80 + i)
	}
	for i := range int(chrUnits) {
		b.chr[i*chrUnit] = uint8(0xC0 + i)
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

// readPRG は読み出した値だけを返す。
func readPRG(t *testing.T, c Cartridge, addr uint16) uint8 {
	t.Helper()
	v, handled := c.ReadPRG(addr)
	if !handled {
		t.Fatalf("$%04X が handled されていない", addr)
	}
	return v
}

// TestUxROMSwitchesFirstWindow はマッパー 2 が $8000-$BFFF を切り替え、
// $C000-$FFFF を末尾のバンクに固定することを確かめる。
func TestUxROMSwitchesFirstWindow(t *testing.T) {
	// PRG 8 バンク、CHR-RAM
	c, _ := buildMapper(t, 2, 8, 0)

	if got := readPRG(t, c, 0xC000); got != 7 {
		t.Errorf("$C000 のバンク = %d, 期待 7（末尾に固定）", got)
	}
	for _, bank := range []uint8{0, 3, 7} {
		c.WritePRG(0x8000, bank)
		if got := readPRG(t, c, 0x8000); got != bank {
			t.Errorf("バンク %d を選んだのに $8000 = %d", bank, got)
		}
		if got := readPRG(t, c, 0xC000); got != 7 {
			t.Errorf("バンク %d を選んだ後の $C000 = %d, 期待 7", bank, got)
		}
	}
	// 範囲を超えるバンク番号は剰余で折り返す。
	c.WritePRG(0x8000, 9)
	if got := readPRG(t, c, 0x8000); got != 1 {
		t.Errorf("バンク 9 のとき $8000 = %d, 期待 1（8 バンクで折り返す）", got)
	}
}

// TestUxROMCHRRAMIsWritable はマッパー 2 の CHR が RAM であることを
// 確かめる。
func TestUxROMCHRRAMIsWritable(t *testing.T) {
	c, _ := buildMapper(t, 2, 8, 0)
	c.WriteCHR(0x1234, 0x5A)
	if got := c.ReadCHR(0x1234); got != 0x5A {
		t.Errorf("CHR-RAM へ書けていない（$%02X）", got)
	}
}

// TestAxROMSwitches32KiBAndMirroring はマッパー 7 が 32 KiB 単位で
// 切り替え、bit 4 で 1 画面の面を選ぶことを確かめる。
func TestAxROMSwitches32KiBAndMirroring(t *testing.T) {
	c, _ := buildMapper(t, 7, 8, 0)

	// 電源投入時は 1 画面 A。
	if got := c.MapNametable(0x2400); got.Offset >= 0x400 {
		t.Errorf("電源投入時が 1 画面 A でない（offset $%03X）", got.Offset)
	}

	c.WritePRG(0x8000, 0x01)
	// 32 KiB バンク 1 は 16 KiB バンク 2 と 3。
	if got := readPRG(t, c, 0x8000); got != 2 {
		t.Errorf("32 KiB バンク 1 の先頭 = %d, 期待 2", got)
	}
	if got := readPRG(t, c, 0xC000); got != 3 {
		t.Errorf("32 KiB バンク 1 の後半 = %d, 期待 3", got)
	}

	c.WritePRG(0x8000, 0x11)
	if got := c.MapNametable(0x2000); got.Offset < 0x400 {
		t.Errorf("bit 4 を立てても 1 画面 B にならない（offset $%03X）", got.Offset)
	}
	if got := readPRG(t, c, 0x8000); got != 2 {
		t.Errorf("bit 4 が PRG バンクに混ざっている（%d）", got)
	}
}

// TestGxROMSplitsRegister はマッパー 66 が bit 5-4 を PRG、bit 1-0 を
// CHR に割り当てることを確かめる。
func TestGxROMSplitsRegister(t *testing.T) {
	c, _ := buildMapper(t, 66, 8, 4)

	c.WritePRG(0x8000, 0x13)
	// PRG は 32 KiB バンク 1 = 16 KiB バンク 2、CHR は 8 KiB バンク 3。
	if got := readPRG(t, c, 0x8000); got != 2 {
		t.Errorf("PRG の先頭 = %d, 期待 2", got)
	}
	if got := c.ReadCHR(0x0000); got != 0xC3 {
		t.Errorf("CHR の先頭 = $%02X, 期待 $C3", got)
	}
}

// writeMMC1 は MMC1 のシリアルポートへ 5 bit を送る。
func writeMMC1(c Cartridge, addr uint16, v uint8) {
	for i := range 5 {
		c.WritePRG(addr, (v>>uint(i))&1)
	}
}

// TestMMC1SerialWriteTakesFiveWrites は 5 回目の書き込みで反映される
// ことを確かめる。
func TestMMC1SerialWriteTakesFiveWrites(t *testing.T) {
	c, _ := buildMapper(t, 1, 8, 0)
	m := c.(*MMC1)

	// 電源投入時は PRG バンクモード 3。$C000 が末尾に固定される。
	if got := readPRG(t, c, 0xC000); got != 7 {
		t.Errorf("電源投入時の $C000 = %d, 期待 7", got)
	}

	// 4 回書いても反映されない。
	for i := range 4 {
		m.cycles += 2
		c.WritePRG(0xE000, (3>>uint(i))&1)
	}
	if got := readPRG(t, c, 0x8000); got != 0 {
		t.Errorf("4 回目までで反映された（$8000 = %d）", got)
	}
	m.cycles += 2
	c.WritePRG(0xE000, 0)
	if got := readPRG(t, c, 0x8000); got != 3 {
		t.Errorf("5 回目で反映されない（$8000 = %d, 期待 3）", got)
	}
}

// TestMMC1IgnoresConsecutiveCycleWrites は連続する CPU サイクルの
// 2 回目以降を無視することを確かめる。
//
// 読み変更書き戻し命令は同じアドレスへ 2 回書く。無視しないと 1 回の
// 意図で 2 bit が入る。
func TestMMC1IgnoresConsecutiveCycleWrites(t *testing.T) {
	c, _ := buildMapper(t, 1, 8, 0)
	m := c.(*MMC1)

	// 電源投入直後は lastWriteCycle が 0 である。実機ではリセットに
	// 7 サイクルかかるため、そこまで進めた状態から試す。
	m.cycles = 100

	// 読み変更書き戻し命令を模す。1 サイクル空けて 2 回書く組を
	// 5 回繰り返す。組の 2 回目だけが無視される。
	for range 5 {
		m.cycles += 3
		c.WritePRG(0xE000, 1)
		m.cycles++
		c.WritePRG(0xE000, 1)
	}
	// 無視していれば 5 bit しか入らず、$1F が prgBank に入る。
	if m.prgBank != 0x1F {
		t.Errorf("prgBank = $%02X, 期待 $1F（連続サイクルを無視していない）", m.prgBank)
	}
}

// TestMMC1ResetSetsPRGMode3 は bit 7 の書き込みで PRG モードが 3 に
// 戻ることを確かめる。
func TestMMC1ResetSetsPRGMode3(t *testing.T) {
	c, _ := buildMapper(t, 1, 8, 0)
	m := c.(*MMC1)

	// PRG モード 0（32 KiB 切り替え）にする。
	m.cycles += 2
	writeMMC1WithCycles(m, 0x8000, 0x00)
	if m.prgMode() != 0 {
		t.Fatalf("PRG モード = %d, 期待 0", m.prgMode())
	}

	m.cycles += 2
	c.WritePRG(0x8000, 0x80)
	if m.prgMode() != 3 {
		t.Errorf("リセット後の PRG モード = %d, 期待 3", m.prgMode())
	}
	if m.shiftReg != 0x10 {
		t.Errorf("shiftReg = $%02X, 期待 $10", m.shiftReg)
	}
}

// writeMMC1WithCycles はサイクルを進めながらシリアルポートへ送る。
func writeMMC1WithCycles(m *MMC1, addr uint16, v uint8) {
	for i := range 5 {
		m.cycles += 2
		m.WritePRG(addr, (v>>uint(i))&1)
	}
}

// TestMMC1Mirroring は control の下位 2 bit がネームテーブル配置を
// 決めることを確かめる。
func TestMMC1Mirroring(t *testing.T) {
	c, _ := buildMapper(t, 1, 8, 0)
	m := c.(*MMC1)

	for _, tt := range []struct {
		value uint8
		want  Mirroring
	}{
		{0x00, MirrorSingleA},
		{0x01, MirrorSingleB},
		{0x02, MirrorVertical},
		{0x03, MirrorHorizontal},
	} {
		writeMMC1WithCycles(m, 0x8000, tt.value)
		if m.mirroring != tt.want {
			t.Errorf("control=$%02X のミラーリング = %v, 期待 %v", tt.value, m.mirroring, tt.want)
		}
	}
}

// TestMMC1CHR4KiBMode は CHR の 4 KiB × 2 モードを確かめる。
func TestMMC1CHR4KiBMode(t *testing.T) {
	// CHR-ROM 32 KiB（4 KiB × 8）
	c, _ := buildMapper(t, 1, 2, 4)
	m := c.(*MMC1)
	// 4 KiB のバンクごとに目印を置く。
	for i := range 8 {
		m.chr.data[i*chrWindow4K] = uint8(0xA0 + i)
	}

	// CHR モードを 4 KiB × 2 にする（control bit 4）。
	writeMMC1WithCycles(m, 0x8000, 0x1C)
	writeMMC1WithCycles(m, 0xA000, 3) // chrBank0 = 3
	writeMMC1WithCycles(m, 0xC000, 5) // chrBank1 = 5

	if got := c.ReadCHR(0x0000); got != 0xA3 {
		t.Errorf("$0000 = $%02X, 期待 $A3", got)
	}
	if got := c.ReadCHR(0x1000); got != 0xA5 {
		t.Errorf("$1000 = $%02X, 期待 $A5", got)
	}

	// 8 KiB モードでは chrBank0 の最下位ビットを無視する。
	writeMMC1WithCycles(m, 0x8000, 0x0C)
	writeMMC1WithCycles(m, 0xA000, 3)
	if got := c.ReadCHR(0x0000); got != 0xA2 {
		t.Errorf("8 KiB モードの $0000 = $%02X, 期待 $A2", got)
	}
	if got := c.ReadCHR(0x1000); got != 0xA3 {
		t.Errorf("8 KiB モードの $1000 = $%02X, 期待 $A3", got)
	}
}

// TestMMC1RAMDisable は prgBank の bit 4 で PRG-RAM が消えることを
// 確かめる。
func TestMMC1RAMDisable(t *testing.T) {
	c, _ := buildMapper(t, 1, 8, 0)
	m := c.(*MMC1)

	c.WritePRG(0x6000, 0x42)
	if got := readPRG(t, c, 0x6000); got != 0x42 {
		t.Fatalf("PRG-RAM へ書けていない（$%02X）", got)
	}
	writeMMC1WithCycles(m, 0xE000, 0x10)
	if _, handled := c.ReadPRG(0x6000); handled {
		t.Error("PRG-RAM を無効にしたのに読めてしまう")
	}
	writeMMC1WithCycles(m, 0xE000, 0x00)
	if got := readPRG(t, c, 0x6000); got != 0x42 {
		t.Errorf("戻した後の $6000 = $%02X, 期待 $42", got)
	}
}

// TestMMC3PRGModes は PRG のバンク割り当てを確かめる。
func TestMMC3PRGModes(t *testing.T) {
	// PRG 128 KiB = 8 KiB × 16
	c, _ := buildMapper(t, 4, 8, 0)
	m := c.(*MMC3)
	for i := range 16 {
		m.prg.data[i*prgWindow8K] = uint8(0x40 + i)
	}

	// R6 = 2, R7 = 5
	c.WritePRG(0x8000, 6)
	c.WritePRG(0x8001, 2)
	c.WritePRG(0x8000, 7)
	c.WritePRG(0x8001, 5)

	for _, tt := range []struct {
		addr uint16
		want uint8
	}{
		{0x8000, 0x42}, // R6
		{0xA000, 0x45}, // R7
		{0xC000, 0x4E}, // 末尾から 2 番目
		{0xE000, 0x4F}, // 末尾
	} {
		if got := readPRG(t, c, tt.addr); got != tt.want {
			t.Errorf("モード 0 の $%04X = $%02X, 期待 $%02X", tt.addr, got, tt.want)
		}
	}

	// モード 1 では $8000 と $C000 が入れ替わる。
	c.WritePRG(0x8000, 0x40|7)
	c.WritePRG(0x8001, 5)
	for _, tt := range []struct {
		addr uint16
		want uint8
	}{
		{0x8000, 0x4E},
		{0xA000, 0x45},
		{0xC000, 0x42},
		{0xE000, 0x4F},
	} {
		if got := readPRG(t, c, tt.addr); got != tt.want {
			t.Errorf("モード 1 の $%04X = $%02X, 期待 $%02X", tt.addr, got, tt.want)
		}
	}
}

// TestMMC3CHRInversion は CHR の割り当てと A12 反転を確かめる。
func TestMMC3CHRInversion(t *testing.T) {
	// CHR 64 KiB = 1 KiB × 64
	c, _ := buildMapper(t, 4, 8, 8)
	m := c.(*MMC3)
	for i := range 64 {
		m.chr.data[i*chrWindow1K] = uint8(i)
	}

	// R0 = 2（2 KiB なので最下位ビットは無視される）、R1 = 8、R2-R5 = 16,17,18,19
	for reg, v := range map[uint8]uint8{0: 3, 1: 8, 2: 16, 3: 17, 4: 18, 5: 19} {
		c.WritePRG(0x8000, reg)
		c.WritePRG(0x8001, v)
	}

	for _, tt := range []struct {
		addr uint16
		want uint8
	}{
		{0x0000, 2}, // R0 は最下位ビットを無視して 2
		{0x0400, 3},
		{0x0800, 8}, // R1
		{0x0C00, 9},
		{0x1000, 16}, // R2
		{0x1400, 17}, // R3
		{0x1800, 18}, // R4
		{0x1C00, 19}, // R5
	} {
		if got := c.ReadCHR(tt.addr); got != tt.want {
			t.Errorf("反転なしの $%04X = %d, 期待 %d", tt.addr, got, tt.want)
		}
	}

	// bit 7 を立てると $0000 側と $1000 側が入れ替わる。
	c.WritePRG(0x8000, 0x80)
	for _, tt := range []struct {
		addr uint16
		want uint8
	}{
		{0x0000, 16},
		{0x0400, 17},
		{0x0800, 18},
		{0x0C00, 19},
		{0x1000, 2},
		{0x1400, 3},
		{0x1800, 8},
		{0x1C00, 9},
	} {
		if got := c.ReadCHR(tt.addr); got != tt.want {
			t.Errorf("反転ありの $%04X = %d, 期待 %d", tt.addr, got, tt.want)
		}
	}
}

// TestMMC3RAMProtect は $A001 の保護ビットを確かめる。
func TestMMC3RAMProtect(t *testing.T) {
	c, _ := buildMapper(t, 4, 8, 0)

	c.WritePRG(0x6000, 0x11)
	if got := readPRG(t, c, 0x6000); got != 0x11 {
		t.Fatalf("PRG-RAM へ書けていない（$%02X）", got)
	}

	// bit 6 で書き込み保護。
	c.WritePRG(0xA001, mmc3RAMEnable|mmc3RAMWriteProtect)
	c.WritePRG(0x6000, 0x22)
	if got := readPRG(t, c, 0x6000); got != 0x11 {
		t.Errorf("書き込み保護が効いていない（$%02X）", got)
	}

	// bit 7 を落とすと読み出しがオープンバスになる。
	c.WritePRG(0xA001, 0)
	if _, handled := c.ReadPRG(0x6000); handled {
		t.Error("チップを無効にしたのに読めてしまう")
	}
}

// TestMMC3Mirroring は $A000 の bit 0 が配置を決めることを確かめる。
func TestMMC3Mirroring(t *testing.T) {
	c, _ := buildMapper(t, 4, 8, 0)
	m := c.(*MMC3)

	c.WritePRG(0xA000, 0)
	if m.mirroring != MirrorVertical {
		t.Errorf("bit 0 = 0 で %v になった。期待 vertical", m.mirroring)
	}
	c.WritePRG(0xA000, 1)
	if m.mirroring != MirrorHorizontal {
		t.Errorf("bit 0 = 1 で %v になった。期待 horizontal", m.mirroring)
	}
}

// TestMMC3IRQCountsScanlines は A12 の立ち上がりでカウンタが進み、
// 0 で IRQ が出ることを確かめる。
func TestMMC3IRQCountsScanlines(t *testing.T) {
	c, _ := buildMapper(t, 4, 8, 0)

	c.WritePRG(0xC000, 3) // ラッチ
	c.WritePRG(0xC001, 0) // リロード要求
	c.WritePRG(0xE001, 0) // 許可

	// 立ち上がりを 1 回起こすたびに low を十分に取る。
	for i := range 4 {
		riseA12(c, uint64(i)*100)
		if i < 3 && c.IRQAsserted() {
			t.Fatalf("%d 回目で IRQ が出た", i+1)
		}
	}
	if !c.IRQAsserted() {
		t.Error("4 回目の立ち上がりで IRQ が出ない")
	}

	// $E000 で止めると線が戻る。
	c.WritePRG(0xE000, 0)
	if c.IRQAsserted() {
		t.Error("$E000 で IRQ が戻らない")
	}
}

// riseA12 は A12 を十分な時間 low にしてから high にする。
func riseA12(c Cartridge, dot uint64) {
	c.NotifyPPUAddress(0x0000, dot)
	c.NotifyPPUAddress(0x0000, dot+a12LowDotsRequired)
	c.NotifyPPUAddress(0x1000, dot+a12LowDotsRequired+1)
}

// TestInfoFollowsBankSwitch は Info() が現在のバンク構成を返すことを
// 確かめる。
func TestInfoFollowsBankSwitch(t *testing.T) {
	c, _ := buildMapper(t, 2, 8, 0)

	before := c.Info()
	if before.MapperName != "UxROM" || before.MapperNumber != 2 {
		t.Fatalf("Info の名前が違う（%s / %d）", before.MapperName, before.MapperNumber)
	}
	if before.PRGBanks[0].BankIndex != 0 {
		t.Fatalf("最初の PRG バンク = %d, 期待 0", before.PRGBanks[0].BankIndex)
	}

	c.WritePRG(0x8000, 5)
	after := c.Info()
	if after.PRGBanks[0].BankIndex != 5 {
		t.Errorf("切り替え後の PRG バンク = %d, 期待 5", after.PRGBanks[0].BankIndex)
	}
	if after.PRGBanks[0].Offset != 5*prgWindowSize {
		t.Errorf("切り替え後のオフセット = %d, 期待 %d", after.PRGBanks[0].Offset, 5*prgWindowSize)
	}
}

// buildMMC1Submapper5 はサブマッパー 5（SEROM / SHROM / SH1ROM）の
// カートリッジを作る。
func buildMMC1Submapper5(t *testing.T) Cartridge {
	t.Helper()
	b := newROMBuilder().ines(8, 0)
	b.header[6] = 1 << 4
	// バイト 7 の bit 3-2 を 10 にして NES 2.0 とする。
	b.header[7] = 0x08
	b.header[8] = 5 << 4
	b.header[11] = 7 // CHR-RAM 8 KiB
	for i := range 8 {
		b.prg[i*prgUnit] = uint8(i)
	}
	rom, err := LoadROM(b.build())
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	if rom.Submapper != 5 {
		t.Fatalf("サブマッパー = %d, 期待 5", rom.Submapper)
	}
	c, err := New(rom)
	if err != nil {
		t.Fatalf("カートリッジを作れない: %v", err)
	}
	return c
}

// TestMMC1Submapper5FixesPRG はサブマッパー 5 で $E000 のレジスタが
// 無機能になり、32 KiB がバンクなしで見えることを確かめる。
func TestMMC1Submapper5FixesPRG(t *testing.T) {
	c := buildMMC1Submapper5(t)
	m := c.(*MMC1)

	if got := readPRG(t, c, 0x8000); got != 0 {
		t.Errorf("$8000 のバンク = %d, 期待 0", got)
	}
	if got := readPRG(t, c, 0xC000); got != 1 {
		t.Errorf("$C000 のバンク = %d, 期待 1", got)
	}

	// $E000 へ 5 bit を送っても何も起こらない。
	writeMMC1WithCycles(m, 0xE000, 0x03)
	if m.prgBank != 0 {
		t.Errorf("prgBank = $%02X, 期待 $00（$E000 は無機能）", m.prgBank)
	}
	if got := readPRG(t, c, 0x8000); got != 0 {
		t.Errorf("書き込み後の $8000 のバンク = %d, 期待 0", got)
	}
	if got := readPRG(t, c, 0xC000); got != 1 {
		t.Errorf("書き込み後の $C000 のバンク = %d, 期待 1", got)
	}

	// CHR とミラーリングのレジスタは働く。
	writeMMC1WithCycles(m, 0x8000, 0x02)
	if m.mirroring != MirrorVertical {
		t.Errorf("ミラーリング = %v, 期待 vertical", m.mirroring)
	}
}
