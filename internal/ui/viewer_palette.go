package ui

import (
	"fmt"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// パレットビューアのマスの大きさ。
const (
	paletteCell = 16
	// forbiddenColor は使用を避ける色 $0D。
	forbiddenColor = 0x0D
)

var colorForbiddenMark = color.RGBA{R: 0xFF, G: 0x20, B: 0x20, A: 0xFF}

// paletteViewer はパレットビューア（設計書 09 編 §9.4.4）。
type paletteViewer struct {
	u   *UI
	src snapshotSource

	entries *pixelView // 32 エントリ（4 列 × 8 行）
	all     *pixelView // 64 色（16 列 × 4 行）
	preview *pixelView // エンファシスとグレースケールを当てた 32 エントリ
	table   *widget.TextGrid
	info    *widget.Label

	emphasis  [3]*widget.Check // 赤・緑・青
	greyscale *widget.Check
	// fromMask はプレビューの設定を $2001 の値に合わせることを表す。
	// 利用者がチェックを変えるとやめる。
	fromMask bool
}

// palettes はパレットビューアを返す。無ければ作る。
func (u *UI) palettes() *paletteViewer {
	if u.paletteViewer == nil {
		u.paletteViewer = &paletteViewer{u: u, src: snapshotSource{u: u, line: -1}, fromMask: true}
	}
	return u.paletteViewer
}

func (v *paletteViewer) Title() string { return i18n.T(i18n.ViewerPalette) }

func (v *paletteViewer) Content() fyne.CanvasObject {
	v.src.acquire()
	v.entries = newPixelView(4*paletteCell, 8*paletteCell, 2)
	v.entries.onTap = func(x, y int) { v.edit(y/paletteCell*4 + x/paletteCell) }
	v.entries.onHover = v.hover
	v.all = newPixelView(16*paletteCell, 4*paletteCell, 2)
	v.all.onHover = func(x, y int, ok bool) {
		if ok {
			v.info.SetText(i18n.T(i18n.PalColorN, y/paletteCell*16+x/paletteCell))
		}
	}
	v.preview = newPixelView(4*paletteCell, 8*paletteCell, 2)
	v.table = widget.NewTextGrid()
	v.info = widget.NewLabel("")
	onUser := func(bool) {
		v.fromMask = false
		v.drawPreview()
	}
	names := []string{i18n.T(i18n.PalRed), i18n.T(i18n.PalGreen), i18n.T(i18n.PalBlue)}
	row := container.NewHBox(widget.NewLabel(i18n.T(i18n.PalPreviewLabel)))
	for i := range v.emphasis {
		v.emphasis[i] = widget.NewCheck(i18n.T(i18n.PalEmphasis, names[i]), onUser)
		row.Add(v.emphasis[i])
	}
	v.greyscale = widget.NewCheck(i18n.T(i18n.PalGreyscale), onUser)
	row.Add(v.greyscale)
	row.Add(widget.NewButton(i18n.T(i18n.PalFromMask), func() {
		v.fromMask = true
		v.drawPreview()
	}))

	grids := container.NewHBox(
		container.NewVBox(widget.NewLabel(i18n.T(i18n.PalRAMTitle)), v.entries),
		container.NewVBox(widget.NewLabel(i18n.T(i18n.PalPreview)), v.preview),
		v.table,
	)
	body := container.NewVBox(
		grids,
		widget.NewLabel(i18n.T(i18n.PalAllColors)), v.all,
		row, v.info,
	)
	bar := container.NewHBox(v.src.lineSelector(v.Refresh), widget.NewLabel(i18n.T(i18n.PalHint)))
	v.Refresh()
	return container.NewBorder(bar, nil, nil, nil, container.NewScroll(body))
}

// Refresh はスナップショットを読んで描き直す。
func (v *paletteViewer) Refresh() {
	if v.entries == nil || !v.src.latest() {
		return
	}
	v.draw()
}

// entryValue はエントリ i（0-31）の値を返す。$3F10 などは $3F00 などと同じ
// 記憶域を指す。スナップショットの Palette は 32 バイトの記憶域そのままである。
func entryValue(snap *debug.Snapshot, i int) uint8 { return snap.Palette[paletteSlot(i)] & 0x3F }

// paletteSlot はエントリの番号を記憶域の位置にする。
func paletteSlot(i int) int {
	if i&0x13 == 0x10 {
		return i & 0x0F
	}
	return i
}

// draw はエントリ・64 色・表・プレビューを描く。
func (v *paletteViewer) draw() {
	snap := &v.src.snap
	img := v.entries.Image()
	for i := range 32 {
		r := image.Rect(i%4*paletteCell, i/4*paletteCell, (i%4+1)*paletteCell, (i/4+1)*paletteCell)
		fill(img, r, v.u.pal.Color(uint16(entryValue(snap, i))))
		if i != paletteSlot(i) {
			// 背景側と同じ記憶域を指すエントリに印を付ける。
			drawRectOutline(img, r.Inset(2), color.RGBA{0x80, 0x80, 0x80, 0xFF})
		}
	}
	drawRectOutline(img, image.Rect(0, 0, paletteCell, paletteCell), colorSelected)
	v.entries.Refresh()

	all := v.all.Image()
	for c := range video.ColorCount {
		r := image.Rect(c%16*paletteCell, c/16*paletteCell, (c%16+1)*paletteCell, (c/16+1)*paletteCell)
		fill(all, r, v.u.pal.Color(uint16(c)))
		if c == forbiddenColor {
			for d := 2; d < paletteCell-2; d++ {
				all.SetRGBA(r.Min.X+d, r.Min.Y+d, colorForbiddenMark)
				all.SetRGBA(r.Max.X-1-d, r.Min.Y+d, colorForbiddenMark)
			}
		}
	}
	v.all.Refresh()

	rows := make([]widget.TextGridRow, 0, 8)
	for g := range 8 {
		name := i18n.T(i18n.PalBGGroup, g)
		if g >= 4 {
			name = i18n.T(i18n.PalSpriteGroup, g-4)
		}
		text := name
		for k := range 4 {
			i := g*4 + k
			text += fmt.Sprintf("  $%04X=$%02X", 0x3F00+i, entryValue(snap, i))
		}
		if g == 0 {
			text += i18n.T(i18n.PalBackdropNote)
		}
		if g == 4 {
			text += i18n.T(i18n.PalMirrorNote)
		}
		rows = append(rows, textRow(text, nil))
	}
	setRows(v.table, rows)
	v.drawPreview()
}

// drawPreview はエンファシスとグレースケールを当てた色を描く。
func (v *paletteViewer) drawPreview() {
	if !v.src.ok {
		return
	}
	snap := &v.src.snap
	if v.fromMask {
		for i, c := range v.emphasis {
			c.SetChecked(snap.Mask&(0x20<<i) != 0)
		}
		v.greyscale.SetChecked(snap.Mask&0x01 != 0)
		v.fromMask = true
	}
	var emph uint16
	for i, c := range v.emphasis {
		if c.Checked {
			emph |= 1 << i
		}
	}
	img := v.preview.Image()
	for i := range 32 {
		idx := uint16(entryValue(snap, i))
		if v.greyscale.Checked {
			idx &= 0x30
		}
		r := image.Rect(i%4*paletteCell, i/4*paletteCell, (i%4+1)*paletteCell, (i/4+1)*paletteCell)
		fill(img, r, v.u.pal.Color(idx|emph<<video.EmphasisShift))
	}
	v.preview.Refresh()
}

// hover はマウスの下のエントリの情報を出す。
func (v *paletteViewer) hover(x, y int, ok bool) {
	if !ok || !v.src.ok {
		v.info.SetText("")
		return
	}
	i := y/paletteCell*4 + x/paletteCell
	text := fmt.Sprintf("$%04X = $%02X", 0x3F00+i, entryValue(&v.src.snap, i))
	if s := paletteSlot(i); s != i {
		text += i18n.T(i18n.PalSameSlot, 0x3F00+s)
	}
	v.info.SetText(text)
}

// edit はエントリ i の色を 64 色から選ぶダイアログを開く。
func (v *paletteViewer) edit(i int) {
	if v.u.win == nil {
		return
	}
	picker := newPixelView(16*paletteCell, 4*paletteCell, 2)
	img := picker.Image()
	for c := range video.ColorCount {
		fill(img, image.Rect(c%16*paletteCell, c/16*paletteCell, (c%16+1)*paletteCell, (c/16+1)*paletteCell),
			v.u.pal.Color(uint16(c)))
	}
	picker.Refresh()
	var d dialog.Dialog
	picker.onTap = func(x, y int) {
		d.Hide()
		v.setColor(i, uint8(y/paletteCell*16+x/paletteCell))
	}
	d = dialog.NewCustom(i18n.T(i18n.PalColorDialog, 0x3F00+i), i18n.T(i18n.CommonCancel), picker, v.u.win)
	d.Show()
}

// setColor はエントリ i に色 c を書く。
func (v *paletteViewer) setColor(i int, c uint8) {
	if err := v.u.emu.Poke(debug.SpacePPU, 0x3F00+i, c, false); err != nil {
		v.u.showError(err)
		return
	}
	if v.src.ok {
		v.src.snap.Palette[paletteSlot(i)] = c
		v.draw()
	}
}

func (v *paletteViewer) OnClose() { v.src.release() }
