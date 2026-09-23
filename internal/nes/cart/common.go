package cart

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// common はどのマッパーにも共通する部分。
//
// PRG-RAM・CHR・ネームテーブル・不揮発メモリの扱いは大半のマッパーで同じで
// ある。各マッパーが個別に書くと、同じ誤りが複数の場所に入る。
type common struct {
	rom *ROM

	// prgRAM は $6000-$7FFF に現れる RAM。無いとき長さ 0。
	prgRAM []uint8
	// prgRAMBattery は prgRAM が不揮発かを表す。
	prgRAMBattery bool

	// chr は CHR-ROM または CHR-RAM。
	chr *banked
	// chrWritable は CHR が RAM かを表す。
	chrWritable bool

	mirroring Mirroring
}

// initCommon は共通部分を初期化する。chrWindows は CHR のウィンドウ数。
func (c *common) initCommon(rom *ROM, chrBankSize, chrWindows int) {
	c.rom = rom
	c.mirroring = rom.Mirroring

	size := rom.PRGRAMSize + rom.PRGNVRAMSize
	if size > 0 {
		c.prgRAM = make([]uint8, size)
		c.prgRAMBattery = rom.PRGNVRAMSize > 0
	}

	switch {
	case len(rom.CHR) > 0:
		c.chr = newBanked(rom.CHR, chrBankSize, chrWindows)
		c.chrWritable = false
	default:
		n := rom.CHRRAMSize + rom.CHRNVRAMSize
		if n == 0 {
			n = chrUnit
		}
		c.chr = newBanked(make([]uint8, n), chrBankSize, chrWindows)
		c.chrWritable = true
	}
}

// readPRGRAM は $6000-$7FFF を読む。
//
// PRG-RAM が 8 KiB 未満のとき、その大きさでミラーして埋める。オープンバスを
// 返さないのは、実機でアドレスデコードが上位ビットを見ないためである。
func (c *common) readPRGRAM(addr uint16) (uint8, bool) {
	if len(c.prgRAM) == 0 {
		return 0, false
	}
	return c.prgRAM[int(addr-0x6000)%len(c.prgRAM)], true
}

// writePRGRAM は $6000-$7FFF へ書く。
func (c *common) writePRGRAM(addr uint16, v uint8) {
	if len(c.prgRAM) == 0 {
		return
	}
	c.prgRAM[int(addr-0x6000)%len(c.prgRAM)] = v
}

// readCHRWindow は CHR のウィンドウから読む。
func (c *common) readCHRWindow(window int, offset uint16) uint8 {
	v, ok := c.chr.read(window, offset)
	if !ok {
		return 0
	}
	return v
}

// writeCHRWindow は CHR のウィンドウへ書く。CHR-ROM への書き込みは無視する。
func (c *common) writeCHRWindow(window int, offset uint16, v uint8) {
	if !c.chrWritable {
		return
	}
	c.chr.write(window, offset, v)
}

// MapNametable は $2000-$2FFF のアクセス先を返す。
func (c *common) MapNametable(addr uint16) NametableTarget {
	return MapNametableWith(c.mirroring, addr)
}

// NotifyPPUAddress は既定では何もしない。
func (c *common) NotifyPPUAddress(addr uint16, dot uint64) {}

// Tick は既定では何もしない。
func (c *common) Tick(cycles int) {}

// IRQAsserted は既定では false を返す。
func (c *common) IRQAsserted() bool { return false }

// PRGRAM は $6000-$7FFF に現れる RAM の全体を返す。
func (c *common) PRGRAM() []uint8 { return c.prgRAM }

// BatteryRAM は不揮発メモリの内容を返す。
func (c *common) BatteryRAM() []uint8 {
	if !c.prgRAMBattery {
		return nil
	}
	return c.prgRAM
}

