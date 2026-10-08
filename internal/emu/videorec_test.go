package emu

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
	"github.com/takaakimizuno/shogun-emulator/internal/video/mp4rec"
)

// runWithInputs は決まった入力で n フレーム進める。
func runWithInputs(t *testing.T, e *Emulator, n int) {
	t.Helper()
	for i, b := range []uint8{0, input.ButtonA, input.ButtonRight, 0} {
		e.Input.Set(0, b)
		runFrames(t, e, n/4+i%2)
	}
}

// TestVideoRecordingKeepsStateAndCounts は録画しても状態のハッシュが変わらず、
// 進めたフレーム数と音声の量が動画に入ることを確かめる（設計書 08 編 §8.8.2）。
func TestVideoRecordingKeepsStateAndCounts(t *testing.T) {
	plain := newPausedEmulator(t)
	runWithInputs(t, plain, 120)

	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	before := framesOf(t, e)
	runWithInputs(t, e, 120)
	recorded := framesOf(t, e) - before
	st := e.Status().Video
	if !st.Recording {
		t.Error("録画中の表示になっていない")
	}
	if st.Frames != recorded {
		t.Errorf("Status.Video.Frames = %d、進めたフレーム数 %d", st.Frames, recorded)
	}
	// 停止すると Status.Video が空になるため、フレームレートは先に読む。
	rate := st.FrameRate
	if rate <= 0 {
		t.Fatalf("FrameRate = %v", rate)
	}
	// Writer は足りない音声を 0 で埋めるため、動画の音声の量だけでは
	// APU の出力が届いたかが分からない。録画中にリサンプラの出力の累計を読む。
	var delivered uint64
	if !e.WithMachine(func(*nes.NES) { delivered = e.video.delivered }) {
		t.Fatal("本体を参照できない")
	}
	expected := math.Round(float64(recorded) * mp4rec.SampleRate / rate)
	if float64(delivered) < 0.9*expected {
		t.Errorf("録画のリサンプラの出力 = %d、期待 %v の 9 割以上", delivered, expected)
	}
	if err := e.StopRecordingVideo(); err != nil {
		t.Fatal(err)
	}
	if e.Status().Video.Recording {
		t.Error("止めても録画中のまま")
	}
	if hashOf(t, e) != hashOf(t, plain) {
		t.Error("録画すると状態のハッシュが変わる")
	}
	info, err := mp4rec.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(info.VideoFrames) != recorded {
		t.Errorf("動画のフレーム数 = %d、進めたフレーム数 %d", info.VideoFrames, recorded)
	}
	want := int(math.Round(float64(recorded) * mp4rec.SampleRate / rate))
	if info.AudioSamples != want {
		t.Errorf("音声 = %d、期待 %d", info.AudioSamples, want)
	}
	if info.Width != 256 || info.Height != 240 {
		t.Errorf("大きさ = %dx%d", info.Width, info.Height)
	}
}

// TestVideoClosesOnROMChange は録画中に ROM を閉じても、再生できる
// ファイルとして閉じることを確かめる（設計書 08 編 §8.8.4）。
func TestVideoClosesOnROMChange(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	runFrames(t, e, 10)
	if err := e.Unload(); err != nil {
		t.Fatal(err)
	}
	if e.Status().Video.Recording {
		t.Error("ROM を閉じても録画中のまま")
	}
	info, err := mp4rec.Inspect(path)
	if err != nil {
		t.Fatalf("閉じたファイルを読めない: %v", err)
	}
	if info.VideoFrames != 10 {
		t.Errorf("フレーム数 = %d", info.VideoFrames)
	}
}

// TestVideoClosesOnROMLoad は録画中に別の ROM を開いたとき、それまでの分を
// 閉じ、新しい ROM では録画していないことを確かめる（設計書 08 編 §8.8.4）。
func TestVideoClosesOnROMLoad(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	runFrames(t, e, 6)
	if err := e.LoadROM(writePollingROM(t)); err != nil {
		t.Fatal(err)
	}
	if e.Status().Video.Recording {
		t.Error("別の ROM を開いても録画中のまま")
	}
	info, err := mp4rec.Inspect(path)
	if err != nil {
		t.Fatalf("閉じたファイルを読めない: %v", err)
	}
	if info.VideoFrames != 6 {
		t.Errorf("フレーム数 = %d", info.VideoFrames)
	}
}

// TestVideoClosesOnStop は録画中にエミュレータを止めたとき（アプリケーションの
// 終了）、ファイルを閉じて再生できる形で残すことを確かめる（設計書 08 編 §8.8.4）。
func TestVideoClosesOnStop(t *testing.T) {
	e := newPausedEmulator(t)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	runFrames(t, e, 7)
	e.Stop()
	info, err := mp4rec.Inspect(path)
	if err != nil {
		t.Fatalf("閉じたファイルを読めない: %v", err)
	}
	if info.VideoFrames != 7 {
		t.Errorf("フレーム数 = %d", info.VideoFrames)
	}
}

// TestVideoStopsOnWriteError は書き出せない場所を選んだときにエラーを返し、
// エミュレーションが続くことを確かめる。
func TestVideoStopsOnWriteError(t *testing.T) {
	e := newPausedEmulator(t)
	bad := filepath.Join(t.TempDir(), "no-such-dir", "a.mp4")
	if err := e.StartRecordingVideo(bad, video.DefaultPalette(), 1, video.Overscan{}); err == nil {
		t.Fatal("書けない場所で録画を始められた")
	}
	if e.Status().Video.Recording {
		t.Error("失敗したのに録画中になっている")
	}
	runFrames(t, e, 5) // 止まっていない
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Error("ファイルが作られている")
	}
}

// TestVideoRejectsDoubleStart は録画中に重ねて始められないことを確かめる。
func TestVideoRejectsDoubleStart(t *testing.T) {
	e := newPausedEmulator(t)
	dir := t.TempDir()
	if err := e.StartRecordingVideo(filepath.Join(dir, "a.mp4"), video.DefaultPalette(), 1, video.Overscan{}); err != nil {
		t.Fatal(err)
	}
	if err := e.StartRecordingVideo(filepath.Join(dir, "b.mp4"), video.DefaultPalette(), 1, video.Overscan{}); err == nil {
		t.Error("録画中に 2 本目を始められた")
	}
	_ = e.StopRecordingVideo()
}

// TestVideoStartWithoutROM は ROM を開いていないとき録画を始められないことを確かめる。
func TestVideoStartWithoutROM(t *testing.T) {
	e := New(stateTestConfig(t))
	e.Start()
	t.Cleanup(e.Stop)
	path := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err == nil {
		t.Error("ROM が無いのに録画を始められた")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("ファイルが作られている")
	}
}
