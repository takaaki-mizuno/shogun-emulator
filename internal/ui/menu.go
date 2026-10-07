package ui

import (
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// rewindMenuFrames はメニューの「巻き戻し」で遡るフレーム数。
//
// ホットキーは押している間だけ遡る。メニューからは 1 回の操作で
// 遡るため、1 秒に相当する量を選ぶ。
const rewindMenuFrames = 60

// speedChoices はメニューに並べる速度倍率。
//
// 最後の 1 つは待ちを行わない境界であり、表示は「最速」になる。
var speedChoices = []float64{0.25, 0.5, 1.0, 2.0, emu.UncappedSpeed}

// buildMainMenu はメニューバーを組み立てる。
func (u *UI) buildMainMenu() *fyne.MainMenu {
	return fyne.NewMainMenu(
		u.fileMenu(),
		u.runMenu(),
		u.stateMenu(),
		u.movieMenu(),
		u.viewMenu(),
		u.debugMenu(),
		u.aiMenu(),
		u.settingsMenu(),
		u.helpMenu(),
	)
}

// fileMenu はファイルメニューを作る。
func (u *UI) fileMenu() *fyne.Menu {
	open := fyne.NewMenuItem(i18n.T(i18n.MenuOpenROM), u.openROMDialog)
	// OS ごとの修飾キー（macOS は Command、他は Control）を使う。
	open.Shortcut = &desktop.CustomShortcut{
		KeyName:  fyne.KeyO,
		Modifier: fyne.KeyModifierShortcutDefault,
	}

	u.recentItem = fyne.NewMenuItem(i18n.T(i18n.MenuRecentROMs), nil)
	u.updateRecentMenu()

	closeROM := fyne.NewMenuItem(i18n.T(i18n.MenuCloseROM), func() {
		u.showError(u.emu.Unload())
		u.afterROMChange()
	})
	quit := fyne.NewMenuItem(i18n.T(i18n.MenuQuit), func() { u.win.Close() })
	quit.IsQuit = true

	return fyne.NewMenu(i18n.T(i18n.MenuFile), open, u.recentItem, fyne.NewMenuItemSeparator(), closeROM, quit)
}

// runMenu は実行メニューを作る。
func (u *UI) runMenu() *fyne.Menu {
	pause := fyne.NewMenuItem(i18n.T(i18n.MenuPause), u.togglePause)
	step := fyne.NewMenuItem(i18n.T(i18n.MenuFrameAdvance), u.frameAdvance)
	reset := fyne.NewMenuItem(i18n.T(i18n.MenuReset), func() { u.showError(u.emu.Reset(false)) })
	hardReset := fyne.NewMenuItem(i18n.T(i18n.MenuHardReset), func() { u.showError(u.emu.Reset(true)) })

	speeds := make([]*fyne.MenuItem, 0, len(speedChoices))
	for _, f := range speedChoices {
		speeds = append(speeds, fyne.NewMenuItem(speedText(f), func() { u.SetSpeed(f) }))
	}
	speed := fyne.NewMenuItem(i18n.T(i18n.MenuSpeed), nil)
	speed.ChildMenu = fyne.NewMenu("", speeds...)

	rewind := fyne.NewMenuItem(i18n.T(i18n.MenuRewind), func() { u.showError(u.emu.Rewind(rewindMenuFrames)) })

	return fyne.NewMenu(i18n.T(i18n.MenuRun), pause, step, fyne.NewMenuItemSeparator(), reset, hardReset,
		fyne.NewMenuItemSeparator(), speed, rewind)
}

// viewMenu は表示メニューを作る。
func (u *UI) viewMenu() *fyne.Menu {
	scales := make([]*fyne.MenuItem, 0, config.MaxScale)
	for s := config.MinScale; s <= config.MaxScale; s++ {
		scales = append(scales, fyne.NewMenuItem(i18n.T(i18n.MenuScaleN, s), func() { u.setScale(s) }))
	}
	scale := fyne.NewMenuItem(i18n.T(i18n.MenuScale), nil)
	scale.ChildMenu = fyne.NewMenu("", scales...)

	full := fyne.NewMenuItem(i18n.T(i18n.MenuFullscreen), u.toggleFullscreen)

	layout := fyne.NewMenuItem(i18n.T(i18n.MenuViewerLayout), nil)
	layout.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem(i18n.T(i18n.MenuLayoutWindows), func() { u.SetViewerLayout(config.LayoutWindows) }),
		fyne.NewMenuItem(i18n.T(i18n.MenuLayoutTabs), func() { u.SetViewerLayout(config.LayoutDocked) }),
	)

	pattern := fyne.NewMenuItem(i18n.T(i18n.ViewerPattern), func() { u.toggleViewer(u.patterns()) })
	nametable := fyne.NewMenuItem(i18n.T(i18n.ViewerNametable), func() { u.toggleViewer(u.nametables()) })
	sprite := fyne.NewMenuItem(i18n.T(i18n.ViewerSprite), func() { u.toggleViewer(u.sprites()) })
	palette := fyne.NewMenuItem(i18n.T(i18n.ViewerPalette), func() { u.toggleViewer(u.palettes()) })
	apuView := fyne.NewMenuItem("APU", func() { u.toggleViewer(u.apus()) })
	cpuView := fyne.NewMenuItem(i18n.T(i18n.ViewerCPU), func() { u.showViewer(u.cpuDebugger()) })
	memory := fyne.NewMenuItem(i18n.T(i18n.MenuMemoryNew), func() { u.showViewer(u.newMemoryViewer()) })
	logs := fyne.NewMenuItem(i18n.T(i18n.ViewerLog), func() { u.showViewer(u.logs()) })

	return fyne.NewMenu(i18n.T(i18n.MenuView), scale, full, fyne.NewMenuItemSeparator(),
		pattern, nametable, sprite, palette, apuView, fyne.NewMenuItemSeparator(),
		cpuView, memory, logs, fyne.NewMenuItemSeparator(), layout)
}

