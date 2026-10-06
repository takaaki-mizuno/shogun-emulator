package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// writeLoopROM は LDA #$42 / STA $0300 / JMP $8000 を繰り返す NROM を書く。
func writeLoopROM(t testing.TB) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	copy(prg, []uint8{
		0xA9, 0x42, // LDA #$42
		0x8D, 0x00, 0x03, // STA $0300
		0x4C, 0x00, 0x80, // JMP $8000
	})
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	path := filepath.Join(t.TempDir(), "loop.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// newDebugTestUI は ROM を読み込んで一時停止した UI と別ウィンドウの置き場所を作る。
func newDebugTestUI(t testing.TB) *UI {
	t.Helper()
	a := test.NewApp()
	dir := t.TempDir()
	e := emu.New(emu.Config{
		Emulation:  config.EmulationConfig{Region: config.RegionNTSC, RAMInitPattern: "zero"},
		NewPacer:   func(*region.Region) emu.Pacer { return emu.NewNoPacer() },
		SymbolsDir: filepath.Join(dir, "symbols"),
		TraceDir:   filepath.Join(dir, "traces"),
		PatchesDir: filepath.Join(dir, "patches"),
	})
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(writeLoopROM(t)); err != nil {
		t.Fatal(err)
	}
	e.Pause()
	waitFor(t, func() bool { return e.Status().Paused })

	cfg := config.Default()
	u := &UI{
		app:     a,
		emu:     e,
		cfg:     cfg,
		pressed: map[string]bool{},
		status:  newStatusBar(e.Frames),
		host:    newWindowHost(a),
		pal:     video.DefaultPalette(),
	}
	u.win = a.NewWindow("test")
	return u
}

// waitFor は条件が成り立つまで待つ。
func waitFor(t testing.TB, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("条件が成り立たないまま時間切れになった")
		}
		time.Sleep(time.Millisecond)
	}
}

// hooksOf は本体に設定されているフックを返す。
func hooksOf(t *testing.T, u *UI) nes.Hooks {
	t.Helper()
	var h nes.Hooks
	u.emu.WithMachine(func(n *nes.NES) { h = n.Hooks() })
	return h
}

// anyHook はいずれかのフックが設定されているかを返す。
func anyHook(h nes.Hooks) bool {
	return h.OnCPURead != nil || h.OnCPUWrite != nil || h.OnInstructionStart != nil ||
		h.OnBeforeExec != nil || h.OnCycle != nil || h.OnInterrupt != nil ||
		h.OnSprite0Hit != nil || h.OnFrameComplete != nil
}

// TestDebugViewersSetAndClearHooks はビューアを開くと必要なフックが付き、
// すべて閉じると nil に戻ることを確かめる（設計書 09 編 §9.6）。
func TestDebugViewersSetAndClearHooks(t *testing.T) {
	u := newDebugTestUI(t)
	if anyHook(hooksOf(t, u)) {
		t.Fatal("ビューアを開く前からフックが付いている")
	}

	cpuV := u.cpuDebugger()
	mem1 := u.newMemoryViewer()
	mem2 := u.newMemoryViewer()
	u.host.Show(cpuV)
	u.host.Show(mem1)
	u.host.Show(mem2)
	u.host.Show(u.logs())

	h := hooksOf(t, u)
	if h.OnBeforeExec == nil {
		t.Error("CPU デバッガを開いても OnBeforeExec が無い")
	}
	if h.OnFrameComplete == nil {
		t.Error("メモリビューアを開いても OnFrameComplete が無い")
	}
	if mem1.Title() == mem2.Title() {
		t.Errorf("2 つのメモリビューアの題名が同じ: %q", mem1.Title())
	}

	u.host.Hide(mem1)
	if hooksOf(t, u).OnFrameComplete == nil {
		t.Error("メモリビューアが 1 つ残っているのに OnFrameComplete が外れた")
	}
	u.host.Hide(mem2)
	u.host.Hide(cpuV)
	u.host.Hide(u.logs())
	if h := hooksOf(t, u); anyHook(h) {
		t.Errorf("すべて閉じた後もフックが残っている: %+v", h)
	}
}

// TestTracingMenuTogglesHook はトレースの記録の切り替えでフックが付け外し
// されることを確かめる。
func TestTracingMenuTogglesHook(t *testing.T) {
	u := newDebugTestUI(t)
	u.win.SetMainMenu(u.buildMainMenu())

	u.toggleTracing()
	if hooksOf(t, u).OnBeforeExec == nil || !u.traceItem.Checked {
		t.Error("トレースを始めても OnBeforeExec が無い")
	}
	u.toggleTracing()
	if anyHook(hooksOf(t, u)) || u.traceItem.Checked {
		t.Error("トレースを止めてもフックが残っている")
	}
}

// TestCPUViewerMarksCurrentLine は逆アセンブルの現在の行に > が付くことを確かめる。
func TestCPUViewerMarksCurrentLine(t *testing.T) {
	u := newDebugTestUI(t)
	v := u.cpuDebugger()
	u.host.Show(v)
	v.Refresh()

	cur := v.view.CurrentLine
	if cur < 0 {
		t.Fatal("現在の行が見つからない")
	}
	row := v.grid.grid.RowText(cur)
	if !strings.HasPrefix(row, ">") {
		t.Errorf("現在の行 = %q, > で始まらない", row)
	}
	if !strings.Contains(row, "LDA") && !strings.Contains(row, "STA") && !strings.Contains(row, "JMP") {
		t.Errorf("現在の行 = %q, 期待する命令が無い", row)
	}

	// カーソルの行にブレークポイントを置くと * が付く。
	v.selectRow(cur, 0)
	v.toggleBreakAtCursor()
	if row := v.grid.grid.RowText(v.view.CurrentLine); !strings.Contains(row[:4], "*") {
		t.Errorf("ブレークポイントの印が無い: %q", row)
	}
	if n := len(u.emu.Debugger().Breakpoints()); n != 1 {
		t.Errorf("ブレークポイントの数 = %d, 期待 1", n)
	}
}

