package scenario

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// 画面の比較（設計書 14 編 §14.17.2）。
//
// obs.screenshot は常に既定のパレットで描く。お手本も既定のパレットで書く
// ため、お手本の各画素が既定のパレットの色であることを確かめてから画素
// ごとに比べれば、パレットインデックスとエンファシスで比べたことになる。
// 利用者がパレットの設定を変えても結果は変わらない。

// rgb は透明度を除いた色。
type rgb struct{ r, g, b uint8 }

var (
	paletteOnce   sync.Once
	paletteColors map[rgb]bool
)

// defaultColors は既定のパレットの 512 通り（パレットインデックス 64 ×
// エンファシス 8）の色を返す。
func defaultColors() map[rgb]bool {
	paletteOnce.Do(func() {
		p := video.DefaultPalette()
		paletteColors = map[rgb]bool{}
		for v := uint16(0); v < video.ColorCount*video.EmphasisCount; v++ {
			c := p.Color(v)
			paletteColors[rgb{c.R, c.G, c.B}] = true
		}
	})
	return paletteColors
}

func rgbAt(img image.Image, x, y int) rgb {
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	return rgb{c.R, c.G, c.B}
}

// actualPath は実際の画面を書くパスを返す（<お手本>.actual.png）。
func actualPath(golden string) string {
	return strings.TrimSuffix(golden, filepath.Ext(golden)) + ".actual.png"
}

// assertScreen は assert_screen を評価する。
func (x *run) assertScreen(st Step) *Failure {
	var shot agent.ScreenshotResult
	if err := x.call("obs.screenshot", map[string]any{"scale": 1}, &shot); err != nil {
		return &Failure{Message: callErr("obs.screenshot", err)}
	}
	if shot.Image == nil {
		return &Failure{Message: "画面がまだ無い（フレームを進めてから比べる）"}
	}
	actualPNG := shot.Image.Data
	actual, err := png.Decode(bytes.NewReader(actualPNG))
	if err != nil {
		return &Failure{Message: fmt.Sprintf("画面を読めない: %v", err)}
	}
	golden := st.Screen.Golden
	reason := compareGolden(golden, actual, st.Screen.MaxDiffPixels)
	if reason == "" {
		return nil
	}
	if x.r.opts.UpdateGolden {
		if err := writeFile(golden, actualPNG); err != nil {
			return &Failure{Message: fmt.Sprintf("お手本を書けない: %v", err)}
		}
		x.res.Updated = append(x.res.Updated, golden)
		return nil
	}
	f := &Failure{Message: reason, Expr: filepath.Base(golden)}
	ap := actualPath(golden)
	if err := writeFile(ap, actualPNG); err == nil {
		x.res.Actual = append(x.res.Actual, ap)
		f.Actual = "実際の画面: " + ap
	}
	return f
}

// compareGolden はお手本と画面を比べ、一致しない理由を返す。一致したとき空。
func compareGolden(golden string, actual image.Image, maxDiff int) string {
	data, err := os.ReadFile(golden)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("お手本 %s が無い（shogun run --update-golden で今の画面から作る）", golden)
	}
	if err != nil {
		return fmt.Sprintf("お手本を読めない: %v", err)
	}
	want, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Sprintf("お手本 %s を PNG として読めない: %v", golden, err)
	}
	wb, ab := want.Bounds(), actual.Bounds()
	if wb.Dx() != ab.Dx() || wb.Dy() != ab.Dy() {
		return fmt.Sprintf("お手本の大きさ %dx%d が画面の大きさ %dx%d と違う", wb.Dx(), wb.Dy(), ab.Dx(), ab.Dy())
	}
	colors := defaultColors()
	diff, firstX, firstY := 0, -1, -1
	for y := 0; y < wb.Dy(); y++ {
		for x := 0; x < wb.Dx(); x++ {
			w := rgbAt(want, wb.Min.X+x, wb.Min.Y+y)
			if !colors[w] {
				return fmt.Sprintf("お手本の (%d, %d) の色 #%02X%02X%02X は既定のパレットに無い（お手本は既定のパレットで書く）", x, y, w.r, w.g, w.b)
			}
			if w != rgbAt(actual, ab.Min.X+x, ab.Min.Y+y) {
				if diff == 0 {
					firstX, firstY = x, y
				}
				diff++
			}
		}
	}
	if diff > maxDiff {
		return fmt.Sprintf("%d 画素が一致しない（許容 %d、最初は (%d, %d)）", diff, maxDiff, firstX, firstY)
	}
	return ""
}

// writeFile はディレクトリを作ってから書く。
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
