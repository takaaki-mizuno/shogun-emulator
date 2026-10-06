package ui

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"strings"
)

// memLayout はメモリビューアの 1 画面分の並び（設計書 09 編 §9.4.5）。
//
// 表示行ごとに先頭アドレスを持つ。見出し行と区切り線の行は -1 とする。
// タップした位置からバイトを求めるのに使う。
type memLayout struct {
	perRow int
	digits int
	// base は表示するアドレスの起点（PRG-RAM は $6000）。
	base int
	// rows は表示行ごとの先頭オフセット。-1 は見出しか区切り線。
	rows []int
}

// addrDigits は大きさ size の空間のアドレスを書くのに要る 16 進の桁数。
func addrDigits(size, base int) int {
	d := 4
	for last := base + size - 1; last >= 1<<(4*d); d++ {
	}
	return d
}

// prefixLen は行頭の "$XXXX:  " の長さ。
func (l memLayout) prefixLen() int { return 1 + l.digits + 3 }

// byteColumn は行内の i 番目のバイトの 16 進 2 桁の先頭の桁。
//
// 8 バイトごとに空白を 1 つ多く入れる。
func (l memLayout) byteColumn(i int) int { return l.prefixLen() + i*3 + i/8 }

// asciiColumn は文字表示の先頭の桁。
//
// 最後のバイトの 16 進 2 桁と、続く空白 2 つの後ろにある。
func (l memLayout) asciiColumn() int { return l.byteColumn(l.perRow-1) + 4 }

// hit は表示行 row・桁 col にあるバイトのオフセットと上位の桁かを返す。
func (l memLayout) hit(row, col int) (offset int, high, ok bool) {
	if row < 0 || row >= len(l.rows) || l.rows[row] < 0 {
		return 0, false, false
	}
	start := l.rows[row]
	for i := range l.perRow {
		c := l.byteColumn(i)
		if col == c || col == c+1 {
			return start + i, col == c, true
		}
	}
	if a := l.asciiColumn(); col >= a && col < a+l.perRow {
		return start + col - a, true, true
	}
	return 0, false, false
}

// header は見出し行の文字列を作る。
func (l memLayout) header() string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", l.prefixLen()))
	for i := range l.perRow {
		if i > 0 {
			b.WriteByte(' ')
			if i%8 == 0 {
				b.WriteByte(' ')
			}
		}
		fmt.Fprintf(&b, "%02X", i%256)
	}
	return b.String()
}

// line は 1 行分の文字列を作る。
func (l memLayout) line(offset int, data []uint8) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$%0*X:  ", l.digits, l.base+offset)
	for i := range l.perRow {
		if i > 0 {
			b.WriteByte(' ')
			if i%8 == 0 {
				b.WriteByte(' ')
			}
		}
		if i < len(data) {
			fmt.Fprintf(&b, "%02X", data[i])
		} else {
			b.WriteString("  ")
		}
	}
	b.WriteString("  ")
	for i := range l.perRow {
		c := byte(' ')
		if i < len(data) {
			c = '.'
			if data[i] >= 0x20 && data[i] < 0x7F {
				c = data[i]
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// separatorBefore は行 offset の前に区切り線を入れるかを返す。
//
// ゼロページとスタックページの境界（$0100 と $0200）に入れる。内蔵
// RAM と CPU アドレス空間だけが対象である。
func separatorBefore(zeroPageSpace bool, offset int) bool {
	return zeroPageSpace && (offset == 0x100 || offset == 0x200)
}

// parseSearch は検索語をバイト列にする。
//
// asText が false のとき空白区切りまたは連続した 16 進として読む。
func parseSearch(s string, asText bool) ([]uint8, error) {
	if asText {
		if s == "" {
			return nil, errors.New(i18n.T(i18n.MemSearchEmpty))
		}
		return []uint8(s), nil
	}
	t := strings.NewReplacer(" ", "", "$", "", ",", "").Replace(s)
	if t == "" || len(t)%2 != 0 {
		return nil, errors.New(i18n.T(i18n.MemSearchBadHex, s))
	}
	b, err := hex.DecodeString(t)
	if err != nil {
		return nil, errors.New(i18n.T(i18n.MemSearchBadHex, s))
	}
	return b, nil
}

// searchFrom は data の from から後ろで pat を探し、見つからなければ
// 先頭から from の手前までを探す。見つからないとき -1。
func searchFrom(data, pat []uint8, from int) int {
	if from < 0 || from > len(data) {
		from = 0
	}
	if i := bytes.Index(data[from:], pat); i >= 0 {
		return from + i
	}
	end := min(from+len(pat)-1, len(data))
	if i := bytes.Index(data[:end], pat); i >= 0 {
		return i
	}
	return -1
}
