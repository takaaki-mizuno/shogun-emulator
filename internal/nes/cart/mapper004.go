package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// MMC3 のウィンドウの大きさ。
const (
	prgWindow8K = 8 * 1024
	chrWindow1K = 1 * 1024
)

// 設定 emulation.mmc3IrqVariant が受け付ける値。
const (
	// MMC3IRQSharp はカウンタが 0 に等しいときに IRQ を出す版。
	MMC3IRQSharp = "sharp"
	// MMC3IRQNEC はカウンタが 0 にデクリメントされたときに IRQ を出す版。
	MMC3IRQNEC = "nec"
)

// MMC3 はマッパー 4。
//
// PRG を 8 KiB × 4、CHR を 1 KiB × 8 で構成し、PPU アドレスバスの A12 を
// 監視してスキャンライン IRQ を出す。
type MMC3 struct {
	common
	prg *banked

	// bankSelect は $8000 偶数に書かれた値。
	bankSelect uint8
	// banks は R0-R7。
	banks [8]uint8

	// ramProtect は $A001 に書かれた値。
	ramProtect uint8

	// irqLatch は $C000 に書かれた値。リロード時にカウンタへ入る。
	irqLatch uint8
	// irqCounter は現在のカウンタ。
	irqCounter uint8
	// irqReload はリロードが要求されていることを表す。
	irqReload bool
	// irqEnabled は IRQ の生成が許可されていることを表す。
	irqEnabled bool
	// irqAsserted は IRQ 線をアサートしていることを表す。
	irqAsserted bool
	// necVariant はカウンタが 0 にデクリメントされたときだけ IRQ を出す版か。
	necVariant bool

	a12 a12Filter
	// lastNotifyDot は最後に A12 の通知を受けたドット。
	lastNotifyDot uint64

	// fourScreen は 4 画面構成のカートリッジかを表す。
	fourScreen bool

	// a12Rises は A12 の立ち上がりを数えた回数。
	a12Rises uint64
}

// bankSelect のビット。
const (
	// mmc3TargetMask は更新対象のレジスタ番号。
	mmc3TargetMask = 0x07
	// mmc3PRGModeBit は PRG のバンク配置を入れ替える。
	mmc3PRGModeBit = 0x40
	// mmc3CHRInvertBit は CHR のバンク配置を A12 で入れ替える。
	mmc3CHRInvertBit = 0x80
)

// ramProtect のビット。
const (
	// mmc3RAMWriteProtect は 1 のとき PRG-RAM への書き込みを無視する。
	mmc3RAMWriteProtect = 0x40
	// mmc3RAMEnable は 1 のとき PRG-RAM を有効にする。
	mmc3RAMEnable = 0x80
)

// newMMC3 はマッパー 4 を作る。
func newMMC3(rom *ROM, o Options) (Cartridge, error) {
	m := &MMC3{}
	m.initCommon(rom, chrWindow1K, 8)
	m.prg = newBanked(rom.PRG, prgWindow8K, 4)
	m.necVariant = o.MMC3IRQVariant == MMC3IRQNEC
	m.fourScreen = rom.Mirroring == MirrorFourScreen

	// 電源投入時は PRG-RAM を有効かつ書き込み可能とする。$A001 へ書かずに
	// PRG-RAM を使うプログラムがあるためである。
	m.ramProtect = mmc3RAMEnable
	m.applyBanks()
	return m, nil
}

// prgModeAlt は PRG の配置を入れ替えるモードかを返す。
func (m *MMC3) prgModeAlt() bool { return m.bankSelect&mmc3PRGModeBit != 0 }

// chrInvert は CHR の配置を入れ替えるモードかを返す。
func (m *MMC3) chrInvert() bool { return m.bankSelect&mmc3CHRInvertBit != 0 }

