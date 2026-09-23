// Command gen-palette は調査文書の表から既定のパレットファイルを生成する。
//
// 色の値を Go のソースに書き写さず、`docs/research/03_ppu.md` の 10.3 節の
// 表を唯一の出所とする。表を直せば再生成で反映される。
//
// 使い方:
//
//	go run ./tools/gen-palette
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// sourceDoc は色の表を持つ文書。
const sourceDoc = "docs/research/03_ppu.md"

// outputFile は生成するパレットファイル。
const outputFile = "assets/palettes/default.pal"

// sectionHeading は表を含む節の見出し。
const sectionHeading = "### 10.3"

// colorCount は 2C02 の色数。
const colorCount = 64

// rowPattern は表の 1 行から行頭のアドレスと 16 個の色を取り出す。
var rowPattern = regexp.MustCompile(`^\|\s*\$([0-9A-F]0)\s*\|(.*)\|\s*$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-palette: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(root, sourceDoc))
	if err != nil {
		return err
	}
	rgb, err := parseTable(string(b))
	if err != nil {
		return fmt.Errorf("%s: %w", sourceDoc, err)
	}
	dst := filepath.Join(root, outputFile)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, rgb, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s を書いた（%d バイト、%d 色）\n", outputFile, len(rgb), len(rgb)/3)
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

// parseTable は節の中の表を読み、64 色 × 3 バイトを返す。
func parseTable(doc string) ([]uint8, error) {
	idx := strings.Index(doc, sectionHeading)
	if idx < 0 {
		return nil, fmt.Errorf("節 %q が見つからない", sectionHeading)
	}
	out := make([]uint8, 0, colorCount*3)
	for _, line := range strings.Split(doc[idx:], "\n") {
		m := rowPattern.FindStringSubmatch(line)
		if m == nil {
			// 表が終わったら読み終わり
			if len(out) == colorCount*3 {
				break
			}
			continue
		}
		base, err := strconv.ParseUint(m[1], 16, 8)
		if err != nil {
			return nil, err
		}
		if int(base) != len(out)/3 {
			return nil, fmt.Errorf("行 $%s の位置が色 %d と合わない", m[1], len(out)/3)
		}
		cells := strings.Split(m[2], "|")
		if len(cells) != 16 {
			return nil, fmt.Errorf("行 $%s の列数が %d である", m[1], len(cells))
		}
		for i, c := range cells {
			v, err := parseHexColor(strings.TrimSpace(c))
			if err != nil {
				return nil, fmt.Errorf("行 $%s の列 %d: %w", m[1], i, err)
			}
			out = append(out, v...)
		}
	}
	if len(out) != colorCount*3 {
		return nil, fmt.Errorf("色の数が %d である（%d を期待）", len(out)/3, colorCount)
	}
	return out, nil
}

// parseHexColor は "RRGGBB" を 3 バイトにする。
func parseHexColor(s string) ([]uint8, error) {
	if len(s) != 6 {
		return nil, fmt.Errorf("%q は 6 桁の 16 進数ではない", s)
	}
	out := make([]uint8, 3)
	for i := range out {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", s, err)
		}
		out[i] = uint8(v)
	}
	return out, nil
}