// TestMemoryViewerEditsByTyping は 16 進の桁をタイプすると値が書き換わる
// ことを確かめる。
func TestMemoryViewerEditsByTyping(t *testing.T) {
	u := newDebugTestUI(t)
	v := u.newMemoryViewer()
	u.host.Show(v)
	v.grid.Resize(fyne.NewSize(600, 400))

	v.gotoAddr("0400")
	v.typeRune('A')
	v.typeRune('5')
	var got uint8
	u.emu.WithMachine(func(n *nes.NES) { got = n.Bus.Peek(0x0400) })
	if got != 0xA5 {
		t.Errorf("$0400 = $%02X, 期待 $A5", got)
	}
	if v.cursor != 0x0401 {
		t.Errorf("書き換えた後のカーソル = $%04X, 期待 $0401", v.cursor)
	}

	// 検索で書いた値を見つける。
	v.searchE.SetText("A5")
	v.cursor = 0
	v.search()
	if v.cursor != 0x0400 {
		t.Errorf("検索結果 = $%04X, 期待 $0400", v.cursor)
	}

	// 名前とウォッチはシンボルへ残る。
	v.setLabel("score")
	v.addWatch()
	sym := u.emu.Debugger().Symbols()
	if sym.LabelAt(0x0400, nil) != "score" {
		t.Errorf("名前 = %q, 期待 score", sym.LabelAt(0x0400, nil))
	}
	if w := sym.Watch(); len(w) != 1 || w[0] != 0x0400 {
		t.Errorf("ウォッチ = %v", w)
	}
}

// TestLogViewerFiltersByCategory はカテゴリで絞り込めることを確かめる。
func TestLogViewerFiltersByCategory(t *testing.T) {
	u := newDebugTestUI(t)
	l := u.emu.Debugger().Logger()
	l.Warnf("互換性の警告")
	l.Errorf("内部エラー")

	v := u.logs()
	u.host.Show(v)
	if text := v.grid.Text(); !strings.Contains(text, "互換性の警告") || !strings.Contains(text, "内部エラー") {
		t.Errorf("すべての行が表示されていない: %q", text)
	}
	v.filter.SetSelected("error")
	if text := v.grid.Text(); strings.Contains(text, "互換性の警告") || !strings.Contains(text, "内部エラー") {
		t.Errorf("error で絞り込めていない: %q", text)
	}
	v.filter.SetSelected(allCategoriesLabel)
	v.search.SetText("互換")
	if text := v.grid.Text(); !strings.Contains(text, "互換性の警告") || strings.Contains(text, "内部エラー") {
		t.Errorf("検索で絞り込めていない: %q", text)
	}
}

// TestBreakpointPanelParsesInputs は入力欄の読み取りを確かめる。
func TestBreakpointPanelParsesInputs(t *testing.T) {
	s, e, err := parseAddrRange("$0300-$03FF")
	if err != nil || s != 0x0300 || e != 0x03FF {
		t.Errorf("範囲 = %04X-%04X, %v", s, e, err)
	}
	if s, e, err = parseAddrRange("C000"); err != nil || s != 0xC000 || e != 0xC000 {
		t.Errorf("単独 = %04X-%04X, %v", s, e, err)
	}
	line, dot, err := parsePPUPosition("241, 1")
	if err != nil || line != 241 || dot != 1 {
		t.Errorf("PPU 位置 = %d,%d, %v", line, dot, err)
	}
	if _, _, err := parsePPUPosition("400,1"); err == nil {
		t.Error("範囲外の行を受け付けた")
	}
}

// TestStatusShowsBreakReason はステータスバーに止まった理由が出ることを確かめる。
func TestStatusShowsBreakReason(t *testing.T) {
	s := emu.Status{Paused: true, Break: "実行 $8000", MidInstruction: true}
	if got := runStateText(s); got != "停止: 実行 $8000（命令の途中）" {
		t.Errorf("表示 = %q", got)
	}
}

// TestBreakShowsCPUDebugger はブレークポイントで止まると CPU デバッガが
// 前面に出ることを確かめる。
func TestBreakShowsCPUDebugger(t *testing.T) {
	u := newDebugTestUI(t)
	u.emu.SetOnBreak(func(debug.BreakInfo) { u.breakPending.Store(true) })
	u.emu.WithDebugger(func(d *debug.Debugger) {
		d.AddBreakpoint(debug.Breakpoint{Kind: debug.BreakWrite, AddrStart: 0x0300, AddrEnd: 0x0300, Enabled: true})
	})
	u.emu.Resume()
	waitFor(t, func() bool { return u.breakPending.Load() })
	u.refreshViewers()
	if !u.host.IsVisible(u.cpuDebugger()) {
		t.Error("止まっても CPU デバッガが表示されていない")
	}
	if !strings.Contains(u.emu.Status().Break, "$0300") {
		t.Errorf("止まった理由 = %q", u.emu.Status().Break)
	}
}

// writeFile はテスト用のファイルを書く。
func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
