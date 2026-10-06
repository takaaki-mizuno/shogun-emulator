package ui

import (
	"errors"
	"fmt"
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// rowWidths は 1 行のバイト数の選択肢。
var rowWidths = []string{"8", "16", "32"}

// memoryViewer はメモリビューア（設計書 09 編 §9.4.5）。
//
// 見えている行だけを読み、TextGrid へ流し込む。空間全体を毎回描くと、
// PRG-ROM のような大きい空間で UI スレッドが止まるためである。
// 複数を同時に開ける。
type memoryViewer struct {
	u    *UI
	id   int
	open bool

	space  debug.Space
	perRow int
	// top は画面の先頭に表示する行の番号（行 = perRow バイト）。
	top int
	// cursor は選んでいるバイトのオフセット。-1 は選んでいないことを表す。
	cursor int
	// lowNibble は次にタイプした桁が下位の桁であることを表す。
	lowNibble bool

	grid     *tapGrid
	slider   *widget.Slider
	spaceSel *widget.Select
	info     *widget.Label
	gotoE    *widget.Entry
	searchE  *widget.Entry
	textMode *widget.Check
	labelE   *widget.Entry
	watchG   *widget.TextGrid

	// size は空間の大きさ。Refresh のたびに読み直す。
	size   int
	layout memLayout
	// sliderSet は Slider の値を表示側から変えている最中であることを表す。
	sliderSet bool
}

// newMemoryViewer はメモリビューアを作る。番号を振って題名を区別する。
func (u *UI) newMemoryViewer() *memoryViewer {
	u.memoryCount++
	return &memoryViewer{u: u, id: u.memoryCount, space: debug.SpaceCPU, perRow: 16, cursor: -1}
}

func (v *memoryViewer) Title() string { return i18n.T(i18n.MemTitle, v.id) }

func (v *memoryViewer) Content() fyne.CanvasObject {
	if !v.open {
		v.open = true
		v.u.debugUsers.memory++
		v.u.applyDebugFeatures()
	}

	v.grid = newTapGrid()
	v.grid.onTap = v.tap
	v.grid.onScroll = func(n int) { v.scrollTo(v.top + n) }
	v.grid.onRune = v.typeRune
	v.grid.onKey = v.typeKey

	v.slider = widget.NewSlider(0, 1)
	v.slider.Orientation = widget.Vertical
	v.slider.OnChanged = func(f float64) {
		if v.sliderSet {
			return
		}
		// 縦のスライダーは下が最小なので、上端を先頭に対応させる。
		v.scrollTo(int(v.slider.Max - f))
	}

	names := make([]string, 0)
	for _, s := range debug.Spaces() {
		names = append(names, s.String())
	}
	v.spaceSel = widget.NewSelect(names, func(name string) {
		for _, s := range debug.Spaces() {
			if s.String() == name && s != v.space {
				v.space, v.top, v.cursor = s, 0, -1
				v.Refresh()
			}
		}
	})
	v.spaceSel.SetSelected(v.space.String())
	width := widget.NewSelect(rowWidths, func(s string) {
		n, _ := strconv.Atoi(s)
		if n > 0 && n != v.perRow {
			first := v.top * v.perRow
			v.perRow = n
			v.top = first / n
			v.Refresh()
		}
	})
	width.SetSelected(strconv.Itoa(v.perRow))

	v.gotoE = widget.NewEntry()
	v.gotoE.SetPlaceHolder(i18n.T(i18n.MemAddress))
	v.gotoE.OnSubmitted = v.gotoAddr
	v.searchE = widget.NewEntry()
	v.searchE.SetPlaceHolder(i18n.T(i18n.MemSearchPlaceholder))
	v.searchE.OnSubmitted = func(string) { v.search() }
	v.textMode = widget.NewCheck(i18n.T(i18n.MemTextMode), nil)
	v.labelE = widget.NewEntry()
	v.labelE.SetPlaceHolder(i18n.T(i18n.MemLabelPlaceholder))
	v.labelE.OnSubmitted = v.setLabel
	v.info = widget.NewLabel("")
	v.watchG = widget.NewTextGrid()

	toolbar := container.NewVBox(
		container.NewHBox(v.spaceSel, widget.NewLabel(i18n.T(i18n.MemRowWidth)), width,
			widget.NewLabel(i18n.T(i18n.MemGoto)), container.NewGridWrap(fyne.NewSize(120, 36), v.gotoE)),
		container.NewBorder(nil, nil, nil,
			container.NewHBox(v.textMode, widget.NewButton(i18n.T(i18n.MemFindNext), v.search)), v.searchE),
	)
	watchBox := container.NewVBox(
		widget.NewLabelWithStyle(i18n.T(i18n.MemWatch), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			widget.NewButton(i18n.T(i18n.MemWatchAdd), v.addWatch),
			widget.NewButton(i18n.T(i18n.MemWatchRemove), v.removeWatch),
		),
		v.watchG,
		widget.NewLabelWithStyle(i18n.T(i18n.MemLabel), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		v.labelE,
	)
	bottom := container.NewVBox(v.info)
	body := container.NewBorder(nil, nil, nil, v.slider, v.grid)
	v.Refresh()
	return container.NewBorder(toolbar, bottom, nil, container.NewVScroll(watchBox), body)
}

// memorySnapshot はエミュレーションゴルーチンで読んだ表示内容。
type memorySnapshot struct {
	size  int
	data  []uint8
	pc    int
	sp    int
	watch []watchValue
	label string
}

// watchValue はウォッチリストの 1 項目。
type watchValue struct {
	addr  uint16
	name  string
	value uint8
}

// Refresh は見えている範囲を読み直して描く。
func (v *memoryViewer) Refresh() {
	if v.grid == nil {
		return
	}
	rows := v.grid.visibleRows() - 1
	rows = max(rows, 1)
	// 区切り線の分だけ行が減るため、2 行多く読む。
	first := v.top * v.perRow
	count := (rows + 2) * v.perRow
	snap := v.read(first, count)
	v.size = snap.size
	v.layout = memLayout{perRow: v.perRow, digits: addrDigits(max(v.size, 1), v.space.Base()), base: v.space.Base()}

	maxTop := max((v.size+v.perRow-1)/v.perRow-rows, 0)
	if v.top > maxTop {
		v.top = maxTop
	}
	v.sliderSet = true
	v.slider.Max = float64(maxTop)
	if maxTop == 0 {
		v.slider.Max = 1
	}
	v.slider.SetValue(v.slider.Max - float64(v.top))
	v.sliderSet = false

	v.draw(first, rows, snap)
	v.drawWatch(snap.watch)
	v.drawInfo(snap)
}

// read は first から count バイトとカーソル周りの情報を読む。
func (v *memoryViewer) read(first, count int) memorySnapshot {
	snap := memorySnapshot{pc: -1, sp: -1}
	v.u.emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		if n == nil {
			return
		}
		snap.size = debug.Size(n, v.space)
		if first+count > snap.size {
			count = max(snap.size-first, 0)
		}
		snap.data = make([]uint8, count)
		debug.ReadMemory(n, v.space, first, snap.data)
		snap.pc, snap.sp = v.pointers(n)
		if sym := d.Symbols(); sym != nil {
			for _, a := range sym.Watch() {
				snap.watch = append(snap.watch, watchValue{addr: a, name: d.Label(a), value: n.Bus.Peek(a)})
			}
			if a, ok := v.cpuAddr(v.cursor); ok {
				snap.label = d.Label(a)
			}
		}
	})
	return snap
}

