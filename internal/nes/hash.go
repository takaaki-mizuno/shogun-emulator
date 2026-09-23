package nes

import (
	"encoding/binary"
	"hash/fnv"
)

// StateHash はエミュレーション状態のハッシュを返す。
//
// 入力ムービーの desync 検出に使う（設計書 08 編 §8.7.3）。対象は
// メモリと CPU のレジスタと累積サイクル数であり、画面出力を含めない。
// 状態が一致していれば画面も一致するため、状態を比べるほうが原因の
// 切り分けに直接つながる。
//
// FNV-1a を使う。処理系によらず同じ値になり、3 つの OS で比較できる。
func (n *NES) StateHash() [8]uint8 {
	h := fnv.New64a()
	h.Write(n.Bus.RAM())
	h.Write(n.PPU.CIRAM())
	h.Write(n.PPU.OAM())
	h.Write(n.PPU.Palette())
	h.Write(n.Cart.PRGRAM())

	c := n.CPU
	var regs [8]uint8
	regs[0] = c.A
	regs[1] = c.X
	regs[2] = c.Y
	regs[3] = c.S
	regs[4] = uint8(c.PC)
	regs[5] = uint8(c.PC >> 8)
	regs[6] = c.P()
	h.Write(regs[:7])

	var cycles [8]uint8
	binary.LittleEndian.PutUint64(cycles[:], n.Bus.Cycles())
	h.Write(cycles[:])

	var out [8]uint8
	binary.LittleEndian.PutUint64(out[:], h.Sum64())
	return out
}
