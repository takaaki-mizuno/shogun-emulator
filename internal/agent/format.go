package agent

import (
	"fmt"
	"strings"
)

// hex8 は 1 バイトを "$42" の形にする。
func hex8(v uint8) string { return fmt.Sprintf("$%02X", v) }

// hex16 はアドレスを "$C123" の形にする。
func hex16(v uint16) string { return fmt.Sprintf("$%04X", v) }

// hexRows はバイト列を 16 バイトごとの "00 01 …" の行にする。
func hexRows(b []uint8) []string {
	var rows []string
	for i := 0; i < len(b); i += 16 {
		end := min(i+16, len(b))
		var sb strings.Builder
		for j, v := range b[i:end] {
			if j > 0 {
				sb.WriteByte(' ')
			}
			fmt.Fprintf(&sb, "%02X", v)
		}
		rows = append(rows, sb.String())
	}
	return rows
}
