package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// chrWindow4K は MMC1 の CHR ウィンドウの大きさ。
const chrWindow4K = 4 * 1024

// mmc1PRGBlock は PRG バンク番号の 1 ブロック分（256 KiB）のバンク数。
//
// 512 KiB の構成（SUROM）では、CHR レジスタの bit 4 でこのブロックを選ぶ。
// PRG バンクのレジスタが 4 bit しかないためである。
const mmc1PRGBlock = 16

// mmc1SubmapperFixedPRG は PRG のバンク切り替えを持たないサブマッパー。
//
// SEROM・SHROM・SH1ROM は MMC1 を載せているが PRG のバンク線が
// 繋がっていない。$E000 のレジスタは書けても何も起こらない。
const mmc1SubmapperFixedPRG = 5

// MMC1 はマッパー 1。
//
// $8000-$FFFF への書き込みを 5 回集めて 1 つの内部レジスタへ反映する
// シリアルポートを持つ。どのレジスタへ入るかは 5 回目の書き込みの
// アドレスで決まる。
type MMC1 struct {
	common
	prg *banked

	// shiftReg は集めている途中のビット。初期値 $10。
	//
	// 番兵の 1 を bit 4 に置き、右へずらす。1 が bit 0 に来た書き込みが
	// 5 回目になる。別にカウンタを持たないのは実機と同じ形である。
	shiftReg uint8
	// control は $8000-$9FFF のレジスタ。
	control uint8
	// chrBank0 は $A000-$BFFF のレジスタ。
	chrBank0 uint8
	// chrBank1 は $C000-$DFFF のレジスタ。
	chrBank1 uint8
	// prgBank は $E000-$FFFF のレジスタ。
	prgBank uint8

	// cycles は電源投入からの CPU サイクル数。
	cycles uint64
	// lastWriteCycle は最後に $8000 以上へ書き込まれたサイクル。
	lastWriteCycle uint64

	// fixedPRG はサブマッパー 5 を表す。
	fixedPRG bool
	// ramBankable は PRG-RAM が 8 KiB を超え、バンク切り替えを持つことを表す。
	ramBankable bool
	// prgBlocks は 256 KiB ブロックの数。2 のとき SUROM の構成である。
	// CHR-ROM の基板では 1 とする。
	prgBlocks int
}

// control のビット。
const (
	mmc1MirrorMask   = 0x03
	mmc1PRGModeShift = 2
	mmc1PRGModeMask  = 0x03
	mmc1CHRModeBit   = 0x10
)

// newMMC1 はマッパー 1 を作る。
func newMMC1(rom *ROM, o Options) (Cartridge, error) {
	m := &MMC1{}
	m.initCommon(rom, chrWindow4K, 2)
	m.prg = newBanked(rom.PRG, prgWindowSize, 2)
	m.fixedPRG = rom.Submapper == mmc1SubmapperFixedPRG
	// CHR レジスタの余ったビットを PRG-RAM のバンクと PRG のブロックに
	// 使う基板（SOROM・SUROM・SXROM）は、いずれも CHR-RAM を載せている。
	// CHR-ROM の基板ではそのビットが CHR のバンク番号として意味を持つ。
	// CHR-ROM の基板で転用すると、CHR を切り替えるたびに PRG-RAM の
	// 見える場所が変わり、セーブデータが散らばる。
	if m.chrWritable {
		m.ramBankable = len(m.prgRAM) > mmc1RAMWindow
		m.prgBlocks = max(len(rom.PRG)/(mmc1PRGBlock*prgWindowSize), 1)
	}

	m.shiftReg = 0x10
	// 電源投入時は PRG バンクモード 3 とする。$C000-$FFFF が末尾の
	// バンクに固定され、リセットベクタが読めることが保証される。
	m.control = mmc1PRGModeMask << mmc1PRGModeShift
	m.applyBanks()
	return m, nil
}

// prgMode は PRG バンクモードを返す。
func (m *MMC1) prgMode() uint8 {
	return (m.control >> mmc1PRGModeShift) & mmc1PRGModeMask
}