// debugMenu はデバッグメニューを作る（設計書 10 編 §10.5）。
func (u *UI) debugMenu() *fyne.Menu {
	bps := fyne.NewMenuItem(i18n.T(i18n.MenuBreakpoints), func() {
		if u.bpViewer == nil {
			u.bpViewer = newBreakpointViewer(u)
		}
		u.showViewer(u.bpViewer)
	})
	u.traceItem = fyne.NewMenuItem(i18n.T(i18n.MenuTraceRecord), u.toggleTracing)
	dump := fyne.NewMenuItem(i18n.T(i18n.MenuTraceDump), func() {
		path, err := u.emu.DumpTraceDefault()
		if err != nil {
			u.showError(err)
			return
		}
		u.status.notify(i18n.T(i18n.StatusTraceDumped, path))
	})
	u.traceFileItem = fyne.NewMenuItem(i18n.T(i18n.MenuTraceLive), u.toggleTraceFile)
	logs := fyne.NewMenuItem(i18n.T(i18n.MenuLogCategories), func() { u.showViewer(u.logs()) })
	u.overlayItem = fyne.NewMenuItem(i18n.T(i18n.MenuOverlayEnabled), u.toggleOverlay)
	u.overlayItem.Checked = true
	clearOverlay := fyne.NewMenuItem(i18n.T(i18n.MenuOverlayClear), func() {
		if err := u.emu.ClearOverlay(); err != nil {
			u.showError(err)
			return
		}
		u.status.notify(i18n.T(i18n.StatusOverlayCleared))
	})
	return fyne.NewMenu(i18n.T(i18n.MenuDebug), bps, fyne.NewMenuItemSeparator(),
		u.traceItem, dump, u.traceFileItem, fyne.NewMenuItemSeparator(), logs,
		fyne.NewMenuItemSeparator(), u.overlayItem, clearOverlay)
}

// toggleViewer はビューアを開いていれば閉じ、閉じていれば開く。
func (u *UI) toggleViewer(v Viewer) {
	if u.host == nil {
		return
	}
	if u.host.IsVisible(v) {
		u.host.Hide(v)
		return
	}
	u.host.Show(v)
}

// toggleOverlay はオーバーレイの有効・無効を切り替える（設計書 09 編 §9.4.8）。
//
// 無効にすると ROM の元の内容に戻る。編集の前後で挙動を比べるために使う。
func (u *UI) toggleOverlay() {
	on := !u.emu.OverlayStatus().Enabled
	if err := u.emu.SetOverlayEnabled(on); err != nil {
		u.showError(err)
		return
	}
	u.syncOverlayItem()
}

