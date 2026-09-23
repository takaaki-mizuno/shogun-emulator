// Package state はエミュレーション状態の直列化を担う。
//
// 形式はセクション付きのバイナリである。各セクションは名前と長さを持ち、
// 読み出し側が知らないセクションを長さ分読み飛ばせる。これにより、
// バージョン違いでの部分的な互換が取れる。
//
// Reader はエラーを内部に蓄積する。各読み出しメソッドはエラーを返さず、
// 最後に Err で確認する。呼び出し側のコードが読み出しの順序だけを表す
// ようにするためである。
package state

import "errors"

// ErrTruncated はデータが途中で切れていることを表す。
var ErrTruncated = errors.New("state: データが途中で切れている")

// ErrSectionMismatch は期待したセクションが見つからないことを表す。
var ErrSectionMismatch = errors.New("state: セクションが一致しない")

// ErrBadFormat は形式が壊れていることを表す。
var ErrBadFormat = errors.New("state: 形式が壊れている")

// Snapshotter は自身の状態を直列化できるコンポーネントを表す。
//
// 実装は SaveState と LoadState で同じ順序・同じ個数の値を読み書きする。
// 順序が食い違うと往復テスト（internal/testrom）が検出する。
type Snapshotter interface {
	SaveState(w *Writer)
	LoadState(r *Reader) error
}

// maxSectionNameLen はセクション名の長さの上限。
// 壊れたデータを読んだときに巨大なメモリ確保を避けるために設ける。
const maxSectionNameLen = 255
