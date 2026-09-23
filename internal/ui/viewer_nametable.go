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
)

// オーバーレイの線の色。
var (
	colorScrollFrame = color.RGBA{R: 0xFF, G: 0x40, B: 0x40, A: 0xFF}
	colorAttrGrid    = color.RGBA{R: 0x40, G: 0xA0, B: 0xFF, A: 0x90}
	colorTileGrid    = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x50}
)

// nametableViewer はネームテーブルビューア（設計書 09 編 §9.4.2）。
//
// 4 面を 2×2 の 512×480 ピクセルで描く。$2000-$2FFF をミラーリングを
// 通して読むため、同じ内容が 2 か所に出る配置も実機どおりに表れる。
type nametableViewer struct {
	u   *UI
	src snapshotSource

	view                           *pixelView
	showScroll, showAttr, showTile bool
	info                           *widget.Label
}

// nametables はネームテーブルビューアを返す。無ければ作る。
func (u *UI) nametables() *nametableViewer {
	if u.nametableViewer == nil {
		u.nametableViewer = &nametableViewer{u: u, src: snapshotSource{u: u, line: -1}, showScroll: true}
	}
	return u.nametableViewer
}

func (v *nametableViewer) Title() string { return i18n.T(i18n.ViewerNametable) }

func (v *nametableViewer) Content() fyne.CanvasObject {
	v.src.acquire()
	v.view = newPixelView(512, 480, 1)
	v.view.onHover = v.hover
	v.view.onTap = v.editTile
	v.view.onSecondary = v.editAttribute
	v.info = widget.NewLabel("")
	check := func(label string, p *bool) *widget.Check {
		c := widget.NewCheck(label, func(on bool) {
			*p = on
			v.draw()
		})
		c.SetChecked(*p)
		return c
	}
	zoom := widget.NewSelect([]string{"1", "2"}, func(s string) {
		if s == "2" {
			v.view.SetZoom(2)
		} else {
			v.view.SetZoom(1)
		}
	})
	zoom.SetSelected("1")
	bar := container.NewHBox(
		check(i18n.T(i18n.NTScrollFrame), &v.showScroll),
		check(i18n.T(i18n.NTAttrGrid), &v.showAttr),
		check(i18n.T(i18n.NTTileGrid), &v.showTile),
		widget.NewLabel(i18n.T(i18n.CommonZoom)), zoom,
		v.src.lineSelector(v.Refresh),
	)
	hint := widget.NewLabel(i18n.T(i18n.NTHint))
	v.Refresh()
	return container.NewBorder(bar, container.NewVBox(v.info, hint), nil, nil,
		container.NewScroll(container.NewCenter(v.view)))
}

// Refresh はスナップショットを読んで描き直す。
func (v *nametableViewer) Refresh() {
	if v.view == nil || !v.src.latest() {
		return
	}
	v.draw()
}

// cellInfo はネームテーブル上のタイル (tx, ty)（0-63, 0-59）の情報。
type cellInfo struct {
	addr     uint16 // ネームテーブルのアドレス
	attrAddr uint16 // 属性のアドレス
	tile     uint8
	attr     uint8 // 属性バイト
	palette  uint8 // このタイルに当たるパレット番号
	shift    uint8 // 属性バイト内のビット位置
}

// cell は 512×480 上のタイル位置の情報を返す。
func cell(snap *debug.Snapshot, tx, ty int) cellInfo {
	q := (ty/30)*2 + tx/32
	lx, ly := tx%32, ty%30
	base := q * 0x400
	i := base + ly*32 + lx
	ai := base + 0x3C0 + (ly/4)*8 + lx/4
	shift := uint8((ly%4)/2*4 + (lx%4)/2*2)
	attr := snap.Nametables[ai]
	return cellInfo{
		addr:     uint16(0x2000 + i),
		attrAddr: uint16(0x2000 + ai),
		tile:     snap.Nametables[i],
		attr:     attr,
		palette:  attr >> shift & 3,
		shift:    shift,
	}
}

// scrollOrigin はスクロール枠の左上を 512×480 上の座標で返す（設計書 09 編 §9.4.2）。
func scrollOrigin(v uint16, fineX uint8) (int, int) {
	x := int(v>>10&1)*256 + int(v&0x1F)*8 + int(fineX)
	y := int(v>>11&1)*240 + int(v>>5&0x1F)*8 + int(v>>12&7)
	return x, y
}