// chrMode4K は CHR が 4 KiB × 2 のモードかを返す。
func (m *MMC1) chrMode4K() bool { return m.control&mmc1CHRModeBit != 0 }

// prgBlockBase は 512 KiB 構成で選ばれている 256 KiB ブロックの先頭バンクを返す。
//
// ブロックの選択には chrBank0 の bit 4 を使う。4 KiB の CHR モードでは
// 2 つの CHR レジスタが別のブロックを示しうるが、この構成を使うゲームは
// CHR-RAM 8 KiB であり、片方しか意味を持たない。
func (m *MMC1) prgBlockBase() int {
	if m.prgBlocks < 2 {
		return 0
	}
	return int(m.chrBank0>>4&1) * mmc1PRGBlock
}

// applyBanks はレジスタの内容をウィンドウへ反映する。
//
// レジスタが変わるたびに計算し直す。読み出しのたびに計算すると、
// 同じ計算が 1 秒に 100 万回以上走る。
func (m *MMC1) applyBanks() {
	base := m.prgBlockBase()
	bank := int(m.prgBank & 0x0F)
	switch {
	case m.fixedPRG:
		// PRG のバンク線が繋がっていない。32 KiB がそのまま見える。
		m.prg.setBank(0, 0)
		m.prg.setBank(1, 1)
	case m.prgMode() < 2:
		// 32 KiB 切り替え。最下位ビットを無視する。
		m.prg.setBank(0, base+(bank&^1))
		m.prg.setBank(1, base+(bank&^1)+1)
	case m.prgMode() == 2:
		// $8000 をブロックの先頭に固定する。
		m.prg.setBank(0, base)
		m.prg.setBank(1, base+bank)
	default:
		// $C000 をブロックの末尾に固定する。
		m.prg.setBank(0, base+bank)
		m.prg.setBank(1, base+mmc1PRGBlock-1)
	}

	if m.chrMode4K() {
		m.chr.setBank(0, int(m.chrBank0))
		m.chr.setBank(1, int(m.chrBank1))
		return
	}
	// 8 KiB モード。最下位ビットを無視して連続する 2 つを割り当てる。
	m.chr.setBank(0, int(m.chrBank0&^1))
	m.chr.setBank(1, int(m.chrBank0&^1)+1)
}

// mirroringFromControl は control の下位 2 bit から配置を返す。
func mirroringFromControl(control uint8) Mirroring {
	switch control & mmc1MirrorMask {
	case 0:
		return MirrorSingleA
	case 1:
		return MirrorSingleB
	case 2:
		return MirrorVertical
	}
	return MirrorHorizontal
}

// ramEnabled は PRG-RAM が有効かを返す。
//
// prgBank の bit 4 が 1 のとき無効になる。MMC1B 以降の挙動である。
func (m *MMC1) ramEnabled() bool { return m.prgBank&0x10 == 0 }

// mmc1RAMWindow は $6000-$7FFF に現れる PRG-RAM の大きさ。
const mmc1RAMWindow = 8 * 1024

// ramBank は PRG-RAM のバンク番号を返す。
//
// 8 KiB を超える構成（SOROM・SXROM）では chrBank0 の bit 3-2 で選ぶ。
func (m *MMC1) ramBank() int {
	if !m.ramBankable {
		return 0
	}
	return int(m.chrBank0 >> 2 & 0x03)
}

// ReadPRG は $4020-$FFFF を読む。
func (m *MMC1) ReadPRG(addr uint16) (uint8, bool) {
	switch {
	case addr >= 0x8000:
		window := 0
		if addr >= 0xC000 {
			window = 1
		}
		return m.prg.read(window, addr&(prgWindowSize-1))
	case addr >= 0x6000:
		if !m.ramEnabled() {
			return 0, false
		}
		return m.readRAMBank(addr)
	}
	return 0, false
}

// readRAMBank はバンクを考慮して PRG-RAM を読む。
func (m *MMC1) readRAMBank(addr uint16) (uint8, bool) {
	if len(m.prgRAM) == 0 {
		return 0, false
	}
	return m.prgRAM[m.ramIndex(addr)], true
}