// pointers は PC とスタックポインタが指す位置をこの空間のオフセットで返す。
// この空間に無いときは -1。
func (v *memoryViewer) pointers(n *nes.NES) (pc, sp int) {
	pc, sp = -1, -1
	switch v.space {
	case debug.SpaceCPU:
		pc = int(n.CPU.PC)
		sp = 0x100 | int(n.CPU.S)
	case debug.SpaceRAM:
		sp = 0x100 | int(n.CPU.S)
	case debug.SpacePRGROM:
		if off, ok := n.Cart.PRGOffset(n.CPU.PC); ok {
			pc = off
		}
	}
	return pc, sp
}

// cpuAddr はこの空間のオフセットを CPU アドレスにする。対応しないとき false。
func (v *memoryViewer) cpuAddr(off int) (uint16, bool) {
	if off < 0 {
		return 0, false
	}
	switch v.space {
	case debug.SpaceCPU, debug.SpaceRAM:
		return uint16(off), true
	case debug.SpacePRGRAM:
		return uint16(0x6000 + off), true
	}
	return 0, false
}

// draw は読んだバイトを色付きで TextGrid へ流し込む。
func (v *memoryViewer) draw(first, rows int, snap memorySnapshot) {
	l := &v.layout
	l.rows = l.rows[:0]
	out := make([]widget.TextGridRow, 0, rows+1)
	out = append(out, textRow(l.header(), cellStyle(dimColor(), nil)))
	l.rows = append(l.rows, -1)

	zeroPage := v.space == debug.SpaceCPU || v.space == debug.SpaceRAM
	changes := v.u.emu.Debugger().Changes()
	for off := first; len(out) <= rows && off-first < len(snap.data); off += v.perRow {
		if separatorBefore(zeroPage, off) && off != first {
			out = append(out, textRow(strings.Repeat("─", len(l.line(off, nil))), cellStyle(dimColor(), nil)))
			l.rows = append(l.rows, -1)
			if len(out) > rows {
				break
			}
		}
		end := min(off-first+v.perRow, len(snap.data))
		data := snap.data[off-first : end]
		row := textRow(l.line(off, data), nil)
		for i := range data {
			bg := v.byteBackground(off+i, snap, changes)
			if bg == nil {
				continue
			}
			st := cellStyle(nil, bg)
			c := l.byteColumn(i)
			row.Cells[c].Style = st
			row.Cells[c+1].Style = st
			row.Cells[l.asciiColumn()+i].Style = st
		}
		out = append(out, row)
		l.rows = append(l.rows, off)
	}
	setRows(v.grid.grid, out)
}

