package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// CNROM はマッパー 3。PRG は固定で、CHR を 8 KiB 単位で切り替える。
//
// $8000-$FFFF への書き込みで CHR バンクを選ぶ。PRG のバンク切り替えは
// 持たない。
type CNROM struct {
	common
	prg *banked
}

// newCNROM はマッパー 3 を作る。
func newCNROM(rom *ROM, o Options) (Cartridge, error) {
	m := &CNROM{}
	m.initCommon(rom, chrUnit, 1)
	m.prg = newBanked(rom.PRG, prgWindowSize, 2)
	m.prg.setBank(0, 0)
	m.prg.setBank(1, 1)
	return m, nil
}

// ReadPRG は $4020-$FFFF を読む。
func (m *CNROM) ReadPRG(addr uint16) (uint8, bool) {
	switch {
	case addr >= 0x8000:
		window := 0
		if addr >= 0xC000 {
			window = 1
		}
		return m.prg.read(window, addr&(prgWindowSize-1))
	case addr >= 0x6000:
		return m.readPRGRAM(addr)
	}
	return 0, false
}

// WritePRG は $4020-$FFFF へ書く。
//
// $8000 以降への書き込みは CHR バンクの選択になる。バンク番号はバンク総数で
// 剰余を取るため、上位ビットが立っていても範囲に収まる。
func (m *CNROM) WritePRG(addr uint16, v uint8) {
	switch {
	case addr >= 0x8000:
		m.chr.setBank(0, int(v))
	case addr >= 0x6000:
		m.writePRGRAM(addr, v)
	}
}

// peekPRG は副作用なしに PRG を読む。バス競合の層が使う。
func (m *CNROM) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// ReadCHR は $0000-$1FFF を読む。
func (m *CNROM) ReadCHR(addr uint16) uint8 {
	return m.readCHRWindow(0, addr&0x1FFF)
}

// WriteCHR は $0000-$1FFF へ書く。
func (m *CNROM) WriteCHR(addr uint16, v uint8) {
	m.writeCHRWindow(0, addr&0x1FFF, v)
}

// Info は構成を返す。
func (m *CNROM) Info() Info {
	return Info{
		MapperName:   "CNROM",
		MapperNumber: 3,
		Submapper:    m.rom.Submapper,
		PRGBanks: []BankView{
			{CPUOrPPUAddr: 0x8000, Size: prgWindowSize, SourceKind: "PRG-ROM",
				BankIndex: m.prg.normalizedBank(0), Offset: m.prg.bankOffset(0)},
			{CPUOrPPUAddr: 0xC000, Size: prgWindowSize, SourceKind: "PRG-ROM",
				BankIndex: m.prg.normalizedBank(1), Offset: m.prg.bankOffset(1)},
		},
		CHRBanks:  m.chrBankViews(chrUnit),
		Mirroring: m.mirroring,
	}
}

// SaveState は状態を書く。
func (m *CNROM) SaveState(w *state.Writer) {
	end := w.Section("mapper003")
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *CNROM) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper003")
	m.loadCommon(r)
	end()
	return r.Err()
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *CNROM) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
