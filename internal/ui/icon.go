package ui

import (
	"runtime"

	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/assets"
)

// iconResource は埋め込んだアイコンを Fyne の資源として持つ。
//
// 1 回だけ作る。ウィンドウごとに作ると同じ内容が複数回読み込まれる。
// macOS では Dock に出るため、角丸のタイルの形のものを使う（設計書 13 編 §13.3）。
var iconResource = func() fyne.Resource {
	if runtime.GOOS == "darwin" {
		return fyne.NewStaticResource("icon-macos.png", assets.IconMacOS)
	}
	return fyne.NewStaticResource("icon.png", assets.Icon)
}()

// logoResource はアプリ内に表示するロゴ。
var logoResource = fyne.NewStaticResource("logo.png", assets.Logo)

// appIcon はアプリケーションのアイコンを返す。
func appIcon() fyne.Resource { return iconResource }