// syncOverlayItem はメニューのチェックをオーバーレイの状態に合わせる。
func (u *UI) syncOverlayItem() {
	if u.overlayItem == nil {
		return
	}
	u.overlayItem.Checked = u.emu.OverlayStatus().Enabled
	u.refreshMainMenu()
}

// showViewer はビューアを表示する。
func (u *UI) showViewer(v Viewer) {
	if u.host != nil {
		u.host.Show(v)
	}
}

// toggleTracing はトレースの記録を切り替える。
//
// 記録していないときは命令ごとのフックを外す（設計書 09 編 §9.6）。
func (u *UI) toggleTracing() {
	u.debugUsers.tracing = !u.debugUsers.tracing
	if !u.debugUsers.tracing && u.traceFileItem != nil && u.traceFileItem.Checked {
		u.toggleTraceFile()
	}
	u.applyDebugFeatures()
	u.traceItem.Checked = u.debugUsers.tracing
	u.refreshMainMenu()
}

// toggleTraceFile はトレースの常時出力を切り替える。常時出力には記録が要るため、
// 始めるときに記録も有効にする。
func (u *UI) toggleTraceFile() {
	if u.traceFileItem.Checked {
		u.showError(u.emu.StopTraceFile())
		u.traceFileItem.Checked = false
		u.refreshMainMenu()
		return
	}
	path, err := u.emu.StartTraceFile()
	if err != nil {
		u.showError(err)
		return
	}
	u.traceFileItem.Checked = true
	if !u.debugUsers.tracing {
		u.debugUsers.tracing = true
		u.traceItem.Checked = true
		u.applyDebugFeatures()
	}
	u.status.notify(i18n.T(i18n.StatusTraceLive, path))
	u.refreshMainMenu()
}

// refreshMainMenu はメニューのチェックの印を描き直す。
func (u *UI) refreshMainMenu() {
	if u.win != nil && u.win.MainMenu() != nil {
		u.win.MainMenu().Refresh()
	}
}

// settingsMenu は設定メニューを作る（設計書 10 編 §10.5）。
func (u *UI) settingsMenu() *fyne.Menu {
	open := fyne.NewMenuItem(i18n.T(i18n.MenuOpenSettings), func() { u.openSettings(settingsTabEmulation) })
	open.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: fyne.KeyModifierShortcutDefault}
	keys := fyne.NewMenuItem(i18n.T(i18n.MenuOpenKeybindings), func() { u.openSettings(settingsTabInput) })
	return fyne.NewMenu(i18n.T(i18n.MenuSettings), open, keys)
}

// helpMenu はヘルプメニューを作る。
func (u *UI) helpMenu() *fyne.Menu {
	about := fyne.NewMenuItem(i18n.T(i18n.MenuAbout), u.showAbout)
	return fyne.NewMenu(i18n.T(i18n.MenuHelp), about)
}

// aboutLogoSize は「バージョン情報」のロゴの大きさ。
const aboutLogoSize = 128

