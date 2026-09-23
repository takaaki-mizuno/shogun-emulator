package debug

import (
	"errors"
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
)

// Space はメモリビューアが表示するアドレス空間（設計書 09 編 §9.4.5）。
type Space uint8

// アドレス空間の一覧。
const (
	// SpaceCPU は CPU アドレス空間 $0000-$FFFF。
	SpaceCPU Space = iota
	// SpaceRAM は内蔵 RAM $0000-$07FF。ミラーを畳む。
	SpaceRAM
	// SpacePPU は PPU アドレス空間 $0000-$3FFF。
	SpacePPU
	// SpaceOAM は OAM $00-$FF。
	SpaceOAM
	// SpacePRGROM は PRG-ROM のファイル上のオフセット。
	SpacePRGROM
	// SpaceCHR は CHR-ROM（または CHR-RAM）のオフセット。
	SpaceCHR
	// SpacePRGRAM は PRG-RAM。$6000 から始まる。
	SpacePRGRAM

	spaceCount
)

// Spaces は空間の一覧を並び順で返す。
func Spaces() []Space {
	out := make([]Space, 0, spaceCount)
	for s := range spaceCount {
		out = append(out, s)
	}
	return out
}

// String は空間の名前を返す。
func (s Space) String() string {
	switch s {
	case SpaceCPU:
		return "CPU アドレス空間"
	case SpaceRAM:
		return "内蔵 RAM"
	case SpacePPU:
		return "PPU アドレス空間"
	case SpaceOAM:
		return "OAM"
	case SpacePRGROM:
		return "PRG-ROM"
	case SpaceCHR:
		return "CHR"
	case SpacePRGRAM:
		return "PRG-RAM"
	}
	return "?"
}

// Base は表示するアドレスの起点を返す。PRG-RAM は $6000 から数える。
func (s Space) Base() int {
	if s == SpacePRGRAM {
		return 0x6000
	}
	return 0
}

// Region は変更追跡の領域を返す。追跡しない空間では false。
//
// CPU アドレス空間は内蔵 RAM と PRG-RAM の範囲だけを追跡する。
func (s Space) Region(addr int) (Region, int, bool) {
	switch s {
	case SpaceRAM:
		return RegionRAM, addr, true
	case SpaceCPU:
		switch {
		case addr < 0x2000:
			return RegionRAM, addr & 0x07FF, true
		case addr >= 0x6000 && addr < 0x8000:
			return RegionPRGRAM, addr - 0x6000, true
		}
	case SpaceOAM:
		return RegionOAM, addr, true
	case SpacePRGRAM:
		return RegionPRGRAM, addr, true
	case SpacePPU:
		switch {
		case addr >= 0x3F00:
			return RegionPalette, addr & 0x1F, true
		case addr >= 0x2000:
			return RegionCIRAM, addr & 0x0FFF, true
		}
	}
	return 0, 0, false
}

// Size は空間の大きさを返す。
func Size(n *nes.NES, s Space) int {
	switch s {
	case SpaceCPU:
		return 0x10000
	case SpaceRAM:
		return 0x800
	case SpacePPU:
		return 0x4000
	case SpaceOAM:
		return 0x100
	case SpacePRGROM:
		return len(n.ROM.PRG)
	case SpaceCHR:
		if len(n.ROM.CHR) > 0 {
			return len(n.ROM.CHR)
		}
		return 0x2000
	case SpacePRGRAM:
		return len(n.Cart.PRGRAM())
	}
	return 0
}

// ReadMemory は空間 s の addr から len(dst) バイトを副作用なしに読む。
//
// Bus.Peek と PPU.PeekVRAM を通す。表示のために $2002 を読んで VBlank
// フラグを消すようなことを起こさない。
func ReadMemory(n *nes.NES, s Space, addr int, dst []uint8) {
	size := Size(n, s)
	for i := range dst {
		a := addr + i
		if a < 0 || a >= size {
			dst[i] = 0
			continue
		}
		dst[i] = readByte(n, s, a)
	}
}

