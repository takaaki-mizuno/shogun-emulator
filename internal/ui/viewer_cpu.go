package ui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// disasmLines は CPU デバッガに並べる逆アセンブルの行数。
const disasmLines = 32

// cpuViewer は CPU デバッガ（設計書 09 編 §9.4.6・§9.5）。
//
// 逆アセンブル・レジスタ・スタック・コールスタック・保留中の割り込み・
// 実行制御のボタン・ブレークポイント一覧を 1 つにまとめる。
type cpuViewer struct {
	u    *UI
	open bool

	grid  *tapGrid
	regs  map[debug.Register]*widget.Entry
	flags *widget.Label
	pos   *widget.Label
	irq   *widget.Label
	stack *widget.Label
	calls *widget.Label
	// cursor は選んだ行のアドレス。カーソル位置まで実行とブレーク
	// ポイントの切り替えに使う。
	cursor    uint16
	hasCursor bool
	cursorLbl *widget.Label
	panel     *breakpointPanel

	view debug.CPUView
}

// cpuDebugger は CPU デバッガを返す。無ければ作る。
func (u *UI) cpuDebugger() *cpuViewer {
	if u.cpuViewer == nil {
		u.cpuViewer = &cpuViewer{u: u}
	}
	return u.cpuViewer
}

// registerOrder はレジスタの並び順。
var registerOrder = []debug.Register{debug.RegPC, debug.RegA, debug.RegX, debug.RegY, debug.RegS, debug.RegP}

// registerName はレジスタの表示名。
func registerName(r debug.Register) string {
	switch r {
	case debug.RegPC:
		return "PC"
	case debug.RegA:
		return "A"
	case debug.RegX:
		return "X"
	case debug.RegY:
		return "Y"
	case debug.RegS:
		return "S"
	case debug.RegP:
		return "P"
	}
	return "?"
}

func (v *cpuViewer) Title() string { return "CPU デバッガ" }

