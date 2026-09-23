package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// NROM はマッパー 0。バンク切り替えを持たない。
//
// PRG-ROM は 16 KiB または 32 KiB。16 KiB のとき $C000-$FFFF は
// $8000-$BFFF のミラーになる。CHR は 8 KiB 固定。
type NROM struct {
	common
	prg *banked
}

// prgWindowSize は PRG のウィンドウの大きさ。$8000 と $C000 の 2 枚。
const prgWindowSize = 16 * 1024

// newNROM はマッパー 0 を作る。
func newNROM(rom *ROM, o Options) (Cartridge, error) {
	m := &NROM{}
	m.initCommon(rom, chrUnit, 1)
	m.prg = newBanked(rom.PRG, prgWindowSize, 2)
	m.prg.setBank(0, 0)
	// 16 KiB の ROM ではバンク総数が 1 になり、剰余により
	// 後半のウィンドウも同じバンクを指す。
	m.prg.setBank(1, 1)
	return m, nil
}

// ReadPRG は $4020-$FFFF を読む。
func (m *NROM) ReadPRG(addr uint16) (uint8, bool) {
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

// WritePRG は $4020-$FFFF へ書く。PRG-ROM への書き込みは無視する。
func (m *NROM) WritePRG(addr uint16, v uint8) {
	if addr >= 0x6000 && addr < 0x8000 {
		m.writePRGRAM(addr, v)
	}
}

// peekPRG は副作用なしに PRG を読む。バス競合の層が使う。
func (m *NROM) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// ReadCHR は $0000-$1FFF を読む。
func (m *NROM) ReadCHR(addr uint16) uint8 {
	return m.readCHRWindow(0, addr&0x1FFF)
}

// WriteCHR は $0000-$1FFF へ書く。
func (m *NROM) WriteCHR(addr uint16, v uint8) {
	m.writeCHRWindow(0, addr&0x1FFF, v)
}

// Info は構成を返す。
func (m *NROM) Info() Info {
	return Info{
		MapperName:   "NROM",
		MapperNumber: 0,
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
func (m *NROM) SaveState(w *state.Writer) {
	end := w.Section("mapper000")
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *NROM) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper000")
	m.loadCommon(r)
	end()
	return r.Err()
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *NROM) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