// byteBackground は 1 バイトの背景色を決める。カーソル・PC・SP・変更の順に優先する。
func (v *memoryViewer) byteBackground(off int, snap memorySnapshot, changes *debug.ChangeTracker) color.Color {
	switch off {
	case v.cursor:
		return colorCursorBackground
	case snap.pc:
		return colorPCBackground
	case snap.sp:
		return colorSPBackground
	}
	if r, roff, ok := v.space.Region(off); ok {
		return heatColor(changes.Heat(r, roff))
	}
	return nil
}

// drawWatch はウォッチリストを描く。
func (v *memoryViewer) drawWatch(list []watchValue) {
	var b strings.Builder
	for _, w := range list {
		fmt.Fprintf(&b, "$%04X %-12s %02X (%d)\n", w.addr, w.name, w.value, w.value)
	}
	if len(list) == 0 {
		b.WriteString(i18n.T(i18n.CommonNone))
	}
	v.watchG.SetText(strings.TrimRight(b.String(), "\n"))
}

// drawInfo はカーソル位置と編集の方法を表示する。
func (v *memoryViewer) drawInfo(snap memorySnapshot) {
	if v.cursor < 0 {
		v.info.SetText(i18n.T(i18n.MemHint))
		return
	}
	mode := i18n.T(i18n.MemNoSideEffect)
	if v.u.cfg.Debug.MemoryEditWrite {
		mode = i18n.T(i18n.MemBusWrite)
	}
	text := i18n.T(i18n.MemCursor, v.layout.digits, v.space.Base()+v.cursor, mode)
	if snap.label != "" {
		text += i18n.T(i18n.MemCursorLabel) + snap.label
	}
	v.info.SetText(text)
	if c := fyne.CurrentApp().Driver().CanvasForObject(v.labelE); c == nil || c.Focused() != v.labelE {
		v.labelE.SetText(snap.label)
	}
}

// scrollTo は先頭の行を変える。
func (v *memoryViewer) scrollTo(top int) {
	v.top = max(top, 0)
	v.Refresh()
}

// tap はタップしたバイトをカーソルにする。
func (v *memoryViewer) tap(row, col int) {
	off, _, ok := v.layout.hit(row, col)
	if !ok || off >= v.size {
		return
	}
	v.cursor, v.lowNibble = off, false
	v.Refresh()
}

// moveCursor はカーソルを delta バイト動かし、見える位置へスクロールする。
func (v *memoryViewer) moveCursor(delta int) {
	if v.cursor < 0 {
		return
	}
	c := v.cursor + delta
	if c < 0 || c >= v.size {
		return
	}
	v.cursor, v.lowNibble = c, false
	v.ensureVisible()
	v.Refresh()
}

// ensureVisible はカーソルが画面に入るよう先頭の行を合わせる。
func (v *memoryViewer) ensureVisible() {
	rows := max(v.grid.visibleRows()-3, 1)
	row := v.cursor / v.perRow
	switch {
	case row < v.top:
		v.top = row
	case row >= v.top+rows:
		v.top = row - rows + 1
	}
}

// typeKey は矢印キーでカーソルを動かす。
func (v *memoryViewer) typeKey(k *fyne.KeyEvent) {
	switch k.Name {
	case fyne.KeyLeft:
		v.moveCursor(-1)
	case fyne.KeyRight:
		v.moveCursor(1)
	case fyne.KeyUp:
		v.moveCursor(-v.perRow)
	case fyne.KeyDown:
		v.moveCursor(v.perRow)
	case fyne.KeyPageUp:
		v.scrollTo(v.top - v.grid.visibleRows())
	case fyne.KeyPageDown:
		v.scrollTo(v.top + v.grid.visibleRows())
	}
}

