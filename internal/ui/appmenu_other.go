//go:build !darwin

package ui

// localizeAppMenu は macOS 以外では何もしない。アプリケーションメニューが無い。
func localizeAppMenu() {}
