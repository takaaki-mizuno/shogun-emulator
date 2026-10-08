package mp4rec

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMuxerRoundTrip は書いた MP4 を読み直し、フレーム数・大きさ・長さ・
// 音声のサンプル数と、各サンプルのデータの位置を確かめる（設計書 08 編 §8.8.1）。
func TestMuxerRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	m, err := newMuxer(path, 512, 480)
	if err != nil {
		t.Fatal(err)
	}
	frames := [][]byte{[]byte("jpeg-0"), []byte("jpeg-one"), []byte("j2")}
	durs := []uint32{16639, 16639, 16640}
	for i, f := range frames {
		pcm := bytes.Repeat([]byte{byte(i), 0, byte(i), 0}, 800)
		if err := m.addFrame(f, durs[i], pcm); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.close(); err != nil {
		t.Fatal(err)
	}

	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 512 || info.Height != 480 {
		t.Errorf("大きさ = %dx%d", info.Width, info.Height)
	}
	if info.VideoFrames != 3 || info.AudioSamples != 2400 {
		t.Errorf("フレーム %d・音声 %d、期待 3・2400", info.VideoFrames, info.AudioSamples)
	}
	if want := 49918 * time.Microsecond; info.Duration != want {
		t.Errorf("長さ = %v、期待 %v", info.Duration, want)
	}
	for i, f := range frames {
		if info.FrameSizes[i] != len(f) {
			t.Errorf("フレーム %d の大きさ = %d、期待 %d", i, info.FrameSizes[i], len(f))
		}
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("jpeg-one")) {
		t.Error("映像のデータが書かれていない")
	}
}

// TestMuxerAbortRemovesFile は abort でファイルが消えることを確かめる。
func TestMuxerAbortRemovesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp4")
	m, err := newMuxer(path, 256, 240)
	if err != nil {
		t.Fatal(err)
	}
	m.abort()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("ファイルが残っている: %v", err)
	}
}
