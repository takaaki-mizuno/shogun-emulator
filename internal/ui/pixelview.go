package ui

import (
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"image"
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// pixelView は image.RGBA を整数倍に拡大して表示し、マウスの位置を画像上の
// ピクセル座標に直して渡すウィジェット。
//
// パターンテーブル・ネームテーブル・スプライト・パレットの各ビューアと
// タイルのピクセルエディタが使う。
type pixelView struct {
	widget.BaseWidget
	img  *canvas.Image
	rgba *image.RGBA
	zoom int

	// onHover は画像上の位置を受け取る。外へ出たとき ok が false。
	onHover func(x, y int, ok bool)
	// onTap と onSecondary はクリックした位置を受け取る。
	onTap       func(x, y int)
	onSecondary func(x, y int)
	// onDrag はドラッグ中の位置と、前回からの移動量（画像のピクセル単位）を受け取る。
	onDrag func(x, y int, dx, dy float32)
	// onDragEnd はドラッグを終えたときに呼ばれる。
	onDragEnd func()
}

// newPixelView は w×h の画像を zoom 倍で表示するウィジェットを作る。
func newPixelView(w, h, zoom int) *pixelView {
	v := &pixelView{rgba: image.NewRGBA(image.Rect(0, 0, w, h))}
	v.img = canvas.NewImageFromImage(v.rgba)
	v.img.ScaleMode = canvas.ImageScalePixels
	v.img.FillMode = canvas.ImageFillStretch
	v.ExtendBaseWidget(v)
	v.SetZoom(zoom)
	return v
}

// CreateRenderer は画像をそのまま描く。
func (v *pixelView) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(v.img) }

// MinSize は拡大した画像の大きさ。
func (v *pixelView) MinSize() fyne.Size {
	b := v.rgba.Bounds()
	return fyne.NewSize(float32(b.Dx()*v.zoom), float32(b.Dy()*v.zoom))
}

// SetZoom は拡大率を変える。
func (v *pixelView) SetZoom(zoom int) {
	v.zoom = max(zoom, 1)
	v.Refresh()
}

// Image は描画先の画像を返す。描いたら Refresh を呼ぶ。
func (v *pixelView) Image() *image.RGBA { return v.rgba }

// Refresh は画像を描き直す。
func (v *pixelView) Refresh() {
	v.img.Refresh()
	v.BaseWidget.Refresh()
}

// toPixel はウィジェット上の位置を画像のピクセル座標に直す。
func (v *pixelView) toPixel(p fyne.Position) (int, int, bool) {
	b := v.rgba.Bounds()
	size := v.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return 0, 0, false
	}
	x := int(p.X * float32(b.Dx()) / size.Width)
	y := int(p.Y * float32(b.Dy()) / size.Height)
	if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
		return 0, 0, false
	}
	return x, y, true
}

// scale は画像の 1 ピクセルがウィジェット上で占める大きさ。
func (v *pixelView) scale() (float32, float32) {
	b := v.rgba.Bounds()
	size := v.Size()
	return size.Width / float32(b.Dx()), size.Height / float32(b.Dy())
}

// MouseIn はマウスが入ったときの位置を渡す。
func (v *pixelView) MouseIn(e *desktop.MouseEvent) { v.MouseMoved(e) }

// MouseMoved はマウスの位置を渡す。
func (v *pixelView) MouseMoved(e *desktop.MouseEvent) {
	if v.onHover == nil {
		return
	}
	x, y, ok := v.toPixel(e.Position)
	v.onHover(x, y, ok)
}

// MouseOut は外へ出たことを渡す。
func (v *pixelView) MouseOut() {
	if v.onHover != nil {
		v.onHover(0, 0, false)
	}
}

// Tapped はクリックした位置を渡す。
func (v *pixelView) Tapped(e *fyne.PointEvent) {
	if x, y, ok := v.toPixel(e.Position); ok && v.onTap != nil {
		v.onTap(x, y)
	}
}

// TappedSecondary は右クリックした位置を渡す。
func (v *pixelView) TappedSecondary(e *fyne.PointEvent) {
	if x, y, ok := v.toPixel(e.Position); ok && v.onSecondary != nil {
		v.onSecondary(x, y)
	}
}

