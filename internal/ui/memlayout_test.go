package ui

import (
	"bytes"
	"testing"
)

// TestMemLayoutHit はタップした桁からバイトを求められることを確かめる。
func TestMemLayoutHit(t *testing.T) {
	l := memLayout{perRow: 16, digits: 4, rows: []int{-1, 0x100, -1, 0x200}}
	line := l.line(0x100, make([]uint8, 16))
	if got := line[:8]; got != "$0100:  " {
		t.Errorf("行頭 = %q", got)
	}
	cases := []struct {
		row, col int
		off      int
		high, ok bool
	}{
		{1, l.byteColumn(0), 0x100, true, true},
		{1, l.byteColumn(0) + 1, 0x100, false, true},
		{1, l.byteColumn(9), 0x109, true, true},
		{1, l.asciiColumn() + 3, 0x103, true, true},
		{3, l.byteColumn(15) + 1, 0x20F, false, true},
		{0, l.byteColumn(0), 0, false, false},
		{2, l.byteColumn(0), 0, false, false},
		{1, 0, 0, false, false},
	}
	for _, c := range cases {
		off, high, ok := l.hit(c.row, c.col)
		if ok != c.ok || (ok && (off != c.off || high != c.high)) {
			t.Errorf("hit(%d,%d) = %X,%v,%v, 期待 %X,%v,%v", c.row, c.col, off, high, ok, c.off, c.high, c.ok)
		}
	}
	// 8 バイトごとの空白の位置が見出しと合う。
	if h := l.header(); h[l.byteColumn(8):l.byteColumn(8)+2] != "08" {
		t.Errorf("見出しの 8 バイト目の位置がずれている: %q", h)
	}
}

// TestAddrDigits は空間の大きさに合った桁数を選ぶことを確かめる。
func TestAddrDigits(t *testing.T) {
	cases := []struct{ size, base, want int }{
		{0x800, 0, 4}, {0x10000, 0, 4}, {0x2000, 0x6000, 4}, {0x80000, 0, 5}, {0x400000, 0, 6},
	}
	for _, c := range cases {
		if got := addrDigits(c.size, c.base); got != c.want {
			t.Errorf("addrDigits(%X,%X) = %d, 期待 %d", c.size, c.base, got, c.want)
		}
	}
}

// TestSearch はバイト列と文字列の検索を確かめる。
func TestSearch(t *testing.T) {
	pat, err := parseSearch("A9 00", false)
	if err != nil || !bytes.Equal(pat, []uint8{0xA9, 0x00}) {
		t.Fatalf("parseSearch = %X, %v", pat, err)
	}
	if _, err := parseSearch("A9 0", false); err == nil {
		t.Error("奇数桁を受け付けた")
	}
	data := []uint8{0xA9, 0x00, 0x11, 0xA9, 0x00}
	if got := searchFrom(data, pat, 1); got != 3 {
		t.Errorf("後ろ側の検索 = %d, 期待 3", got)
	}
	if got := searchFrom(data, pat, 4); got != 0 {
		t.Errorf("折り返しの検索 = %d, 期待 0", got)
	}
	text, _ := parseSearch("HI", true)
	if got := searchFrom([]uint8("xxHIxx"), text, 0); got != 2 {
		t.Errorf("文字列の検索 = %d, 期待 2", got)
	}
}
