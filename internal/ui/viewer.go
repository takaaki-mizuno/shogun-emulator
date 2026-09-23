package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// Viewer は配置の形を知らない表示部品。
//
// 別ウィンドウに置かれるかタブに置かれるかを部品が知らないようにする。
// フェーズ 11 で追加するビューアを、配置の仕組みを変えても書き直さずに
// 済ませるためである。
type Viewer interface {
	// Title はウィンドウタイトルとタブ名に使う。
	Title() string

	// Content は表示内容を返す。表示を始めるたびに呼ばれる。
	Content() fyne.CanvasObject

	// Refresh は表示を更新する。UI スレッドから呼ばれる。
	Refresh()

	// OnClose は表示をやめるときに呼ばれる。
	OnClose()
}

// ViewerHost はビューアの置き場所。
type ViewerHost interface {
	// Show はビューアを表示する。すでに表示しているときは前面に出す。
	Show(v Viewer)

	// Hide は表示をやめる。
	Hide(v Viewer)

	// IsVisible は表示しているかを返す。
	IsVisible(v Viewer) bool

	// Visible は表示しているビューアを、表示を始めた順に返す。
	Visible() []Viewer

	// Close はすべての表示をやめる。配置を切り替えるときに使う。
	Close()
}

// windowHost は各ビューアを別ウィンドウに置く。
type windowHost struct {
	app fyne.App
	// order は表示を始めた順。map をたどる順序に依存しないために持つ。
	order   []Viewer
	windows map[Viewer]fyne.Window
}

// newWindowHost は別ウィンドウの配置を作る。
func newWindowHost(a fyne.App) ViewerHost {
	return &windowHost{app: a, windows: map[Viewer]fyne.Window{}}
}

// Show はビューアのウィンドウを開く。
func (h *windowHost) Show(v Viewer) {
	if w, ok := h.windows[v]; ok {
		w.RequestFocus()
		return
	}
	w := h.app.NewWindow(v.Title())
	w.SetContent(v.Content())
	// ウィンドウを閉じたときに表示状態を合わせる。閉じた後も表示中の
	// ままにすると、メニューの状態と実際の表示が食い違う。
	w.SetOnClosed(func() { h.forget(v) })
	h.windows[v] = w
	h.order = append(h.order, v)
	w.Show()
}

// Hide はビューアのウィンドウを閉じる。
func (h *windowHost) Hide(v Viewer) {
	w, ok := h.windows[v]
	if !ok {
		return
	}
	h.forget(v)
	w.Close()
}

// forget は表示の記録から取り除く。
func (h *windowHost) forget(v Viewer) {
	if _, ok := h.windows[v]; !ok {
		return
	}
	delete(h.windows, v)
	for i, x := range h.order {
		if x == v {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
	v.OnClose()
}

// IsVisible はウィンドウを開いているかを返す。
func (h *windowHost) IsVisible(v Viewer) bool {
	_, ok := h.windows[v]
	return ok
}

// Visible は開いているビューアを順に返す。
func (h *windowHost) Visible() []Viewer {
	return append([]Viewer(nil), h.order...)
}

// Close はすべてのウィンドウを閉じる。
func (h *windowHost) Close() {
	for _, v := range h.Visible() {
		h.Hide(v)
	}
}

// dockedHost は各ビューアをメインウィンドウ内のタブに置く。
type dockedHost struct {
	tabs  *container.AppTabs
	order []Viewer
	items map[Viewer]*container.TabItem
}

// newDockedHost はタブの配置を作る。
func newDockedHost(tabs *container.AppTabs) ViewerHost {
	return &dockedHost{tabs: tabs, items: map[Viewer]*container.TabItem{}}
}

// Show はタブを追加する。
func (h *dockedHost) Show(v Viewer) {
	if item, ok := h.items[v]; ok {
		h.tabs.Select(item)
		return
	}
	item := container.NewTabItem(v.Title(), v.Content())
	h.items[v] = item
	h.order = append(h.order, v)
	h.tabs.Append(item)
	h.tabs.Select(item)
}

// Hide はタブを取り除く。
func (h *dockedHost) Hide(v Viewer) {
	item, ok := h.items[v]
	if !ok {
		return
	}
	delete(h.items, v)
	for i, x := range h.order {
		if x == v {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
	h.tabs.Remove(item)
	v.OnClose()
}

// IsVisible はタブがあるかを返す。
func (h *dockedHost) IsVisible(v Viewer) bool {
	_, ok := h.items[v]
	return ok
}

// Visible は表示しているビューアを順に返す。
func (h *dockedHost) Visible() []Viewer {
	return append([]Viewer(nil), h.order...)
}

// Close はすべてのタブを取り除く。
func (h *dockedHost) Close() {
	for _, v := range h.Visible() {
		h.Hide(v)
	}
}

// switchHost は配置を切り替える。
//
// 表示していたビューアを新しい置き場所で開き直す。Content を作り直すのは、
// 同じ fyne.CanvasObject を 2 か所に置けないためである。
func switchHost(from, to ViewerHost) {
	visible := from.Visible()
	from.Close()
	for _, v := range visible {
		to.Show(v)
	}
}
