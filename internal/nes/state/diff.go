package state

import (
	"encoding/binary"
	"fmt"
)

// FirstDiff は 2 つのステートを比較し、最初に内容が異なるセクションの位置を
// 人間が読める形で返す。一致していれば空文字列を返す。
//
// 往復テスト（internal/testrom）が失敗したときに、どのコンポーネントの状態が
// 復元されていないかを示すために使う。セクション名の階層をドットで連結した
// 形（例 "nes.ppu.sprites"）で返す。
func FirstDiff(a, b []byte) string {
	if d := diffSections(a, b, ""); d != "" {
		return d
	}
	if len(a) != len(b) {
		return fmt.Sprintf("全体の長さが異なる（%d バイト と %d バイト）", len(a), len(b))
	}
	return ""
}

// section は 1 つのセクションの範囲を表す。
type section struct {
	name string
	body []byte
}

// parseSections はバイト列を先頭からセクションの並びとして読む。
// 壊れている場合は読めたところまでを返し、ok に false を返す。
func parseSections(b []byte) (secs []section, ok bool) {
	pos := 0
	for pos < len(b) {
		if pos+1 > len(b) {
			return secs, false
		}
		nameLen := int(b[pos])
		pos++
		if nameLen == 0 || pos+nameLen+4 > len(b) {
			return secs, false
		}
		name := string(b[pos : pos+nameLen])
		pos += nameLen
		bodyLen := int(binary.LittleEndian.Uint32(b[pos:]))
		pos += 4
		if pos+bodyLen > len(b) {
			return secs, false
		}
		secs = append(secs, section{name: name, body: b[pos : pos+bodyLen]})
		pos += bodyLen
	}
	return secs, true
}

// diffSections は同じ階層のセクションを順に比較する。
func diffSections(a, b []byte, prefix string) string {
	as, aok := parseSections(a)
	bs, bok := parseSections(b)
	if !aok || !bok {
		// セクションとして読めない = 素のバイト列。中身を直接比べる。
		return diffRaw(a, b, prefix)
	}
	if len(as) == 0 && len(bs) == 0 {
		return diffRaw(a, b, prefix)
	}

	for i := range as {
		if i >= len(bs) {
			return prefix + as[i].name + " が片方にしかない"
		}
		if as[i].name != bs[i].name {
			return fmt.Sprintf("%s のセクションの並びが異なる（%q と %q）",
				trimDot(prefix), as[i].name, bs[i].name)
		}
		p := prefix + as[i].name + "."
		if d := diffSections(as[i].body, bs[i].body, p); d != "" {
			return d
		}
	}
	if len(bs) > len(as) {
		return prefix + bs[len(as)].name + " が片方にしかない"
	}
	return ""
}

// diffRaw は素のバイト列を比較し、最初に異なるオフセットを返す。
func diffRaw(a, b []byte, prefix string) string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return fmt.Sprintf("%s オフセット %d で異なる（0x%02X と 0x%02X）",
				trimDot(prefix), i, a[i], b[i])
		}
	}
	if len(a) != len(b) {
		return fmt.Sprintf("%s の長さが異なる（%d バイト と %d バイト）",
			trimDot(prefix), len(a), len(b))
	}
	return ""
}

func trimDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	if s == "" {
		return "（最上位）"
	}
	return s
}

// SectionNames はステートに含まれるセクション名を階層付きで列挙する。
// デバッグ表示と、設計書の「保存する状態」の表との照合に使う。
func SectionNames(b []byte) []string {
	var out []string
	collect(b, "", &out)
	return out
}

func collect(b []byte, prefix string, out *[]string) {
	secs, ok := parseSections(b)
	if !ok {
		return
	}
	for _, s := range secs {
		full := prefix + s.name
		*out = append(*out, full)
		collect(s.body, full+".", out)
	}
}