// readByte は 1 バイトを副作用なしに読む。
func readByte(n *nes.NES, s Space, a int) uint8 {
	switch s {
	case SpaceCPU:
		return n.Bus.Peek(uint16(a))
	case SpaceRAM:
		return n.Bus.RAM()[a]
	case SpacePPU:
		return n.PPU.PeekVRAM(uint16(a))
	case SpaceOAM:
		return n.PPU.OAM()[a]
	case SpacePRGROM:
		return n.ROM.PRG[a]
	case SpaceCHR:
		if len(n.ROM.CHR) > 0 {
			return n.ROM.CHR[a]
		}
		return n.PPU.PeekVRAM(uint16(a))
	case SpacePRGRAM:
		return n.Cart.PRGRAM()[a]
	}
	return 0
}

// WriteMemory は空間 s の addr へ書く（設計書 09 編 §9.4.8）。
//
// 既定では副作用を起こさない。withSideEffects が true のときは CPU
// アドレス空間の書き込みに Bus.Write を使う。レジスタへの書き込みを
// 試すための動作である（設計書 09 編 §9.4.5）。
//
// PRG-ROM と CHR-ROM への書き込みはオーバーレイへ行う。ROM ファイルを
// 書き換えず、無効にすれば元の内容に戻せる（設計書 06 編 §6.8）。
func WriteMemory(n *nes.NES, s Space, addr int, v uint8, withSideEffects bool) error {
	if addr < 0 || addr >= Size(n, s) {
		return fmt.Errorf("debug: アドレス $%X は範囲外である", addr)
	}
	switch s {
	case SpaceCPU:
		if withSideEffects {
			n.Bus.Write(uint16(addr), v)
			return nil
		}
		if off, ok := n.Cart.PRGOffset(uint16(addr)); ok {
			return n.ROM.Overlay().SetPRG(off, v)
		}
		n.Bus.Poke(uint16(addr), v)
	case SpaceRAM:
		n.Bus.RAM()[addr] = v
	case SpacePPU:
		switch {
		case addr >= 0x3F00:
			n.PPU.Palette()[paletteSlot(addr)] = v & 0x3F
		case addr >= 0x2000:
			// ネームテーブルはミラーリングを通して CIRAM に写る。
			t := n.Cart.MapNametable(uint16(addr))
			if t.Kind != cart.NametableCIRAM {
				return errors.New("debug: カートリッジ側のネームテーブルは書き換えられない")
			}
			n.PPU.CIRAM()[int(t.Offset)%len(n.PPU.CIRAM())] = v
		default:
			return writeCHR(n, uint16(addr), v)
		}
	case SpaceOAM:
		n.PPU.OAM()[addr] = v
	case SpacePRGROM:
		return n.ROM.Overlay().SetPRG(addr, v)
	case SpaceCHR:
		if len(n.ROM.CHR) > 0 {
			return n.ROM.Overlay().SetCHR(addr, v)
		}
		n.Cart.WriteCHR(uint16(addr), v)
	case SpacePRGRAM:
		n.Cart.PRGRAM()[addr] = v
	default:
		return errors.New("debug: この空間は書き換えられない")
	}
	return nil
}

// writeCHR は PPU の $0000-$1FFF へ書く。
//
// CHR-RAM へは直接書く。CHR-ROM へは現在のバンク構成で CHR-ROM の
// オフセットに直し、オーバーレイへ書く。
func writeCHR(n *nes.NES, addr uint16, v uint8) error {
	if len(n.ROM.CHR) == 0 {
		n.Cart.WriteCHR(addr, v)
		return nil
	}
	off, ok := CHROffset(n.Cart.Info().CHRBanks, addr)
	if !ok {
		return fmt.Errorf("debug: PPU $%04X に対応する CHR-ROM が無い", addr)
	}
	return n.ROM.Overlay().SetCHR(off, v)
}

// CHROffset は PPU アドレスをバンク構成から CHR のオフセットに直す。
func CHROffset(banks []cart.BankView, addr uint16) (int, bool) {
	for _, b := range banks {
		base := int(b.CPUOrPPUAddr)
		if int(addr) >= base && int(addr) < base+b.Size {
			return int(b.Offset) + int(addr) - base, true
		}
	}
	return 0, false
}

// paletteSlot はパレットのアドレスを 32 エントリの添字にする。
//
// $3F10・$3F14・$3F18・$3F1C は背景の $3F00・$3F04・$3F08・$3F0C と
// 同じ記憶域を指す。
func paletteSlot(addr int) int {
	i := addr & 0x1F
	if i&0x13 == 0x10 {
		i &= 0x0F
	}
	return i
}