// draw は 4 面とオーバーレイを描く。
func (v *nametableViewer) draw() {
	if !v.src.ok {
		return
	}
	snap := &v.src.snap
	img := v.view.Image()
	bgBase := 0
	if snap.Ctrl&0x10 != 0 {
		bgBase = 0x1000
	}
	for ty := range 60 {
		for tx := range 64 {
			c := cell(snap, tx, ty)
			chr := snap.CHR[bgBase+int(c.tile)*16 : bgBase+int(c.tile)*16+16]
			for y := range 8 {
				for x := range 8 {
					px := tilePixel(chr, x, y, false, false)
					idx := snap.Palette[0]
					if px != 0 {
						idx = snap.Palette[int(c.palette)*4+int(px)]
					}
					img.SetRGBA(tx*8+x, ty*8+y, v.u.pal.Color(uint16(idx&0x3F)))
				}
			}
		}
	}
	if v.showTile {
		drawGrid(img, 8, colorTileGrid)
	}
	if v.showAttr {
		drawGrid(img, 16, colorAttrGrid)
	}
	if v.showScroll {
		x, y := scrollOrigin(snap.ScrollV, snap.FineX)
		drawRectOutline(img, image.Rect(x, y, x+256, y+240), colorScrollFrame)
	}
	v.view.Refresh()
}

// drawGrid は step ピクセルごとの格子を重ねる。
func drawGrid(img *image.RGBA, step int, c color.RGBA) {
	b := img.Bounds()
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if x%step == 0 || y%step == 0 {
				blend(img, x, y, c)
			}
		}
	}
}

// hover はマウスの下のタイルの情報を出す。
func (v *nametableViewer) hover(x, y int, ok bool) {
	if !ok || !v.src.ok {
		v.info.SetText("")
		return
	}
	c := cell(&v.src.snap, x/8, y/8)
	v.info.SetText(i18n.T(i18n.NTHover,
		c.addr, c.tile, c.attrAddr, c.attr, c.palette))
}

// editTile はタイル番号を選ぶダイアログを開き、選んだ番号を書く。
func (v *nametableViewer) editTile(x, y int) {
	if !v.src.ok || v.u.win == nil {
		return
	}
	c := cell(&v.src.snap, x/8, y/8)
	snap := v.src.snap
	bgBase := 0
	if snap.Ctrl&0x10 != 0 {
		bgBase = 0x1000
	}
	picker := newPixelView(128, 128, 3)
	img := picker.Image()
	for t := range 256 {
		chr := snap.CHR[bgBase+t*16 : bgBase+t*16+16]
		for py := range 8 {
			for px := range 8 {
				idx := snap.Palette[0]
				if p := tilePixel(chr, px, py, false, false); p != 0 {
					idx = snap.Palette[int(c.palette)*4+int(p)]
				}
				img.SetRGBA(t%16*8+px, t/16*8+py, v.u.pal.Color(uint16(idx&0x3F)))
			}
		}
	}
	picker.Refresh()
	var d dialog.Dialog
	picker.onTap = func(px, py int) {
		tile := uint8(py/8*16 + px/8)
		d.Hide()
		v.poke(int(c.addr), tile)
	}
	d = dialog.NewCustom(i18n.T(i18n.NTTileDialog, c.addr, c.tile), i18n.T(i18n.CommonCancel), picker, v.u.win)
	d.Show()
}

// editAttribute は 16×16 の属性領域のパレット番号を選ぶダイアログを開く。
func (v *nametableViewer) editAttribute(x, y int) {
	if !v.src.ok || v.u.win == nil {
		return
	}
	c := cell(&v.src.snap, x/8, y/8)
	sel := widget.NewRadioGroup([]string{"0", "1", "2", "3"}, nil)
	sel.Horizontal = true
	sel.SetSelected(fmt.Sprint(c.palette))
	dialog.ShowForm(i18n.T(i18n.NTAttrDialog, c.attrAddr), i18n.T(i18n.MenuSettings), i18n.T(i18n.CommonCancel),
		[]*widget.FormItem{widget.NewFormItem(i18n.T(i18n.ViewerPalette), sel)},
		func(ok bool) {
			if !ok || sel.Selected == "" {
				return
			}
			p := uint8(sel.Selected[0] - '0')
			attr := c.attr&^(3<<c.shift) | p<<c.shift
			v.poke(int(c.attrAddr), attr)
		}, v.u.win)
}

// poke はネームテーブルへ書き、表示を更新する。
func (v *nametableViewer) poke(addr int, value uint8) {
	if err := v.u.emu.Poke(debug.SpacePPU, addr, value, false); err != nil {
		v.u.showError(err)
		return
	}
	v.Refresh()
}

func (v *nametableViewer) OnClose() { v.src.release() }
