package ui

import (
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// aspectNumerator と aspectDenominator は NES のピクセルの縦横比。
//
// 実機のピクセルは正方形ではない。横を 8/7 倍すると実機のブラウン管に
// 近い形になる。
const (
	aspectNumerator   = 8
	aspectDenominator = 7
)

// screen はゲーム画面。
//
// image.RGBA を使い回す。毎フレーム確保すると、1 秒間に 60 回の
// 120 KiB の確保が発生する。
type screen struct {
	img *canvas.Image
	// overlay はスプライトの矩形を重ねる層。
	overlay *spriteOverlay
	// content は画像と重ねる層をまとめたもの。
	content fyne.CanvasObject
	rgba    *image.RGBA

	// frame は取り出したフレームの置き場。表示側が持つ。
	frame *video.Frame
	pal   *video.Palette

	// src は表示するフレーム上の範囲。オーバースキャンで決まる。
	src image.Rectangle

	frames   *emu.FrameBuffer
	overscan video.Overscan
	height   int
	scale    int
	aspect   bool
}

// newScreen は画面を作る。
func newScreen(frames *emu.FrameBuffer, pal *video.Palette, cfg config.VideoConfig) *screen {
	s := &screen{
		frame:  video.NewFrame(),
		pal:    pal,
		frames: frames,
		height: video.Height,
		scale:  clampScale(cfg.Scale),
		aspect: cfg.AspectRatioCorrection,
		overscan: video.Overscan{
			Top:    clampOverscan(cfg.OverscanTop),
			Bottom: clampOverscan(cfg.OverscanBottom),
			Left:   clampOverscan(cfg.OverscanLeft),
			Right:  clampOverscan(cfg.OverscanRight),
		},
	}
	s.src = s.overscan.Rect(s.height)
	s.rgba = image.NewRGBA(image.Rect(0, 0, s.src.Dx(), s.src.Dy()))

	// 起動直後は黒を表示する。
	s.frame.Clear(blackPaletteValue)
	s.pal.ApplyRect(s.frame, s.src, s.rgba)

	s.img = canvas.NewImageFromImage(s.rgba)
	// 拡大したときにドットがぼけないようにする。
	s.img.ScaleMode = canvas.ImageScalePixels
	s.img.FillMode = canvas.ImageFillContain
	s.img.SetMinSize(s.minSize())
	s.overlay = newSpriteOverlay(s.img)
	s.content = container.NewStack(s.img, s.overlay)
	return s
}

// clampScale は拡大率を設定できる範囲に収める。
func clampScale(v int) int {
	if v < config.MinScale {
		return config.MinScale
	}
	if v > config.MaxScale {
		return config.MaxScale
	}
	return v
}

// clampOverscan は隠す量を設定できる範囲に収める。
func clampOverscan(v int) int {
	if v < 0 {
		return 0
	}
	if v > config.MaxOverscan {
		return config.MaxOverscan
	}
	return v
}

// CanvasObject は画面の表示内容を返す。
func (s *screen) CanvasObject() fyne.CanvasObject { return s.content }

// setSpriteBoxes はメイン画面に重ねるスプライトの矩形を差し替える。
// 座標は NES の画面座標とする。nil で消す。
func (s *screen) setSpriteBoxes(boxes []image.Rectangle) {
	s.overlay.setBoxes(s.src, boxes)
}

// minSize は拡大率とオーバースキャンから決まる最小の大きさを返す。
func (s *screen) minSize() fyne.Size {
	w, h := s.PixelSize()
	return fyne.NewSize(float32(w), float32(h))
}

// PixelSize は拡大率を適用した表示の大きさを返す。
func (s *screen) PixelSize() (int, int) {
	w := s.src.Dx() * s.scale
	h := s.src.Dy() * s.scale
	if s.aspect {
		w = w * aspectNumerator / aspectDenominator
	}
	return w, h
}

// SetScale は拡大率を変える。
func (s *screen) SetScale(scale int) {
	s.scale = clampScale(scale)
	s.img.SetMinSize(s.minSize())
	s.img.Refresh()
}

// Scale は現在の拡大率を返す。
func (s *screen) Scale() int { return s.scale }

// SetPictureHeight は表示する画の高さを変える。
//
// リージョンによって異なるため、ROM を読み込んだときに設定する。
func (s *screen) SetPictureHeight(height int) {
	if height <= 0 || height == s.height {
		return
	}
	s.height = height
	s.resize()
}

// resize は表示範囲と描画先の画像を作り直す。
func (s *screen) resize() {
	s.src = s.overscan.Rect(s.height)
	s.rgba = image.NewRGBA(image.Rect(0, 0, s.src.Dx(), s.src.Dy()))
	s.pal.ApplyRect(s.frame, s.src, s.rgba)
	s.img.Image = s.rgba
	s.img.SetMinSize(s.minSize())
	s.img.Refresh()
}

// refresh は完成したフレームがあれば表示を更新する。
//
// UI スレッドから、垂直同期より短い間隔で呼ぶ。フレームが無いときは
// 何もしない。Refresh はダーティフラグを立てるだけで、実際の描画は
// Fyne の描画ループが垂直同期に合わせて行う。
func (s *screen) refresh() {
	if !s.frames.Take(s.frame) {
		return
	}
	s.pal.ApplyRect(s.frame, s.src, s.rgba)
	s.img.Refresh()
}

// Clear は画面を黒にする。ROM を閉じたときに呼ぶ。
//
// 閉じた後も前の画面が残ると、まだ動いているように見える。
func (s *screen) Clear() {
	s.frame.Clear(blackPaletteValue)
	s.pal.ApplyRect(s.frame, s.src, s.rgba)
	s.img.Refresh()
}

// blackPaletteValue は黒のパレット値。$0F が黒である（$0D は使わない）。
const blackPaletteValue = 0x0F

// Frame は最後に表示したフレームを返す。
func (s *screen) Frame() *video.Frame { return s.frame }

// Overscan は隠す量を返す。
func (s *screen) Overscan() video.Overscan { return s.overscan }

// PictureHeight は表示する画の高さを返す。
func (s *screen) PictureHeight() int { return s.height }
