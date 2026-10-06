package agent

import (
	"strconv"
	"strings"
)

// buttonNames はボタンの名前とビット（設計書 14 編 §14.8.5）。bit 0 から
// A・B・Select・Start・Up・Down・Left・Right の並び（設計書 07 編）。
var buttonNames = []struct {
	name string
	bit  uint8
}{
	{"a", 0x01}, {"b", 0x02}, {"select", 0x04}, {"start", 0x08},
	{"up", 0x10}, {"down", 0x20}, {"left", 0x40}, {"right", 0x80},
	{"u", 0x10}, {"d", 0x20}, {"l", 0x40}, {"r", 0x80},
}

// ParseInput は入力の書き方（"R+A"、"$81"、""）をボタンのビットにする。
//
// 大文字・小文字を区別しない。上下・左右の同時押しもそのまま通す。
// エージェントは実機で可能な押し方を意図して試すことがあるためである。
func ParseInput(s string) (uint8, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, nil
	}
	if strings.HasPrefix(t, "$") {
		v, err := strconv.ParseUint(t[1:], 16, 8)
		if err != nil {
			return 0, Errorf(KindInvalidParams, "入力 %q のビットマスクを読めない（$00–$FF）", s)
		}
		return uint8(v), nil
	}
	var out uint8
	pos := 0
	for _, part := range strings.Split(t, "+") {
		name := strings.ToLower(strings.TrimSpace(part))
		bit, ok := uint8(0), false
		for _, b := range buttonNames {
			if b.name == name {
				bit, ok = b.bit, true
				break
			}
		}
		if !ok {
			e := Errorf(KindInvalidParams, "入力 %q の %d 文字目のボタン %q を知らない（A・B・Select・Start・Up・Down・Left・Right、U・D・L・R、$81 の形）", s, pos+1, part)
			e.Position = pos
			return 0, e
		}
		out |= bit
		pos += len(part) + 1
	}
	return out, nil
}

// FormatInput はボタンのビットを "R+A" の形にする。
func FormatInput(b uint8) string {
	var parts []string
	for _, n := range []struct {
		name string
		bit  uint8
	}{{"Up", 0x10}, {"Down", 0x20}, {"Left", 0x40}, {"Right", 0x80}, {"A", 0x01}, {"B", 0x02}, {"Select", 0x04}, {"Start", 0x08}} {
		if b&n.bit != 0 {
			parts = append(parts, n.name)
		}
	}
	return strings.Join(parts, "+")
}
