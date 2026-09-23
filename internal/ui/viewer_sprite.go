package ui

import (
	"errors"
	"fmt"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"image"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// スプライトビューアの一覧の 1 マスの大きさ。8×16 のスプライトに 1 ピクセルの枠。
const (
	spriteCellW = 10
	spriteCellH = 18
)

var (
	colorSprite0    = color.RGBA{R: 0xFF, G: 0x40, B: 0x40, A: 0xFF}
	colorSelected   = color.RGBA{R: 0xFF, G: 0xD0, B: 0x00, A: 0xFF}
	colorUndrawn    = color.RGBA{R: 0x40, G: 0x40, B: 0x40, A: 0xB0}
	colorLineCount  = color.RGBA{R: 0x40, G: 0xA0, B: 0xFF, A: 0xFF}
	colorLineOver   = color.RGBA{R: 0xFF, G: 0x40, B: 0x40, A: 0xFF}
	colorLineBorder = color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}
)

// spriteViewer はスプライトビューア（設計書 09 編 §9.4.3）。
type spriteViewer struct {
	u   *UI
	src snapshotSource

	sheet    *pixelView // 64 スプライトの絵
	preview  *pixelView // 画面上の配置
	lines    *pixelView // スキャンラインごとのスプライト数
	table    *widget.TextGrid
	selected int
	fields   [4]*widget.Entry // Y・タイル・属性・X
	detail   *widget.Label
	boxes    bool

	// dragX と dragY はドラッグの端数。1 ピクセルに満たない移動を溜める。
	dragX, dragY float32
}

// sprites はスプライトビューアを返す。無ければ作る。
func (u *UI) sprites() *spriteViewer {
	if u.spriteViewer == nil {
		u.spriteViewer = &spriteViewer{u: u, src: snapshotSource{u: u, line: -1}}
	}
	return u.spriteViewer
}

func (v *spriteViewer) Title() string { return i18n.T(i18n.ViewerSprite) }

