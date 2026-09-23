package ui

import (
	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// ウィンドウの名前（設計書 10 編 §10.7）。設定 ui.windows のキーになる。
const (
	windowMain        = "main"
	windowPattern     = "pattern"
	windowNametable   = "nametable"
	windowSprite      = "sprite"
	windowPalette     = "palette"
	windowAPU         = "apu"
	windowCPU         = "cpu"
	windowBreakpoints = "breakpoints"
	windowLog         = "log"
	windowMemory      = "memory"
)

// namedViewer はウィンドウ状態を保存するビューア。
type namedViewer interface {
	Viewer
	// WindowName は設定 ui.windows のキーを返す。
	WindowName() string
}

func (v *patternViewer) WindowName() string    { return windowPattern }
func (v *nametableViewer) WindowName() string  { return windowNametable }
func (v *spriteViewer) WindowName() string     { return windowSprite }
func (v *paletteViewer) WindowName() string    { return windowPalette }
func (v *apuViewer) WindowName() string        { return windowAPU }
func (v *cpuViewer) WindowName() string        { return windowCPU }
func (v *breakpointViewer) WindowName() string { return windowBreakpoints }
func (v *logViewer) WindowName() string        { return windowLog }
func (v *memoryViewer) WindowName() string     { return windowMemory }

// windowSize は保存したサイズを返す。無いとき false。
func (u *UI) windowSize(name string) (fyne.Size, bool) {
	st, ok := u.cfg.UI.Windows[name]
	if !ok || st.Width <= 0 || st.Height <= 0 {
		return fyne.Size{}, false
	}
	return fyne.NewSize(float32(st.Width), float32(st.Height)), true
}

// recordWindow はウィンドウのサイズと表示状態を保存する設定へ入れる。
func (u *UI) recordWindow(name string, size fyne.Size, visible bool) {
	u.update(func(c *config.Config) {
		if c.UI.Windows == nil {
			c.UI.Windows = map[string]config.WindowState{}
		}
		st := c.UI.Windows[name]
		if size.Width > 0 && size.Height > 0 {
			st.Width, st.Height = int(size.Width), int(size.Height)
		}
		st.Visible = visible
		c.UI.Windows[name] = st
	})
}

// saveWindowStates は終了するときに開いているウィンドウを記録して保存する。
func (u *UI) saveWindowStates() {
	if u.win != nil && !u.win.FullScreen() {
		u.recordWindow(windowMain, u.win.Canvas().Size(), true)
	}
	if u.host == nil {
		return
	}
	for _, v := range u.host.Visible() {
		nv, ok := v.(namedViewer)
		if !ok {
			continue
		}
		u.recordWindow(nv.WindowName(), u.host.Size(v), true)
	}
}

// reopenViewers は前回開いていたビューアを開き直す。メモリビューアは除く。
func (u *UI) reopenViewers() {
	for _, v := range []namedViewer{u.patterns(), u.nametables(), u.sprites(), u.palettes(), u.apus(),
		u.cpuDebugger(), u.logs(), u.breakpoints()} {
		if st, ok := u.cfg.UI.Windows[v.WindowName()]; ok && st.Visible {
			u.showViewer(v)
		}
	}
}

// breakpoints はブレークポイント一覧を返す。無ければ作る。
func (u *UI) breakpoints() *breakpointViewer {
	if u.bpViewer == nil {
		u.bpViewer = newBreakpointViewer(u)
	}
	return u.bpViewer
}
