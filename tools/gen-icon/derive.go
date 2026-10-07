package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

// 原本と生成物の置き場所（設計書 13 編 §13.4）。モジュールのルートからの相対パス。
const (
	sourcePath   = "assets/icon.png"
	png256       = "assets/icon/icon.png"
	macOSPNG     = "assets/icon/icon-macos.png"
	logoPNG      = "assets/icon/logo.png"
	generatedDir = "assets/icon/generated"
	icnsPath     = generatedDir + "/icon.icns"
	icoPath      = generatedDir + "/icon.ico"
	linuxDir     = generatedDir + "/linux"
)

// macOS のタイルの寸法（1024 px の枠に対する値）。
const (
	tileCanvas = 1024
	tileSize   = 824
	tileRadius = 185
)

// 実行時のアイコンとロゴの大きさ。
const (
	runtimeSize = 256
	macOSSize   = 512
	logoSize    = 512
)

// linuxSizes は Linux 向けに書く PNG の大きさ。
var linuxSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// icoSizes は Windows の .ico に含める大きさ。
var icoSizes = []int{16, 24, 32, 48, 64, 128, 256}

// icnsElements は macOS の .icns に含める要素の種類と大きさ（ピクセル）。
// iconutil の iconset と同じ 10 要素とする。@2x の要素（ic10〜ic14）は、
// 表示上の大きさの 2 倍のピクセル数を持つ。icp6 は版によって 48 と 64 の
// どちらとも解釈されるため使わず、64 ピクセルは ic12（32 の @2x）で表す。
var icnsElements = []struct {
	kind string
	size int
}{
	{"icp4", 16}, {"ic11", 32}, {"icp5", 32}, {"ic12", 64}, {"ic07", 128},
	{"ic13", 256}, {"ic08", 256}, {"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
}

// derive は原画から各 OS 向けの形式とロゴを作り、root の下へ書く。
//
// Windows と Linux は原画をそのまま縮小した正方形、macOS は角丸のタイルとする。
func derive(src image.Image, root string) error {
	square := squareOf(src)
	tile := macOSTile(square)
	cache := map[*image.NRGBA]map[int][]byte{square: {}, tile: {}}
	pngOf := func(img *image.NRGBA, size int) ([]byte, error) {
		if b, ok := cache[img][size]; ok {
			return b, nil
		}
		b, err := encodePNG(resize(img, size))
		if err != nil {
			return nil, err
		}
		cache[img][size] = b
		return b, nil
	}

	singles := []struct {
		path string
		img  *image.NRGBA
		size int
	}{
		{png256, square, runtimeSize},
		{macOSPNG, tile, macOSSize},
		{logoPNG, square, logoSize},
	}
	for _, f := range singles {
		b, err := pngOf(f.img, f.size)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(root, f.path), b); err != nil {
			return err
		}
	}
	for _, size := range linuxSizes {
		b, err := pngOf(square, size)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(root, linuxDir, fmt.Sprintf("icon-%d.png", size)), b); err != nil {
			return err
		}
	}

	var icoImages [][]byte
	for _, size := range icoSizes {
		b, err := pngOf(square, size)
		if err != nil {
			return err
		}
		icoImages = append(icoImages, b)
	}
	if err := writeFile(filepath.Join(root, icoPath), buildICO(icoSizes, icoImages)); err != nil {
		return err
	}

	var kinds []string
	var icnsImages [][]byte
	for _, e := range icnsElements {
		b, err := pngOf(tile, e.size)
		if err != nil {
			return err
		}
		kinds = append(kinds, e.kind)
		icnsImages = append(icnsImages, b)
	}
	return writeFile(filepath.Join(root, icnsPath), buildICNS(kinds, icnsImages))
}

// squareOf は原画の中央の正方形を NRGBA で返す。原画が正方形でないときは
// 短い辺に合わせて切り出す。
func squareOf(src image.Image) *image.NRGBA {
	b := src.Bounds()
	n := min(b.Dx(), b.Dy())
	origin := image.Pt(b.Min.X+(b.Dx()-n)/2, b.Min.Y+(b.Dy()-n)/2)
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	draw.Draw(dst, dst.Bounds(), src, origin, draw.Src)
	return dst
}

