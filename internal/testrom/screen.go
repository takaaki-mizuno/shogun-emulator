package testrom

import "strings"

// ScreenPeeker は PPU のアドレス空間を副作用なしに読める機械。
type ScreenPeeker interface {
	PeekVRAM(addr uint16) uint8
}

// 画面の大きさ。ネームテーブルは 32x30 タイルである。
const (
	screenCols = 32
	screenRows = 30
)

// ScreenText はネームテーブルの内容をテキストとして返す。
//
// 結果を `$6000` へ書かず画面に表示するテスト ROM がある。blargg の
// テスト ROM はタイル番号を ASCII コードと同じに並べた CHR を使うため、
// ネームテーブルのバイト列をそのまま文字として読める。
//
// 末尾の空白を除いた行だけを返す。画面の大部分が空白であり、そのまま
// 返すと差分が読みにくいためである。
func ScreenText(m ScreenPeeker) string {
	var sb strings.Builder
	for row := range screenRows {
		line := make([]byte, 0, screenCols)
		for col := range screenCols {
			c := m.PeekVRAM(uint16(0x2000 + row*screenCols + col))
			if c < 0x20 || c > 0x7E {
				c = ' '
			}
			line = append(line, c)
		}
		if s := strings.TrimRight(string(line), " "); s != "" {
			sb.WriteString(strings.TrimSpace(s))
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// compactFontLetters は詰めた字形の文字数。
//
// 字形を $01 から並べる ROM がある。ASCII のまま並べるものより CHR が
// 小さくて済むためである。
const compactFontLetters = 26

// ScreenTextCompactFont は字形を $01-$1A に大文字で並べた ROM の画面を
// テキストとして返す。
//
// Holy Mapperel がこの並べ方を使う。数字と記号は ASCII と同じ位置に
// あるため、$01-$1A だけを読み替える。
func ScreenTextCompactFont(m ScreenPeeker) string {
	var sb strings.Builder
	for row := range screenRows {
		line := make([]byte, 0, screenCols)
		for col := range screenCols {
			c := m.PeekVRAM(uint16(0x2000 + row*screenCols + col))
			switch {
			case c >= 1 && c <= compactFontLetters:
				c = 'A' + c - 1
			case c < 0x20 || c > 0x7E:
				c = ' '
			}
			line = append(line, c)
		}
		if s := strings.TrimRight(string(line), " "); s != "" {
			sb.WriteString(strings.TrimSpace(s))
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
