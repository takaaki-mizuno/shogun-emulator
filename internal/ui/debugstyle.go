package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// デバッガの表示に使う色。
//
// 背景色は半透明にする。明るいテーマと暗いテーマのどちらでも文字が
// 読めるようにするためである。
var (
	// colorPCBackground は PC が指すバイトと現在の行の背景（黄色）。
	colorPCBackground = color.NRGBA{R: 0xE8, G: 0xC5, B: 0x00, A: 0x70}
	// colorSPBackground はスタックポインタが指す位置の背景（青）。
	colorSPBackground = color.NRGBA{R: 0x30, G: 0x70, B: 0xE0, A: 0x70}
	// colorCursorBackground はメモリビューアのカーソルの背景。
	colorCursorBackground = color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0x90}
	// colorUnofficial は非公式命令の文字色（橙）。
	colorUnofficial = color.NRGBA{R: 0xE0, G: 0x80, B: 0x20, A: 0xFF}
	// colorBreakpoint はブレークポイントの印の文字色（赤）。
	colorBreakpoint = color.NRGBA{R: 0xE0, G: 0x30, B: 0x30, A: 0xFF}
)

// heatColor は変更追跡の値から背景色を作る。値が 0 のとき nil を返す。
//
// 赤の不透明度で濃さを表す。1.0 が直前のフレームで書き換わったことを表す。
func heatColor(h float32) color.Color {
	if h <= 0 {
		return nil
	}
	return color.NRGBA{R: 0xE0, G: 0x20, B: 0x20, A: uint8(40 + h*160)}
}

// dimColor は推定の逆アセンブルに使う薄い文字色を返す。
func dimColor() color.Color {
	return theme.Color(theme.ColorNameDisabled)
}

// cellStyle は前景色と背景色からセルの書式を作る。
func cellStyle(fg, bg color.Color) widget.TextGridStyle {
	if fg == nil && bg == nil {
		return nil
	}
	return &widget.CustomTextGridStyle{
		TextStyle: fyne.TextStyle{Monospace: true},
		FGColor:   fg,
		BGColor:   bg,
	}
}

// textRow は文字列を 1 行分のセルにする。
func textRow(s string, style widget.TextGridStyle) widget.TextGridRow {
	cells := make([]widget.TextGridCell, 0, len(s))
	for _, r := range s {
		cells = append(cells, widget.TextGridCell{Rune: r, Style: style})
	}
	return widget.TextGridRow{Cells: cells}
}

// setRows は TextGrid の中身を差し替える。行数が変わるときも扱う。
func setRows(g *widget.TextGrid, rows []widget.TextGridRow) {
	g.Rows = rows
	g.Refresh()
}

// monospace は等幅の文字の大きさを返す。
func monospaceCell() fyne.Size {
	return fyne.MeasureText("M", theme.TextSize(), fyne.TextStyle{Monospace: true})
}

// container2 はラベルと入力欄を横に並べる。入力欄に 90 の幅を与える。
func container2(label fyne.CanvasObject, entry fyne.CanvasObject) fyne.CanvasObject {
	return container.NewHBox(label, container.NewGridWrap(fyne.NewSize(90, entry.MinSize().Height), entry))
}
