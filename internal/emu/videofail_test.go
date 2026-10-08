//go:build darwin || linux

package emu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// videoFailLimit は録画の途中で書き込みを失敗させるファイルの大きさの上限。
// 1 フレームは JPEG と 800 サンプルの PCM（3200 バイト）で数 KB になるため、
// 数フレームで越える。
const videoFailLimit = 32 << 10

// TestVideoMidRecordingWriteFailure は録画の途中で書き込みに失敗したとき、
// 録画が止まってエラーが Status に載り、エミュレーションは続き、
// StopRecordingVideo がそのエラーを返すことを確かめる（設計書 08 編 §8.8.3）。
func TestVideoMidRecordingWriteFailure(t *testing.T) {
	runWithFileSizeLimit(t, func(t *testing.T, limit func(uint64)) {
		e := newPausedEmulator(t)
		path := filepath.Join(t.TempDir(), "a.mp4")
		limit(videoFailLimit)
		if err := e.StartRecordingVideo(path, video.DefaultPalette(), 1, video.Overscan{}); err != nil {
			t.Fatal(err)
		}
		before := framesOf(t, e)
		runFrames(t, e, 120)
		if got := framesOf(t, e) - before; got != 120 {
			t.Errorf("進んだフレーム数 = %d、エミュレーションが止まった", got)
		}
		st := e.Status().Video
		if st.Recording {
			t.Fatal("書き込みに失敗しても録画中のまま")
		}
		if st.Error == "" {
			t.Error("Status.Video.Error にエラーが載っていない")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("書きかけのファイルが残っている")
		}
		runFrames(t, e, 5) // 止まっていない
		err := e.StopRecordingVideo()
		if err == nil {
			t.Fatal("StopRecordingVideo が途中の失敗を返さない")
		}
		if !errors.Is(err, syscall.EFBIG) {
			t.Errorf("返したエラー = %v、上限を越えた書き込みの失敗（EFBIG）でない", err)
		}
		if e.Status().Video.Error != "" {
			t.Error("エラーを返した後も Status.Video.Error が残っている")
		}
		if err := e.StopRecordingVideo(); err != nil {
			t.Errorf("2 度目の StopRecordingVideo がエラーを返す: %v", err)
		}
	})
}

// TestVideoErrorClearedOnNewRecording は途中の失敗のエラーが、次の録画を
// 始めたときに消えることを確かめる。
func TestVideoErrorClearedOnNewRecording(t *testing.T) {
	runWithFileSizeLimit(t, func(t *testing.T, limit func(uint64)) {
		e := newPausedEmulator(t)
		dir := t.TempDir()
		limit(videoFailLimit)
		if err := e.StartRecordingVideo(filepath.Join(dir, "a.mp4"), video.DefaultPalette(), 1, video.Overscan{}); err != nil {
			t.Fatal(err)
		}
		runFrames(t, e, 120)
		if e.Status().Video.Error == "" {
			t.Fatal("途中の失敗が Status に載っていない")
		}
		if err := e.StartRecordingVideo(filepath.Join(dir, "b.mp4"), video.DefaultPalette(), 1, video.Overscan{}); err != nil {
			t.Fatal(err)
		}
		if st := e.Status().Video; !st.Recording || st.Error != "" {
			t.Errorf("新しい録画の状態 = %+v、前のエラーが残っている", st)
		}
		if err := e.StopRecordingVideo(); err != nil {
			t.Errorf("新しい録画の停止で前のエラーを返した: %v", err)
		}
	})
}

// TestExportVideoMidWriteFailure は書き出しの途中で書き込みに失敗したとき、
// ExportVideo がエラーを返し、ファイルを残さないことを確かめる（設計書
// 08 編 §8.8.4）。
func TestExportVideoMidWriteFailure(t *testing.T) {
	runWithFileSizeLimit(t, func(t *testing.T, limit func(uint64)) {
		e, rom, moviePath, _ := recordedMovie(t)
		out := filepath.Join(t.TempDir(), "a.mp4")
		limit(videoFailLimit)
		err := e.ExportVideo(context.Background(), VideoExport{
			ROMPath: rom, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
		})
		if err == nil {
			t.Fatal("書き込みに失敗しても ExportVideo が成功を返した")
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Error("書きかけのファイルが残っている")
		}
	})
}
