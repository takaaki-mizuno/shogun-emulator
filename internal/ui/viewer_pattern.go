package ui

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// patternPaletteLabels はパターンテーブルに当てるパレットの選択肢。
// 0 がグレースケール、1-4 が背景 0-3、5-8 がスプライト 0-3。
var patternPaletteLabels = []string{
	"グレースケール", "背景 0", "背景 1", "背景 2", "背景 3",
	"スプライト 0", "スプライト 1", "スプライト 2", "スプライト 3",
}

// zoomLabels は拡大率の選択肢。
var zoomLabels = []string{"1", "2", "3", "4", "5", "6", "7", "8"}

// patternViewer はパターンテーブルビューア（設計書 09 編 §9.4.1）。
//
// 16×16 タイルのグリッドを 2 面、左右に並べて 256×128 ピクセルで描く。
type patternViewer struct {
	u    *UI
	open bool
	src  snapshotSource

	view    *pixelView
	palette int
	tall    bool
	info    *widget.Label
	banks   *widget.Label
}

// patterns はパターンテーブルビューアを返す。無ければ作る。
func (u *UI) patterns() *patternViewer {
	if u.patternViewer == nil {
		u.patternViewer = &patternViewer{u: u, src: snapshotSource{u: u, line: -1}}
	}
	return u.patternViewer
}

func (v *patternViewer) Title() string { return "パターンテーブル" }

func (v *patternViewer) Content() fyne.CanvasObject {
	v.open = true
	v.src.acquire()
	v.view = newPixelView(256, 128, 2)
	v.view.onHover = v.hover
	v.view.onTap = v.tap
	v.info = widget.NewLabel("")
	v.banks = widget.NewLabel("")

	pal := widget.NewSelect(patternPaletteLabels, func(s string) {
		for i, l := range patternPaletteLabels {
			if l == s {
				v.palette = i
			}
		}
		v.draw()
	})
	pal.SetSelectedIndex(v.palette)
	tall := widget.NewCheck("8×16 の並び", func(on bool) {
		v.tall = on
		v.draw()
	})
	tall.SetChecked(v.tall)
	zoom := widget.NewSelect(zoomLabels, func(s string) {
		n, _ := strconv.Atoi(s)
		v.view.SetZoom(n)
	})
	zoom.SetSelected(strconv.Itoa(v.view.zoom))

	bar := container.NewHBox(pal, tall, widget.NewLabel("拡大"), zoom, v.src.lineSelector(v.Refresh))
	v.Refresh()
	return container.NewBorder(bar, container.NewVBox(v.info, v.banks), nil, nil,
		container.NewScroll(container.NewCenter(v.view)))
}

// Refresh はスナップショットを読んで描き直す。
func (v *patternViewer) Refresh() {
	if v.view == nil || !v.src.latest() {
		return
	}
	v.draw()
	var b strings.Builder
	b.WriteString("バンク構成: ")
	for i, bank := range v.src.snap.BankView {
		if i > 0 {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "$%04X=%s#%d", bank.CPUOrPPUAddr, bank.SourceKind, bank.BankIndex)
	}
	v.banks.SetText(b.String())
}

// tileAt は画像上のタイルの位置から、面とタイル番号を返す。
func (v *patternViewer) tileAt(col, row int) (table, tile int) {
	table = col / 16
	col %= 16
	if v.tall {
		pair := (row/2)*16 + col
		return table, pair*2 + row%2
	}
	return table, row*16 + col
}

// tilePos はタイル番号から画像上のタイルの位置（面の中）を返す。
func (v *patternViewer) tilePos(tile int) (col, row int) {
	if v.tall {
		pair := tile / 2
		return pair % 16, (pair/16)*2 + tile%2
	}
	return tile % 16, tile / 16
}

// colorOf は色番号 c をパレットの選択に従って色にする。
func (v *patternViewer) colorOf(snap *debug.Snapshot, c uint8) color.RGBA {
	return chrColor(v.u, snap, v.palette, c)
}