func (v *spriteViewer) Content() fyne.CanvasObject {
	v.src.acquire()
	v.sheet = newPixelView(8*spriteCellW, 8*spriteCellH, 3)
	v.sheet.onTap = func(x, y int) { v.selectSprite(y/spriteCellH*8 + x/spriteCellW) }
	v.preview = newPixelView(256, 240, 2)
	v.preview.onTap = v.pick
	v.preview.onDrag = v.drag
	v.preview.onDragEnd = func() { v.dragX, v.dragY = 0, 0 }
	v.lines = newPixelView(48, 240, 2)
	v.table = widget.NewTextGrid()
	v.detail = widget.NewLabel("")

	names := []string{"Y", i18n.T(i18n.SprTile), i18n.T(i18n.SprAttr), "X"}
	form := container.NewGridWithColumns(8)
	for i := range v.fields {
		e := widget.NewEntry()
		k := i
		e.OnSubmitted = func(s string) { v.setField(k, s) }
		v.fields[i] = e
		form.Add(widget.NewLabel(names[i]))
		form.Add(e)
	}
	boxes := widget.NewCheck(i18n.T(i18n.SprBoxes), func(on bool) {
		v.boxes = on
		v.updateBoxes()
	})
	boxes.SetChecked(v.boxes)

	left := container.NewVBox(
		widget.NewLabelWithStyle(i18n.T(i18n.SprSheetTitle), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewCenter(v.sheet), v.detail, form,
	)
	right := container.NewVBox(
		widget.NewLabelWithStyle(i18n.T(i18n.SprPreviewTitle), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(v.preview, v.lines),
	)
	bar := container.NewHBox(boxes, v.src.lineSelector(v.Refresh))
	v.Refresh()
	top := container.NewHBox(left, right)
	return container.NewBorder(bar, nil, nil, nil,
		container.NewVSplit(container.NewScroll(top), container.NewScroll(v.table)))
}

// Refresh はスナップショットを読んで描き直す。
func (v *spriteViewer) Refresh() {
	if v.sheet == nil || !v.src.latest() {
		return
	}
	v.draw()
}

// spriteInfo は OAM の 1 スプライト分。
type spriteInfo struct {
	y, tile, attr, x uint8
}

func (s spriteInfo) palette() uint8 { return s.attr & 3 }
func (s spriteInfo) behind() bool   { return s.attr&0x20 != 0 }
func (s spriteInfo) flipH() bool    { return s.attr&0x40 != 0 }
func (s spriteInfo) flipV() bool    { return s.attr&0x80 != 0 }

// spriteAt は OAM の i 番目を返す。
func spriteAt(snap *debug.Snapshot, i int) spriteInfo {
	o := snap.OAM[i*4 : i*4+4]
	return spriteInfo{y: o[0], tile: o[1], attr: o[2], x: o[3]}
}

// spritePixel はスプライトの (x, y) の色番号を返す。8×16 では y が 0-15。
func spritePixel(snap *debug.Snapshot, s spriteInfo, x, y int) uint8 {
	h := snap.SpriteHeight
	if s.flipV() {
		y = h - 1 - y
	}
	var base int
	tile := int(s.tile)
	if h == 16 {
		base = int(s.tile&1) * 0x1000
		tile &^= 1
		if y >= 8 {
			tile++
			y -= 8
		}
	} else if snap.Ctrl&0x08 != 0 {
		base = 0x1000
	}
	chr := snap.CHR[base+tile*16 : base+tile*16+16]
	return tilePixel(chr, x, y, s.flipH(), false)
}

// spriteColor はスプライトの色番号 c の色を返す。
func (v *spriteViewer) spriteColor(snap *debug.Snapshot, s spriteInfo, c uint8) color.RGBA {
	return v.u.pal.Color(uint16(snap.Palette[16+int(s.palette())*4+int(c)] & 0x3F))
}

// draw は一覧・配置・行ごとの数・表を描く。
func (v *spriteViewer) draw() {
	snap := &v.src.snap
	backdrop := v.u.pal.Color(uint16(snap.Palette[0] & 0x3F))

	sheet := v.sheet.Image()
	fill(sheet, sheet.Bounds(), color.RGBA{0x20, 0x20, 0x20, 0xFF})
	for i := range 64 {
		s := spriteAt(snap, i)
		ox, oy := i%8*spriteCellW+1, i/8*spriteCellH+1
		fill(sheet, image.Rect(ox, oy, ox+8, oy+snap.SpriteHeight), backdrop)
		for y := range snap.SpriteHeight {
			for x := range 8 {
				if c := spritePixel(snap, s, x, y); c != 0 {
					sheet.SetRGBA(ox+x, oy+y, v.spriteColor(snap, s, c))
				}
			}
		}
		cellRect := image.Rect(ox-1, oy-1, ox+spriteCellW-1, oy+spriteCellH-1)
		if !snap.SpriteDrawn[i] {
			for y := oy; y < oy+snap.SpriteHeight; y++ {
				for x := ox; x < ox+8; x++ {
					blend(sheet, x, y, colorUndrawn)
				}
			}
		}
		switch i {
		case v.selected:
			drawRectOutline(sheet, cellRect, colorSelected)
		case 0:
			drawRectOutline(sheet, cellRect, colorSprite0)
		}
	}
	v.sheet.Refresh()

	prev := v.preview.Image()
	fill(prev, prev.Bounds(), backdrop)
	for i := 63; i >= 0; i-- {
		s := spriteAt(snap, i)
		for y := range snap.SpriteHeight {
			for x := range 8 {
				if c := spritePixel(snap, s, x, y); c != 0 {
					px, py := int(s.x)+x, int(s.y)+1+y
					if px < 256 && py < 240 {
						prev.SetRGBA(px, py, v.spriteColor(snap, s, c))
					}
				}
			}
		}
	}
	sel := spriteAt(snap, v.selected)
	drawClippedRect(prev, image.Rect(int(sel.x), int(sel.y)+1, int(sel.x)+8, int(sel.y)+1+snap.SpriteHeight), colorSelected)
	v.preview.Refresh()

	lines := v.lines.Image()
	fill(lines, lines.Bounds(), color.RGBA{0x20, 0x20, 0x20, 0xFF})
	for y, n := range snap.ScanlineSpriteCount {
		c := colorLineCount
		if n > 8 {
			c = colorLineOver
		}
		fill(lines, image.Rect(0, y, min(int(n)*2, 48), y+1), c)
	}
	fill(lines, image.Rect(16, 0, 17, 240), colorLineBorder) // 8 個の位置
	v.lines.Refresh()

	v.drawTable()
	v.drawDetail()
	v.updateBoxes()
}

// drawClippedRect は画面の外へはみ出す部分を描かずに枠を描く。
func drawClippedRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	b := img.Bounds()
	for x := r.Min.X; x < r.Max.X; x++ {
		for _, y := range []int{r.Min.Y, r.Max.Y - 1} {
			if (image.Point{x, y}).In(b) {
				img.SetRGBA(x, y, c)
			}
		}
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for _, x := range []int{r.Min.X, r.Max.X - 1} {
			if (image.Point{x, y}).In(b) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

// drawTable は 64 スプライトの数値の表を描く。
func (v *spriteViewer) drawTable() {
	snap := &v.src.snap
	rows := make([]widget.TextGridRow, 0, 66)
	rows = append(rows, textRow(i18n.T(i18n.SprTableHeader), cellStyle(dimColor(), nil)))
	for i := range 64 {
		s := spriteAt(snap, i)
		pri := i18n.T(i18n.SprFront)
		if s.behind() {
			pri = i18n.T(i18n.SprBack)
		}
		flip := []byte("--")
		if s.flipH() {
			flip[0] = 'H'
		}
		if s.flipV() {
			flip[1] = 'V'
		}
		drawn := "○"
		if !snap.SpriteDrawn[i] {
			drawn = "×"
		}
		text := fmt.Sprintf("%02d  $%02X   $%02X   $%02X   %d      %s    %s   $%02X   %s",
			i, s.y, s.tile, s.attr, s.palette(), pri, flip, s.x, drawn)
		var fg, bg color.Color
		switch {
		case i == 0:
			fg = colorSprite0
		case !snap.SpriteDrawn[i]:
			fg = dimColor()
		}
		if i == v.selected {
			bg = colorPCBackground
		}
		rows = append(rows, textRow(text, cellStyle(fg, bg)))
	}
	var over []string
	for y, n := range snap.ScanlineSpriteCount {
		if n > 8 {
			over = append(over, fmt.Sprintf("%d(%d)", y, n))
		}
	}
	summary := i18n.T(i18n.SprOverflowNone)
	if len(over) > 0 {
		summary = i18n.T(i18n.SprOverflowPrefix) + strings.Join(over, " ")
	}
	rows = append(rows, textRow(summary, cellStyle(colorLineOver, nil)))
	setRows(v.table, rows)
}

// drawDetail は選んだスプライトの値を入力欄へ出す。入力中の欄は書き換えない。
func (v *spriteViewer) drawDetail() {
	s := spriteAt(&v.src.snap, v.selected)
	vals := [4]uint8{s.y, s.tile, s.attr, s.x}
	c := fyne.CurrentApp().Driver().CanvasForObject(v.sheet)
	for i, e := range v.fields {
		if c != nil && c.Focused() == e {
			continue
		}
		e.SetText(fmt.Sprintf("$%02X", vals[i]))
	}
	v.detail.SetText(i18n.T(i18n.SprDetail,
		v.selected, s.palette(), map[bool]string{false: i18n.T(i18n.SprInFront), true: i18n.T(i18n.SprBehind)}[s.behind()], s.flipH(), s.flipV()))
}

// selectSprite はスプライトを選ぶ。
func (v *spriteViewer) selectSprite(i int) {
	if i < 0 || i >= 64 {
		return
	}
	v.selected = i
	if v.src.ok {
		v.draw()
	}
}

// pick は配置の画面で、クリックした位置にある最も手前のスプライトを選ぶ。
func (v *spriteViewer) pick(x, y int) {
	if !v.src.ok {
		return
	}
	snap := &v.src.snap
	for i := range 64 {
		s := spriteAt(snap, i)
		if x >= int(s.x) && x < int(s.x)+8 && y >= int(s.y)+1 && y < int(s.y)+1+snap.SpriteHeight {
			v.selectSprite(i)
			return
		}
	}
}

// drag は選んだスプライトを動かした量だけ移す。
func (v *spriteViewer) drag(_, _ int, dx, dy float32) {
	if !v.src.ok {
		return
	}
	v.dragX += dx
	v.dragY += dy
	mx, my := int(v.dragX), int(v.dragY)
	if mx == 0 && my == 0 {
		return
	}
	v.dragX -= float32(mx)
	v.dragY -= float32(my)
	s := spriteAt(&v.src.snap, v.selected)
	base := v.selected * 4
	if my != 0 && !v.poke(base, uint8(int(s.y)+my)) {
		return
	}
	if mx != 0 && !v.poke(base+3, uint8(int(s.x)+mx)) {
		return
	}
	// 次のスナップショットを待たずに手元の写しを動かす。
	v.src.snap.OAM[base] = uint8(int(s.y) + my)
	v.src.snap.OAM[base+3] = uint8(int(s.x) + mx)
	v.draw()
}

// setField は入力欄の値を OAM へ書く。
func (v *spriteViewer) setField(k int, s string) {
	val, err := parseHexValue(s)
	if err != nil || val > 0xFF {
		v.u.showError(errors.New(i18n.T(i18n.SprBadValue, s)))
		return
	}
	if v.poke(v.selected*4+k, uint8(val)) && v.src.ok {
		v.src.snap.OAM[v.selected*4+k] = uint8(val)
		v.draw()
	}
}

// poke は OAM へ書く。失敗したとき false。
func (v *spriteViewer) poke(addr int, val uint8) bool {
	if err := v.u.emu.Poke(debug.SpaceOAM, addr, val, false); err != nil {
		v.u.showError(err)
		return false
	}
	return true
}

// updateBoxes はメイン画面に重ねる矩形を合わせる。
func (v *spriteViewer) updateBoxes() {
	if v.u.screen == nil {
		return
	}
	if !v.boxes || !v.src.ok {
		v.u.screen.setSpriteBoxes(nil)
		return
	}
	snap := &v.src.snap
	var boxes []image.Rectangle
	for i := range 64 {
		s := spriteAt(snap, i)
		if int(s.y) >= 239 {
			continue
		}
		boxes = append(boxes, image.Rect(int(s.x), int(s.y)+1, int(s.x)+8, int(s.y)+1+snap.SpriteHeight))
	}
	v.u.screen.setSpriteBoxes(boxes)
}

// OnClose は購読をやめ、メイン画面の矩形を消す。
func (v *spriteViewer) OnClose() {
	v.src.release()
	if v.u.screen != nil {
		v.u.screen.setSpriteBoxes(nil)
	}
}
