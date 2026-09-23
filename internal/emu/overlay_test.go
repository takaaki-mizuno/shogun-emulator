package emu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// TestOverlaySavedAndRestored は CHR の編集がオーバーレイとして保存され、
// ROM を読み込み直すと復元されることを確かめる（設計書 06 編 §6.8）。
func TestOverlaySavedAndRestored(t *testing.T) {
	cfg := stateTestConfig(t)
	e := New(cfg)
	e.Start()
	t.Cleanup(e.Stop)
	rom := writeDebugROM(t)
	if err := e.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	if err := e.Poke(debug.SpacePPU, 0x0010, 0x7E, false); err != nil {
		t.Fatal(err)
	}
	if err := e.Poke(debug.SpacePRGROM, 0x0100, 0xEA, false); err != nil {
		t.Fatal(err)
	}
	e.SaveOverlay()
	files, _ := filepath.Glob(filepath.Join(cfg.PatchesDir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("オーバーレイのファイル = %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), `"offset": 16`) {
		t.Errorf("ファイルに CHR の変更が無い: %s", data)
	}

	// 読み込み直すと変更が戻る。
	if err := e.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	var chr, prg uint8
	e.WithMachine(func(n *nes.NES) {
		chr = n.PPU.PeekVRAM(0x0010)
		prg = n.Bus.Peek(0x8100)
	})
	if chr != 0x7E || prg != 0xEA {
		t.Errorf("復元した値 = CHR $%02X, PRG $%02X", chr, prg)
	}
	if st := e.OverlayStatus(); !st.Enabled || st.CHR != 1 || st.PRG != 1 {
		t.Errorf("オーバーレイの状態 = %+v", st)
	}

	// 無効にすると元に戻り、その状態も保存される。
	if err := e.SetOverlayEnabled(false); err != nil {
		t.Fatal(err)
	}
	e.WithMachine(func(n *nes.NES) { chr = n.PPU.PeekVRAM(0x0010) })
	if chr != 0 {
		t.Errorf("無効にした後の CHR = $%02X", chr)
	}
	if err := e.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	if st := e.OverlayStatus(); st.Enabled {
		t.Error("無効の状態が保存されていない")
	}

	// 消去するとファイルも消える。
	if err := e.SetOverlayEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := e.ClearOverlay(); err != nil {
		t.Fatal(err)
	}
	if files, _ := filepath.Glob(filepath.Join(cfg.PatchesDir, "*.json")); len(files) != 0 {
		t.Errorf("消去した後もファイルが残っている: %v", files)
	}
}

// TestOverlayEditRefusedWhileRecording はムービーの記録中にオーバーレイを
// 変えられないことを確かめる。
func TestOverlayEditRefusedWhileRecording(t *testing.T) {
	cfg := stateTestConfig(t)
	e := New(cfg)
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(writeDebugROM(t)); err != nil {
		t.Fatal(err)
	}
	if err := e.StartRecordingMovie(filepath.Join(t.TempDir(), "m.movie")); err != nil {
		t.Fatal(err)
	}
	if err := e.Poke(debug.SpacePPU, 0x0010, 1, false); err == nil {
		t.Error("記録中の CHR の編集を受け付けた")
	}
	if err := e.SetOverlayEnabled(false); err == nil {
		t.Error("記録中のオーバーレイの切り替えを受け付けた")
	}
	if err := e.StopRecordingMovie(); err != nil {
		t.Fatal(err)
	}
}

// TestStateWarnsOnOverlayMismatch はオーバーレイが保存時と違うステートを
// 読み込んだとき、ロードを続けて warn.compat に記録することを確かめる
// （設計書 08 編 §8.2.2）。
func TestStateWarnsOnOverlayMismatch(t *testing.T) {
	cfg := stateTestConfig(t)
	e := New(cfg)
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(writeDebugROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	saved, err := e.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Poke(debug.SpacePPU, 0x0020, 0x11, false); err != nil {
		t.Fatal(err)
	}
	if err := e.LoadState(saved); err != nil {
		t.Fatalf("オーバーレイが違うとロードが失敗した: %v", err)
	}
	// オーバーレイはステートに含まれないため、ロードしても編集は残る。
	var chr uint8
	e.WithMachine(func(n *nes.NES) { chr = n.PPU.PeekVRAM(0x0020) })
	if chr != 0x11 {
		t.Errorf("ロードした後の CHR $0020 = $%02X, 期待 $11（オーバーレイは保存しない）", chr)
	}
	var found bool
	for _, entry := range e.Debugger().Logger().Entries(debug.CatWarnCompat, "オーバーレイ", 0) {
		found = found || strings.Contains(entry.Message, "オーバーレイ")
	}
	if !found {
		t.Error("オーバーレイの不一致が warn.compat に記録されていない")
	}
}
