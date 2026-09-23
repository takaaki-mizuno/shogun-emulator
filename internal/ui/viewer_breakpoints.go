package ui

import (
	"errors"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// breakpointPanel はブレークポイントの一覧と追加の欄。
//
// CPU デバッガとブレークポイント一覧のビューアの両方に置く。
type breakpointPanel struct {
	u     *UI
	list  *widget.List
	items []debug.Breakpoint

	kind      *widget.Select
	addr      *widget.Entry
	event     *widget.Select
	condition *widget.Entry
	root      fyne.CanvasObject
}

// breakKindLabels は種別の選択肢。debug.BreakKind の順に並べる。
var breakKindLabels = []string{i18n.T(i18n.MenuRun), i18n.T(i18n.BPKindRead), i18n.T(i18n.BPKindWrite), i18n.T(i18n.BPKindPPU), i18n.T(i18n.BPKindEvent)}

// eventLabels はイベントの選択肢。debug.EventKind の順に並べる。
func eventLabels() []string {
	var out []string
	for e := debug.EventNMI; e <= debug.EventMMC3IRQReloadWithoutClocks; e++ {
		out = append(out, e.String())
	}
	return out
}

// newBreakpointPanel は一覧を作る。
func newBreakpointPanel(u *UI) *breakpointPanel {
	p := &breakpointPanel{u: u}
	p.list = widget.NewList(
		func() int { return len(p.items) },
		func() fyne.CanvasObject {
			check := widget.NewCheck("", nil)
			label := widget.NewLabel("")
			cond := widget.NewButton(i18n.T(i18n.BPConditionButton), nil)
			del := widget.NewButton(i18n.T(i18n.CommonDelete), nil)
			return container.NewBorder(nil, nil, check, container.NewHBox(cond, del), label)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= len(p.items) {
				return
			}
			b := p.items[i]
			row := o.(*fyne.Container)
			label := row.Objects[0].(*widget.Label)
			check := row.Objects[1].(*widget.Check)
			buttons := row.Objects[2].(*fyne.Container)
			label.SetText(i18n.T(i18n.BPRow, b.ID, b.Describe(), b.HitCount))
			check.OnChanged = nil
			check.SetChecked(b.Enabled)
			id := b.ID
			check.OnChanged = func(on bool) { p.setEnabled(id, on) }
			buttons.Objects[0].(*widget.Button).OnTapped = func() { p.editCondition(id, b.Condition) }
			buttons.Objects[1].(*widget.Button).OnTapped = func() { p.remove(id) }
		},
	)

	p.kind = widget.NewSelect(breakKindLabels, func(string) { p.updateInputs() })
	p.addr = widget.NewEntry()
	p.addr.SetPlaceHolder(i18n.T(i18n.BPAddrPlaceholder))
	p.event = widget.NewSelect(eventLabels(), nil)
	p.condition = widget.NewEntry()
	p.condition.SetPlaceHolder(i18n.T(i18n.BPConditionPlaceholder))
	add := widget.NewButton(i18n.T(i18n.CommonAdd), p.add)
	p.kind.SetSelectedIndex(0)
	p.event.SetSelectedIndex(0)

	form := container.NewVBox(
		container.NewBorder(nil, nil, p.kind, add, p.addr),
		container.NewBorder(nil, nil, widget.NewLabel(i18n.T(i18n.BPKindEvent)), nil, p.event),
		p.condition,
	)
	p.root = container.NewBorder(nil, form, nil, nil, p.list)
	return p
}

// updateInputs は種別に合わせて入力欄を切り替える。
func (p *breakpointPanel) updateInputs() {
	if p.kind.SelectedIndex() == int(debug.BreakEvent) {
		p.addr.Disable()
		p.event.Enable()
		return
	}
	p.addr.Enable()
	p.event.Disable()
}

// refresh は一覧を読み直す。
func (p *breakpointPanel) refresh() {
	p.items = p.u.emu.Debugger().Breakpoints()
	p.list.Refresh()
}

// add は入力欄の内容からブレークポイントを加える。
func (p *breakpointPanel) add() {
	b, err := p.parseInputs()
	if err != nil {
		p.u.showError(err)
		return
	}
	var cerr error
	p.u.emu.WithDebugger(func(d *debug.Debugger) {
		id := d.AddBreakpoint(b)
		if expr := strings.TrimSpace(p.condition.Text); expr != "" {
			cerr = d.SetBreakpointCondition(id, expr)
			if cerr != nil {
				d.RemoveBreakpoint(id)
			}
		}
	})
	if cerr != nil {
		p.u.showError(cerr)
		return
	}
	p.u.emu.SaveSymbols()
	p.condition.SetText("")
	p.refresh()
}

// parseInputs は入力欄を読む。
func (p *breakpointPanel) parseInputs() (debug.Breakpoint, error) {
	b := debug.Breakpoint{Kind: debug.BreakKind(p.kind.SelectedIndex()), Enabled: true}
	text := strings.TrimSpace(p.addr.Text)
	switch b.Kind {
	case debug.BreakExec, debug.BreakRead, debug.BreakWrite:
		start, end, err := parseAddrRange(text)
		if err != nil {
			return b, err
		}
		b.AddrStart, b.AddrEnd = start, end
	case debug.BreakPPUPosition:
		line, dot, err := parsePPUPosition(text)
		if err != nil {
			return b, err
		}
		b.Scanline, b.Dot = line, dot
	case debug.BreakEvent:
		b.Event = debug.EventKind(p.event.SelectedIndex())
	}
	return b, nil
}

// parseAddrRange は "$C000" または "$0300-$03FF" を読む。
func parseAddrRange(s string) (uint16, uint16, error) {
	parts := strings.SplitN(s, "-", 2)
	start, err := debug.ParseAddr(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end := start
	if len(parts) == 2 {
		if end, err = debug.ParseAddr(parts[1]); err != nil {
			return 0, 0, err
		}
	}
	return start, end, nil
}

// parsePPUPosition は "行,ドット" を 10 進で読む。
func parsePPUPosition(s string) (int, int, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New(i18n.T(i18n.BPPPUFormat, s))
	}
	line, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	dot, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || line < 0 || line > 311 || dot < 0 || dot > 340 {
		return 0, 0, errors.New(i18n.T(i18n.BPPPURange, s))
	}
	return line, dot, nil
}

// setEnabled は有効・無効を切り替える。
func (p *breakpointPanel) setEnabled(id int, on bool) {
	p.u.emu.WithDebugger(func(d *debug.Debugger) { d.SetBreakpointEnabled(id, on) })
	p.u.emu.SaveSymbols()
	p.refresh()
}

// remove はブレークポイントを取り除く。
func (p *breakpointPanel) remove(id int) {
	p.u.emu.WithDebugger(func(d *debug.Debugger) { d.RemoveBreakpoint(id) })
	p.u.emu.SaveSymbols()
	p.refresh()
}

// editCondition は条件式を入力するダイアログを出す。
func (p *breakpointPanel) editCondition(id int, current *debug.Condition) {
	entry := widget.NewEntry()
	if current != nil {
		entry.SetText(current.Expr)
	}
	dialog.ShowForm(i18n.T(i18n.BPConditionTitle), i18n.T(i18n.MenuSettings), i18n.T(i18n.CommonCancel),
		[]*widget.FormItem{widget.NewFormItem(i18n.T(i18n.BPConditionField), entry)},
		func(ok bool) {
			if !ok {
				return
			}
			var err error
			p.u.emu.WithDebugger(func(d *debug.Debugger) {
				err = d.SetBreakpointCondition(id, strings.TrimSpace(entry.Text))
			})
			if err != nil {
				p.u.showError(err)
				return
			}
			p.u.emu.SaveSymbols()
			p.refresh()
		}, p.u.win)
}

// breakpointViewer はブレークポイント一覧を単独で表示するビューア。
type breakpointViewer struct {
	u     *UI
	panel *breakpointPanel
}

// newBreakpointViewer はビューアを作る。
func newBreakpointViewer(u *UI) *breakpointViewer { return &breakpointViewer{u: u} }

func (v *breakpointViewer) Title() string { return i18n.T(i18n.ViewerBreakpoints) }

func (v *breakpointViewer) Content() fyne.CanvasObject {
	v.panel = newBreakpointPanel(v.u)
	v.panel.refresh()
	return v.panel.root
}

func (v *breakpointViewer) Refresh() {
	if v.panel != nil {
		v.panel.refresh()
	}
}

func (v *breakpointViewer) OnClose() {}
