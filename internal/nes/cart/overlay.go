package cart

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"slices"
)

// Patch はオーバーレイの 1 バイト分の変更。
type Patch struct {
	Offset uint32
	Value  uint8
}

// Overlay は PRG-ROM と CHR-ROM への変更を、ROM ファイルを書き換えずに
// 保持する（設計書 06 編 §6.8）。
//
// 値を ROM.PRG と ROM.CHR へ直接書き込んで反映する。マッパーはこの 2 つの
// スライスを共有しているため、読み出しの経路に分岐を入れずに済む。CHR の
// 読み出しは毎秒 100 万回を超え、読み出しのたびに変更の一覧を引くと通常の
// プレイが遅くなる。
//
// 変更の一覧は map ではなくオフセットの昇順のスライスで持つ。internal/nes は
// map をたどらない（設計書 12 編 §12.7）。
type Overlay struct {
	enabled bool
	prg     patchList
	chr     patchList
}

// patchList は 1 つの領域への変更。
type patchList struct {
	name    string
	patches []Patch
	data    []uint8
	// orig は最初の変更の時点で取った元の内容。
	orig []uint8
}

// Overlay は ROM のオーバーレイを返す。無ければ作る。
func (r *ROM) Overlay() *Overlay {
	if r.overlay == nil {
		r.overlay = &Overlay{
			enabled: true,
			prg:     patchList{name: "PRG-ROM", data: r.PRG},
			chr:     patchList{name: "CHR-ROM", data: r.CHR},
		}
	}
	return r.overlay
}

// OverlayHash は ROM のオーバーレイのハッシュを返す。作っていないとき 0。
func (r *ROM) OverlayHash() [8]uint8 { return r.overlay.Hash() }

// Enabled は有効かを返す。
func (o *Overlay) Enabled() bool { return o.enabled }

// Empty は変更が無いかを返す。
func (o *Overlay) Empty() bool { return len(o.prg.patches) == 0 && len(o.chr.patches) == 0 }

// HasCHR は CHR のオーバーレイを持てるか（CHR-ROM か）を返す。
func (o *Overlay) HasCHR() bool { return len(o.chr.data) > 0 }

// SetPRG は PRG-ROM のオフセット offset の値を変える。
func (o *Overlay) SetPRG(offset int, v uint8) error { return o.prg.set(offset, v, o.enabled) }

// SetCHR は CHR-ROM のオフセット offset の値を変える。
func (o *Overlay) SetCHR(offset int, v uint8) error { return o.chr.set(offset, v, o.enabled) }

// set は 1 バイトの変更を記録し、有効なら反映する。
//
// 元の内容と同じ値に戻したときは記録から外す。変更の無いオーバーレイの
// ハッシュを 0 に保つためである。
func (l *patchList) set(offset int, v uint8, enabled bool) error {
	if offset < 0 || offset >= len(l.data) {
		return fmt.Errorf("cart: %s のオフセット $%X は範囲外である", l.name, offset)
	}
	if l.orig == nil {
		l.orig = slices.Clone(l.data)
	}
	off := uint32(offset)
	i, found := slices.BinarySearchFunc(l.patches, off, func(p Patch, t uint32) int {
		return int(p.Offset) - int(t)
	})
	switch {
	case l.orig[offset] == v:
		if found {
			l.patches = slices.Delete(l.patches, i, i+1)
		}
	case found:
		l.patches[i].Value = v
	default:
		l.patches = slices.Insert(l.patches, i, Patch{Offset: off, Value: v})
	}
	if enabled {
		l.data[offset] = v
	}
	return nil
}

// apply は有効・無効に合わせて変更した位置の値を書く。
func (l *patchList) apply(enabled bool) {
	for _, p := range l.patches {
		if enabled {
			l.data[p.Offset] = p.Value
		} else {
			l.data[p.Offset] = l.orig[p.Offset]
		}
	}
}

// clear はすべての変更を捨て、元の内容に戻す。
func (l *patchList) clear() {
	l.apply(false)
	l.patches = nil
}

// SetEnabled は有効・無効を切り替える。
//
// 無効にすると変更した位置へ元の内容を書き戻し、有効にすると変更を書き直す。
func (o *Overlay) SetEnabled(on bool) {
	if o.enabled == on {
		return
	}
	o.enabled = on
	o.prg.apply(on)
	o.chr.apply(on)
}

// Clear はすべての変更を捨て、元の内容に戻す。
func (o *Overlay) Clear() {
	o.prg.clear()
	o.chr.clear()
}

// Patches は変更をオフセットの昇順に返す。
func (o *Overlay) Patches() (prg, chr []Patch) {
	return slices.Clone(o.prg.patches), slices.Clone(o.chr.patches)
}

// Load は保存した変更を読み込む。今ある変更は捨てる。
func (o *Overlay) Load(prg, chr []Patch, enabled bool) error {
	o.Clear()
	o.SetEnabled(true)
	for _, p := range prg {
		if err := o.SetPRG(int(p.Offset), p.Value); err != nil {
			return err
		}
	}
	for _, p := range chr {
		if err := o.SetCHR(int(p.Offset), p.Value); err != nil {
			return err
		}
	}
	o.SetEnabled(enabled)
	return nil
}

// Hash はオーバーレイがエミュレーションへ与える影響のハッシュを返す。
//
// 無効のときと変更が無いときは 0 を返す。セーブステートのヘッダに
// 入れ、ロード時に比べる（設計書 08 編 §8.2.2）。
func (o *Overlay) Hash() [8]uint8 {
	var out [8]uint8
	if o == nil || !o.enabled || o.Empty() {
		return out
	}
	h := fnv.New64a()
	var buf [5]uint8
	for i, l := range [2]*patchList{&o.prg, &o.chr} {
		h.Write([]uint8{uint8(i)})
		for _, p := range l.patches {
			binary.LittleEndian.PutUint32(buf[:4], p.Offset)
			buf[4] = p.Value
			h.Write(buf[:])
		}
	}
	binary.LittleEndian.PutUint64(out[:], h.Sum64())
	return out
}
