package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// newTestUI は画面を開かずにキー処理だけを試せる UI を作る。
func newTestUI(t *testing.T) *UI {
	t.Helper()
	e := emu.New(emu.Config{NewPacer: func(*region.Region) emu.Pacer { return emu.NewNoPacer() }})
	e.Start()
	t.Cleanup(e.Stop)
	return &UI{
		emu:       e,
		cfg:       config.Default(),
		bindings:  config.DefaultKeybindings().Resolve(),
		pressed:   map[string]bool{},
		baseSpeed: 1.0,
		status:    newStatusBar(e.Frames),
	}
}

// TestKeyDownSetsButton は既定の割り当てでボタンが押されることを確かめる。
func TestKeyDownSetsButton(t *testing.T) {
	u := newTestUI(t)

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyX}) // 1P の A
	if got := u.emu.Input.Buttons(0); got != input.ButtonA {
		t.Errorf("押下状態 = %#08b, 期待 %#08b", got, input.ButtonA)
	}

	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyX})
	if got := u.emu.Input.Buttons(0); got != 0 {
		t.Errorf("離した後 = %#08b, 期待 0", got)
	}
}

// TestKeyDownIgnoresRepeat はキーリピートを無視することを確かめる。
//
// 押し続けの通知で切り替えの操作が連打されないようにする。
func TestKeyDownIgnoresRepeat(t *testing.T) {
	u := newTestUI(t)

	for range 5 {
		u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeySpace}) // 一時停止
	}
	if !u.paused {
		t.Error("一時停止していない")
	}

	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeySpace})
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeySpace})
	if u.paused {
		t.Error("押し直しで再開しなかった")
	}
}

// TestKeyUpWithoutKeyDownIsIgnored は押していないキーの離上を無視する
// ことを確かめる。ウィンドウに戻ったときに届く通知で状態を壊さない。
func TestKeyUpWithoutKeyDownIsIgnored(t *testing.T) {
	u := newTestUI(t)
	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeySpace})
	if u.paused {
		t.Error("離上だけで一時停止が切り替わった")
	}
}

// TestUnknownKeyIsIgnored は変換表に無いキーを無視することを確かめる。
func TestUnknownKeyIsIgnored(t *testing.T) {
	u := newTestUI(t)
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyName("存在しないキー")})
	if len(u.pressed) != 0 {
		t.Errorf("押下を記録してしまった: %v", u.pressed)
	}
}

// TestFastForwardIsHeld は早送りが押している間だけ効くことを確かめる。
func TestFastForwardIsHeld(t *testing.T) {
	u := newTestUI(t)

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyTab})
	waitSpeed(t, u, fastForwardSpeed)

	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyTab})
	waitSpeed(t, u, 1.0)
}

// TestSlowMotionIsHeld はスローが押している間だけ効くことを確かめる。
func TestSlowMotionIsHeld(t *testing.T) {
	u := newTestUI(t)

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyBackTick})
	waitSpeed(t, u, slowMotionSpeed)

	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyBackTick})
	waitSpeed(t, u, 1.0)
}

// TestFastForwardWinsOverSlowMotion は両方を押したとき早送りが
// 優先されることを確かめる。
func TestFastForwardWinsOverSlowMotion(t *testing.T) {
	u := newTestUI(t)
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyBackTick})
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyTab})
	waitSpeed(t, u, fastForwardSpeed)

	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyTab})
	waitSpeed(t, u, slowMotionSpeed)
}

// TestBaseSpeedIsRestoredAfterFastForward はメニューで変えた速度倍率が
// 早送りの後に戻ることを確かめる。
func TestBaseSpeedIsRestoredAfterFastForward(t *testing.T) {
	u := newTestUI(t)
	u.SetSpeed(2.0)
	waitSpeed(t, u, 2.0)

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyTab})
	waitSpeed(t, u, fastForwardSpeed)
	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyTab})
	waitSpeed(t, u, 2.0)
}

// waitSpeed は速度倍率が期待の値になるまで待つ。
func waitSpeed(t *testing.T, u *UI, want float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.emu.Status().Speed == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("速度倍率が %v にならない（現在 %v）", want, u.emu.Status().Speed)
}

// TestOneKeyRunsAllActions は同じキーに複数のアクションを割り当てたとき
// すべて実行されることを確かめる。
func TestOneKeyRunsAllActions(t *testing.T) {
	u := newTestUI(t)
	u.bindings = map[string][]config.Action{
		"KeyQ": {config.ActionP1A, config.ActionP2B, config.ActionPause},
	}

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyQ})
	if got := u.emu.Input.Buttons(0); got != input.ButtonA {
		t.Errorf("1P = %#08b, 期待 %#08b", got, input.ButtonA)
	}
	if got := u.emu.Input.Buttons(1); got != input.ButtonB {
		t.Errorf("2P = %#08b, 期待 %#08b", got, input.ButtonB)
	}
	if !u.paused {
		t.Error("一時停止していない")
	}
}

// TestReleaseAllKeysClearsButtons はフォーカスを失ったときに
// 押しっぱなしが残らないことを確かめる。
func TestReleaseAllKeysClearsButtons(t *testing.T) {
	u := newTestUI(t)
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyX})
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyRight})
	u.releaseAllKeys()

	if got := u.emu.Input.Buttons(0); got != 0 {
		t.Errorf("押下状態 = %#08b, 期待 0", got)
	}
	if len(u.pressed) != 0 {
		t.Errorf("押下の記録が残っている: %v", u.pressed)
	}
}

// TestSlotHotkeysChangeSlot はスロットを選ぶホットキーが働くことを
// 確かめる。
func TestSlotHotkeysChangeSlot(t *testing.T) {
	u := newTestUI(t)

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyF6}) // nextSlot
	if got := u.emu.Slot(); got != 1 {
		t.Errorf("次のスロット = %d, 期待 1", got)
	}
	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyF6})

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyF4}) // prevSlot
	if got := u.emu.Slot(); got != 0 {
		t.Errorf("前のスロット = %d, 期待 0", got)
	}
}

// TestStateHotkeysAreBound はステートと巻き戻しのホットキーが既定の
// 割り当てにあることを確かめる。
//
// 設計書 07 編 §7.6 の既定値に対応する。
func TestStateHotkeysAreBound(t *testing.T) {
	bindings := config.DefaultKeybindings().Resolve()
	for code, want := range map[string]config.Action{
		"F5":        config.ActionSaveState,
		"F7":        config.ActionLoadState,
		"Backspace": config.ActionRewind,
	} {
		found := false
		for _, a := range bindings[code] {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s に %v が割り当てられていない（%v）", code, want, bindings[code])
		}
	}
}

// TestRewindHotkeyIsHeld は巻き戻しのキーを押している間だけ要求が
// 続くことを確かめる。
//
// ROM を読み込んでいないため巻き戻しは始まらない。ここで確かめるのは
// 押下と離上で例外が起きず、状態が戻ることである。
func TestRewindHotkeyIsHeld(t *testing.T) {
	u := newTestUI(t)

	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	u.onKeyUp(&fyne.KeyEvent{Name: fyne.KeyBackspace})

	if u.emu.Status().Rewinding {
		t.Error("キーを離しても巻き戻し中のままである")
	}
}