// Dragged はドラッグ中の位置と移動量を渡す。
func (v *pixelView) Dragged(e *fyne.DragEvent) {
	if v.onDrag == nil {
		return
	}
	sx, sy := v.scale()
	x, y, ok := v.toPixel(e.Position)
	if !ok {
		x, y = -1, -1
	}
	v.onDrag(x, y, e.Dragged.DX/sx, e.Dragged.DY/sy)
}

// DragEnd はドラッグを終えたことを渡す。
func (v *pixelView) DragEnd() {
	if v.onDragEnd != nil {
		v.onDragEnd()
	}
}

// fill は画像の矩形を 1 色で塗る。
func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// blend は画像の 1 ピクセルに半透明の色を重ねる。
func blend(img *image.RGBA, x, y int, c color.RGBA) {
	if !(image.Point{x, y}).In(img.Bounds()) {
		return
	}
	o := img.RGBAAt(x, y)
	a := uint32(c.A)
	mix := func(dst, src uint8) uint8 { return uint8((uint32(dst)*(255-a) + uint32(src)*a) / 255) }
	img.SetRGBA(x, y, color.RGBA{mix(o.R, c.R), mix(o.G, c.G), mix(o.B, c.B), 255})
}

// tilePixel は CHR のタイルの (x, y) の色番号（0-3）を返す。
//
// chr はタイルの先頭から 16 バイト。前半 8 バイトが下位プレーン、後半が上位プレーン。
func tilePixel(chr []uint8, x, y int, flipH, flipV bool) uint8 {
	if flipH {
		x = 7 - x
	}
	if flipV {
		y = 7 - y
	}
	shift := 7 - x
	lo := chr[y] >> shift & 1
	hi := chr[y+8] >> shift & 1
	return hi<<1 | lo
}

// greyLevels はパレットを当てないときの 4 階調。
var greyLevels = [4]color.RGBA{
	{0, 0, 0, 255}, {85, 85, 85, 255}, {170, 170, 170, 255}, {255, 255, 255, 255},
}

// snapshotSource はビューアがスナップショットを購読する位置を持つ。
//
// 取得位置はビューアごとに独立して持つ（設計書 09 編 §9.3.1）。
type snapshotSource struct {
	u    *UI
	set  *debug.SnapshotSet
	line int
	snap debug.Snapshot
	ok   bool
}

// acquire は購読を始める。
func (s *snapshotSource) acquire() {
	if s.set != nil {
		return
	}
	s.u.emu.WithDebugger(func(d *debug.Debugger) { s.set = d.AcquireSnapshots(s.line) })
}

// release は購読をやめる。
func (s *snapshotSource) release() {
	if s.set == nil {
		return
	}
	set := s.set
	s.set = nil
	s.u.emu.WithDebugger(func(d *debug.Debugger) { d.ReleaseSnapshots(set) })
}

// setLine は取得位置を変える。
func (s *snapshotSource) setLine(line int) {
	if line == s.line {
		return
	}
	s.line = line
	if s.set != nil {
		s.release()
		s.acquire()
	}
}

// latest は直近のスナップショットを読む。無いとき false。
func (s *snapshotSource) latest() bool {
	if s.set == nil {
		return false
	}
	s.ok = s.set.Latest(&s.snap)
	return s.ok
}

// lineSelector は取得位置を選ぶ入力欄を作る。空欄でフレーム末とする。
func (s *snapshotSource) lineSelector(onChange func()) fyne.CanvasObject {
	e := widget.NewEntry()
	e.SetPlaceHolder(i18n.T(i18n.SnapFrameEnd))
	if s.line >= 0 {
		e.SetText(strconv.Itoa(s.line))
	}
	e.OnSubmitted = func(text string) {
		line := -1
		if n, err := strconv.Atoi(text); err == nil && n >= 0 && n < 240 {
			line = n
		} else {
			e.SetText("")
		}
		s.setLine(line)
		if onChange != nil {
			onChange()
		}
	}
	label := widget.NewLabel(i18n.T(i18n.SnapLine))
	return container2(label, e)
}