// applyBanks はレジスタの内容をウィンドウへ反映する。
func (m *MMC3) applyBanks() {
	// R6 と R7 は上位 2 bit を無視する。
	r6 := int(m.banks[6] & 0x3F)
	r7 := int(m.banks[7] & 0x3F)
	// 負のバンク番号は末尾からの指定になる。
	if m.prgModeAlt() {
		m.prg.setBank(0, -2)
		m.prg.setBank(1, r7)
		m.prg.setBank(2, r6)
	} else {
		m.prg.setBank(0, r6)
		m.prg.setBank(1, r7)
		m.prg.setBank(2, -2)
	}
	m.prg.setBank(3, -1)

	// R0 と R1 は 2 KiB のバンクであり、最下位ビットを無視する。
	r0 := int(m.banks[0] &^ 1)
	r1 := int(m.banks[1] &^ 1)
	small := [4]int{int(m.banks[2]), int(m.banks[3]), int(m.banks[4]), int(m.banks[5])}
	if m.chrInvert() {
		for i, b := range small {
			m.chr.setBank(i, b)
		}
		m.chr.setBank(4, r0)
		m.chr.setBank(5, r0+1)
		m.chr.setBank(6, r1)
		m.chr.setBank(7, r1+1)
		return
	}
	m.chr.setBank(0, r0)
	m.chr.setBank(1, r0+1)
	m.chr.setBank(2, r1)
	m.chr.setBank(3, r1+1)
	for i, b := range small {
		m.chr.setBank(4+i, b)
	}
}

// ramEnabled は PRG-RAM が有効かを返す。
func (m *MMC3) ramEnabled() bool { return m.ramProtect&mmc3RAMEnable != 0 }

// ramWritable は PRG-RAM へ書き込めるかを返す。
func (m *MMC3) ramWritable() bool { return m.ramProtect&mmc3RAMWriteProtect == 0 }

// ReadPRG は $4020-$FFFF を読む。
func (m *MMC3) ReadPRG(addr uint16) (uint8, bool) {
	switch {
	case addr >= 0x8000:
		window := int(addr-0x8000) / prgWindow8K
		return m.prg.read(window, addr&(prgWindow8K-1))
	case addr >= 0x6000:
		if !m.ramEnabled() {
			return 0, false
		}
		return m.readPRGRAM(addr)
	}
	return 0, false
}

// peekPRG は副作用なしに PRG を読む。
func (m *MMC3) peekPRG(addr uint16) uint8 {
	v, _ := m.ReadPRG(addr)
	return v
}

// WritePRG は $4020-$FFFF へ書く。
//
// $8000 以上は 4 対のレジスタになる。偶数アドレスが下位、奇数アドレスが
// 上位である。
func (m *MMC3) WritePRG(addr uint16, v uint8) {
	if addr < 0x6000 {
		return
	}
	if addr < 0x8000 {
		if m.ramEnabled() && m.ramWritable() {
			m.writePRGRAM(addr, v)
		}
		return
	}

	odd := addr&1 != 0
	switch {
	case addr < 0xA000:
		if odd {
			m.banks[m.bankSelect&mmc3TargetMask] = v
		} else {
			m.bankSelect = v
		}
		m.applyBanks()
	case addr < 0xC000:
		if odd {
			m.ramProtect = v
			return
		}
		// 4 画面構成ではネームテーブルの配置をマッパーが決めない。
		if m.fourScreen {
			return
		}
		// bit 0 が 0 のとき CIRAM A10 に PPU A10 を与える配置になる。
		// 極性を逆にすると背景が入れ替わり、Holy Mapperel の基板判別が
		// 別のマッパーと結論する。
		if v&1 != 0 {
			m.mirroring = MirrorHorizontal
			return
		}
		m.mirroring = MirrorVertical
	case addr < 0xE000:
		if odd {
			// リロード要求。カウンタは 0 になる。
			m.irqCounter = 0
			m.irqReload = true
			return
		}
		// ラッチはカウンタの現在値を変えない。
		m.irqLatch = v
	default:
		if odd {
			m.irqEnabled = true
			return
		}
		m.irqEnabled = false
		m.irqAsserted = false
	}
}

