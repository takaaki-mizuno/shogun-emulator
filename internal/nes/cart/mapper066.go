package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// GxROM はマッパー 66。PRG を 32 KiB、CHR を 8 KiB 単位で切り替える。
type GxROM struct {
	common
	prg *banked
	reg uint8
}

// newGxROM はマッパー 66 を作る。
func newGxROM(rom *ROM, o Options) (Cartridge, error) {
	m := &GxROM{}
	m.initCommon(rom, chrUnit, 1)
	m.prg = newBanked(rom.PRG, prgWindow32K, 1)
	return m, nil
}

// ReadPRG は $4020-$FFFF を読む。
func (m *GxROM) ReadPRG(addr uint16) (uint8, bool) {
	switch {
	case addr >= 0x8000:
		return m.prg.read(0, addr&(prgWindow32K-1))
	case addr >= 0x6000:
		return m.readPRGRAM(addr)
	}
	return 0, false
}

// peekPRG は副作用なしに PRG-ROM を読む。
func (m *GxROM) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// WritePRG は $4020-$FFFF へ書く。bit 5-4 が PRG、bit 1-0 が CHR。
func (m *GxROM) WritePRG(addr uint16, v uint8) {
	switch {
	case addr >= 0x8000:
		m.reg = v
		m.prg.setBank(0, int(v>>4)&0x03)
		m.chr.setBank(0, int(v)&0x03)
	case addr >= 0x6000:
		m.writePRGRAM(addr, v)
	}
}

// ReadCHR は $0000-$1FFF を読む。
func (m *GxROM) ReadCHR(addr uint16) uint8 { return m.readCHRWindow(0, addr&0x1FFF) }

// WriteCHR は $0000-$1FFF へ書く。
func (m *GxROM) WriteCHR(addr uint16, v uint8) { m.writeCHRWindow(0, addr&0x1FFF, v) }

// Info は構成を返す。
func (m *GxROM) Info() Info {
	return Info{
		MapperName:   "GxROM",
		MapperNumber: 66,
		Submapper:    m.rom.Submapper,
		PRGBanks:     prgBankViews(m.prg, prgWindow32K, 0x8000),
		CHRBanks:     m.chrBankViews(chrUnit),
		Mirroring:    m.mirroring,
	}
}

// SaveState は状態を書く。
func (m *GxROM) SaveState(w *state.Writer) {
	end := w.Section("mapper066")
	endRegs := w.Section("regs")
	w.U8(m.reg)
	w.Int(m.prg.windows[0])
	w.Int(m.chr.windows[0])
	endRegs()
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *GxROM) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper066")
	endRegs := r.RequireSection("regs")
	m.reg = r.U8()
	m.prg.setBank(0, r.Int())
	m.chr.setBank(0, r.Int())
	endRegs()
	m.loadCommon(r)
	end()
	return r.Err()
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *GxROM) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