// typeRune は 16 進の桁を受け取り、カーソルのバイトを書き換える。
//
// 上位の桁、下位の桁の順に書き、下位の桁を書いたら次のバイトへ進む。
func (v *memoryViewer) typeRune(r rune) {
	if v.cursor < 0 {
		return
	}
	d, err := strconv.ParseUint(string(r), 16, 8)
	if err != nil {
		return
	}
	var cur uint8
	v.u.emu.WithDebugger(func(dbg *debug.Debugger) {
		if n := dbg.Machine(); n != nil {
			var b [1]uint8
			debug.ReadMemory(n, v.space, v.cursor, b[:])
			cur = b[0]
		}
	})
	val := cur&0x0F | uint8(d)<<4
	if v.lowNibble {
		val = cur&0xF0 | uint8(d)
	}
	if err := v.u.emu.Poke(v.space, v.cursor, val, v.u.cfg.Debug.MemoryEditWrite); err != nil {
		v.u.showError(err)
		return
	}
	if v.lowNibble {
		v.moveCursor(1)
		return
	}
	v.lowNibble = true
	v.Refresh()
}

// gotoAddr は入力したアドレスへ移動する。
func (v *memoryViewer) gotoAddr(s string) {
	a, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(s), "$"), 16, 32)
	if err != nil {
		v.u.showError(errors.New(i18n.T(i18n.MemBadAddr, s)))
		return
	}
	off := int(a) - v.space.Base()
	if off < 0 || off >= v.size {
		v.u.showError(errors.New(i18n.T(i18n.MemOutOfRange, a)))
		return
	}
	v.cursor, v.lowNibble = off, false
	v.top = off / v.perRow
	v.Refresh()
}

// search はカーソルの次の位置から検索語を探す。
func (v *memoryViewer) search() {
	pat, err := parseSearch(v.searchE.Text, v.textMode.Checked)
	if err != nil {
		v.u.showError(err)
		return
	}
	var data []uint8
	v.u.emu.WithDebugger(func(d *debug.Debugger) {
		if n := d.Machine(); n != nil {
			data = make([]uint8, debug.Size(n, v.space))
			debug.ReadMemory(n, v.space, 0, data)
		}
	})
	at := searchFrom(data, pat, v.cursor+1)
	if at < 0 {
		v.info.SetText(i18n.T(i18n.MemNotFound))
		return
	}
	v.cursor, v.lowNibble = at, false
	v.ensureVisible()
	v.Refresh()
}

// withSymbols はシンボルを変更し、ファイルへ保存する。
func (v *memoryViewer) withSymbols(fn func(s *debug.Symbols)) {
	v.u.emu.WithDebugger(func(d *debug.Debugger) {
		if s := d.Symbols(); s != nil {
			fn(s)
		}
	})
	v.u.emu.SaveSymbols()
	v.Refresh()
}

// addWatch はカーソルのアドレスをウォッチリストへ加える。
func (v *memoryViewer) addWatch() {
	if a, ok := v.cpuAddr(v.cursor); ok {
		v.withSymbols(func(s *debug.Symbols) { s.AddWatch(a) })
	}
}

// removeWatch はカーソルのアドレスをウォッチリストから外す。
func (v *memoryViewer) removeWatch() {
	if a, ok := v.cpuAddr(v.cursor); ok {
		v.withSymbols(func(s *debug.Symbols) { s.RemoveWatch(a) })
	}
}

// setLabel はカーソルのアドレスに名前を付ける。
func (v *memoryViewer) setLabel(name string) {
	a, ok := v.cpuAddr(v.cursor)
	if !ok {
		v.u.showError(errors.New(i18n.T(i18n.MemLabelSpaces)))
		return
	}
	// $8000 以上は現在見えているバンクの位置で持つ（設計書 09 編 §9.9）。
	v.u.emu.WithDebugger(func(d *debug.Debugger) {
		if s, n := d.Symbols(), d.Machine(); s != nil && n != nil {
			s.SetLabel(a, name, n.Cart.PRGOffset)
		}
	})
	v.u.emu.SaveSymbols()
	v.Refresh()
}

// OnClose は変更追跡の利用をやめる。
func (v *memoryViewer) OnClose() {
	if !v.open {
		return
	}
	v.open = false
	v.u.debugUsers.memory--
	v.u.applyDebugFeatures()
}
