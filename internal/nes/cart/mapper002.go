package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// UxROM はマッパー 2。PRG の前半を切り替え、後半を末尾に固定する。
//
// CHR は 8 KiB の RAM である。バンク切り替えを持たない代わりに、
// プログラムがタイルを書き込む。
type UxROM struct {
	common
	prg *banked
}

// newUxROM はマッパー 2 を作る。
func newUxROM(rom *ROM, o Options) (Cartridge, error) {
	m := &UxROM{}
	m.initCommon(rom, chrUnit, 1)
	m.prg = newBanked(rom.PRG, prgWindowSize, 2)
	m.prg.setBank(0, 0)
	// 後半は末尾のバンクに固定する。負のバンク番号は末尾からの指定になる。
	m.prg.setBank(1, -1)
	return m, nil
}

// ReadPRG は $4020-$FFFF を読む。
func (m *UxROM) ReadPRG(addr uint16) (uint8, bool) {
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

// peekPRG は副作用なしに PRG-ROM を読む。バス競合の層が使う。
func (m *UxROM) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// WritePRG は $4020-$FFFF へ書く。
//
// $8000 以降への書き込みの下位 8 bit が前半のバンク番号になる。
func (m *UxROM) WritePRG(addr uint16, v uint8) {
	switch {
	case addr >= 0x8000:
		m.prg.setBank(0, int(v))
	case addr >= 0x6000:
		m.writePRGRAM(addr, v)
	}
}

// ReadCHR は $0000-$1FFF を読む。
func (m *UxROM) ReadCHR(addr uint16) uint8 { return m.readCHRWindow(0, addr&0x1FFF) }

// WriteCHR は $0000-$1FFF へ書く。
func (m *UxROM) WriteCHR(addr uint16, v uint8) { m.writeCHRWindow(0, addr&0x1FFF, v) }

// Info は構成を返す。
func (m *UxROM) Info() Info {
	return Info{
		MapperName:   "UxROM",
		MapperNumber: 2,
		Submapper:    m.rom.Submapper,
		PRGBanks:     prgBankViews(m.prg, prgWindowSize, 0x8000),
		CHRBanks:     m.chrBankViews(chrUnit),
		Mirroring:    m.mirroring,
	}
}

// SaveState は状態を書く。
func (m *UxROM) SaveState(w *state.Writer) {
	end := w.Section("mapper002")
	endRegs := w.Section("regs")
	w.Int(m.prg.windows[0])
	endRegs()
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *UxROM) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper002")
	endRegs := r.RequireSection("regs")
	m.prg.setBank(0, r.Int())
	endRegs()
	m.loadCommon(r)
	end()
	return r.Err()
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *UxROM) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
