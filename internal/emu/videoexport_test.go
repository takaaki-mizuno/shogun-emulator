package emu

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
	"github.com/takaakimizuno/shogun-emulator/internal/video/mp4rec"
)

// writeLoopROMForExport は writePollingROM と同じ ROM の、未使用領域だけを
// 変えたものを書く。ムービーの ROM ハッシュの照合を確かめるために、
// 記録と異なる ROM として使う。
func writeLoopROMForExport(t *testing.T) string {
	t.Helper()
	path := writePollingROM(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[16+0x100] = 0xEA
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// recordedMovie は ROM と、その ROM で記録したムービーのパスを返す。
func recordedMovie(t *testing.T) (e *Emulator, rom, moviePath string, total uint64) {
	t.Helper()
	rom = writePollingROM(t)
	e = New(stateTestConfig(t))
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	moviePath = filepath.Join(t.TempDir(), "a.movie")
	recordSession(t, e, moviePath)
	data, err := os.ReadFile(moviePath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return e, rom, moviePath, m.Header.TotalFrames
}

// TestExportVideoFromMovie は操作の記録から書いた動画のフレーム数が記録の
// 総フレーム数と一致し、表示中のエミュレータが変わらないことを確かめる
// （設計書 08 編 §8.8.4・§8.8.5）。
func TestExportVideoFromMovie(t *testing.T) {
	e, rom, moviePath, total := recordedMovie(t)
	before := hashOf(t, e)
	out := filepath.Join(t.TempDir(), "a.mp4")
	var last uint64
	err := e.ExportVideo(context.Background(), VideoExport{
		ROMPath: rom, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
		Progress: func(done, all uint64) { last = done },
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := mp4rec.Inspect(out)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(info.VideoFrames) != total {
		t.Errorf("フレーム数 = %d、記録 %d", info.VideoFrames, total)
	}
	if last != total {
		t.Errorf("最後の進み具合 = %d、期待 %d", last, total)
	}
	if hashOf(t, e) != before {
		t.Error("書き出しで表示中のエミュレータの状態が変わった")
	}
}

// TestExportVideoRejectsOtherROM は記録と違う ROM では書き出さず、ファイルを
// 残さないことを確かめる。
func TestExportVideoRejectsOtherROM(t *testing.T) {
	e, _, moviePath, _ := recordedMovie(t)
	other := writeLoopROMForExport(t)
	out := filepath.Join(t.TempDir(), "a.mp4")
	err := e.ExportVideo(context.Background(), VideoExport{
		ROMPath: other, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
	})
	if err == nil {
		t.Fatal("違う ROM で書き出せた")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("ファイルが残っている")
	}
}

// TestExportVideoCancelRemovesFile は取り消すと書きかけのファイルを消す
// ことを確かめる。
func TestExportVideoCancelRemovesFile(t *testing.T) {
	e, rom, moviePath, _ := recordedMovie(t)
	out := filepath.Join(t.TempDir(), "a.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	err := e.ExportVideo(ctx, VideoExport{
		ROMPath: rom, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
		Progress: func(done, all uint64) { cancel() },
	})
	if err == nil {
		t.Fatal("取り消してもエラーにならない")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("書きかけのファイルが残っている")
	}
}

// TestExportVideoDoesNotTouchPatches は書き出しが利用者の実際のオーバーレイ
// （改造パッチ）の保存先を変えないことを確かめる（設計書 08 編 §8.8.4）。
//
// 「変更が無く有効」なオーバーレイを実際の保存先に置く。子 Emulator が
// これをそのまま読み込み、編集せずに Stop すると、storeOverlay は
// 「何も編集していない ROM のためにファイルを残さない」という分岐
// （internal/emu/overlay.go の saveOverlay）で元のファイルを削除する。
// 子の保存先を一時ディレクトリへ隔離していなければ、これは利用者の
// 実データの削除になる。
func TestExportVideoDoesNotTouchPatches(t *testing.T) {
	e, rom, moviePath, _ := recordedMovie(t)
	patchesDir := e.cfg.Dirs.PatchesDir(e.cfg.PatchesDir)
	if err := os.MkdirAll(patchesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var overlayPath string
	if !e.WithMachine(func(n *nes.NES) { overlayPath = e.patchesPath(n.ROM) }) {
		t.Fatal("本体を参照できない")
	}
	want := []byte(`{"version":1,"enabled":true,"prg":[],"chr":[]}` + "\n")
	if err := os.WriteFile(overlayPath, want, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "a.mp4")
	if err := e.ExportVideo(context.Background(), VideoExport{
		ROMPath: rom, MoviePath: moviePath, OutPath: out, Palette: video.DefaultPalette(), Scale: 1,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("実際のオーバーレイが消えた: %v", err)
	}
	if string(got) != string(want) {
		t.Error("実際のオーバーレイの内容が書き出しで変わった")
	}
	entries, err := os.ReadDir(patchesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("保存先のファイル数 = %d、期待 1（余計なファイルが増えた）", len(entries))
	}
}
