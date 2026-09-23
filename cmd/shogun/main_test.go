package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// TestVersionFlag は --version がバージョン情報を標準出力へ書き、正常終了する
// ことを確かめる。
func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Errorf("終了コード = %d, 期待 0", code)
	}
	out := stdout.String()
	for _, want := range []string{appName, version, "Go:"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が含まれない:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("標準エラー出力に書いている: %s", stderr.String())
	}
}

// TestHelpFlag は --help がヘルプを表示して正常終了することを確かめる。
func TestHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Errorf("終了コード = %d, 期待 0", code)
	}
	if !strings.Contains(stderr.String(), "使い方:") {
		t.Errorf("ヘルプが表示されていない:\n%s", stderr.String())
	}
}

// TestUnknownFlag は知らないオプションで終了コード 2 を返すことを確かめる。
func TestUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--nonexistent"}, &stdout, &stderr); code != 2 {
		t.Errorf("終了コード = %d, 期待 2", code)
	}
}

// TestHelpListsAllOptions は --help が設計書 11 編 §11.5.1 の全オプションを
// 分類して並べることを確かめる。
func TestHelpListsAllOptions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run([]string{"-h"}, &stdout, &stderr)
	out := stderr.String()
	for _, want := range []string{"表示と音声:", "保存先:", "ステートとムービー:", "決定論:", "デバッグ:", "headless:",
		"--help", "--version", "--config", "--portable", "--region", "--scale", "--fullscreen", "--no-audio",
		"--audio-buffer", "--sample-rate", "--speed", "--save-dir", "--state-dir", "--load-state",
		"--save-state-on-exit", "--movie", "--record-movie", "--movie-verify", "--no-movie-verify",
		"--ram-init", "--ram-seed", "--deterministic", "--debug", "--log", "--log-dir", "--log-categories",
		"--trace-log", "--break-at", "--headless", "--frames", "--screenshot"} {
		if !strings.Contains(out, want) {
			t.Errorf("ヘルプに %q が無い", want)
		}
	}
}

// TestVersionIncludesBuildInfo は --version にビルド日時と Go のバージョンが出ることを確かめる。
func TestVersionIncludesBuildInfo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	run([]string{"--version"}, &stdout, &stderr)
	for _, want := range []string{"コミット", "ビルド日時", "Go:"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("バージョン表示に %q が無い:\n%s", want, stdout.String())
		}
	}
}

// TestBadArgumentsExitWith2 は不正な引数で終了コード 2 を返すことを確かめる。
func TestBadArgumentsExitWith2(t *testing.T) {
	for _, args := range [][]string{
		{"--scale", "20"},
		{"--audio-buffer", "1"},
		{"--speed", "100"},
		{"--log", "bogus"},
		{"--log-categories", "mapper,nope"},
		{"--break-at", "zz"},
		{"--region", "moon"},
		{"--headless"},
		{"a.nes", "b.nes"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: 終了コード = %d, 期待 2", args, code)
		}
	}
}

// TestGUIOptionsOverride は GUI で使うオプションが使う設定へ入ることを確かめる。
func TestGUIOptionsOverride(t *testing.T) {
	args := []string{"--fullscreen", "--no-audio", "--audio-buffer", "50", "--scale", "4", "--region", "pal",
		"--save-dir", "/s", "--state-dir", "/t", "--log", "stdout", "--log-dir", "/l",
		"--log-categories", "mapper,dma", "--speed", "2", "--debug", "--break-at", "C000,$8000", "game.nes"}
	opts, code, ok := parseArgs(args, io.Discard)
	if !ok {
		t.Fatalf("引数を読めない（%d）", code)
	}
	if !opts.debug || opts.speed != 2 || opts.romPath != "game.nes" {
		t.Errorf("オプション = %+v", opts)
	}
	cfg := config.Default()
	if err := applyOptions(cfg, opts); err != nil {
		t.Fatal(err)
	}
	if !cfg.Video.Fullscreen || cfg.Audio.Enabled || cfg.Audio.BufferMilliseconds != 50 || cfg.Video.Scale != 4 ||
		cfg.Emulation.Region != "pal" || cfg.Paths.SaveDir != "/s" || cfg.Paths.StateDir != "/t" ||
		cfg.Debug.LogOutput != "stdout" || cfg.Paths.LogDir != "/l" || len(cfg.Debug.LogCategories) != 2 {
		t.Errorf("設定へ入っていない: %+v", cfg)
	}
	addrs, err := parseBreakAddrs(opts.breakAt)
	if err != nil || len(addrs) != 2 || addrs[0] != 0xC000 || addrs[1] != 0x8000 {
		t.Errorf("ブレークのアドレス = %v, %v", addrs, err)
	}
}