// macOSTile は 1024 px の枠の中央に、角丸の正方形として原画を置いた画像を作る。
// 外側は透明にする。
func macOSTile(square *image.NRGBA) *image.NRGBA {
	inner := resize(square, tileSize)
	dst := image.NewNRGBA(image.Rect(0, 0, tileCanvas, tileCanvas))
	off := (tileCanvas - tileSize) / 2
	for y := range tileSize {
		for x := range tileSize {
			cov := roundedCoverage(x, y, tileSize, tileRadius)
			if cov == 0 {
				continue
			}
			c := inner.NRGBAAt(x, y)
			c.A = uint8((int(c.A)*cov + roundedSamples*roundedSamples/2) / (roundedSamples * roundedSamples))
			dst.SetNRGBA(off+x, off+y, c)
		}
	}
	return dst
}

// roundedSamples は角の透明度を求めるときの 1 画素の分割数（一辺あたり）。
const roundedSamples = 4

// roundedCoverage は一辺 size・角の半径 r の角丸の正方形について、画素 (x, y)
// を roundedSamples×roundedSamples に分けた点のうち内側にある数を返す。
func roundedCoverage(x, y, size, r int) int {
	n := 0
	for sy := range roundedSamples {
		for sx := range roundedSamples {
			px := float64(x) + (float64(sx)+0.5)/roundedSamples
			py := float64(y) + (float64(sy)+0.5)/roundedSamples
			if insideRounded(px, py, float64(size), float64(r)) {
				n++
			}
		}
	}
	return n
}

// insideRounded は点 (px, py) が角丸の正方形の内側にあるかを返す。
func insideRounded(px, py, size, r float64) bool {
	cx := min(max(px, r), size-r)
	cy := min(max(py, r), size-r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// resize は画像を size ピクセル四方へ Catmull-Rom 補間で縮小する。
func resize(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// encodePNG は画像を PNG にする。圧縮の度合いを固定し、同じ画像から同じ
// バイト列を作る。
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildICO は PNG の要素を並べた .ico を作る。
//
// ICONDIR（6 バイト）、要素ごとの ICONDIRENTRY（16 バイト）、PNG の本体の順に
// 並べる。幅と高さの 256 は 0 で表す。
func buildICO(sizes []int, images [][]byte) []byte {
	var buf bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&buf, le, [3]uint16{0, 1, uint16(len(images))})
	offset := 6 + 16*len(images)
	for i, img := range images {
		dim := uint8(sizes[i])
		if sizes[i] >= 256 {
			dim = 0
		}
		buf.Write([]byte{dim, dim, 0, 0})
		binary.Write(&buf, le, [2]uint16{1, 32})
		binary.Write(&buf, le, [2]uint32{uint32(len(img)), uint32(offset)})
		offset += len(img)
	}
	for _, img := range images {
		buf.Write(img)
	}
	return buf.Bytes()
}

// buildICNS は PNG の要素を並べた .icns を作る。
//
// 先頭に "icns" と全体の長さ、続いて要素ごとに 4 文字の種類と要素の長さ
// （8 バイトの見出しを含む）と PNG の本体を置く。数値はビッグエンディアン。
func buildICNS(kinds []string, images [][]byte) []byte {
	var body bytes.Buffer
	be := binary.BigEndian
	for i, img := range images {
		body.WriteString(kinds[i])
		binary.Write(&body, be, uint32(8+len(img)))
		body.Write(img)
	}
	var buf bytes.Buffer
	buf.WriteString("icns")
	binary.Write(&buf, be, uint32(8+body.Len()))
	buf.Write(body.Bytes())
	return buf.Bytes()
}

// readPNG は PNG を読む。
func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// writeFile はディレクトリを作ってから書く。
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
