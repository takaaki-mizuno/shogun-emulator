package video

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestSavePNG は書き出した PNG がオーバースキャン後の大きさになることを
// 確かめる。
func TestSavePNG(t *testing.T) {
	f := NewFrame()
	f.Clear(0x0F)
	path := filepath.Join(t.TempDir(), "sub", "shot.png")
	if err := SavePNG(f, DefaultPalette(), DefaultOverscan(), Height, path); err != nil {
		t.Fatal(err)
	}

	in, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	cfg, err := png.DecodeConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != Width || cfg.Height != Height-16 {
		t.Errorf("大きさ = %dx%d, 期待 %dx%d", cfg.Width, cfg.Height, Width, Height-16)
	}
}
