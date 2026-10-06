package agent

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// CPUInfo は cpu.get の結果。
type CPUInfo struct {
	PC         string   `json:"pc"`
	Symbol     string   `json:"symbol,omitempty"`
	A          string   `json:"a"`
	X          string   `json:"x"`
	Y          string   `json:"y"`
	S          string   `json:"s"`
	P          string   `json:"p"`
	Flags      string   `json:"flags"`
	Cycles     uint64   `json:"cycles"`
	Frame      uint64   `json:"frame"`
	Scanline   int      `json:"scanline"`
	Dot        int      `json:"dot"`
	NMIPending bool     `json:"nmi_pending"`
	IRQSources []string `json:"irq_sources,omitempty"`
	// Stack は $01FF から S+1 までの内容（深い側から）。
	Stack []string `json:"stack,omitempty"`
}

// cpuInfo は CPU の状態を読む。エミュレーションゴルーチンで呼ぶ。
func cpuInfo(d *debug.Debugger) CPUInfo {
	v := d.CPUView(0)
	c := CPUInfo{
		PC: hex16(v.PC), Symbol: d.NearestLabel(v.PC), A: hex8(v.A), X: hex8(v.X), Y: hex8(v.Y),
		S: hex8(v.S), P: hex8(v.P), Flags: v.PFlags(), Cycles: v.Cycles, Frame: v.Frame,
		Scanline: v.Scanline, Dot: v.Dot, NMIPending: v.NMIPending, IRQSources: v.IRQSources,
	}
	for i := len(v.Stack) - 1; i >= 0; i-- {
		c.Stack = append(c.Stack, hex8(v.Stack[i]))
	}
	return c
}