// showAbout はロゴと版を並べた「バージョン情報」を出す（設計書 10 編 §10.5）。
func (u *UI) showAbout() {
	logo := canvas.NewImageFromResource(logoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.ScaleMode = canvas.ImageScaleSmooth
	logo.SetMinSize(fyne.NewSize(aboutLogoSize, aboutLogoSize))
	title := widget.NewLabelWithStyle(appTitle, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	details := widget.NewLabelWithStyle(i18n.T(i18n.AboutDetails, u.version, u.commit, u.buildDate),
		fyne.TextAlignCenter, fyne.TextStyle{})
	dialog.ShowCustom(i18n.T(i18n.MenuAbout), i18n.T(i18n.CommonClose),
		container.NewVBox(container.NewCenter(logo), title, details), u.win)
}

// setScale は拡大率を変え、ウィンドウの大きさを合わせる。
func (u *UI) setScale(scale int) {
	u.update(func(c *config.Config) { c.Video.Scale = scale })
	u.screen.SetScale(scale)
	if !u.win.FullScreen() {
		u.win.Resize(u.preferredSize())
	}
}

// openROMDialog は ROM を選ぶダイアログを出す。
//
// ダイアログの表示中はエミュレーションが進み続ける。ダイアログを
// 閉じるまで画面が止まると、開くのをやめたときに不自然になる。
func (u *UI) openROMDialog() {
	dir, _ := filepath.Abs(u.cfg.Paths.ROMDir)
	path, err := zenity.SelectFile(
		zenity.Title(i18n.T(i18n.DialogOpenROM)),
		zenity.Filename(dir),
		zenity.FileFilters{{
			Name:     i18n.T(i18n.FilterNESROM),
			Patterns: []string{"*.nes"},
			CaseFold: true,
		}},
	)
	if isCanceled(err) {
		return
	}
	if err != nil {
		u.showError(err)
		return
	}
	u.OpenROM(path)
}

// OpenROM は ROM を読み込む。
//
// 最近使った ROM の一覧から選んだファイルが無いときは、エラーを出して
// 一覧から除く。
func (u *UI) OpenROM(path string) {
	if _, err := os.Stat(path); err != nil {
		u.removeRecent(path)
		u.showError(err)
		return
	}
	if err := u.emu.LoadROM(path); err != nil {
		u.showError(err)
		return
	}
	u.addRecent(path)
	u.agentROMLoaded(path)
	u.paused = false
	u.afterROMChange()
}

// NotifyROMLoaded は画面を開く前に読み込んだ ROM を一覧へ加える。
//
// 起動時の引数で読み込んだ場合に使う。Run を呼ぶ前に呼ぶ。
func (u *UI) NotifyROMLoaded(path string) {
	u.addRecent(path)
	u.agentUI.romPath = path
}

// afterROMChange は ROM の読み込みと取り外しの後に表示を合わせる。
func (u *UI) afterROMChange() {
	s := u.emu.Status()
	u.win.SetTitle(u.windowTitle())
	if !s.Loaded {
		u.screen.Clear()
	}
	u.screen.SetPictureHeight(s.PictureHeight)
	u.syncOverlayItem()
	if !u.win.FullScreen() {
		u.win.Resize(u.preferredSize())
	}
	u.releaseAllKeys()
}

// addRecent は最近使った ROM の一覧の先頭へ加え、設定に保存する。
// 同じものは先頭へ移す。
func (u *UI) addRecent(path string) {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	u.update(func(c *config.Config) {
		list := []config.RecentROM{{Path: path, Name: filepath.Base(path)}}
		for _, r := range c.UI.RecentROMs {
			if r.Path != path {
				list = append(list, r)
			}
		}
		if len(list) > config.MaxRecentROMs {
			list = list[:config.MaxRecentROMs]
		}
		c.UI.RecentROMs = list
	})
	u.updateRecentMenu()
}

// removeRecent は一覧から除いて設定に保存する。
func (u *UI) removeRecent(path string) {
	u.update(func(c *config.Config) {
		list := c.UI.RecentROMs[:0:0]
		for _, r := range c.UI.RecentROMs {
			if r.Path != path {
				list = append(list, r)
			}
		}
		c.UI.RecentROMs = list
	})
	u.updateRecentMenu()
}

// updateRecentMenu は最近使った ROM の項目を作り直す。
func (u *UI) updateRecentMenu() {
	if u.recentItem == nil {
		return
	}
	if len(u.cfg.UI.RecentROMs) == 0 {
		empty := fyne.NewMenuItem(i18n.T(i18n.CommonNone), nil)
		empty.Disabled = true
		u.recentItem.ChildMenu = fyne.NewMenu("", empty)
		return
	}
	items := make([]*fyne.MenuItem, 0, len(u.cfg.UI.RecentROMs))
	for _, r := range u.cfg.UI.RecentROMs {
		items = append(items, fyne.NewMenuItem(r.Name, func() { u.OpenROM(r.Path) }))
	}
	u.recentItem.ChildMenu = fyne.NewMenu("", items...)
	// メニューバーを組み立てている途中はまだ設定されていない。
	if u.win != nil && u.win.MainMenu() != nil {
		u.win.MainMenu().Refresh()
	}
}
