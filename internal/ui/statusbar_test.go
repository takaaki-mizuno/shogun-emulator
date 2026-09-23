package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// TestStatusBarShowsRunState はステータスバーに設計書 10 編 §10.1 の
// 項目が並ぶことを確かめる。
func TestStatusBarShowsRunState(t *testing.T) {
	test.NewApp()
	b := newStatusBar(emu.NewFrameBuffer())

	s := emu.Status{
		Loaded:       true,
		ROMName:      "smb",
		MapperName:   "NROM",
		MapperNumber: 0,
		RegionName:   "NTSC",
		Speed:        1.0,
	}
	text := b.text(s)
	for _, want := range []string{"fps", "等速", "実行中", "smb", "NROM", "NTSC"} {
		if !strings.Contains(text, want) {
			t.Errorf("表示に %q が含まれない: %s", want, text)
		}
	}

	s.Paused = true
	if got := b.text(s); !strings.Contains(got, "一時停止") {
		t.Errorf("一時停止が表示されない: %s", got)
	}

	s.Paused = false
	s.Speed = 2
	if got := b.text(s); !strings.Contains(got, "2 倍速") {
		t.Errorf("速度倍率が表示されない: %s", got)
	}
}

// TestStatusBarWithoutROM は ROM が無いときの表示を確かめる。
func TestStatusBarWithoutROM(t *testing.T) {
	test.NewApp()
	b := newStatusBar(emu.NewFrameBuffer())
	if got := b.text(emu.Status{}); !strings.Contains(got, "ROM") {
		t.Errorf("表示 = %q", got)
	}
}

// TestStatusBarUpdatesOnlyOnChange は同じ内容で書き換えないことを確かめる。
func TestStatusBarUpdatesOnlyOnChange(t *testing.T) {
	test.NewApp()
	b := newStatusBar(emu.NewFrameBuffer())
	s := emu.Status{Loaded: true, ROMName: "smb", MapperName: "NROM", Speed: 1}

	b.update(s)
	first := b.label.Text
	b.label.SetText("別の内容")
	b.update(s)
	if b.label.Text != "別の内容" {
		t.Errorf("内容が変わっていないのに書き換えた: %q", b.label.Text)
	}

	s.Paused = true
	b.update(s)
	if b.label.Text == "別の内容" || b.label.Text == first {
		t.Errorf("内容が変わったのに書き換えていない: %q", b.label.Text)
	}
}

// TestStatusBarNotify は一時的な知らせが表示されることを確かめる。
func TestStatusBarNotify(t *testing.T) {
	test.NewApp()
	b := newStatusBar(emu.NewFrameBuffer())
	b.notify("保存しました")
	if got := b.text(emu.Status{Loaded: true, Speed: 1}); !strings.Contains(got, "保存しました") {
		t.Errorf("知らせが表示されない: %s", got)
	}
}

// TestStatusBarCountsDroppedFrames は表示が追いつかないことが分かる
// 表示になることを確かめる。
func TestStatusBarCountsDroppedFrames(t *testing.T) {
	test.NewApp()
	b := newStatusBar(emu.NewFrameBuffer())

	b.dropped = 2 // 測定の区間に満たないため直接置く

	if got := b.text(emu.Status{Loaded: true, Speed: 1}); !strings.Contains(got, "表示落ち 2") {
		t.Errorf("表示落ちが出ない: %s", got)
	}
}

// TestSpeedTextShowsUncapped は待ちを行わない倍率で「最速」と表示する
// ことを確かめる。倍率をそのまま出すと、その速さで動いていると誤解を招く。
func TestSpeedTextShowsUncapped(t *testing.T) {
	tests := []struct {
		speed float64
		want  string
	}{
		{0.25, "0.25 倍速"},
		{1, "等速"},
		{2, "2 倍速"},
		{emu.UncappedSpeed, "最速"},
		{16, "最速"},
	}
	for _, tt := range tests {
		if got := speedText(tt.speed); got != tt.want {
			t.Errorf("speedText(%v) = %q, 期待 %q", tt.speed, got, tt.want)
		}
	}
}