// ramIndex は PRG-RAM 内のインデックスを返す。
func (m *MMC1) ramIndex(addr uint16) int {
	i := m.ramBank()*mmc1RAMWindow + int(addr-0x6000)
	return i % len(m.prgRAM)
}

// peekPRG は副作用なしに PRG を読む。
func (m *MMC1) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// WritePRG は $4020-$FFFF へ書く。
//
// $8000 以上への書き込みはシリアルポートへ入る。連続する CPU サイクルの
// 2 回目以降は無視する。読み変更書き戻し命令が同じアドレスへ 2 回書くため、
// 無視しないと 1 回の意図で 2 bit が入る。
func (m *MMC1) WritePRG(addr uint16, v uint8) {
	if addr < 0x8000 {
		if addr >= 0x6000 && m.ramEnabled() && len(m.prgRAM) > 0 {
			m.prgRAM[m.ramIndex(addr)] = v
		}
		return
	}

	consecutive := m.cycles == m.lastWriteCycle+1
	m.lastWriteCycle = m.cycles
	if consecutive {
		return
	}

	if v&0x80 != 0 {
		// リセット。番兵を戻し、PRG バンクモードを 3 にする。
		m.shiftReg = 0x10
		m.control |= mmc1PRGModeMask << mmc1PRGModeShift
		m.applyBanks()
		return
	}

	full := m.shiftReg&1 != 0
	m.shiftReg = (m.shiftReg >> 1) | ((v & 1) << 4)
	if !full {
		return
	}

	value := m.shiftReg & 0x1F
	switch (addr >> 13) & 3 {
	case 0:
		m.control = value
		m.mirroring = mirroringFromControl(value)
	case 1:
		m.chrBank0 = value
	case 2:
		m.chrBank1 = value
	case 3:
		if !m.fixedPRG {
			m.prgBank = value
		}
	}
	m.shiftReg = 0x10
	m.applyBanks()
}

// ReadCHR は $0000-$1FFF を読む。
func (m *MMC1) ReadCHR(addr uint16) uint8 {
	window, offset := chrWindowOf(addr, chrWindow4K)
	return m.readCHRWindow(window, offset)
}

// WriteCHR は $0000-$1FFF へ書く。
func (m *MMC1) WriteCHR(addr uint16, v uint8) {
	window, offset := chrWindowOf(addr, chrWindow4K)
	m.writeCHRWindow(window, offset, v)
}

// Tick は CPU サイクルの経過を数える。
//
// 連続サイクル書き込みの判定に使う。
func (m *MMC1) Tick(cycles int) { m.cycles += uint64(cycles) }

// Info は構成を返す。
func (m *MMC1) Info() Info {
	return Info{
		MapperName:   "MMC1",
		MapperNumber: 1,
		Submapper:    m.rom.Submapper,
		PRGBanks:     prgBankViews(m.prg, prgWindowSize, 0x8000),
		CHRBanks:     m.chrBankViews(chrWindow4K),
		Mirroring:    m.mirroring,
	}
}

// SaveState は状態を書く。
func (m *MMC1) SaveState(w *state.Writer) {
	end := w.Section("mapper001")
	endRegs := w.Section("regs")
	w.U8(m.shiftReg)
	w.U8(m.control)
	w.U8(m.chrBank0)
	w.U8(m.chrBank1)
	w.U8(m.prgBank)
	w.U64(m.cycles)
	w.U64(m.lastWriteCycle)
	endRegs()
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *MMC1) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper001")
	endRegs := r.RequireSection("regs")
	m.shiftReg = r.U8()
	m.control = r.U8()
	m.chrBank0 = r.U8()
	m.chrBank1 = r.U8()
	m.prgBank = r.U8()
	m.cycles = r.U64()
	m.lastWriteCycle = r.U64()
	endRegs()
	m.loadCommon(r)
	end()
	if err := r.Err(); err != nil {
		return err
	}
	// ウィンドウはレジスタから計算し直す。saveCommon が書いた CHR の
	// ウィンドウも同じ値になる。
	m.applyBanks()
	return nil
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *MMC1) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
