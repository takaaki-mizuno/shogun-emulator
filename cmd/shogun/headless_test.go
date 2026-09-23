package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// writeLoopROM は画面を描かずに動き続ける NROM の ROM を書き、パスを返す。
func writeLoopROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	// $2001 に $08 を書いて背景を有効にし、無限ループする。
	copy(prg, []uint8{
		0xA9, 0x08, // LDA #$08
		0x8D, 0x01, 0x20, // STA $2001
		0x4C, 0x05, 0x80, // JMP $8005
	})
	prg[0x7FFC] = 0x00
	prg[0x7FFD] = 0x80

	data := make([]uint8, 0, 16+len(prg)+8*1024)
	data = append(data, []uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	data = append(data, prg...)
	data = append(data, make([]uint8, 8*1024)...)

	path := filepath.Join(t.TempDir(), "loop.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runCLI は引数を渡して実行し、終了コードと標準出力・標準エラーを返す。
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// TestHeadlessRecordsAndReplaysMovie は headless でムービーを記録し、
// 再生して検査できることを確かめる。
func TestHeadlessRecordsAndReplaysMovie(t *testing.T) {
	rom := writeLoopROM(t)
	dir := t.TempDir()
	moviePath := filepath.Join(dir, "test.movie")

	code, _, stderr := runCLI(t,
		"--headless", "--deterministic", "--frames", "40",
		"--state-dir", dir, "--record-movie", moviePath, rom)
	if code != exitOK {
		t.Fatalf("記録の終了コード = %d（%s）", code, stderr)
	}
	data, err := os.ReadFile(moviePath)
	if err != nil {
		t.Fatalf("ムービーが書き出されていない: %v", err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatalf("ムービーを読めない: %v", err)
	}
	if m.Header.TotalFrames < 40 {
		t.Errorf("記録されたフレーム数 = %d, 期待 40 以上", m.Header.TotalFrames)
	}

	code, _, stderr = runCLI(t,
		"--headless", "--deterministic", "--state-dir", dir, "--movie", moviePath, rom)
	if code != exitOK {
		t.Fatalf("再生の終了コード = %d（%s）", code, stderr)
	}
}

// TestHeadlessDetectsDesync は desync を検出したとき終了コード 3 を返す
// ことを確かめる。
func TestHeadlessDetectsDesync(t *testing.T) {
	rom := writeLoopROM(t)
	dir := t.TempDir()
	moviePath := filepath.Join(dir, "test.movie")

	if code, _, stderr := runCLI(t, "--headless", "--deterministic", "--frames", "80",
		"--state-dir", dir, "--record-movie", moviePath, rom); code != exitOK {
		t.Fatalf("記録の終了コード = %d（%s）", code, stderr)
	}

	// チェックサムを 1 つ壊す。再生すると状態が食い違う。
	data, err := os.ReadFile(moviePath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := movie.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	broken := false
	for i := range m.Records {
		if m.Records[i].Kind == movie.KindChecksum && i > 2 {
			m.Records[i].Hash[0] ^= 0xFF
			broken = true
			break
		}
	}
	if !broken {
		t.Fatal("チェックサムが記録されていない")
	}
	if err := os.WriteFile(moviePath, m.Encode(), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "--headless", "--deterministic",
		"--state-dir", dir, "--movie", moviePath, rom)
	if code != exitMovieDesync {
		t.Fatalf("終了コード = %d, 期待 %d", code, exitMovieDesync)
	}
	if !strings.Contains(stdout, "状態が一致しない") {
		t.Errorf("食い違いの報告が無い: %s", stdout)
	}

	// 検証を切ると最後まで再生する。
	code, _, _ = runCLI(t, "--headless", "--deterministic", "--no-movie-verify",
		"--state-dir", dir, "--movie", moviePath, rom)
	if code != exitOK {
		t.Errorf("検証を切っても終了コードが %d である", code)
	}
}

// TestHeadlessSavesStateOnExit は終了時にステートを保存することを
// 確かめる。
func TestHeadlessSavesStateOnExit(t *testing.T) {
	rom := writeLoopROM(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "exit.state")

	code, _, stderr := runCLI(t, "--headless", "--frames", "10",
		"--state-dir", dir, "--save-state-on-exit", statePath, rom)
	if code != exitOK {
		t.Fatalf("終了コード = %d（%s）", code, stderr)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ステートが保存されていない: %v", err)
	}
	if _, err := state.ReadHeader(data); err != nil {
		t.Errorf("保存したステートを読めない: %v", err)
	}
}

// TestHeadlessLoadsState は --load-state で復元して続きから実行できる
// ことを確かめる。
func TestHeadlessLoadsState(t *testing.T) {
	rom := writeLoopROM(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "exit.state")

	if code, _, _ := runCLI(t, "--headless", "--frames", "30",
		"--state-dir", dir, "--save-state-on-exit", statePath, rom); code != exitOK {
		t.Fatal("保存に失敗した")
	}
	saved, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	h, err := state.ReadHeader(saved)
	if err != nil {
		t.Fatal(err)
	}

	after := filepath.Join(dir, "after.state")
	if code, _, stderr := runCLI(t, "--headless", "--frames", "10",
		"--state-dir", dir, "--load-state", statePath, "--save-state-on-exit", after, rom); code != exitOK {
		t.Fatalf("復元に失敗した（%s）", stderr)
	}
	data, err := os.ReadFile(after)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := state.ReadHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if h2.Frames <= h.Frames {
		t.Errorf("復元した続きから進んでいない（%d → %d フレーム）", h.Frames, h2.Frames)
	}
}

// TestApplyOptionsDeterministic は --deterministic が値の定まらない
// 状態を固定することを確かめる。
func TestApplyOptionsDeterministic(t *testing.T) {
	cfg := config.Default()
	cfg.Emulation.CPUPPUAlignment = 2
	cfg.Emulation.DMAGetPutPhase = 1
	cfg.Emulation.RAMInitPattern = "random"
	cfg.Emulation.RAMSeed = 12345

	if err := applyOptions(cfg, options{deterministic: true}); err != nil {
		t.Fatal(err)
	}
	if cfg.Emulation.RAMInitPattern != "zero" {
		t.Errorf("RAM パターン = %q, 期待 \"zero\"", cfg.Emulation.RAMInitPattern)
	}
	if cfg.Emulation.RAMSeed != 0 {
		t.Errorf("シード = %d, 期待 0", cfg.Emulation.RAMSeed)
	}
	if cfg.Emulation.CPUPPUAlignment != 0 || cfg.Emulation.DMAGetPutPhase != 0 {
		t.Errorf("位相が固定されていない（%d, %d）",
			cfg.Emulation.CPUPPUAlignment, cfg.Emulation.DMAGetPutPhase)
	}
}

// TestApplyOptionsRejectsBadValues は受け付けられない指定を断ることを
// 確かめる。
func TestApplyOptionsRejectsBadValues(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts options
	}{
		{"知らない RAM パターン", options{ramInit: "bogus"}},
		{"検証の指定が矛盾", options{movieVerify: true, noMovieVerify: true}},
		{"headless に ROM が無い", options{headless: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := applyOptions(config.Default(), tt.opts); err == nil {
				t.Errorf("%s を受け入れた", tt.name)
			}
		})
	}
}

// TestRAMSeedAndInitAreApplied は --ram-init と --ram-seed が設定へ
// 反映されることを確かめる。
func TestRAMSeedAndInitAreApplied(t *testing.T) {
	cfg := config.Default()
	if err := applyOptions(cfg, options{ramInit: "ff", ramSeed: 99}); err != nil {
		t.Fatal(err)
	}
	if cfg.Emulation.RAMInitPattern != "ff" {
		t.Errorf("RAM パターン = %q", cfg.Emulation.RAMInitPattern)
	}
	if cfg.Emulation.RAMSeed != 99 {
		t.Errorf("シード = %d", cfg.Emulation.RAMSeed)
	}
}