// ReadCHR は $0000-$1FFF を読む。
func (m *MMC3) ReadCHR(addr uint16) uint8 {
	window, offset := chrWindowOf(addr, chrWindow1K)
	return m.readCHRWindow(window, offset)
}

// WriteCHR は $0000-$1FFF へ書く。
func (m *MMC3) WriteCHR(addr uint16, v uint8) {
	window, offset := chrWindowOf(addr, chrWindow1K)
	m.writeCHRWindow(window, offset, v)
}

// NotifyPPUAddress は A12 の立ち上がりを数え、カウンタを進める。
func (m *MMC3) NotifyPPUAddress(addr uint16, dot uint64) {
	elapsed := int(dot - m.lastNotifyDot)
	m.lastNotifyDot = dot
	if !m.a12.notify(addr, elapsed) {
		return
	}
	m.a12Rises++

	prev := m.irqCounter
	requested := m.irqReload
	if m.irqCounter == 0 || m.irqReload {
		m.irqCounter = m.irqLatch
		m.irqReload = false
	} else {
		m.irqCounter--
	}
	if m.irqCounter != 0 || !m.irqEnabled {
		return
	}
	// NEC 版では、カウンタが自然に 0 に達したあとのリロードで IRQ を
	// 出さない。$C001 への書き込みによるリロードでは、カウンタが
	// すでに 0 であっても出す。
	if m.necVariant && prev == 0 && !requested {
		return
	}
	m.irqAsserted = true
}

// A12Rises は A12 の立ち上がりを数えた回数を返す。
//
// 走査線あたりの回数が実機と一致していることを確かめるために使う。
// 回数が増えるとスキャンライン IRQ が早まり、画面の分割位置がずれる。
func (m *MMC3) A12Rises() uint64 { return m.a12Rises }

// IRQAsserted は IRQ 線をアサートしているかを返す。
func (m *MMC3) IRQAsserted() bool { return m.irqAsserted }

// Info は構成を返す。
func (m *MMC3) Info() Info {
	return Info{
		MapperName:   "MMC3",
		MapperNumber: 4,
		Submapper:    m.rom.Submapper,
		PRGBanks:     prgBankViews(m.prg, prgWindow8K, 0x8000),
		CHRBanks:     m.chrBankViews(chrWindow1K),
		Mirroring:    m.mirroring,
	}
}

// SaveState は状態を書く。
func (m *MMC3) SaveState(w *state.Writer) {
	end := w.Section("mapper004")
	endRegs := w.Section("regs")
	w.U8(m.bankSelect)
	for _, b := range m.banks {
		w.U8(b)
	}
	w.U8(m.ramProtect)
	w.U8(m.irqLatch)
	w.U8(m.irqCounter)
	w.Bool(m.irqReload)
	w.Bool(m.irqEnabled)
	w.Bool(m.irqAsserted)
	m.a12.saveState(w)
	w.U64(m.lastNotifyDot)
	endRegs()
	m.saveCommon(w)
	end()
}

// LoadState は状態を読む。
func (m *MMC3) LoadState(r *state.Reader) error {
	end := r.RequireSection("mapper004")
	endRegs := r.RequireSection("regs")
	m.bankSelect = r.U8()
	for i := range m.banks {
		m.banks[i] = r.U8()
	}
	m.ramProtect = r.U8()
	m.irqLatch = r.U8()
	m.irqCounter = r.U8()
	m.irqReload = r.Bool()
	m.irqEnabled = r.Bool()
	m.irqAsserted = r.Bool()
	m.a12.loadState(r)
	m.lastNotifyDot = r.U64()
	endRegs()
	m.loadCommon(r)
	end()
	if err := r.Err(); err != nil {
		return err
	}
	m.applyBanks()
	return nil
}

// PRGOffset は CPU アドレスに対応する PRG-ROM のオフセットを返す。
func (m *MMC3) PRGOffset(addr uint16) (int, bool) { return m.prg.cpuOffset(addr) }
