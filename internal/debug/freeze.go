package debug

import "fmt"

// Freeze は値を固定し続ける位置（設計書 14 編 §14.13.2）。
//
// デバッグのための機能であり、チート（Game Genie などのコード）としては
// 扱わない。
type Freeze struct {
	// Addr は CPU アドレス。内蔵 RAM はミラーを畳んだ $0000–$07FF で持つ。
	Addr uint16
	// Value は固定する値。1–4 バイト。Value[i] が Addr+i の値である。
	Value []uint8
}

// MaxFreezes は Instance あたりの Freeze の上限。
const MaxFreezes = 64

// NormalizeFreezeAddr は Freeze の対象の位置かを確かめ、内蔵 RAM のミラーを
// 畳んだアドレスを返す。対象は内蔵 RAM と PRG-RAM（$6000–$7FFF）である。
func NormalizeFreezeAddr(addr uint16, size int) (uint16, error) {
	if size < 1 || size > 4 {
		return 0, fmt.Errorf("size は 1 から 4 バイトとする（%d）", size)
	}
	end := int(addr) + size - 1
	switch {
	case end < 0x2000:
		a := addr & 0x07FF
		if int(a)+size-1 > 0x07FF {
			return 0, fmt.Errorf("$%04X から %d バイトは内蔵 RAM の終わりをまたぐ", addr, size)
		}
		return a, nil
	case addr >= 0x6000 && end <= 0x7FFF:
		return addr, nil
	}
	return 0, fmt.Errorf("$%04X は Freeze できない（内蔵 RAM $0000–$07FF と PRG-RAM $6000–$7FFF だけ）", addr)
}

// SetFreezes は Freeze の一覧を差し替え、各位置へ値を書く。エミュレーション
// ゴルーチンで呼ぶ。
//
// 以降、Freeze した位置への CPU の書き込みの直後に値を書き戻す。書き込みの
// フックはバスへ書いた後に呼ばれるため、プログラムが次に読むときには
// 固定した値に戻っている。RMW 命令は読んだ値を CPU の中で持つため、
// 命令の中の計算には影響しない。
func (d *Debugger) SetFreezes(list []Freeze) {
	d.freezes = append(d.freezes[:0:0], list...)
	if d.n != nil {
		for _, f := range d.freezes {
			for i, v := range f.Value {
				d.n.Bus.Poke(f.Addr+uint16(i), v)
			}
		}
	}
	d.updateHooks()
}

// Freezes は Freeze の一覧を返す。
func (d *Debugger) Freezes() []Freeze { return append([]Freeze(nil), d.freezes...) }

// applyFreeze は Freeze した位置への書き込みの直後に値を書き戻す。
func (d *Debugger) applyFreeze(addr uint16) {
	a := addr
	if a < 0x2000 {
		a &= 0x07FF
	}
	for _, f := range d.freezes {
		if a >= f.Addr && int(a) < int(f.Addr)+len(f.Value) {
			d.n.Bus.Poke(addr, f.Value[a-f.Addr])
		}
	}
}