// Content は表示を組み立て、CPU の表示に必要なフックを有効にする。
func (v *cpuViewer) Content() fyne.CanvasObject {
	if !v.open {
		v.open = true
		v.u.debugUsers.cpu++
		v.u.applyDebugFeatures()
	}

	v.grid = newTapGrid()
	v.grid.onTap = v.selectRow
	v.cursorLbl = widget.NewLabel("カーソル: なし")

	v.regs = map[debug.Register]*widget.Entry{}
	regForm := container.NewGridWithColumns(4)
	for _, r := range registerOrder {
		e := widget.NewEntry()
		reg := r
		e.OnSubmitted = func(s string) { v.setRegister(reg, s) }
		v.regs[r] = e
		regForm.Add(widget.NewLabel(registerName(r)))
		regForm.Add(e)
	}
	v.flags = widget.NewLabel("")
	v.pos = widget.NewLabel("")
	v.irq = widget.NewLabel("")
	v.stack = widget.NewLabel("")
	v.stack.Wrapping = fyne.TextWrapWord
	v.calls = widget.NewLabel("")

	e := v.u.emu
	buttons := container.NewGridWithColumns(5,
		widget.NewButton("実行", func() { e.Resume() }),
		widget.NewButton("停止", func() { e.Pause() }),
		widget.NewButton("サイクル", func() { e.Step(emu.StepCycle) }),
		widget.NewButton("命令", func() { e.Step(emu.StepInstruction) }),
		widget.NewButton("オーバー", func() { e.Step(emu.StepOver) }),
		widget.NewButton("アウト", func() { e.Step(emu.StepOut) }),
		widget.NewButton("スキャンライン", func() { e.Step(emu.StepScanline) }),
		widget.NewButton("フレーム", func() { e.Step(emu.StepFrame) }),
		widget.NewButton("カーソルまで", v.runToCursor),
		widget.NewButton("ブレーク切替", v.toggleBreakAtCursor),
	)

	v.panel = newBreakpointPanel(v.u)

	side := container.NewVBox(
		widget.NewLabelWithStyle("レジスタ", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		regForm, v.flags, v.pos, v.irq,
		widget.NewLabelWithStyle("スタック", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		v.stack,
		widget.NewLabelWithStyle("コールスタック（推定）", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		v.calls,
	)
	top := container.NewBorder(nil, container.NewVBox(v.cursorLbl, buttons), nil,
		container.NewVScroll(side), v.grid)
	split := container.NewVSplit(top, v.panel.root)
	split.Offset = 0.7
	v.Refresh()
	return split
}

// Refresh はエミュレーションゴルーチンから表示内容を受け取って描き直す。
func (v *cpuViewer) Refresh() {
	if v.grid == nil {
		return
	}
	var view debug.CPUView
	v.u.emu.WithDebugger(func(d *debug.Debugger) { view = d.CPUView(disasmLines) })
	v.view = view
	v.drawListing()
	v.drawState()
	v.panel.refresh()
}

// drawListing は逆アセンブルを TextGrid へ流し込む。
func (v *cpuViewer) drawListing() {
	rows := make([]widget.TextGridRow, 0, len(v.view.Lines))
	for i, l := range v.view.Lines {
		current := i == v.view.CurrentLine
		text := debug.FormatLine(l, current, v.view.ExecBreaks[l.Addr])
		if l.Label != "" {
			text += "  " + l.Label + ":"
		}
		fg, bg := lineColors(l, current, v.hasCursor && l.Addr == v.cursor)
		row := textRow(text, cellStyle(fg, bg))
		if v.view.ExecBreaks[l.Addr] && len(row.Cells) > 2 {
			row.Cells[2].Style = cellStyle(colorBreakpoint, bg)
		}
		rows = append(rows, row)
	}
	setRows(v.grid.grid, rows)
}

// lineColors は逆アセンブルの 1 行の文字色と背景色を決める。
func lineColors(l debug.Line, current, cursor bool) (fg, bg color.Color) {
	switch {
	case !l.Official && !l.Data:
		fg = colorUnofficial
	case l.Estimated:
		fg = dimColor()
	}
	switch {
	case current:
		bg = colorPCBackground
	case cursor:
		bg = colorCursorBackground
	}
	return fg, bg
}

// drawState はレジスタとスタックの表示を更新する。
func (v *cpuViewer) drawState() {
	vw := v.view
	values := map[debug.Register]string{
		debug.RegPC: fmt.Sprintf("$%04X", vw.PC),
		debug.RegA:  fmt.Sprintf("$%02X", vw.A),
		debug.RegX:  fmt.Sprintf("$%02X", vw.X),
		debug.RegY:  fmt.Sprintf("$%02X", vw.Y),
		debug.RegS:  fmt.Sprintf("$%02X", vw.S),
		debug.RegP:  fmt.Sprintf("$%02X", vw.P),
	}
	focused := v.focusedObject()
	for r, e := range v.regs {
		// 入力中の欄は書き換えない。
		if focused != e {
			e.SetText(values[r])
		}
	}
	v.flags.SetText("フラグ: " + vw.PFlags())

	pos := fmt.Sprintf("サイクル: %d\nフレーム: %d  スキャンライン: %d  ドット: %d",
		vw.Cycles, vw.Frame, vw.Scanline, vw.Dot)
	if g, ok := v.u.emu.GatePosition(); ok {
		pos += fmt.Sprintf("\n命令の途中（サイクル %d、行 %d、ドット %d）", g.Cycles, g.Scanline, g.Dot)
	}
	v.pos.SetText(pos)

	irq := "保留中の割り込み: "
	var pending []string
	if vw.NMIPending {
		pending = append(pending, "NMI")
	}
	for _, s := range vw.IRQSources {
		pending = append(pending, "IRQ（"+s+"）")
	}
	if len(pending) == 0 {
		irq += "なし"
	} else {
		irq += strings.Join(pending, "、")
	}
	v.irq.SetText(irq)

	v.stack.SetText(formatStack(vw.S, vw.Stack))
	v.calls.SetText(formatCalls(vw.Calls))

	if v.hasCursor {
		v.cursorLbl.SetText(fmt.Sprintf("カーソル: $%04X", v.cursor))
	}
}

// formatStack は $01FF から S+1 までを並べる。上に近い（新しい）値を先に出す。
func formatStack(s uint8, stack []uint8) string {
	if len(stack) == 0 {
		return "（空）"
	}
	var b strings.Builder
	for i, val := range stack {
		addr := 0x0100 + int(s) + 1 + i
		if i > 0 {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "%04X:%02X", addr, val)
	}
	return b.String()
}

// formatCalls はコールスタックを新しい順に並べる。
func formatCalls(frames []debug.CallFrame) string {
	if len(frames) == 0 {
		return "（なし）"
	}
	var b strings.Builder
	for i := len(frames) - 1; i >= 0; i-- {
		f := frames[i]
		fmt.Fprintf(&b, "%s $%04X ← $%04X\n", f.Kind, f.To, f.From)
	}
	return strings.TrimRight(b.String(), "\n")
}

// focusedObject はこのビューアが置かれたキャンバスでフォーカスを持つ
// ものを返す。
func (v *cpuViewer) focusedObject() fyne.Focusable {
	c := fyne.CurrentApp().Driver().CanvasForObject(v.grid)
	if c == nil {
		return nil
	}
	return c.Focused()
}

// selectRow はタップした行をカーソルにする。
func (v *cpuViewer) selectRow(row, _ int) {
	if row < 0 || row >= len(v.view.Lines) {
		return
	}
	v.cursor = v.view.Lines[row].Addr
	v.hasCursor = true
	v.cursorLbl.SetText(fmt.Sprintf("カーソル: $%04X", v.cursor))
	v.drawListing()
}

// runToCursor はカーソルの行まで実行する。
func (v *cpuViewer) runToCursor() {
	if v.hasCursor {
		v.u.emu.RunTo(v.cursor)
	}
}

// toggleBreakAtCursor はカーソルの行の実行ブレークポイントを切り替える。
func (v *cpuViewer) toggleBreakAtCursor() {
	if !v.hasCursor {
		return
	}
	addr := v.cursor
	v.u.emu.WithDebugger(func(d *debug.Debugger) {
		for _, b := range d.Breakpoints() {
			if b.Kind == debug.BreakExec && b.AddrStart == addr && b.AddrEnd == addr {
				d.RemoveBreakpoint(b.ID)
				return
			}
		}
		d.AddBreakpoint(debug.Breakpoint{Kind: debug.BreakExec, AddrStart: addr, AddrEnd: addr, Enabled: true})
	})
	v.u.emu.SaveSymbols()
	v.Refresh()
}

// setRegister は入力欄の値をレジスタへ書く。
func (v *cpuViewer) setRegister(r debug.Register, s string) {
	val, err := parseHexValue(s)
	if err != nil {
		v.u.showError(err)
		return
	}
	v.u.emu.WithDebugger(func(d *debug.Debugger) {
		if d.Machine() != nil {
			d.SetRegister(r, val)
		}
	})
	v.Refresh()
}

// parseHexValue は "$42"・"42"（16 進）・"#$42" を読む。
func parseHexValue(s string) (uint16, error) {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "#")
	t = strings.TrimPrefix(t, "$")
	v, err := strconv.ParseUint(t, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("16 進の値として読めない（%q）", s)
	}
	return uint16(v), nil
}

// OnClose は CPU の表示に使うフックを外す。
func (v *cpuViewer) OnClose() {
	if !v.open {
		return
	}
	v.open = false
	v.u.debugUsers.cpu--
	v.u.applyDebugFeatures()
}
