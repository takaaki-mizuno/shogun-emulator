// Package assets は実行ファイルへ埋め込むデータを持つ。
//
// データだけを持ち、処理を持たない。embed 以外のパッケージを参照しない。
// 埋め込みの宣言は参照するファイルと同じディレクトリ以下しか書けないため、
// 使う側のパッケージではなくデータの置き場所に宣言を置く。
package assets

import _ "embed"

// DefaultPalette は既定の NTSC パレット。64 色 × 3 バイト。
//
// 中身は `docs/research/03_ppu.md` の 10.3 節の表から
// `go run ./tools/gen-palette` で生成する。
//
//go:embed palettes/default.pal
var DefaultPalette []byte

// Icon はアプリケーションのアイコン。256 ピクセル四方の PNG。
// Windows と Linux のウィンドウのアイコンに使う。
//
// 原画 icon.png から `go run ./tools/gen-icon` で生成する。
//
//go:embed icon/icon.png
var Icon []byte

// IconMacOS は macOS 向けの角丸のタイルの形のアイコン。512 ピクセル四方の PNG。
// .app に入れずに起動したときの Dock の表示に使う。
//
//go:embed icon/icon-macos.png
var IconMacOS []byte

// Logo はアプリ内に表示するロゴ。512 ピクセル四方の PNG。
//
//go:embed icon/logo.png
var Logo []byte
