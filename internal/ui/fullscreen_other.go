//go:build !darwin

package ui

import "fyne.io/fyne/v2"

// nativeFullScreen は macOS 以外では問い合わせない。Fyne の状態がそのまま正しい。
func nativeFullScreen(fyne.Window) (full, ok bool) { return false, false }
