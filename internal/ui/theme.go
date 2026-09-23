package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// fixedVariantTheme は既定のテーマの配色を明暗のどちらかに固定する
// （設計書 10 編 §10.10）。
type fixedVariantTheme struct {
	fyne.Theme
	variant fyne.ThemeVariant
}

// Color は指定した明暗で色を返す。
func (t fixedVariantTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Theme.Color(name, t.variant)
}

// applyTheme は設定 ui.theme を反映する。auto のとき OS の設定に従う。
func (u *UI) applyTheme() {
	if u.app == nil {
		return
	}
	switch u.cfg.UI.Theme {
	case config.ThemeLight:
		u.app.Settings().SetTheme(fixedVariantTheme{theme.DefaultTheme(), theme.VariantLight})
	case config.ThemeDark:
		u.app.Settings().SetTheme(fixedVariantTheme{theme.DefaultTheme(), theme.VariantDark})
	default:
		u.app.Settings().SetTheme(theme.DefaultTheme())
	}
}
