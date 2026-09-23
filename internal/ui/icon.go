package ui

import (
	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/assets"
)

// iconResource は埋め込んだアイコンを Fyne の資源として持つ。
//
// 1 回だけ作る。ウィンドウごとに作ると同じ内容が複数回読み込まれる。
var iconResource = fyne.NewStaticResource("icon.png", assets.Icon)

// appIcon はアプリケーションのアイコンを返す。
func appIcon() fyne.Resource { return iconResource }
