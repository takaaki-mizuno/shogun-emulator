//go:build darwin || linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHeadlessVideoWriteFailureExitCode は --record-video の録画が途中の
// 書き込みの失敗で止まったとき、エラーを出して終了コード 1 で終わることを
// 確かめる（設計書 11 編 §11.5.2）。
func TestHeadlessVideoWriteFailureExitCode(t *testing.T) {
	runWithFileSizeLimit(t, func(t *testing.T, limit func(uint64)) {
		rom := writeLoopROM(t)
		dir := t.TempDir()
		out := filepath.Join(dir, "out.mp4")
		limit(32 << 10)
		code, _, stderr := runCLI(t, "--headless", "--deterministic", "--frames", "120",
			"--state-dir", dir, "--record-video", out, rom)
		if code != exitROMError {
			t.Errorf("終了コード = %d、期待 %d（%s）", code, exitROMError, stderr)
		}
		if n := strings.Count(stderr, "too large"); n != 1 {
			t.Errorf("書き込みの失敗が標準エラーに %d 回出ている（期待 1 回）: %q", n, stderr)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Error("書きかけのファイルが残っている")
		}
	})
}