// chrColor はパレットの選択 choice（0 がグレースケール）と色番号 c から色を決める。
func chrColor(u *UI, snap *debug.Snapshot, choice int, c uint8) color.RGBA {
	if choice <= 0 {
		return greyLevels[c&3]
	}
	idx := snap.Palette[0]
	if c != 0 {
		idx = snap.Palette[(choice-1)*4+int(c)]
	}
	return u.pal.Color(uint16(idx & 0x3F))
}

// draw は 512 タイルを描く。
func (v *patternViewer) draw() {
	if !v.src.ok {
		return
	}
	img := v.view.Image()
	snap := &v.src.snap
	for table := range 2 {
		for tile := range 256 {
			col, row := v.tilePos(tile)
			base := table*0x1000 + tile*16
			chr := snap.CHR[base : base+16]
			ox, oy := table*128+col*8, row*8
			for y := range 8 {
				for x := range 8 {
					img.SetRGBA(ox+x, oy+y, v.colorOf(snap, tilePixel(chr, x, y, false, false)))
				}
			}
		}
	}
	v.view.Refresh()
}

// hover はマウスの下のタイルの情報を出す。
func (v *patternViewer) hover(x, y int, ok bool) {
	if !ok || !v.src.ok {
		v.info.SetText("")
		return
	}
	table, tile := v.tileAt(x/8, y/8)
	addr := uint16(table*0x1000 + tile*16)
	text := fmt.Sprintf("タイル $%02X  CHR $%04X", tile, addr)
	if off, found := debug.CHROffset(v.src.snap.BankView, addr); found {
		for _, b := range v.src.snap.BankView {
			if addr >= b.CPUOrPPUAddr && int(addr) < int(b.CPUOrPPUAddr)+b.Size {
				text += fmt.Sprintf("  %s バンク %d（オフセット $%05X）", b.SourceKind, b.BankIndex, off)
				break
			}
		}
	}
	v.info.SetText(text)
}

// tap はタイルのピクセルエディタを開く。
func (v *patternViewer) tap(x, y int) {
	table, tile := v.tileAt(x/8, y/8)
	v.u.openTileEditor(uint16(table*0x1000+tile*16), v.palette)
}

// OnClose は購読をやめる。
func (v *patternViewer) OnClose() {
	v.open = false
	v.src.release()
}

// tileEditor はタイルのピクセルエディタ（設計書 09 編 §9.4.1）。
//
// 書き込みは Emulator.Poke を通す。CHR-ROM ではオーバーレイへ、CHR-RAM
// では直接書く（設計書 09 編 §9.4.8）。
type tileEditor struct {
	u       *UI
	addr    uint16
	palette int

	view    *pixelView
	colors  [4]*widget.Button
	current uint8
	data    [16]uint8
	pal     debug.Snapshot
	undo    [][16]uint8
	// stroke はドラッグで描いている最中であることを表す。
	stroke bool
	status *widget.Label
}

// openTileEditor はタイルのピクセルエディタを開く。同じタイルは 1 つだけ。
func (u *UI) openTileEditor(addr uint16, palette int) {
	if u.tileEditors == nil {
		u.tileEditors = map[uint16]*tileEditor{}
	}
	ed, ok := u.tileEditors[addr]
	if !ok {
		ed = &tileEditor{u: u, addr: addr, palette: palette, current: 1}
		u.tileEditors[addr] = ed
	}
	ed.palette = palette
	u.showViewer(ed)
}

func (ed *tileEditor) Title() string { return fmt.Sprintf("タイル $%04X", ed.addr) }

func (ed *tileEditor) Content() fyne.CanvasObject {
	ed.view = newPixelView(8, 8, 32)
	ed.view.onTap = func(x, y int) {
		ed.paint(x, y)
		ed.endStroke()
	}
	ed.view.onDrag = func(x, y int, _, _ float32) {
		if x >= 0 {
			ed.paint(x, y)
		}
	}
	ed.view.onDragEnd = ed.endStroke
	ed.status = widget.NewLabel("")
	row := container.NewHBox()
	for i := range 4 {
		c := uint8(i)
		ed.colors[i] = widget.NewButton(fmt.Sprintf("色 %d", i), func() {
			ed.current = c
			ed.drawButtons()
		})
		row.Add(ed.colors[i])
	}
	undo := widget.NewButton("元に戻す", ed.undoLast)
	ed.Refresh()
	return container.NewBorder(container.NewVBox(row, undo), ed.status, nil, nil, container.NewCenter(ed.view))
}

