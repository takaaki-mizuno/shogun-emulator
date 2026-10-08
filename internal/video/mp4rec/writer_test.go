package mp4rec

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

const ntscRate = 60.0988

func testFrame(n int) *video.Frame {
	f := video.NewFrame()
	for y := range video.Height {
		for x := range video.Width {
			f.Set(x, y, uint16((x/16+y/16+n)%64))
		}
	}
	return f
}

func writeFrames(t *testing.T, path string, n int, pcmPerFrame int) {
	t.Helper()
	w, err := Create(path, video.DefaultPalette(), Options{Scale: 2, FrameRate: ntscRate,
		Overscan: video.DefaultOverscan(), PictureHeight: 240})
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := w.WriteFrame(testFrame(i), make([]int16, pcmPerFrame)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestWriterSizeAndCounts は大きさ（オーバースキャンを除いて 2 倍）・フレーム数・
// 音声の量を確かめる（設計書 08 編 §8.8.1・§8.8.2）。
func TestWriterSizeAndCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	writeFrames(t, path, 120, 800)
	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 512 || info.Height != 448 {
		t.Errorf("大きさ = %dx%d、期待 512x448", info.Width, info.Height)
	}
	if info.VideoFrames != 120 {
		t.Errorf("フレーム数 = %d", info.VideoFrames)
	}
	if want := int(math.Round(120 * SampleRate / ntscRate)); info.AudioSamples != want {
		t.Errorf("音声 = %d、期待 %d", info.AudioSamples, want)
	}
}

// TestWriterPadsMissingAudio は音声が届かないフレームを無音で埋め、映像と
// 音声の長さがそろうことを確かめる（巻き戻しやステートの読み込みの直後）。
func TestWriterPadsMissingAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	writeFrames(t, path, 60, 0)
	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := int(math.Round(60 * SampleRate / ntscRate)); info.AudioSamples != want {
		t.Errorf("音声 = %d、期待 %d", info.AudioSamples, want)
	}
}

// TestWriterIsDeterministic は同じ入力から同じバイト列ができることを確かめる。
func TestWriterIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.mp4"), filepath.Join(dir, "b.mp4")
	writeFrames(t, a, 30, 801)
	writeFrames(t, b, 30, 801)
	da, _ := os.ReadFile(a)
	db, _ := os.ReadFile(b)
	if !bytes.Equal(da, db) {
		t.Error("同じ入力から違うファイルができた")
	}
}

// TestWriterCopiesPalette は Create が受け取ったパレットを写して持ち、呼び出し
// 側が後からパレットを書き換えても動画の色が変わらないことを確かめる。
// GUI は設定の保存でパレットをその場で書き換えるため、写さないと書き出し用の
// ゴルーチンとデータ競合になる。
func TestWriterCopiesPalette(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "want.mp4")
	writeFrames(t, want, 10, 800)

	got := filepath.Join(dir, "got.mp4")
	pal := video.DefaultPalette()
	w, err := Create(got, pal, Options{Scale: 2, FrameRate: ntscRate,
		Overscan: video.DefaultOverscan(), PictureHeight: 240})
	if err != nil {
		t.Fatal(err)
	}
	// 全色を黒にする。写していなければ、この後のフレームが黒くなる。
	*pal = video.Palette{}
	for i := range 10 {
		if err := w.WriteFrame(testFrame(i), make([]int16, 800)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	dw, _ := os.ReadFile(want)
	dg, _ := os.ReadFile(got)
	if !bytes.Equal(dw, dg) {
		t.Error("Create の後にパレットを書き換えると動画の色が変わる")
	}
}

// TestWriterRejectsNilPalette はパレットが無いとき断ることを確かめる。
func TestWriterRejectsNilPalette(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	if _, err := Create(path, nil, Options{Scale: 1, FrameRate: ntscRate, PictureHeight: 240}); err == nil {
		t.Error("パレットが無いのに作れた")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("ファイルが作られている")
	}
}

// TestWriterRejectsBadScale は 1–3 以外の倍率を断ることを確かめる。
func TestWriterRejectsBadScale(t *testing.T) {
	for _, s := range []int{0, 4} {
		_, err := Create(filepath.Join(t.TempDir(), "a.mp4"), video.DefaultPalette(),
			Options{Scale: s, FrameRate: ntscRate, PictureHeight: 240})
		if err == nil {
			t.Errorf("倍率 %d を受け付けた", s)
		}
	}
}
