package ui

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
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
		u.helpMenu(),
	)
}

// fileMenu はファイルメニューを作る。
func (u *UI) fileMenu() *fyne.Menu {
	open := fyne.NewMenuItem("ROM を開く…", u.openROMDialog)
	// OS ごとの修飾キー（macOS は Command、他は Control）を使う。
	open.Shortcut = &desktop.CustomShortcut{
		KeyName:  fyne.KeyO,
		Modifier: fyne.KeyModifierShortcutDefault,
	}

	u.recentItem = fyne.NewMenuItem("最近使った ROM", nil)
	u.updateRecentMenu()

	closeROM := fyne.NewMenuItem("ROM を閉じる", func() {
		u.showError(u.emu.Unload())
		u.afterROMChange()
	})
	quit := fyne.NewMenuItem("終了", func() { u.win.Close() })
	quit.IsQuit = true

	return fyne.NewMenu("ファイル", open, u.recentItem, fyne.NewMenuItemSeparator(), closeROM, quit)
}

// runMenu は実行メニューを作る。
func (u *UI) runMenu() *fyne.Menu {
	pause := fyne.NewMenuItem("一時停止", u.togglePause)
	step := fyne.NewMenuItem("コマ送り", u.frameAdvance)
	reset := fyne.NewMenuItem("リセット", func() { u.showError(u.emu.Reset(false)) })
	hardReset := fyne.NewMenuItem("ハードリセット", func() { u.showError(u.emu.Reset(true)) })

	speeds := make([]*fyne.MenuItem, 0, len(speedChoices))
	for _, f := range speedChoices {
		speeds = append(speeds, fyne.NewMenuItem(speedText(f), func() { u.SetSpeed(f) }))
	}
	speed := fyne.NewMenuItem("速度", nil)
	speed.ChildMenu = fyne.NewMenu("", speeds...)

	rewind := fyne.NewMenuItem("巻き戻し", func() { u.showError(u.emu.Rewind(rewindMenuFrames)) })

	return fyne.NewMenu("実行", pause, step, fyne.NewMenuItemSeparator(), reset, hardReset,
		fyne.NewMenuItemSeparator(), speed, rewind)
}

// viewMenu は表示メニューを作る。
func (u *UI) viewMenu() *fyne.Menu {
	scales := make([]*fyne.MenuItem, 0, config.MaxScale)
	for s := config.MinScale; s <= config.MaxScale; s++ {
		scales = append(scales, fyne.NewMenuItem(fmt.Sprintf("%d 倍", s), func() { u.setScale(s) }))
	}
	scale := fyne.NewMenuItem("拡大率", nil)
	scale.ChildMenu = fyne.NewMenu("", scales...)

	full := fyne.NewMenuItem("フルスクリーン", u.toggleFullscreen)

	layout := fyne.NewMenuItem("ビューアの配置", nil)
	layout.ChildMenu = fyne.NewMenu("",
		fyne.NewMenuItem("別ウィンドウ", func() { u.SetViewerLayout(config.LayoutWindows) }),
		fyne.NewMenuItem("タブ", func() { u.SetViewerLayout(config.LayoutDocked) }),
	)

	pattern := fyne.NewMenuItem("パターンテーブル", func() { u.toggleViewer(u.patterns()) })
	nametable := fyne.NewMenuItem("ネームテーブル", func() { u.toggleViewer(u.nametables()) })
	sprite := fyne.NewMenuItem("スプライト", func() { u.toggleViewer(u.sprites()) })
	palette := fyne.NewMenuItem("パレット", func() { u.toggleViewer(u.palettes()) })
	apuView := fyne.NewMenuItem("APU", func() { u.toggleViewer(u.apus()) })
	cpuView := fyne.NewMenuItem("CPU デバッガ", func() { u.showViewer(u.cpuDebugger()) })
	memory := fyne.NewMenuItem("メモリ（新しく開く）", func() { u.showViewer(u.newMemoryViewer()) })
	logs := fyne.NewMenuItem("ログ", func() { u.showViewer(u.logs()) })

	return fyne.NewMenu("表示", scale, full, fyne.NewMenuItemSeparator(),
		pattern, nametable, sprite, palette, apuView, fyne.NewMenuItemSeparator(),
		cpuView, memory, logs, fyne.NewMenuItemSeparator(), layout)
}