// Refresh は現在の CHR とパレットを読んで描き直す。描いている最中は読まない。
func (ed *tileEditor) Refresh() {
	if ed.view == nil || ed.stroke {
		return
	}
	ed.u.emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		if n == nil {
			return
		}
		debug.ReadMemory(n, debug.SpacePPU, int(ed.addr), ed.data[:])
		debug.ReadMemory(n, debug.SpacePPU, 0x3F00, ed.pal.Palette[:])
	})
	ed.draw()
}

// draw はタイルを描く。
func (ed *tileEditor) draw() {
	img := ed.view.Image()
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, chrColor(ed.u, &ed.pal, ed.palette, tilePixel(ed.data[:], x, y, false, false)))
		}
	}
	ed.view.Refresh()
	ed.drawButtons()
}

// drawButtons は選んでいる色のボタンを目立たせる。
func (ed *tileEditor) drawButtons() {
	for i, b := range ed.colors {
		if b == nil {
			continue
		}
		if uint8(i) == ed.current {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.MediumImportance
		}
		b.Refresh()
	}
}

// paint は (x, y) を選んでいる色にする。
func (ed *tileEditor) paint(x, y int) {
	if !ed.stroke {
		ed.undo = append(ed.undo, ed.data)
		ed.stroke = true
	}
	bit := uint8(0x80) >> x
	lo, hi := ed.data[y]&^bit, ed.data[y+8]&^bit
	if ed.current&1 != 0 {
		lo |= bit
	}
	if ed.current&2 != 0 {
		hi |= bit
	}
	if lo == ed.data[y] && hi == ed.data[y+8] {
		return
	}
	next := ed.data
	next[y], next[y+8] = lo, hi
	if !ed.write(next) {
		ed.stroke = false
		return
	}
	ed.draw()
}

// write は 16 バイトのうち変わったものを書き込む。失敗したとき false。
func (ed *tileEditor) write(next [16]uint8) bool {
	for i := range next {
		if next[i] == ed.data[i] {
			continue
		}
		if err := ed.u.emu.Poke(debug.SpacePPU, int(ed.addr)+i, next[i], false); err != nil {
			ed.status.SetText(err.Error())
			return false
		}
		ed.data[i] = next[i]
	}
	ed.status.SetText("")
	return true
}

// endStroke は 1 回の描画を終え、オーバーレイを保存する。
func (ed *tileEditor) endStroke() {
	if !ed.stroke {
		return
	}
	ed.stroke = false
	ed.u.emu.SaveOverlay()
}

// undoLast は直前の描画を取り消す。
func (ed *tileEditor) undoLast() {
	if len(ed.undo) == 0 {
		return
	}
	prev := ed.undo[len(ed.undo)-1]
	ed.undo = ed.undo[:len(ed.undo)-1]
	if ed.write(prev) {
		ed.u.emu.SaveOverlay()
		ed.draw()
	}
}

func (ed *tileEditor) OnClose() { ed.stroke = false }

// drawRectOutline は画像に矩形の枠を 1 ピクセルの線で描く。座標は画像の
// 大きさで折り返す。スクロール枠が画面の端をまたぐときに分割して描くためである。
func drawRectOutline(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	wrap := func(x, y int) {
		img.SetRGBA(((x%w)+w)%w, ((y%h)+h)%h, c)
	}
	for x := r.Min.X; x < r.Max.X; x++ {
		wrap(x, r.Min.Y)
		wrap(x, r.Max.Y-1)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		wrap(r.Min.X, y)
		wrap(r.Max.X-1, y)
	}
}
