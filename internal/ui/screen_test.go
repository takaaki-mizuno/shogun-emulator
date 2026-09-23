package ui

import (
	"image/color"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// newTestScreen はフレームの受け渡しの場と画面を作る。
func newTestScreen(cfg config.VideoConfig) (*emu.FrameBuffer, *screen) {
	fb := emu.NewFrameBuffer()
	return fb, newScreen(fb, video.DefaultPalette(), cfg)
}

// TestScreenSizeFollowsScaleAndOverscan は表示の大きさが拡大率と
// オーバースキャンから決まることを確かめる。
func TestScreenSizeFollowsScaleAndOverscan(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.VideoConfig
		w, h int
	}{
		{"1 倍・隠さない", config.VideoConfig{Scale: 1}, 256, 240},
		{"3 倍・上下 8", config.VideoConfig{Scale: 3, OverscanTop: 8, OverscanBottom: 8}, 768, 672},
		{"8 倍", config.VideoConfig{Scale: 8}, 2048, 1920},
		{"範囲外の拡大率は収める", config.VideoConfig{Scale: 99}, 256 * 8, 240 * 8},
		{"アスペクト比補正", config.VideoConfig{Scale: 1, AspectRatioCorrection: true}, 292, 240},
	}
	for _, tt := range tests {
		_, s := newTestScreen(tt.cfg)
		w, h := s.PixelSize()
		if w != tt.w || h != tt.h {
			t.Errorf("%s: 大きさ = %dx%d, 期待 %dx%d", tt.name, w, h, tt.w, tt.h)
		}
	}
}

// TestScreenScalesAtEveryStep は 1 倍から 8 倍まで表示が崩れないことを
// 確かめる。
func TestScreenScalesAtEveryStep(t *testing.T) {
	_, s := newTestScreen(config.VideoConfig{Scale: 1})
	for scale := config.MinScale; scale <= config.MaxScale; scale++ {
		s.SetScale(scale)
		w, h := s.PixelSize()
		if w != video.Width*scale || h != video.Height*scale {
			t.Errorf("%d 倍: 大きさ = %dx%d", scale, w, h)
		}
		if s.img.MinSize().Width != float32(w) {
			t.Errorf("%d 倍: 最小の幅が %v である", scale, s.img.MinSize().Width)
		}
	}
}

// TestScreenRefreshConvertsFrame は完成したフレームが画像へ変換される
// ことを確かめる。
func TestScreenRefreshConvertsFrame(t *testing.T) {
	fb, s := newTestScreen(config.VideoConfig{Scale: 1})

	f := video.NewFrame()
	f.Clear(0x20) // 白
	fb.Put(f)

	s.refresh()
	if got := s.rgba.RGBAAt(0, 0); got != (color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Errorf("(0,0) = %+v, 期待 白", got)
	}
}

// TestScreenRefreshWithEmptyQueueKeepsContent はフレームが無いときに
// 表示を変えないことを確かめる。
func TestScreenRefreshWithEmptyQueueKeepsContent(t *testing.T) {
	fb, s := newTestScreen(config.VideoConfig{Scale: 1})

	f := video.NewFrame()
	f.Clear(0x20)
	fb.Put(f)
	s.refresh()

	before := s.rgba.RGBAAt(0, 0)
	s.refresh() // 空
	if got := s.rgba.RGBAAt(0, 0); got != before {
		t.Errorf("空のときに表示が %+v から %+v へ変わった", before, got)
	}
}

// TestScreenHidesOverscan は隠した範囲が変換の対象から外れることを
// 確かめる。
func TestScreenHidesOverscan(t *testing.T) {
	cfg := config.VideoConfig{Scale: 1, OverscanTop: 8, OverscanBottom: 8}
	fb, s := newTestScreen(cfg)

	f := video.NewFrame()
	f.Clear(0x0F)     // 黒
	f.Set(0, 0, 0x20) // 隠される位置に白
	f.Set(0, 8, 0x16) // 表示される先頭行に赤
	fb.Put(f)
	s.refresh()

	if got := s.rgba.Bounds().Dy(); got != video.Height-16 {
		t.Fatalf("画像の高さ = %d, 期待 %d", got, video.Height-16)
	}
	if got := s.rgba.RGBAAt(0, 0); got == (color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Error("隠した範囲が表示されている")
	}
}

// TestScreenPictureHeightChangesRect はリージョンによって表示する
// 高さが変わることを確かめる。
func TestScreenPictureHeightChangesRect(t *testing.T) {
	_, s := newTestScreen(config.VideoConfig{Scale: 1})
	if _, h := s.PixelSize(); h != 240 {
		t.Fatalf("初期の高さ = %d", h)
	}
	s.SetPictureHeight(239) // PAL
	if _, h := s.PixelSize(); h != 239 {
		t.Errorf("PAL の高さ = %d, 期待 239", h)
	}
	if s.rgba.Bounds().Dy() != 239 {
		t.Errorf("画像の高さ = %d, 期待 239", s.rgba.Bounds().Dy())
	}
}

// TestScreenClearGoesBlack は ROM を閉じたときに画面が黒くなることを
// 確かめる。前の画面が残ると、まだ動いているように見える。
func TestScreenClearGoesBlack(t *testing.T) {
	fb, s := newTestScreen(config.VideoConfig{Scale: 1})
	f := video.NewFrame()
	f.Clear(0x20) // 白
	fb.Put(f)
	s.refresh()

	s.Clear()
	if got := s.rgba.RGBAAt(0, 0); got != (color.RGBA{0x00, 0x00, 0x00, 0xFF}) {
		t.Errorf("(0,0) = %+v, 期待 黒", got)
	}
}
