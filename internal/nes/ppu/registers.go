package ppu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/region"

// Control は $2000 PPUCTRL。
//
// ビットの意味をメソッドで表す。呼び出し側でシフトとマスクを書かない。
type Control uint8

// NametableSelect は v と t の bit 10-11 に入るネームテーブルの選択を返す。
func (c Control) NametableSelect() uint16 { return uint16(c&0x03) << 10 }

// VRAMIncrement は $2007 アクセス後に v へ加える値を返す。
func (c Control) VRAMIncrement() uint16 {
	if c&0x04 != 0 {
		return 32
	}
	return 1
}

// SpritePatternBase は 8x8 スプライトのパターンテーブルの先頭を返す。
func (c Control) SpritePatternBase() uint16 {
	if c&0x08 != 0 {
		return 0x1000
	}
	return 0x0000
}

// BGPatternBase は背景のパターンテーブルの先頭を返す。
func (c Control) BGPatternBase() uint16 {
	if c&0x10 != 0 {
		return 0x1000
	}
	return 0x0000
}

// SpriteHeight はスプライトの高さを返す。8 または 16。
func (c Control) SpriteHeight() int {
	if c&0x20 != 0 {
		return 16
	}
	return 8
}

// NMIEnabled は VBlank で NMI を発生させるかを返す。
func (c Control) NMIEnabled() bool { return c&0x80 != 0 }

// Mask は $2001 PPUMASK。
type Mask uint8

// Greyscale は色を灰色に落とすかを返す。
func (m Mask) Greyscale() bool { return m&0x01 != 0 }

// ShowBGLeft は画面左端 8 ピクセルに背景を出すかを返す。
func (m Mask) ShowBGLeft() bool { return m&0x02 != 0 }

// ShowSpritesLeft は画面左端 8 ピクセルにスプライトを出すかを返す。
func (m Mask) ShowSpritesLeft() bool { return m&0x04 != 0 }

// BGEnabled は背景を描くかを返す。
func (m Mask) BGEnabled() bool { return m&0x08 != 0 }

// SpritesEnabled はスプライトを描くかを返す。
func (m Mask) SpritesEnabled() bool { return m&0x10 != 0 }

// RenderingEnabled は背景かスプライトのいずれかを描くかを返す。
//
// PPU がメモリアクセスとスクロールレジスタの自動更新を行うかは、この値で
// 決まる。
func (m Mask) RenderingEnabled() bool { return m&0x18 != 0 }

// Emphasis は色強調の 3 bit を返す。bit 0 が赤、1 が緑、2 が青。
//
// PPUMASK 上のビット位置はリージョンによって異なる。PAL の PPU では赤と緑が
// 入れ替わっているため、位置を region から取る。
func (m Mask) Emphasis(r *region.Region) uint8 {
	var e uint8
	for i, shift := range r.EmphasisBitShift {
		if m&(1<<shift) != 0 {
			e |= 1 << i
		}
	}
	return e
}

// Status は $2002 PPUSTATUS の上位 3 bit。
type Status uint8

// ステータスのビット。
const (
	// StatusSpriteOverflow は 1 行に 9 個以上のスプライトがあったことを表す。
	StatusSpriteOverflow Status = 0x20
	// StatusSprite0Hit はスプライト 0 と背景が重なったことを表す。
	StatusSprite0Hit Status = 0x40
	// StatusVBlank は VBlank 期間に入ったことを表す。
	StatusVBlank Status = 0x80
)

// VBlank は VBlank フラグを返す。
func (s Status) VBlank() bool { return s&StatusVBlank != 0 }

// Sprite0Hit はスプライト 0 ヒットのフラグを返す。
func (s Status) Sprite0Hit() bool { return s&StatusSprite0Hit != 0 }

// SpriteOverflow はスプライトオーバーフローのフラグを返す。
func (s Status) SpriteOverflow() bool { return s&StatusSpriteOverflow != 0 }
