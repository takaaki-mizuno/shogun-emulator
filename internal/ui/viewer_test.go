package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// stubViewer は配置の仕組みを試すためのビューア。
type stubViewer struct {
	title    string
	contents int
	closed   int
	refresh  int
}

func (v *stubViewer) Title() string { return v.title }

func (v *stubViewer) Content() fyne.CanvasObject {
	v.contents++
	return widget.NewLabel(v.title)
}

func (v *stubViewer) Refresh() { v.refresh++ }

func (v *stubViewer) OnClose() { v.closed++ }

// TestWindowHostShowsAndHides は別ウィンドウの配置を確かめる。
func TestWindowHostShowsAndHides(t *testing.T) {
	a := test.NewApp()
	h := newWindowHost(a)
	v := &stubViewer{title: "パターンテーブル"}

	if h.IsVisible(v) {
		t.Error("開く前から表示中になっている")
	}
	h.Show(v)
	if !h.IsVisible(v) {
		t.Error("開いた後に表示中になっていない")
	}
	if v.contents != 1 {
		t.Errorf("Content の呼び出し回数 = %d, 期待 1", v.contents)
	}

	// 2 回目は作り直さない
	h.Show(v)
	if v.contents != 1 {
		t.Errorf("2 回目で Content が %d 回呼ばれた", v.contents)
	}

	h.Hide(v)
	if h.IsVisible(v) {
		t.Error("閉じた後も表示中になっている")
	}
	if v.closed != 1 {
		t.Errorf("OnClose の呼び出し回数 = %d, 期待 1", v.closed)
	}
}

// TestDockedHostShowsAndHides はタブの配置を確かめる。
func TestDockedHostShowsAndHides(t *testing.T) {
	test.NewApp()
	tabs := container.NewAppTabs()
	h := newDockedHost(tabs)
	v := &stubViewer{title: "ネームテーブル"}

	h.Show(v)
	if len(tabs.Items) != 1 {
		t.Fatalf("タブの数 = %d, 期待 1", len(tabs.Items))
	}
	if tabs.Items[0].Text != v.Title() {
		t.Errorf("タブ名 = %q, 期待 %q", tabs.Items[0].Text, v.Title())
	}

	h.Hide(v)
	if len(tabs.Items) != 0 {
		t.Errorf("閉じた後のタブの数 = %d, 期待 0", len(tabs.Items))
	}
	if v.closed != 1 {
		t.Errorf("OnClose の呼び出し回数 = %d", v.closed)
	}
}

// TestSwitchHostKeepsVisibleViewers は配置を切り替えても表示中の
// ビューアが引き継がれることを確かめる。
func TestSwitchHostKeepsVisibleViewers(t *testing.T) {
	a := test.NewApp()
	from := newWindowHost(a)
	tabs := container.NewAppTabs()
	to := newDockedHost(tabs)

	v1 := &stubViewer{title: "スプライト"}
	v2 := &stubViewer{title: "パレット"}
	from.Show(v1)
	from.Show(v2)

	switchHost(from, to)

	if len(from.Visible()) != 0 {
		t.Errorf("切り替え元に %d 個残っている", len(from.Visible()))
	}
	if got := len(to.Visible()); got != 2 {
		t.Fatalf("切り替え先の数 = %d, 期待 2", got)
	}
	// 順序が保たれること
	if tabs.Items[0].Text != "スプライト" || tabs.Items[1].Text != "パレット" {
		t.Errorf("順序が変わっている: %q, %q", tabs.Items[0].Text, tabs.Items[1].Text)
	}
	// 表示内容は作り直される
	if v1.contents != 2 {
		t.Errorf("Content の呼び出し回数 = %d, 期待 2", v1.contents)
	}
}

// TestHostVisibleOrderIsStable は表示中の一覧の順序が安定していることを
// 確かめる。map をたどる順序に依存すると切り替えのたびに並びが変わる。
func TestHostVisibleOrderIsStable(t *testing.T) {
	test.NewApp()
	tabs := container.NewAppTabs()
	h := newDockedHost(tabs)

	viewers := []*stubViewer{{title: "1"}, {title: "2"}, {title: "3"}, {title: "4"}}
	for _, v := range viewers {
		h.Show(v)
	}
	for range 20 {
		got := h.Visible()
		for i, v := range got {
			if v.Title() != viewers[i].title {
				t.Fatalf("順序が変わった: %d 番目が %q", i, v.Title())
			}
		}
	}
}
