// Package i18n は画面に出す文言を集める（設計書 10 編 §10.9）。
//
// internal/ui のコードは文言を直接書かず、ID で引く。言語を加えるときに
// 表を 1 つ足せば済むようにするためである。言語は日本語だけを持つ。
package i18n

import "fmt"

// ID は文言の識別子。
type ID string

// catalog は使う言語の表。
var catalog = ja

// T は文言を返す。args があるとき fmt.Sprintf の書式として使う。
// 表に無い ID は ID の名前をそのまま返す。
func T(id ID, args ...any) string {
	s, ok := catalog[id]
	if !ok {
		s = string(id)
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
