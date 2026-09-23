package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// menuLabels はメニューの項目名を並べて返す。子メニューは "親 > 子" とする。
func menuLabels(m *fyne.Menu) []string {
	var out []string
	for _, item := range m.Items {
		if item.IsSeparator {
			continue
		}
		out = append(out, item.Label)
		if item.ChildMenu != nil {
			for _, child := range item.ChildMenu.Items {
				out = append(out, item.Label+" > "+child.Label)
			}
		}
	}
	return out
}

// TestMainMenuHasSpecifiedItems はメニューに設計書 10 編 §10.5 の項目が
// 並ぶことを確かめる。このフェーズで扱う範囲に限る。
func TestMainMenuHasSpecifiedItems(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)
	u.version = "test"

	menu := u.buildMainMenu()
	if len(menu.Items) != 7 {
		t.Fatalf("メニューの数 = %d, 期待 7", len(menu.Items))
	}

	want := map[string][]string{
		"ファイル": {"ROM を開く…", "最近使った ROM", "ROM を閉じる", "終了"},
		"実行":   {"一時停止", "コマ送り", "リセット", "ハードリセット", "速度", "巻き戻し"},
		"ステート": {"クイックセーブ", "クイックロード", "スロットを選ぶ", "スロットの一覧…",
			"名前を付けて保存…", "ファイルから読み込み…"},
		"ムービー": {"記録を始める…", "記録を止める", "再生…", "停止"},
		"表示": {"拡大率", "フルスクリーン", "パターンテーブル", "ネームテーブル", "スプライト", "パレット", "APU",
			"CPU デバッガ", "メモリ（新しく開く）", "ログ", "ビューアの配置"},
		"デバッグ": {"ブレークポイント一覧", "トレースの記録", "トレースの書き出し", "トレースの常時出力", "ログカテゴリ…",
			"オーバーレイを有効にする", "オーバーレイを消去"},
		"ヘルプ": {"バージョン情報"},
	}
	for _, m := range menu.Items {
		labels := menuLabels(m)
		for _, w := range want[m.Label] {
			if !slices.Contains(labels, w) {
				t.Errorf("%s に %q が無い: %v", m.Label, w, labels)
			}
		}
	}
}

// TestScaleMenuCoversEveryStep は拡大率のメニューが 1 倍から 8 倍まで
// 並ぶことを確かめる。
func TestScaleMenuCoversEveryStep(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)

	labels := menuLabels(u.viewMenu())
	for s := config.MinScale; s <= config.MaxScale; s++ {
		want := fmt.Sprintf("拡大率 > %d 倍", s)
		if !slices.Contains(labels, want) {
			t.Errorf("%q が無い: %v", want, labels)
		}
	}
}

// TestSpeedMenuSendsSpeed は速度のメニューが速度倍率を変えることを確かめる。
func TestSpeedMenuSendsSpeed(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)

	var item *fyne.MenuItem
	for _, m := range u.runMenu().Items {
		if m.Label == "速度" {
			for _, c := range m.ChildMenu.Items {
				if c.Label == "2 倍速" {
					item = c
				}
			}
		}
	}
	if item == nil {
		t.Fatal("2 倍速の項目が無い")
	}
	item.Action()
	waitSpeed(t, u, 2.0)
}

// TestRecentROMsKeepOrderAndLimit は最近使った ROM の並びと上限を確かめる。
func TestRecentROMsKeepOrderAndLimit(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)

	for i := range maxRecentROMs + 5 {
		u.addRecent(fmt.Sprintf("/roms/%02d.nes", i))
	}
	if len(u.recent) != maxRecentROMs {
		t.Errorf("覚えている数 = %d, 期待 %d", len(u.recent), maxRecentROMs)
	}
	if u.recent[0] != fmt.Sprintf("/roms/%02d.nes", maxRecentROMs+4) {
		t.Errorf("先頭が最新でない: %s", u.recent[0])
	}

	// すでにあるものは先頭へ移る
	u.addRecent(u.recent[3])
	if len(u.recent) != maxRecentROMs {
		t.Errorf("重複で数が増えた: %d", len(u.recent))
	}
	if u.recent[0] == u.recent[1] {
		t.Error("同じものが 2 つ並んでいる")
	}
}

// TestViewerLayoutSwitchIsIdempotent は同じ配置を指定しても何も起きない
// ことを確かめる。ウィンドウが無い状態で呼ばれても落ちない。
func TestViewerLayoutSwitchIsIdempotent(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)
	u.SetViewerLayout(u.cfg.UI.ViewerLayout)
	if u.cfg.UI.ViewerLayout != config.LayoutWindows {
		t.Errorf("配置が %q に変わった", u.cfg.UI.ViewerLayout)
	}
}

// TestViewerLayoutSwitchMovesViewers は配置を切り替えると表示中の
// ビューアが移ることを確かめる。
func TestViewerLayoutSwitchMovesViewers(t *testing.T) {
	u := newTestUI(t)
	u.app = test.NewApp()
	u.pal = video.DefaultPalette()
	u.screen = newScreen(u.emu.Frames, u.pal, u.cfg.Video)
	u.status = newStatusBar(u.emu.Frames)
	u.win = test.NewWindow(nil)
	defer u.win.Close()
	u.win.SetContent(u.buildContent())

	v := &stubViewer{title: "メモリ"}
	u.host.Show(v)
	if !u.host.IsVisible(v) {
		t.Fatal("ビューアが表示されていない")
	}

	u.SetViewerLayout(config.LayoutDocked)
	if !u.host.IsVisible(v) {
		t.Error("切り替え後に表示されていない")
	}
	if u.tabs == nil || len(u.tabs.Items) != 1 {
		t.Errorf("タブへ移っていない")
	}

	u.SetViewerLayout(config.LayoutWindows)
	if !u.host.IsVisible(v) {
		t.Error("別ウィンドウへ戻っていない")
	}
	u.host.Close()
}

// TestSlotMenuCoversEverySlot はスロットを選ぶメニューが 0 から 9 まで
// 並ぶことを確かめる。
func TestSlotMenuCoversEverySlot(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)

	labels := menuLabels(u.stateMenu())
	for i := range emu.SlotCount {
		want := fmt.Sprintf("スロットを選ぶ > スロット %d", i)
		if !slices.Contains(labels, want) {
			t.Errorf("%q が無い: %v", want, labels)
		}
	}
}

// TestStatusBarShowsSlotAndMovie はステータスバーにスロット番号と
// ムービーの状態が出ることを確かめる。
func TestStatusBarShowsSlotAndMovie(t *testing.T) {
	b := newStatusBar(emu.NewFrameBuffer())
	s := emu.Status{
		Loaded:     true,
		ROMName:    "game",
		MapperName: "NROM",
		Speed:      1,
		Slot:       3,
		Movie:      emu.MovieStatus{Recording: true, Frame: 120},
		Rewinding:  true,
	}
	text := b.text(s)
	for _, want := range []string{"スロット 3", "録画中 120 フレーム", "巻き戻し中"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q が無い: %s", want, text)
		}
	}

	s.Movie = emu.MovieStatus{Playing: true, Frame: 5, Total: 100}
	s.Rewinding = false
	if text := b.text(s); !strings.Contains(text, "再生中 5/100") {
		t.Errorf("再生中の表示が無い: %s", text)
	}
}
