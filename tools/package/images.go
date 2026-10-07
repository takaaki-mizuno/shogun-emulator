package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

// logoSource はインストーラの画像に置くロゴ（設計書 13 編 §13.4）。
const logoSource = "assets/icon/logo.png"

// 配色。矢印はロゴの原画の青に合わせる。.dmg の背景を明るくするのは、
// Finder がアイコンの名前を黒い文字で描くためである。
var (
	colorBackgroundTop    = color.NRGBA{0xF7, 0xF9, 0xFC, 0xFF}
	colorBackgroundBottom = color.NRGBA{0xDD, 0xE6, 0xF2, 0xFF}
	colorArrow            = color.NRGBA{0x1E, 0x90, 0xE0, 0xFF}
	colorWhite            = color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	colorBlack            = color.NRGBA{0x00, 0x00, 0x00, 0xFF}
)

// readLogo はロゴを読む。
func readLogo(root string) (image.Image, error) {
	f, err := os.Open(filepath.Join(root, logoSource))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// dmgBackground は .dmg のウィンドウの背景を scale 倍の大きさで描く
// （設計書 13 編 §13.5）。明るいグラデーションに、.app の位置から
// Applications の位置へ向かう矢印を置く。
func dmgBackground(scale int) *image.NRGBA {
	w, h := dmgWindowWidth*scale, dmgWindowHeight*scale
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		c := lerpColor(colorBackgroundTop, colorBackgroundBottom, float64(y)/float64(h-1))
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}

	// 矢印。2 つのアイコンの間に、軸と三角の頭を置く。
	s := float32(scale)
	y := float32(dmgIconY) * s
	x0 := float32(dmgAppX+dmgArrowGap) * s
	x1 := float32(dmgApplicationsX-dmgArrowGap) * s
	shaft, head := 6*s, 20*s
	r := vector.NewRasterizer(w, h)
	r.MoveTo(x0, y-shaft/2)
	r.LineTo(x1-head, y-shaft/2)
	r.LineTo(x1-head, y-head)
	r.LineTo(x1, y)
	r.LineTo(x1-head, y+head)
	r.LineTo(x1-head, y+shaft/2)
	r.LineTo(x0, y+shaft/2)
	r.ClosePath()
	r.Draw(img, img.Bounds(), image.NewUniform(colorArrow), image.Point{})
	return img
}

// lerpColor は a から b へ t（0〜1）の割合で混ぜた色を返す。
func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	mix := func(p, q uint8) uint8 { return uint8(float64(p) + (float64(q)-float64(p))*t + 0.5) }
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 0xFF}
}

// WiX の画面の画像の大きさ（設計書 13 編 §13.6）。
const (
	wixBannerWidth  = 493
	wixBannerHeight = 58
	wixDialogWidth  = 493
	wixDialogHeight = 312
	// wixDialogPanel はようこそ・完了の画面で画像が見える左側の幅。
	// 右側には文言が重なるため白にする。
	wixDialogPanel = 164
)

// wixBanner は WiX の各画面の上部の帯を作る。白地の右端にロゴを置く。
// 左側には画面の見出しの文言が重なる。
func wixBanner(logo image.Image) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, wixBannerWidth, wixBannerHeight))
	fill(img, img.Bounds(), colorWhite)
	n := wixBannerHeight - 8
	at := image.Rect(wixBannerWidth-n-6, 4, wixBannerWidth-6, 4+n)
	draw.CatmullRom.Scale(img, at, logo, logo.Bounds(), draw.Over, nil)
	return encodeBMP(img)
}

// wixDialog は WiX のようこそ・完了の画面の背景を作る。左側を黒にして
// ロゴを置き、右側を白にする。
func wixDialog(logo image.Image) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, wixDialogWidth, wixDialogHeight))
	fill(img, img.Bounds(), colorWhite)
	fill(img, image.Rect(0, 0, wixDialogPanel, wixDialogHeight), colorBlack)
	n := wixDialogPanel - 16
	top := (wixDialogHeight - n) / 2
	at := image.Rect(8, top, 8+n, top+n)
	draw.CatmullRom.Scale(img, at, logo, logo.Bounds(), draw.Over, nil)
	return encodeBMP(img)
}

// fill は矩形を 1 色で塗る。
func fill(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

// encodeBMP は不透明な画像を 24 ビットの BMP にする。
func encodeBMP(img image.Image) []byte {
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	var buf bytes.Buffer
	// bmp.Encode は *image.RGBA が不透明なとき 24 ビットで書く。失敗するのは
	// 書き込み先のエラーだけであり、bytes.Buffer では起こらない。
	_ = bmp.Encode(&buf, rgba)
	return buf.Bytes()
}

// encodePNG は画像を PNG にする。
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
