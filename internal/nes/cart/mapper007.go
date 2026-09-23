package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// prgWindow32K は 32 KiB のバンクウィンドウ。
const prgWindow32K = 32 * 1024

// AxROM はマッパー 7。PRG 全体を 32 KiB 単位で切り替え、ネームテーブルを
// 1 画面で切り替える。
//
// 1 画面ミラーリングは画面全体を一度に書き換える演出に使われる。
type AxROM struct {
	common
	prg *banked
	// reg は最後に書かれた値。
	reg uint8
}

// newAxROM はマッパー 7 を作る。
func newAxROM(rom *ROM, o Options) (Cartridge, error) {
	m := &AxROM{}
	m.initCommon(rom, chrUnit, 1)
	m.prg = newBanked(rom.PRG, prgWindow32K, 1)
	// 電源投入時のミラーリングは 1 画面 A とする。
	m.mirroring = MirrorSingleA
	return m, nil
}

// ReadPRG は $4020-$FFFF を読む。
func (m *AxROM) ReadPRG(addr uint16) (uint8, bool) {
	switch {
	case addr >= 0x8000:
		return m.prg.read(0, addr&(prgWindow32K-1))
	case addr >= 0x6000:
		return m.readPRGRAM(addr)
	}
	return 0, false
}

// peekPRG は副作用なしに PRG-ROM を読む。
func (m *AxROM) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// WritePRG は $4020-$FFFF へ書く。
//
// bit 2-0 が PRG バンク、bit 4 がネームテーブルの選択である。オーバーサイズの
// 構成では bit 3 も PRG バンクに使う。
func (m *AxROM) WritePRG(addr uint16, v uint8) {
	switch {
	case addr >= 0x8000:
		m.reg = v
		m.prg.setBank(0, int(v&0x0F))
		if v&0x10 != 0 {
			m.mirroring = MirrorSingleB
		} else {
			m.mirroring = MirrorSingleA
		}
	case addr >= 0x6000:
		m.writePRGRAM(addr, v)
	}
}

// ReadCHR は $0000-$1FFF を読む。
func (m *AxROM) ReadCHR(addr uint16) uint8 { return m.readCHRWindow(0, addr&0x1FFF) }

// WriteCHR は $0000-$1FFF へ書く。
func (m *AxROM) WriteCHR(addr uint16, v uint8) { m.writeCHRWindow(0, addr&0x1FFF, v) }

// Info は構成を返す。
func (m *AxROM) Info() Info {
	return Info{
		MapperName:   "AxROM",
		MapperNumber: 7,
		Submapper:    m.rom.Submapper,
		PRGBanks:     prgBankViews(m.prg, prgWindow32K, 0x8000),
		CHRBanks:     m.chrBankViews(chrUnit),
		Mirroring:    m.mirroring,
	}
}

// SaveState は状態を書く。
func (m *AxROM) SaveState(w *state.Writer) {
	end := w.Section("mapper007")
	endRegs := w.Section("regs")
	w.U8(m.reg)
	w.Int(m.prg.windows[0])
	endRegs()
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *AxROM) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper007")
	endRegs := r.RequireSection("regs")
	m.reg = r.U8()
	m.prg.setBank(0, r.Int())
	endRegs()
	m.loadCommon(r)
	end()
	return r.Err()
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *AxROM) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
