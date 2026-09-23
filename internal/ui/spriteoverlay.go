package ui

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// colorSpriteBox はメイン画面に重ねるスプライトの矩形の色。
var colorSpriteBox = color.NRGBA{R: 0xFF, G: 0x30, B: 0x30, A: 0xD0}

// spriteOverlay はメイン画面の上にスプライトの矩形を重ねるウィジェット
// （設計書 09 編 §9.4.3）。
//
// 矩形は NES の画面座標で受け取り、画像の範囲へ写す。
type spriteOverlay struct {
	widget.BaseWidget
	img *canvas.Image
	// src は表示しているフレーム上の範囲（オーバースキャンで決まる）。
	src   image.Rectangle
	boxes []image.Rectangle
}

// newSpriteOverlay は img の上に重ねるウィジェットを作る。
func newSpriteOverlay(img *canvas.Image) *spriteOverlay {
	o := &spriteOverlay{img: img}
	o.ExtendBaseWidget(o)
	return o
}

// setBoxes は重ねる矩形を差し替える。nil で消す。
func (o *spriteOverlay) setBoxes(src image.Rectangle, boxes []image.Rectangle) {
	o.src = src
	o.boxes = append(o.boxes[:0], boxes...)
	o.Refresh()
}

// CreateRenderer は矩形の描画を作る。
func (o *spriteOverlay) CreateRenderer() fyne.WidgetRenderer {
	return &spriteOverlayRenderer{o: o}
}

// spriteOverlayRenderer は矩形を canvas.Rectangle で描く。
type spriteOverlayRenderer struct {
	o     *spriteOverlay
	rects []fyne.CanvasObject
}

func (r *spriteOverlayRenderer) Destroy() {}

func (r *spriteOverlayRenderer) MinSize() fyne.Size { return fyne.NewSize(0, 0) }

func (r *spriteOverlayRenderer) Objects() []fyne.CanvasObject { return r.rects }

func (r *spriteOverlayRenderer) Refresh() {
	for len(r.rects) < len(r.o.boxes) {
		rect := canvas.NewRectangle(color.Transparent)
		rect.StrokeColor = colorSpriteBox
		rect.StrokeWidth = 1
		r.rects = append(r.rects, rect)
	}
	r.rects = r.rects[:len(r.o.boxes)]
	r.Layout(r.o.Size())
	canvas.Refresh(r.o)
}

// Layout は矩形を画像の範囲へ写す。重ねる層は画像と同じ矩形に置かれ、
// 画像はその矩形いっぱいに描かれる（screenLayout）。
func (r *spriteOverlayRenderer) Layout(size fyne.Size) {
	o := r.o
	if o.src.Empty() || size.Width <= 0 || size.Height <= 0 {
		return
	}
	sx, sy := size.Width/float32(o.src.Dx()), size.Height/float32(o.src.Dy())
	for i, b := range o.boxes {
		rect := r.rects[i]
		b = b.Sub(o.src.Min)
		rect.Move(fyne.NewPos(float32(b.Min.X)*sx, float32(b.Min.Y)*sy))
		rect.Resize(fyne.NewSize(float32(b.Dx())*sx, float32(b.Dy())*sy))
	}
}
