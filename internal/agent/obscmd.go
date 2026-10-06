package agent

import (
	"encoding/json"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// 観測の Agent Command（設計書 14 編 §14.9、§14.10）。

func registerObs(r *Registry) {
	obs := func(name string, params any, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: ClassObserve, Params: params, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	obs("obs.get", obsGetParams{}, "Observation を返す。include で付ける内容を選ぶ",
		"Return an observation: frame, CPU position, watch values and changes since your last observation. Use include to add image, sprites, nametable, etc.", handleObsGet)
	obs("obs.screenshot", screenshotParams{}, "現在の画面の PNG を返す",
		"Return the current screen as PNG (nearest-neighbor scaled).", handleScreenshot)
	obs("obs.sprites", spritesParams{}, "スプライト一覧（OAM）を返す",
		"List sprites from OAM with position, tile, palette, flags and per-scanline overflow.", handleSprites)
	obs("obs.nametable", nametableParams{}, "ネームテーブルをタイル番号の格子で返す。table を省くと表示中の範囲",
		"Return a nametable as a grid of tile IDs and attribute palettes. Omit table to get the visible screen area.", handleNametable)
	obs("obs.patterns", patternsParams{}, "パターンテーブルをハッシュ・画像・色番号で返す",
		"Return pattern tables as per-tile hashes, an image, or per-pixel color indices.", handlePatterns)
	obs("obs.palette", InstanceParam{}, "パレット RAM を背景とスプライトに分けて返す",
		"Return palette RAM split into background and sprite palettes with RGB values.", handlePalette)
	obs("obs.ppu_writes", InstanceParam{}, "直前のフレームの PPU レジスタ書き込みを時刻つきで返す",
		"Return writes to PPUCTRL/PPUMASK/OAMADDR/PPUSCROLL/PPUADDR/OAMDMA in the last completed frame with scanline and dot.", handlePPUWrites)
	obs("obs.apu", InstanceParam{}, "APU の各チャンネルの状態を返す",
		"Return the state of each APU channel.", handleAPU)
}

type obsGetParams struct {
	InstanceParam
	ObserveParams
}

func handleObsGet(c *Context, raw json.RawMessage) (any, error) {
	var p obsGetParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(p.ObserveParams)
	o, err := parseObserve(data)
	if err != nil {
		return nil, err
	}
	return c.Host.observe(c.Instance, c.Conn, o)
}

type screenshotParams struct {
	InstanceParam
	Scale int `json:"scale,omitempty" desc:"拡大率（1–4）" default:"2"`
}

// ScreenshotResult は obs.screenshot の結果。
type ScreenshotResult struct {
	Image  *Image `json:"image"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Frame  uint64 `json:"frame"`
}

func handleScreenshot(c *Context, raw json.RawMessage) (any, error) {
	var p screenshotParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Scale == 0 {
		p.Scale = c.Host.opts.ImageScale
	}
	if p.Scale < 1 || p.Scale > 4 {
		return nil, Errorf(KindInvalidParams, "scale は 1–4 とする（%d）", p.Scale)
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	data, ok, err := c.Instance.Emu.FramePNG(p.Scale)
	if err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	if !ok {
		return nil, Errorf(KindNotLoaded, "まだ完成したフレームが無い（exec.step で 1 フレーム進める）")
	}
	st := c.Instance.Emu.Status()
	return ScreenshotResult{Image: &Image{MIME: "image/png", Data: data}, Width: 256 * p.Scale,
		Height: st.PictureHeight * p.Scale, Frame: st.Frames}, nil
}

// withSnapshot は命令境界でスナップショットを取り、fn に渡す。
func withSnapshot(inst *Instance, fn func(d *debug.Debugger, s *debug.Snapshot)) error {
	if err := requireLoaded(inst); err != nil {
		return err
	}
	ok := inst.Emu.WithDebugger(func(d *debug.Debugger) {
		if n := d.Machine(); n != nil {
			fn(d, takeSnapshot(n))
		}
	})
	if !ok {
		return Errorf(KindInternalError, "エミュレーションが停止している")
	}
	return nil
}

type spritesParams struct {
	InstanceParam
	All bool `json:"all,omitempty" desc:"画面外（Y が $EF 以上）のスプライトも返す"`
}

func handleSprites(c *Context, raw json.RawMessage) (any, error) {
	var p spritesParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	var r SpritesResult
	err := withSnapshot(c.Instance, func(_ *debug.Debugger, s *debug.Snapshot) { r = spritesFrom(s, p.All) })
	return r, err
}

type nametableParams struct {
	InstanceParam
	Table *int `json:"table,omitempty" desc:"面の番号（0–3）。省くと表示中の範囲"`
}

func handleNametable(c *Context, raw json.RawMessage) (any, error) {
	var p nametableParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Table != nil && (*p.Table < 0 || *p.Table > 3) {
		return nil, Errorf(KindInvalidParams, "table は 0–3 とする（%d）", *p.Table)
	}
	var r NametableResult
	err := withSnapshot(c.Instance, func(d *debug.Debugger, s *debug.Snapshot) {
		if p.Table != nil {
			r = nametableAt(s, *p.Table)
			return
		}
		var pw *PPUWritesResult
		if d.PPUWriteLogEnabled() {
			w := ppuWrites(d)
			pw = &w
		}
		r = visibleNametable(s, pw)
	})
	return r, err
}

type patternsParams struct {
	InstanceParam
	Format  string `json:"format,omitempty" desc:"hash・image・pixels" default:"\"hash\""`
	Palette string `json:"palette,omitempty" desc:"image で適用するパレット（gray・bg0–bg3・sp0–sp3）" default:"\"gray\""`
	Tiles   []int  `json:"tiles,omitempty" desc:"pixels で返すタイル（面 × 256 + 番号、0–511）"`
}

// PatternsHashResult は format: hash の結果。
type PatternsHashResult struct {
	Tables [2][]*string `json:"tables"`
}

// TilePixels は format: pixels の 1 タイル。
type TilePixels struct {
	Tile int      `json:"tile"`
	Rows []string `json:"rows"`
}

func handlePatterns(c *Context, raw json.RawMessage) (any, error) {
	var p patternsParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	var out any
	var ferr error
	err := withSnapshot(c.Instance, func(_ *debug.Debugger, s *debug.Snapshot) {
		switch p.Format {
		case "", "hash":
			out = PatternsHashResult{Tables: patternHashes(s)}
		case "image":
			img, err := patternImage(s, p.Palette)
			if err != nil {
				ferr = err
				return
			}
			data, err := video.EncodeImagePNG(img)
			if err != nil {
				ferr = Errorf(KindInternalError, "%v", err)
				return
			}
			out = struct {
				Image *Image `json:"image"`
			}{&Image{MIME: "image/png", Data: data}}
		case "pixels":
			if len(p.Tiles) == 0 {
				ferr = Errorf(KindInvalidParams, "format: pixels には tiles を指定する")
				return
			}
			var tiles []TilePixels
			for _, t := range p.Tiles {
				if t < 0 || t > 511 {
					ferr = Errorf(KindInvalidParams, "tiles の %d は 0–511 の外である", t)
					return
				}
				px := tilePixels(tileBytes(s, t/256, t%256))
				tp := TilePixels{Tile: t}
				for _, row := range px {
					b := make([]byte, 8)
					for x, v := range row {
						b[x] = '0' + v
					}
					tp.Rows = append(tp.Rows, string(b))
				}
				tiles = append(tiles, tp)
			}
			out = struct {
				Tiles []TilePixels `json:"tiles"`
			}{tiles}
		default:
			ferr = Errorf(KindInvalidParams, "format %q を知らない（hash・image・pixels）", p.Format)
		}
	})
	if err != nil {
		return nil, err
	}
	return out, ferr
}

func handlePalette(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	var r PaletteResult
	err := withSnapshot(c.Instance, func(_ *debug.Debugger, s *debug.Snapshot) { r = paletteFrom(s) })
	return r, err
}

func handlePPUWrites(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var r PPUWritesResult
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) { r = ppuWrites(d) })
	return r, nil
}

func handleAPU(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var r apu.Inspection
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		if n := d.Machine(); n != nil {
			r = n.APU.Inspect()
		}
	})
	return r, nil
}
