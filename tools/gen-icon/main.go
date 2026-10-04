// Command gen-icon はアプリケーションのアイコンを生成する。
//
// 「将」の 1 文字を白抜きで単色背景に置いた図を作り、SVG と PNG を書く。
// 文字の輪郭はフォントから取り出す。画像編集ソフトの操作を手順として
// 残す代わりに、生成する手続きをコードに残す。
//
// 使い方:
//
//	go run ./tools/gen-icon           原本（SVG と 1024 px の PNG）から作り直す
//	go run ./tools/gen-icon -derive   1024 px の PNG から各 OS 向けの形式だけを作る
//
// 各 OS 向けの形式は assets/icon/generated/ に書く（設計書 13 編 §13.4）。
//
// SIL Open Font License のフォントを使う。輪郭はアイコンの図形として
// 埋め込まれ、フォントそのものは配布物に含まれない。
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// glyph はアイコンに置く文字。
const glyph = '将'

// fontCandidates は輪郭を取り出すフォントの候補。
//
// 先頭から順に探し、最初に見つかったものを使う。
var fontCandidates = []string{
	"~/Library/Fonts/HackGen-Bold.ttf",
	"~/Library/Fonts/HackGen-Regular.ttf",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
	"/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc",
	"/System/Library/Fonts/Hiragino Sans GB.ttc",
}

// 出力先。
const (
	svgPath    = "assets/icon/icon.svg"
	png1024    = "assets/icon/icon-1024.png"
	png256     = "assets/icon/icon.png"
	canvasSize = 1024
)

// 配色。
//
// 背景を藍色、文字を白とする。小さく表示したときに文字の形が残るよう、
// 明度の差を大きく取る。
var (
	background = color.RGBA{0x1B, 0x2A, 0x4A, 0xFF}
	foreground = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

// cornerRadiusRatio は角の丸めの半径を一辺に対する比で表したもの。
const cornerRadiusRatio = 0.18

// glyphSizeRatio は文字の大きさを一辺に対する比で表したもの。
const glyphSizeRatio = 0.72

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-icon: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	deriveOnly := flag.Bool("derive", false, "1024 px の PNG から各 OS 向けの形式だけを作る")
	flag.Parse()
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	if *deriveOnly {
		src, err := readPNG(filepath.Join(root, png1024))
		if err != nil {
			return err
		}
		return derive(src, root)
	}
	fontPath, err := findFont()
	if err != nil {
		return err
	}
	segs, err := glyphSegments(fontPath)
	if err != nil {
		return fmt.Errorf("%s: %w", fontPath, err)
	}
	path := layout(segs)

	if err := writeSVG(filepath.Join(root, svgPath), path); err != nil {
		return err
	}
	img := render(path)
	if err := writePNG(filepath.Join(root, png1024), img); err != nil {
		return err
	}
	if err := derive(img, root); err != nil {
		return err
	}
	fmt.Printf("%s を使ってアイコンを生成した\n", fontPath)
	return nil
}

// moduleRoot は go.mod のあるディレクトリを上へたどって探す。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod が見つからない")
		}
		dir = parent
	}
}

// findFont は候補から使えるフォントを探す。
func findFont() (string, error) {
	home, _ := os.UserHomeDir()
	for _, c := range fontCandidates {
		p := c
		if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("漢字を含むフォントが見つからない。候補: %s",
		strings.Join(fontCandidates, ", "))
}

// glyphSegments はフォントから文字の輪郭を取り出す。
func glyphSegments(path string) ([]sfnt.Segment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := parseFont(data)
	if err != nil {
		return nil, err
	}
	var b sfnt.Buffer
	idx, err := f.GlyphIndex(&b, glyph)
	if err != nil {
		return nil, err
	}
	if idx == 0 {
		return nil, fmt.Errorf("フォントに %c が無い", glyph)
	}
	// 単位あたりの大きさを 1000 として取り出し、後で拡大する。
	return f.LoadGlyph(&b, idx, fixed.I(1000), nil)
}

// parseFont は単体のフォントとコレクションの両方を受け付ける。
func parseFont(data []byte) (*sfnt.Font, error) {
	if f, err := sfnt.Parse(data); err == nil {
		return f, nil
	}
	c, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	return c.Font(0)
}

// point は図形上の点。
type point struct{ X, Y float64 }

// command は輪郭を描く 1 手。
type command struct {
	// op は 'M'・'L'・'Q'・'C'・'Z' のいずれか。
	op   byte
	args []point
}

// layout は輪郭を正方形の中央へ収まるように拡大・移動する。
func layout(segs []sfnt.Segment) []command {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	visit(segs, func(p point) {
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	})
	w, h := maxX-minX, maxY-minY
	if w <= 0 || h <= 0 {
		return nil
	}

	target := canvasSize * glyphSizeRatio
	scale := math.Min(target/w, target/h)
	offX := (canvasSize-w*scale)/2 - minX*scale
	offY := (canvasSize-h*scale)/2 - minY*scale

	tr := func(p point) point {
		return point{X: p.X*scale + offX, Y: p.Y*scale + offY}
	}

	var out []command
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			out = append(out, command{'M', []point{tr(fromFixed(s.Args[0]))}})
		case sfnt.SegmentOpLineTo:
			out = append(out, command{'L', []point{tr(fromFixed(s.Args[0]))}})
		case sfnt.SegmentOpQuadTo:
			out = append(out, command{'Q', []point{tr(fromFixed(s.Args[0])), tr(fromFixed(s.Args[1]))}})
		case sfnt.SegmentOpCubeTo:
			out = append(out, command{'C', []point{
				tr(fromFixed(s.Args[0])), tr(fromFixed(s.Args[1])), tr(fromFixed(s.Args[2])),
			}})
		}
	}
	return append(out, command{op: 'Z'})
}

