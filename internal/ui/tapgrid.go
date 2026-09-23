package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// tapGrid は TextGrid に、タップ・右クリック・ホイール・キー入力の
// 受け取りを加えたウィジェット。
//
// TextGrid 自体は入力を受け取らない。逆アセンブルの行を選んだり、
// メモリビューアでバイトを選んで書き換えたりするために包む。
type tapGrid struct {
	widget.BaseWidget
	grid *widget.TextGrid

	// onTap はタップした行と桁を受け取る。
	onTap func(row, col int)
	// onSecondary は右クリックした行と桁と位置を受け取る。
	onSecondary func(row, col int, pos fyne.Position)
	// onScroll はホイールの行数を受け取る。正が下へ進む向き。
	onScroll func(lines int)
	// onRune と onKey はフォーカスがあるときの入力を受け取る。
	onRune func(r rune)
	onKey  func(k *fyne.KeyEvent)

	focused bool
}

// newTapGrid はウィジェットを作る。
func newTapGrid() *tapGrid {
	g := &tapGrid{grid: widget.NewTextGrid()}
	g.ExtendBaseWidget(g)
	return g
}

// CreateRenderer は中の TextGrid をそのまま描く。
func (g *tapGrid) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(g.grid)
}

// Tapped は行と桁を求めて onTap へ渡し、フォーカスを取る。
func (g *tapGrid) Tapped(e *fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(g); c != nil && g.onRune != nil {
		c.Focus(g)
	}
	if g.onTap != nil {
		row, col := g.grid.CursorLocationForPosition(e.Position)
		g.onTap(row, col)
	}
}

// TappedSecondary は右クリックの位置を onSecondary へ渡す。
func (g *tapGrid) TappedSecondary(e *fyne.PointEvent) {
	if g.onSecondary != nil {
		row, col := g.grid.CursorLocationForPosition(e.Position)
		g.onSecondary(row, col, e.AbsolutePosition)
	}
}

// Scrolled はホイールの量を行数にする。
func (g *tapGrid) Scrolled(e *fyne.ScrollEvent) {
	if g.onScroll == nil {
		return
	}
	h := monospaceCell().Height
	lines := int(-e.Scrolled.DY / h)
	if lines == 0 {
		if e.Scrolled.DY < 0 {
			lines = 1
		} else if e.Scrolled.DY > 0 {
			lines = -1
		}
	}
	g.onScroll(lines)
}

// FocusGained はフォーカスを得たことを記録する。
func (g *tapGrid) FocusGained() { g.focused = true }

// FocusLost はフォーカスを失ったことを記録する。
func (g *tapGrid) FocusLost() { g.focused = false }

// TypedRune は文字の入力を onRune へ渡す。
func (g *tapGrid) TypedRune(r rune) {
	if g.onRune != nil {
		g.onRune(r)
	}
}

// TypedKey はキーの入力を onKey へ渡す。
func (g *tapGrid) TypedKey(k *fyne.KeyEvent) {
	if g.onKey != nil {
		g.onKey(k)
	}
}

// visibleRows は今の高さに収まる行数を返す。
func (g *tapGrid) visibleRows() int {
	h := monospaceCell().Height
	if h <= 0 {
		return 1
	}
	n := int(g.Size().Height / h)
	if n < 1 {
		n = 1
	}
	return n
}
