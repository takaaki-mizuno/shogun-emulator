package main

import (
	"bytes"
	"strings"
	"testing"
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
