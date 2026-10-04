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

// 生成物の置き場所（設計書 13 編 §13.4）。モジュールのルートからの相対パス。
const (
	generatedDir = "assets/icon/generated"
	icnsPath     = generatedDir + "/icon.icns"
	icoPath      = generatedDir + "/icon.ico"
	linuxDir     = generatedDir + "/linux"
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

// derive は 1024 ピクセルの原本から各 OS 向けの形式を作り、root の下へ書く。
//
// 原本の SVG を作り直さないため、字形を取り出すフォントが無い環境でも使える。
func derive(src image.Image, root string) error {
	scaled := map[int][]byte{}
	pngOf := func(size int) ([]byte, error) {
		if b, ok := scaled[size]; ok {
			return b, nil
		}
		b, err := encodePNG(resize(src, size))
		if err != nil {
			return nil, err
		}
		scaled[size] = b
		return b, nil
	}

	runtime, err := pngOf(256)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, png256), runtime); err != nil {
		return err
	}
	for _, size := range linuxSizes {
		b, err := pngOf(size)
		if err != nil {
			return err
		}
		if err := writeFile(filepath.Join(root, linuxDir, fmt.Sprintf("icon-%d.png", size)), b); err != nil {
			return err
		}
	}

	var icoImages [][]byte
	for _, size := range icoSizes {
		b, err := pngOf(size)
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
		b, err := pngOf(e.size)
		if err != nil {
			return err
		}
		kinds = append(kinds, e.kind)
		icnsImages = append(icnsImages, b)
	}
	return writeFile(filepath.Join(root, icnsPath), buildICNS(kinds, icnsImages))
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