// SetBatteryRAM は不揮発メモリの内容を設定する。
//
// 長さが異なるときエラーを返す。別のゲームの `.sav` を読み込んで内容を
// 壊さないためである。
func (c *common) SetBatteryRAM(data []uint8) error {
	if !c.prgRAMBattery {
		return fmt.Errorf("cart: このカートリッジは不揮発メモリを持たない")
	}
	if len(data) != len(c.prgRAM) {
		return fmt.Errorf("cart: 不揮発メモリの大きさが合わない（ファイル %d バイト、カートリッジ %d バイト）",
			len(data), len(c.prgRAM))
	}
	copy(c.prgRAM, data)
	return nil
}

// saveCommon は共通部分の状態を書く。
//
// マッパー番号とサブマッパーもここに書く。どのマッパーでも復元時の照合に
// 必要であり、各マッパーが個別に書くと漏れが出る。
func (c *common) saveCommon(w *state.Writer) {
	end := w.Section("common")
	w.U16(c.rom.Mapper)
	w.U8(c.rom.Submapper)
	w.U8(uint8(c.mirroring))
	w.Bytes(c.prgRAM)
	if c.chrWritable {
		w.Bytes(c.chr.data)
	} else {
		w.Bytes(nil)
	}
	w.Int(len(c.chr.windows))
	for _, b := range c.chr.windows {
		w.Int(b)
	}
	end()
}

// loadCommon は共通部分の状態を読む。
//
// マッパー番号が一致しないときエラーを記録する。別のマッパーのステートを
// 読み込んでメモリの内容を壊さないためである。
func (c *common) loadCommon(r *state.Reader) {
	end := r.RequireSection("common")
	mapper := r.U16()
	submapper := r.U8()
	if mapper != c.rom.Mapper || submapper != c.rom.Submapper {
		r.Fail(fmt.Errorf("cart: ステートのマッパーが違う（ファイル %d.%d、ROM %d.%d）",
			mapper, submapper, c.rom.Mapper, c.rom.Submapper))
		end()
		return
	}
	c.mirroring = Mirroring(r.U8())
	copyInto(c.prgRAM, r.Bytes())
	chrData := r.Bytes()
	if c.chrWritable {
		copyInto(c.chr.data, chrData)
	}
	n := r.Int()
	for i := range n {
		b := r.Int()
		if i < len(c.chr.windows) {
			c.chr.windows[i] = b
		}
	}
	end()
}

// copyInto は長さが一致するときだけ複製する。
//
// 長さが違うステートを読んだときに部分的に上書きしない。マッパーの構成が
// 変わっているため、中途半端に復元するより何もしない方が原因が分かる。
func copyInto(dst, src []uint8) {
	if len(dst) != len(src) {
		return
	}
	copy(dst, src)
}

// chrBankViews は CHR のウィンドウ構成をデバッガ用に返す。
func (c *common) chrBankViews(windowSize int) []BankView {
	kind := "CHR-ROM"
	if c.chrWritable {
		kind = "CHR-RAM"
	}
	out := make([]BankView, 0, len(c.chr.windows))
	for i := range c.chr.windows {
		out = append(out, BankView{
			CPUOrPPUAddr: uint16(i * windowSize),
			Size:         windowSize,
			SourceKind:   kind,
			BankIndex:    c.chr.normalizedBank(i),
			Offset:       c.chr.bankOffset(i),
		})
	}
	return out
}

// prgBankViews は PRG のバンク構成をデバッガ向けに並べる。
func prgBankViews(b *banked, windowSize int, base uint16) []BankView {
	out := make([]BankView, 0, len(b.windows))
	for i := range b.windows {
		out = append(out, BankView{
			CPUOrPPUAddr: base + uint16(i*windowSize),
			Size:         windowSize,
			SourceKind:   "PRG-ROM",
			BankIndex:    b.normalizedBank(i),
			Offset:       b.bankOffset(i),
		})
	}
	return out
}

// chrWindowOf は PPU アドレスからウィンドウ番号とウィンドウ内オフセットを返す。
func chrWindowOf(addr uint16, windowSize int) (window int, offset uint16) {
	a := int(addr & 0x1FFF)
	return a / windowSize, uint16(a % windowSize)
}