// Sprite はスプライト 1 個（設計書 14 編 §14.10.1）。
type Sprite struct {
	Index    int    `json:"index"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	ScreenY  int    `json:"screen_y"`
	Tile     string `json:"tile"`
	Palette  int    `json:"palette"`
	Priority string `json:"priority"`
	FlipH    bool   `json:"flip_h"`
	FlipV    bool   `json:"flip_v"`
	Sprite0  bool   `json:"sprite0"`
	Drawn    bool   `json:"drawn"`
}

// SpritesResult は obs.sprites の結果。
type SpritesResult struct {
	Size        string   `json:"size"`
	Sprites     []Sprite `json:"sprites"`
	HiddenCount int      `json:"hidden_count"`
	LinesOver8  []int    `json:"lines_over_8"`
}

// spritesFrom はスナップショットの OAM からスプライト一覧を作る。
// all が false のとき、画面外（Y が $EF 以上）のスプライトを省く。
func spritesFrom(s *debug.Snapshot, all bool) SpritesResult {
	r := SpritesResult{Size: "8x8", Sprites: []Sprite{}, LinesOver8: []int{}}
	if s.SpriteHeight == 16 {
		r.Size = "8x16"
	}
	for i := range 64 {
		o := s.OAM[i*4 : i*4+4]
		y, tile, attr, x := int(o[0]), o[1], o[2], int(o[3])
		if y >= 0xEF && !all {
			r.HiddenCount++
			continue
		}
		prio := "front"
		if attr&0x20 != 0 {
			prio = "behind"
		}
		r.Sprites = append(r.Sprites, Sprite{
			Index: i, X: x, Y: y, ScreenY: y + 1, Tile: hex8(tile), Palette: int(attr & 3),
			Priority: prio, FlipH: attr&0x40 != 0, FlipV: attr&0x80 != 0, Sprite0: i == 0, Drawn: s.SpriteDrawn[i],
		})
	}
	for line, n := range s.ScanlineSpriteCount {
		if n > 8 {
			r.LinesOver8 = append(r.LinesOver8, line)
		}
	}
	return r
}

// Scroll はフレームの開始時のスクロール位置。
type Scroll struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	Nametable int `json:"nametable"`
	FineX     int `json:"fine_x"`
	FineY     int `json:"fine_y"`
}

// NametableResult は obs.nametable の結果（設計書 14 編 §14.10.2）。
type NametableResult struct {
	// Table は面の番号。表示中の範囲のとき -1。
	Table      int      `json:"table"`
	Base       string   `json:"base,omitempty"`
	Mirroring  string   `json:"mirroring"`
	Tiles      []string `json:"tiles"`
	Attributes []string `json:"attributes"`
	Scroll     Scroll   `json:"scroll"`
	Notes      []string `json:"notes,omitempty"`
}

// frameScroll はフレームの開始時の v と fine X からスクロール位置を求める
// （設計書 09 編 §9.4.2 の式）。
func frameScroll(s *debug.Snapshot) Scroll {
	v, fx := int(s.ScrollV), int(s.FineX)
	return Scroll{
		X:         (v>>10&1)*256 + (v&0x1F)*8 + fx,
		Y:         (v>>11&1)*240 + (v>>5&0x1F)*8 + (v >> 12 & 7),
		Nametable: v >> 10 & 3,
		FineX:     fx,
		FineY:     v >> 12 & 7,
	}
}

// tileAt は 64×60 タイルの論理的な 4 面の中のタイル番号を返す。
func tileAt(s *debug.Snapshot, gx, gy int) uint8 {
	gx, gy = (gx%64+64)%64, (gy%60+60)%60
	t := (gy/30)*2 + gx/32
	return s.Nametables[t*0x400+(gy%30)*32+gx%32]
}

// paletteAt はタイルの位置に適用される背景パレットの番号を返す。
func paletteAt(s *debug.Snapshot, gx, gy int) int {
	gx, gy = (gx%64+64)%64, (gy%60+60)%60
	t := (gy/30)*2 + gx/32
	lx, ly := gx%32, gy%30
	b := s.Nametables[t*0x400+0x3C0+(ly/4)*8+lx/4]
	shift := ((ly/2&1)*2 + (lx / 2 & 1)) * 2
	return int(b>>shift) & 3
}

// gridRows は 32×30 のタイルと 16×15 の属性を、左上のタイル位置 (tx, ty) から
// 文字列の行にする。
func gridRows(s *debug.Snapshot, tx, ty int) (tiles, attrs []string) {
	for r := range 30 {
		var b strings.Builder
		for c := range 32 {
			if c > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%02X", tileAt(s, tx+c, ty+r))
		}
		tiles = append(tiles, b.String())
	}
	for r := range 15 {
		var b strings.Builder
		for c := range 16 {
			if c > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%d", paletteAt(s, tx+c*2, ty+r*2))
		}
		attrs = append(attrs, b.String())
	}
	return tiles, attrs
}

// nametableAt は面 table（0–3）の内容を返す。
func nametableAt(s *debug.Snapshot, table int) NametableResult {
	tiles, attrs := gridRows(s, (table&1)*32, (table>>1)*30)
	return NametableResult{
		Table: table, Base: hex16(uint16(0x2000 + table*0x400)), Mirroring: s.Mirroring.String(),
		Tiles: tiles, Attributes: attrs, Scroll: frameScroll(s),
	}
}

// visibleNametable はフレームの開始時のスクロール位置から見える範囲を返す。
// 2 面にまたがるときはつなぐ。pw が書き込みの記録を持てば、フレームの途中の
// スクロールの変更を notes で知らせる。
func visibleNametable(s *debug.Snapshot, pw *PPUWritesResult) NametableResult {
	sc := frameScroll(s)
	tiles, attrs := gridRows(s, sc.X/8, sc.Y/8)
	r := NametableResult{Table: -1, Mirroring: s.Mirroring.String(), Tiles: tiles, Attributes: attrs, Scroll: sc}
	if sc.FineX != 0 || sc.FineY != 0 {
		r.Notes = append(r.Notes, fmt.Sprintf("スクロールがタイルの途中（fine_x %d、fine_y %d）にあり、格子は左上のタイルから並べている", sc.FineX, sc.FineY))
	}
	if pw != nil {
		for _, w := range pw.Writes {
			if (w.Reg == "$2005" || w.Reg == "$2006" || w.Reg == "$2000") && w.Scanline >= 0 && w.Scanline < 240 {
				r.Notes = append(r.Notes, fmt.Sprintf("スキャンライン %d でスクロールに関わるレジスタ %s へ書いている。格子は画面の上端の位置だけを反映している。obs.ppu_writes を見る", w.Scanline, w.Reg))
				break
			}
		}
	}
	return r
}

// PPUWriteInfo は PPU 書き込み 1 件。
type PPUWriteInfo struct {
	Reg      string `json:"reg"`
	Value    string `json:"value"`
	Scanline int    `json:"scanline"`
	Dot      int    `json:"dot"`
	PC       string `json:"pc"`
	Symbol   string `json:"symbol,omitempty"`
}

// PPUWritesResult は obs.ppu_writes の結果（設計書 14 編 §14.10.5）。
type PPUWritesResult struct {
	Frame     uint64         `json:"frame"`
	Writes    []PPUWriteInfo `json:"writes"`
	Truncated bool           `json:"truncated"`
	Complete  bool           `json:"complete"`
	Notes     []string       `json:"notes,omitempty"`
}

// ppuWrites は PPU 書き込みの記録を有効にし、直前のフレームの記録を返す。
func ppuWrites(d *debug.Debugger) PPUWritesResult {
	f, ok := d.RequestPPUWrites()
	r := PPUWritesResult{Frame: f.Frame, Writes: []PPUWriteInfo{}, Truncated: f.Truncated, Complete: f.Complete}
	if !ok {
		r.Notes = append(r.Notes, "記録を始めた。次にフレームが完成した後から取れる")
		return r
	}
	for _, w := range f.Writes {
		r.Writes = append(r.Writes, PPUWriteInfo{
			Reg: hex16(w.Reg), Value: hex8(w.Value), Scanline: w.Scanline, Dot: w.Dot,
			PC: hex16(w.PC), Symbol: d.NearestLabel(w.PC),
		})
	}
	return r
}

// PaletteEntry はパレット RAM の 1 エントリ。
type PaletteEntry struct {
	Addr  string `json:"addr"`
	Value string `json:"value"`
	RGB   string `json:"rgb"`
}

// PaletteResult は obs.palette の結果（設計書 14 編 §14.10.4）。
type PaletteResult struct {
	Backdrop   PaletteEntry       `json:"backdrop"`
	Background [4][4]PaletteEntry `json:"background"`
	Sprite     [4][4]PaletteEntry `json:"sprite"`
}

func rgbHex(c color.RGBA) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// paletteFrom はスナップショットのパレット RAM を読む。
func paletteFrom(s *debug.Snapshot) PaletteResult {
	pal := video.DefaultPalette()
	entry := func(i int) PaletteEntry {
		v := s.Palette[i] & 0x3F
		return PaletteEntry{Addr: hex16(uint16(0x3F00 + i)), Value: hex8(v), RGB: rgbHex(pal.Color(uint16(v)))}
	}
	r := PaletteResult{Backdrop: entry(0)}
	for g := range 4 {
		for i := range 4 {
			r.Background[g][i] = entry(g*4 + i)
			r.Sprite[g][i] = entry(16 + g*4 + i)
		}
	}
	return r
}

// tileBytes はパターンテーブル table のタイル tile の 16 バイトを返す。
func tileBytes(s *debug.Snapshot, table, tile int) []uint8 {
	off := table*0x1000 + tile*16
	return s.CHR[off : off+16]
}

// tilePixels はタイルの 8×8 の色番号（0–3）を返す。
func tilePixels(b []uint8) [8][8]uint8 {
	var p [8][8]uint8
	for y := range 8 {
		lo, hi := b[y], b[y+8]
		for x := range 8 {
			bit := 7 - x
			p[y][x] = (lo>>bit)&1 | ((hi>>bit)&1)<<1
		}
	}
	return p
}

// patternHashes は各タイルの 16 バイトの SHA-1 の先頭 8 桁を返す。全 0 の
// タイルは空の文字列（JSON で null にする）。
func patternHashes(s *debug.Snapshot) [2][]*string {
	var out [2][]*string
	for t := range 2 {
		for i := range 256 {
			b := tileBytes(s, t, i)
			zero := true
			for _, v := range b {
				if v != 0 {
					zero = false
					break
				}
			}
			if zero {
				out[t] = append(out[t], nil)
				continue
			}
			sum := sha1.Sum(b)
			h := hex.EncodeToString(sum[:4])
			out[t] = append(out[t], &h)
		}
	}
	return out
}

// patternImage は 2 面を横に並べた 256×128 の画像を作る。palette は
// "gray"、"bg0"–"bg3"、"sp0"–"sp3"。
func patternImage(s *debug.Snapshot, palette string) (*image.RGBA, error) {
	colors := [4]color.RGBA{{0, 0, 0, 255}, {85, 85, 85, 255}, {170, 170, 170, 255}, {255, 255, 255, 255}}
	if palette != "" && palette != "gray" {
		var base int
		var g int
		if _, err := fmt.Sscanf(palette, "bg%d", &g); err == nil && g >= 0 && g < 4 {
			base = g * 4
		} else if _, err := fmt.Sscanf(palette, "sp%d", &g); err == nil && g >= 0 && g < 4 {
			base = 16 + g*4
		} else {
			return nil, Errorf(KindInvalidParams, "palette %q を知らない（gray・bg0–bg3・sp0–sp3）", palette)
		}
		pal := video.DefaultPalette()
		for i := range 4 {
			idx := base + i
			if i == 0 {
				idx = 0
			}
			colors[i] = pal.Color(uint16(s.Palette[idx] & 0x3F))
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 256, 128))
	for t := range 2 {
		for i := range 256 {
			px := tilePixels(tileBytes(s, t, i))
			ox, oy := t*128+(i%16)*8, (i/16)*8
			for y := range 8 {
				for x := range 8 {
					img.SetRGBA(ox+x, oy+y, colors[px[y][x]])
				}
			}
		}
	}
	return img, nil
}