// visit は輪郭のすべての点を辿る。
func visit(segs []sfnt.Segment, fn func(point)) {
	for _, s := range segs {
		n := 0
		switch s.Op {
		case sfnt.SegmentOpMoveTo, sfnt.SegmentOpLineTo:
			n = 1
		case sfnt.SegmentOpQuadTo:
			n = 2
		case sfnt.SegmentOpCubeTo:
			n = 3
		}
		for i := range n {
			fn(fromFixed(s.Args[i]))
		}
	}
}

// fromFixed は固定小数点の座標を実数にする。
func fromFixed(p fixed.Point26_6) point {
	return point{X: float64(p.X) / 64, Y: float64(p.Y) / 64}
}

// writeSVG は SVG を書く。
func writeSVG(path string, cmds []command) error {
	var b strings.Builder
	r := canvasSize * cornerRadiusRatio
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		canvasSize, canvasSize, canvasSize, canvasSize)
	b.WriteString("\n")
	fmt.Fprintf(&b, `  <rect width="%d" height="%d" rx="%.1f" ry="%.1f" fill="%s"/>`,
		canvasSize, canvasSize, r, r, hex(background))
	b.WriteString("\n")
	fmt.Fprintf(&b, `  <path fill="%s" fill-rule="nonzero" d="%s"/>`, hex(foreground), pathData(cmds))
	b.WriteString("\n</svg>\n")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// pathData は SVG の d 属性の文字列を作る。
func pathData(cmds []command) string {
	var b strings.Builder
	for _, c := range cmds {
		if c.op == 'Z' {
			b.WriteString("Z")
			continue
		}
		b.WriteByte(c.op)
		for i, p := range c.args {
			if i > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(&b, "%.1f %.1f", p.X, p.Y)
		}
		b.WriteString(" ")
	}
	return strings.TrimSpace(b.String())
}

// hex は色を #RRGGBB にする。
func hex(c color.RGBA) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// render は 1024 ピクセルの画像を描く。
func render(cmds []command) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, canvasSize, canvasSize))
	drawRoundedRect(dst, canvasSize*cornerRadiusRatio, background)

	r := vector.NewRasterizer(canvasSize, canvasSize)
	for _, c := range cmds {
		switch c.op {
		case 'M':
			r.MoveTo(f32(c.args[0].X), flipY(c.args[0].Y))
		case 'L':
			r.LineTo(f32(c.args[0].X), flipY(c.args[0].Y))
		case 'Q':
			r.QuadTo(f32(c.args[0].X), flipY(c.args[0].Y), f32(c.args[1].X), flipY(c.args[1].Y))
		case 'C':
			r.CubeTo(f32(c.args[0].X), flipY(c.args[0].Y),
				f32(c.args[1].X), flipY(c.args[1].Y),
				f32(c.args[2].X), flipY(c.args[2].Y))
		case 'Z':
			r.ClosePath()
		}
	}
	r.Draw(dst, dst.Bounds(), image.NewUniform(foreground), image.Point{})
	return dst
}

// f32 は実数を float32 にする。
func f32(v float64) float32 { return float32(v) }

// flipY は上下を反転する。
//
// フォントの座標は上が正であり、画像の座標は下が正である。
func flipY(v float64) float32 { return float32(canvasSize - v) }

// drawRoundedRect は角の丸い長方形で塗りつぶす。
func drawRoundedRect(dst *image.RGBA, radius float64, c color.RGBA) {
	r := vector.NewRasterizer(canvasSize, canvasSize)
	n, s := float32(0), float32(canvasSize)
	rr := float32(radius)
	r.MoveTo(rr, n)
	r.LineTo(s-rr, n)
	r.QuadTo(s, n, s, rr)
	r.LineTo(s, s-rr)
	r.QuadTo(s, s, s-rr, s)
	r.LineTo(rr, s)
	r.QuadTo(n, s, n, s-rr)
	r.LineTo(n, rr)
	r.QuadTo(n, n, rr, n)
	r.ClosePath()
	r.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

// writePNG は画像を PNG として書く。
func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
