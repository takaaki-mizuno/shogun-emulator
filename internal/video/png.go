package video

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

// Image は frame を RGBA 画像にする。
//
// overscan で隠した範囲を除いた大きさの画像を返す。画像の原点は
// (0, 0) とし、フレーム上の位置を持ち回らない。
func Image(f *Frame, p *Palette, o Overscan, height int) *image.RGBA {
	r := o.Rect(height)
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	p.ApplyRect(f, r, dst)
	return dst
}

// EncodePNG は frame を PNG のバイト列にする。
//
// セーブステートに添えるスクリーンショットが、ファイルを介さずに
// バイト列を必要とする。
func EncodePNG(f *Frame, p *Palette, o Overscan, height int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Image(f, p, o, height)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SavePNG は frame を PNG として書く。
//
// 画面をキャプチャせずフレームバッファから作る。表示の更新は非同期で
// あり、画面に出ている内容とフレームの内容が一致するとは限らない。
func SavePNG(f *Frame, p *Palette, o Overscan, height int, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(out, Image(f, p, o, height)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ScaleNearest は src を scale 倍に最近傍法で拡大する。scale が 1 以下の
// ときは src をそのまま返す。
func ScaleNearest(src *image.RGBA, scale int) *image.RGBA {
	if scale <= 1 {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
	for y := range b.Dy() {
		row := src.Pix[y*src.Stride : y*src.Stride+b.Dx()*4]
		for x := range b.Dx() {
			px := row[x*4 : x*4+4]
			for dy := range scale {
				off := (y*scale+dy)*dst.Stride + x*scale*4
				for dx := range scale {
					copy(dst.Pix[off+dx*4:off+dx*4+4], px)
				}
			}
		}
	}
	return dst
}

// EncodeImagePNG は画像を PNG のバイト列にする。
func EncodeImagePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
