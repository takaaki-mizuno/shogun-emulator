package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// onKeyDown はキーを押したときに呼ばれる。
func (u *UI) onKeyDown(ev *fyne.KeyEvent) {
	code, ok := keyCode(ev.Name)
	if !ok {
		return
	}
	// キーリピートを無視する。押し続けで何度も処理すると、一時停止の
	// ような切り替えの操作が連打されたことになる。
	if u.pressed[code] {
		return
	}
	u.pressed[code] = true
	u.dispatch(code, true)
}

// onKeyUp はキーを離したときに呼ばれる。
func (u *UI) onKeyUp(ev *fyne.KeyEvent) {
	code, ok := keyCode(ev.Name)
	if !ok {
		return
	}
	if !u.pressed[code] {
		return
	}
	delete(u.pressed, code)
	u.dispatch(code, false)
}

// dispatch は 1 つの物理キーに割り当てられたアクションをすべて実行する。
func (u *UI) dispatch(code string, pressed bool) {
	for _, a := range u.bindings[code] {
		u.doAction(a, pressed)
	}
}

// doAction はアクションを実行する。
//
// プレイヤー入力は押下状態のビットマスクを更新する。ホットキーは
// エミュレータへコマンドを送る。
func (u *UI) doAction(a config.Action, pressed bool) {
	if u.emu.Input.SetAction(a, pressed) {
		return
	}

	switch a {
	case config.ActionFastForward:
		u.fastForward = pressed
		u.applySpeed()
	case config.ActionSlowMotion:
		u.slowMotion = pressed
		u.applySpeed()
	case config.ActionRewind:
		// 押している間だけ遡る。
		u.emu.SetRewinding(pressed)
	}

	// 以降は押した瞬間だけ実行する。
	if !pressed {
		return
	}
	switch a {
	case config.ActionPause:
		u.togglePause()
	case config.ActionFrameAdvance:
		u.frameAdvance()
	case config.ActionReset:
		u.showError(u.emu.Reset(false))
	case config.ActionHardReset:
		u.showError(u.emu.Reset(true))
	case config.ActionToggleFullscreen:
		u.toggleFullscreen()
	case config.ActionScreenshot:
		u.showError(u.saveScreenshot())
	case config.ActionMute:
		u.toggleMute()
	case config.ActionSaveState:
		u.quickSave()
	case config.ActionLoadState:
		u.quickLoad()
	case config.ActionNextSlot:
		u.status.notify(fmt.Sprintf("スロット %d を選びました", u.emu.NextSlot()))
	case config.ActionPrevSlot:
		u.status.notify(fmt.Sprintf("スロット %d を選びました", u.emu.PrevSlot()))
	}
}

// applySpeed は早送りとスローの状態から速度倍率を決めて送る。
//
// 両方を押しているときは早送りを優先する。押している間の操作であり、
// どちらが後から押されたかを覚えると離したときの扱いが複雑になる。
func (u *UI) applySpeed() {
	speed := u.baseSpeed
	switch {
	case u.fastForward:
		speed = fastForwardSpeed
	case u.slowMotion:
		speed = slowMotionSpeed
	}
	u.emu.SetSpeed(speed)
}

// SetSpeed は基準の速度倍率を変える。
func (u *UI) SetSpeed(factor float64) {
	u.baseSpeed = factor
	u.applySpeed()
}

// togglePause は一時停止と再開を切り替える。
func (u *UI) togglePause() {
	u.paused = !u.paused
	u.emu.SetPaused(u.paused)
}

// frameAdvance は一時停止したまま 1 フレーム進める。
func (u *UI) frameAdvance() {
	u.paused = true
	u.emu.FrameAdvance()
}

// toggleMute は消音を切り替える。
func (u *UI) toggleMute() {
	u.muted = !u.muted
	u.emu.SetMuted(u.muted)
}

// toggleFullscreen はフルスクリーンを切り替える。
func (u *UI) toggleFullscreen() {
	u.cfg.Video.Fullscreen = !u.win.FullScreen()
	u.win.SetFullScreen(u.cfg.Video.Fullscreen)
}

// saveScreenshot は表示中のフレームを PNG として保存する。
//
// 画面をキャプチャせずフレームバッファから作る。表示の更新は非同期で
// あり、画面に出ている内容とフレームの内容が一致するとは限らない。
func (u *UI) saveScreenshot() error {
	s := u.emu.Status()
	if !s.Loaded {
		return nil
	}
	dir, err := config.ScreenshotDir(u.cfg.Paths.ScreenshotDir)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.png", s.ROMName, time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := video.SavePNG(u.screen.Frame(), u.pal, u.screen.Overscan(),
		u.screen.PictureHeight(), path); err != nil {
		return err
	}
	u.status.notify(fmt.Sprintf("%s を保存しました", path))
	return nil
}

// releaseAllKeys はすべてのキーを離した扱いにする。
//
// ウィンドウがフォーカスを失うと、離したことが通知されない。押しっぱなしの
// まま戻ると、ボタンが押されたままになる。
func (u *UI) releaseAllKeys() {
	for code := range u.pressed {
		u.dispatch(code, false)
	}
	clear(u.pressed)
	u.emu.Input.Clear()
}
