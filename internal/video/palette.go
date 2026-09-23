package video

import (
	"fmt"
	"image"
	"image/color"
	"os"

	"github.com/takaakimizuno/shogun-emulator/assets"
)

// ColorCount は PPU の色数。
const ColorCount = 64

// EmphasisCount はエンファシスの組み合わせの数。赤・緑・青の 3 bit。
const EmphasisCount = 8

// パレットファイルの大きさ。
const (
	// BaseFileSize は 64 色分。エンファシスは計算で求める。
	BaseFileSize = ColorCount * 3
	// FullFileSize は 512 色分。エンファシスも含めて記述したもの。
	FullFileSize = ColorCount * EmphasisCount * 3
)

// Palette はパレット値とエンファシスから RGBA への変換表。
//
// 表を先に作るのは、1 フレームで 61440 回引くためである。引くたびに
// 減衰を計算すると浮動小数点の乗算が毎ピクセル 3 回発生する。
type Palette struct {
	table [ColorCount * EmphasisCount]color.RGBA
}

// attenuation はエンファシスで強調しなかった成分に掛ける率。
//
// エンファシスは強調した成分を明るくするのではなく、強調しなかった
// 成分を暗くする。3 bit をすべて立てると全成分が暗くなる。
const attenuation = 0.746

// DefaultPalette は組み込みの既定パレットを返す。
//
// 埋め込んだデータが壊れていることは起こり得ないため、エラーを返さない。
// 万一読めないときは全色を黒にした表を返し、画面が黒くなることで異常を
// 知らせる。パレットを理由に起動できない状態を作らない。
func DefaultPalette() *Palette {
	p, err := LoadPalette(assets.DefaultPalette)
	if err != nil {
		return &Palette{}
	}
	return p
}

// LoadPalette は .pal 形式のデータから変換表を作る。
//
// 192 バイト（64 色）と 1536 バイト（512 色）を受け付ける。512 色の
// 並びは、エンファシスの値を上位として 64 色ずつ 8 組とする。
// エンファシスの bit 0 が赤、1 が緑、2 が青である。
func LoadPalette(data []uint8) (*Palette, error) {
	switch len(data) {
	case BaseFileSize:
		return paletteFromBase(data), nil
	case FullFileSize:
		return paletteFromFull(data), nil
	default:
		return nil, fmt.Errorf("video: パレットファイルの大きさが %d バイトである（%d か %d を期待）",
			len(data), BaseFileSize, FullFileSize)
	}
}

// LoadPaletteFile はパレットファイルを読んで変換表を作る。
func LoadPaletteFile(path string) (*Palette, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := LoadPalette(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// paletteFromBase は 64 色からエンファシス込みの表を作る。
func paletteFromBase(data []uint8) *Palette {
	var p Palette
	for e := range EmphasisCount {
		for c := range ColorCount {
			r := data[c*3+0]
			g := data[c*3+1]
			b := data[c*3+2]
			p.table[e*ColorCount+c] = color.RGBA{
				R: applyEmphasis(r, uint8(e), 0),
				G: applyEmphasis(g, uint8(e), 1),
				B: applyEmphasis(b, uint8(e), 2),
				A: 0xFF,
			}
		}
	}
	return &p
}

// paletteFromFull は 512 色をそのまま表にする。
func paletteFromFull(data []uint8) *Palette {
	var p Palette
	for i := range p.table {
		p.table[i] = color.RGBA{
			R: data[i*3+0],
			G: data[i*3+1],
			B: data[i*3+2],
			A: 0xFF,
		}
	}
	return &p
}

// applyEmphasis は成分 1 つにエンファシスの減衰を適用する。
//
// component は 0 が赤、1 が緑、2 が青。自分以外のビットが 1 つでも
// 立っていれば減衰する。立っている数で重ねない。
func applyEmphasis(v uint8, emphasis uint8, component uint8) uint8 {
	others := emphasis &^ (1 << component)
	if others == 0 {
		return v
	}
	// 四捨五入する。切り捨てると減衰のない色との差が 1 段大きくなる。
	return uint8(float64(v)*attenuation + 0.5)
}

// Color はフレームのピクセル値に対応する色を返す。
func (p *Palette) Color(v uint16) color.RGBA {
	return p.table[v&(PaletteMask|EmphasisMask)]
}

// Apply は frame の内容を dst へ書く。
//
// dst の境界をフレーム上の範囲として扱う。オーバースキャンで隠した
// 範囲を dst の境界で表し、変換の対象から外すためである。
func (p *Palette) Apply(f *Frame, dst *image.RGBA) {
	p.ApplyRect(f, dst.Bounds(), dst)
}

// ApplyRect は frame の src の範囲を dst の先頭から書く。
//
// dst の原点を src に合わせる必要がない。表示側は原点が (0, 0) の
// 画像を使い回し、切り取る位置だけを変えられる。
func (p *Palette) ApplyRect(f *Frame, src image.Rectangle, dst *image.RGBA) {
	w, h := src.Dx(), src.Dy()
	if dst.Rect.Dx() < w || dst.Rect.Dy() < h {
		return
	}
	for y := range h {
		row := dst.Pix[y*dst.Stride:]
		for x := range w {
			c := p.Color(f.At(src.Min.X+x, src.Min.Y+y))
			i := x * 4
			row[i+0] = c.R
			row[i+1] = c.G
			row[i+2] = c.B
			row[i+3] = c.A
		}
	}
}