// debugMenu はデバッグメニューを作る（設計書 10 編 §10.5）。
func (u *UI) debugMenu() *fyne.Menu {
	bps := fyne.NewMenuItem("ブレークポイント一覧", func() {
		if u.bpViewer == nil {
			u.bpViewer = newBreakpointViewer(u)
		}
		u.showViewer(u.bpViewer)
	})
	u.traceItem = fyne.NewMenuItem("トレースの記録", u.toggleTracing)
	dump := fyne.NewMenuItem("トレースの書き出し", func() {
		path, err := u.emu.DumpTraceDefault()
		if err != nil {
			u.showError(err)
			return
		}
		u.status.notify("トレースを " + path + " へ書き出しました")
	})
	u.traceFileItem = fyne.NewMenuItem("トレースの常時出力", u.toggleTraceFile)
	logs := fyne.NewMenuItem("ログカテゴリ…", func() { u.showViewer(u.logs()) })
	u.overlayItem = fyne.NewMenuItem("オーバーレイを有効にする", u.toggleOverlay)
	u.overlayItem.Checked = true
	clearOverlay := fyne.NewMenuItem("オーバーレイを消去", func() {
		if err := u.emu.ClearOverlay(); err != nil {
			u.showError(err)
			return
		}
		u.status.notify("オーバーレイを消去しました")
	})
	return fyne.NewMenu("デバッグ", bps, fyne.NewMenuItemSeparator(),
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
	u.status.notify("トレースを " + path + " へ出力しています")
	u.refreshMainMenu()
}

// refreshMainMenu はメニューのチェックの印を描き直す。
func (u *UI) refreshMainMenu() {
	if u.win != nil && u.win.MainMenu() != nil {
		u.win.MainMenu().Refresh()
	}
}

// helpMenu はヘルプメニューを作る。
func (u *UI) helpMenu() *fyne.Menu {
	about := fyne.NewMenuItem("バージョン情報", func() {
		dialog.ShowCustom("バージョン情報", "閉じる",
			widget.NewLabel(fmt.Sprintf("%s\n%s", appTitle, u.version)), u.win)
	})
	return fyne.NewMenu("ヘルプ", about)
}

// setScale は拡大率を変え、ウィンドウの大きさを合わせる。
func (u *UI) setScale(scale int) {
	u.cfg.Video.Scale = scale
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
		zenity.Title("ROM を開く"),
		zenity.Filename(dir),
		zenity.FileFilters{{
			Name:     "NES の ROM (*.nes)",
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
func (u *UI) OpenROM(path string) {
	if err := u.emu.LoadROM(path); err != nil {
		u.showError(err)
		return
	}
	u.addRecent(path)
	u.paused = false
	u.afterROMChange()
}

// NotifyROMLoaded は画面を開く前に読み込んだ ROM を一覧へ加える。
//
// 起動時の引数で読み込んだ場合に使う。Run を呼ぶ前に呼ぶ。
func (u *UI) NotifyROMLoaded(path string) {
	u.addRecent(path)
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

// addRecent は最近使った ROM の一覧へ加える。
//
// 同じものは先頭へ移す。この段階では記憶するのはメモリ上のみで、
// 終了すると失われる。設定ファイルへの保存はフェーズ 12 で行う。
func (u *UI) addRecent(path string) {
	for i, p := range u.recent {
		if p == path {
			u.recent = append(u.recent[:i], u.recent[i+1:]...)
			break
		}
	}
	u.recent = append([]string{path}, u.recent...)
	if len(u.recent) > maxRecentROMs {
		u.recent = u.recent[:maxRecentROMs]
	}
	u.updateRecentMenu()
}

// updateRecentMenu は最近使った ROM の項目を作り直す。
func (u *UI) updateRecentMenu() {
	if u.recentItem == nil {
		return
	}
	if len(u.recent) == 0 {
		empty := fyne.NewMenuItem("（なし）", nil)
		empty.Disabled = true
		u.recentItem.ChildMenu = fyne.NewMenu("", empty)
		return
	}
	items := make([]*fyne.MenuItem, 0, len(u.recent))
	for _, p := range u.recent {
		items = append(items, fyne.NewMenuItem(filepath.Base(p), func() { u.OpenROM(p) }))
	}
	u.recentItem.ChildMenu = fyne.NewMenu("", items...)
	// メニューバーを組み立てている途中はまだ設定されていない。
	if u.win != nil && u.win.MainMenu() != nil {
		u.win.MainMenu().Refresh()
	}
}
