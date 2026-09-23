package ui

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// defaultLogLines はログビューアに表示する行数の既定値。
const defaultLogLines = 1000

// logViewer はログビューア（設計書 09 編 §9.8）。
//
// Logger が保持する行のうち、選んだカテゴリと検索語に合うものを
// 新しい方から上限の行数まで表示する。
type logViewer struct {
	u *UI

	grid    *widget.TextGrid
	filter  *widget.Select
	search  *widget.Entry
	limit   *widget.Entry
	enabled *widget.CheckGroup

	// lastCount は前回表示した行数と最後の行。変わっていなければ描き直さない。
	lastCount int
	lastText  string
}

// logs はログビューアを返す。無ければ作る。
func (u *UI) logs() *logViewer {
	if u.logViewer == nil {
		u.logViewer = &logViewer{u: u}
	}
	return u.logViewer
}

// allCategoriesLabel はフィルタで全カテゴリを選ぶ項目。
const allCategoriesLabel = "すべて"

func (v *logViewer) Title() string { return "ログ" }

func (v *logViewer) Content() fyne.CanvasObject {
	// grid は最後に作る。入力欄を作る途中の OnChanged で redraw が
	// 呼ばれても、grid が nil の間は何もしない。
	v.grid = nil
	v.lastCount, v.lastText = -1, ""

	v.filter = widget.NewSelect(append([]string{allCategoriesLabel}, debug.CategoryNames()...),
		func(string) { v.redraw() })
	v.filter.SetSelected(allCategoriesLabel)
	v.search = widget.NewEntry()
	v.search.SetPlaceHolder("検索")
	v.search.OnChanged = func(string) { v.redraw() }
	v.limit = widget.NewEntry()
	v.limit.SetText(strconv.Itoa(defaultLogLines))
	v.limit.OnChanged = func(string) { v.redraw() }

	v.enabled = widget.NewCheckGroup(debug.CategoryNames(), nil)
	v.enabled.Horizontal = true
	v.enabled.SetSelected(v.u.cfg.Debug.LogCategories)
	v.enabled.OnChanged = v.setEnabled

	clear := widget.NewButton("消去", func() {
		v.u.emu.Debugger().Logger().Clear()
		v.redraw()
	})

	bar := container.NewBorder(nil, nil,
		container.NewHBox(widget.NewLabel("表示"), v.filter),
		container.NewHBox(widget.NewLabel("行数"), v.limit, clear),
		v.search)
	top := container.NewVBox(
		container.NewHScroll(container.NewHBox(widget.NewLabel("記録する"), v.enabled)),
		bar,
	)
	v.grid = widget.NewTextGrid()
	v.redraw()
	return container.NewBorder(top, nil, nil, nil, container.NewScroll(v.grid))
}

// setEnabled は記録するカテゴリを変え、設定にも残す。
func (v *logViewer) setEnabled(names []string) {
	cats, err := debug.ParseCategories(names)
	if err != nil {
		v.u.showError(err)
		return
	}
	v.u.cfg.Debug.LogCategories = append([]string(nil), names...)
	v.u.emu.WithDebugger(func(d *debug.Debugger) { d.SetLogCategories(cats) })
}

// Refresh は新しい行があれば描き直す。
func (v *logViewer) Refresh() { v.redraw() }

// redraw は条件に合う行を TextGrid へ流し込む。
func (v *logViewer) redraw() {
	if v.grid == nil {
		return
	}
	var filter debug.Category
	if name := v.filter.Selected; name != "" && name != allCategoriesLabel {
		filter, _ = debug.CategoryByName(name)
	}
	limit, err := strconv.Atoi(strings.TrimSpace(v.limit.Text))
	if err != nil || limit <= 0 {
		limit = defaultLogLines
	}
	entries := v.u.emu.Debugger().Logger().Entries(filter, v.search.Text, limit)

	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Time.Format("15:04:05.000"))
		b.WriteString(" [")
		b.WriteString(e.Category.String())
		b.WriteString("] ")
		b.WriteString(e.Message)
		b.WriteByte('\n')
	}
	text := b.String()
	if len(entries) == v.lastCount && text == v.lastText {
		return
	}
	v.lastCount, v.lastText = len(entries), text
	v.grid.SetText(strings.TrimRight(text, "\n"))
}

func (v *logViewer) OnClose() {}
