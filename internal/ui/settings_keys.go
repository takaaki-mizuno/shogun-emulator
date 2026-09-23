package ui

import (
	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// buttonLabels はプレイヤー入力のボタンの表示名。
var buttonLabels = map[config.ButtonName]string{
	config.ButtonUp: i18n.T(i18n.SetTop), config.ButtonDown: i18n.T(i18n.SetBottom), config.ButtonLeft: i18n.T(i18n.SetLeft), config.ButtonRight: i18n.T(i18n.SetRight),
	config.ButtonA: "A", config.ButtonB: "B", config.ButtonStart: "Start", config.ButtonSelect: "Select",
}

// hotkeyLabels はホットキーの表示名。
var hotkeyLabels = map[string]string{
	"pause": i18n.T(i18n.MenuPause), "frameAdvance": i18n.T(i18n.MenuFrameAdvance), "fastForward": i18n.T(i18n.KeyFastForward),
	"slowMotion": i18n.T(i18n.KeySlowMotion), "reset": i18n.T(i18n.MenuReset), "hardReset": i18n.T(i18n.MenuHardReset),
	"saveState": i18n.T(i18n.SetPathState), "loadState": i18n.T(i18n.KeyLoadState), "nextSlot": i18n.T(i18n.KeyNextSlot),
	"prevSlot": i18n.T(i18n.KeyPrevSlot), "rewind": i18n.T(i18n.KeyRewind), "screenshot": i18n.T(i18n.SetPathScreenshot),
	"toggleFullscreen": i18n.T(i18n.KeyToggleFullscreen), "mute": i18n.T(i18n.KeyMute),
}

// turboChoices は連射レートの選択肢。0 は連射しない。
var turboChoices = []string{i18n.T(i18n.SetNoneOption), "5", "10", "15", "20", "30"}

// keyEditor はキーバインドの設定（設計書 10 編 §10.8.1）。
//
// 設定画面の写し keys を編集する。キーの取り込みは設定ウィンドウのキー
// イベントで受け取る。
type keyEditor struct {
	f      *settingsForm
	box    *fyne.Container
	status *widget.Label
	// capture は取り込み中のとき、押されたキーコードを受け取る。
	capture func(code string)
}

// newKeyEditor はキーバインドの設定を作る。
func newKeyEditor(f *settingsForm) *keyEditor {
	return &keyEditor{f: f, box: container.NewVBox(), status: widget.NewLabel("")}
}

// content は一覧と操作を返す。
func (k *keyEditor) content() fyne.CanvasObject {
	k.rebuild()
	reset := widget.NewButton(i18n.T(i18n.KeyResetDefaults), func() {
		*k.f.keys = *config.DefaultKeybindings()
		k.rebuild()
	})
	k.status.Importance = widget.WarningImportance
	return container.NewVBox(k.status, k.box, reset)
}

// rebuild は写しから一覧を作り直す。重複した割り当てを強調する。
func (k *keyEditor) rebuild() {
	k.box.RemoveAll()
	dup := k.f.keys.Duplicates()
	for _, player := range []int{1, 2} {
		k.box.Add(widget.NewLabelWithStyle(i18n.T(i18n.KeyPlayerN, player), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		p := k.f.keys.Player(player)
		for _, name := range config.PlayerButtonOrder() {
			list := &sliceRef{get: func() []config.Binding { return p.Bindings[name] },
				set: func(b []config.Binding) { p.Bindings[name] = b }}
			k.box.Add(k.row(buttonLabels[name], list, dup, k.turboSelect(p, name)))
		}
	}
	k.box.Add(widget.NewLabelWithStyle(i18n.T(i18n.KeyHotkeys), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	for _, name := range config.HotkeyNames() {
		list := &sliceRef{get: func() []config.Binding { return k.f.keys.Hotkeys[name] },
			set: func(b []config.Binding) { k.f.keys.Hotkeys[name] = b }}
		k.box.Add(k.row(hotkeyLabels[name], list, dup, nil))
	}
	k.box.Refresh()
}

// sliceRef は割り当ての一覧を読み書きする。
type sliceRef struct {
	get func() []config.Binding
	set func([]config.Binding)
}

// row は 1 つのアクションの行を作る。割り当てたキーは押すと外れる。
func (k *keyEditor) row(label string, list *sliceRef, dup map[string]bool, extra fyne.CanvasObject) fyne.CanvasObject {
	keys := container.NewHBox()
	for i, b := range list.get() {
		btn := widget.NewButton(b.Code+" ×", func() {
			cur := list.get()
			list.set(append(cur[:i:i], cur[i+1:]...))
			k.rebuild()
		})
		if dup[b.Code] {
			btn.Importance = widget.DangerImportance
		}
		keys.Add(btn)
	}
	add := widget.NewButton(i18n.T(i18n.KeyAdd), func() { k.startCapture(label, list) })
	right := container.NewHBox(add)
	if extra != nil {
		right.Add(extra)
	}
	return container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(170, 36), widget.NewLabel(label)), right, keys)
}

// turboSelect はボタンの連射レートを選ぶ部品を作る。
func (k *keyEditor) turboSelect(p *config.PlayerBindings, name config.ButtonName) fyne.CanvasObject {
	sel := widget.NewSelect(turboChoices, func(s string) {
		hz, err := strconv.Atoi(s)
		if err != nil || hz <= 0 {
			delete(p.Turbo, name)
			return
		}
		p.Turbo[name] = hz
	})
	sel.PlaceHolder = i18n.T(i18n.KeyTurbo)
	if hz := p.Turbo[name]; hz > 0 {
		sel.SetSelected(strconv.Itoa(hz))
		if sel.Selected == "" {
			sel.Options = append(sel.Options, strconv.Itoa(hz))
			sel.SetSelected(strconv.Itoa(hz))
		}
	} else {
		sel.SetSelected(turboChoices[0])
	}
	return sel
}

// startCapture はキーの取り込みを始める。取り込み中はホットキーを止める。
func (k *keyEditor) startCapture(label string, list *sliceRef) {
	k.f.u.keyCapture = true
	k.status.SetText(i18n.T(i18n.KeyCapturePrompt, label))
	k.capture = func(code string) {
		k.capture = nil
		k.f.u.keyCapture = false
		if code == "Escape" {
			k.status.SetText(i18n.T(i18n.KeyCaptureCancelled))
			return
		}
		for _, b := range list.get() {
			if b.Code == code {
				k.status.SetText(i18n.T(i18n.KeyAlreadyBound, code))
				return
			}
		}
		list.set(append(list.get(), config.Key(code)))
		k.status.SetText(i18n.T(i18n.KeyBound, label, code))
		k.rebuild()
	}
}

// keyPressed は取り込み中に押されたキーを受け取る。変換表に無いキーは知らせる。
func (k *keyEditor) keyPressed(name fyne.KeyName) {
	if k.capture == nil {
		return
	}
	if name == fyne.KeyEscape {
		k.capture("Escape")
		return
	}
	code, ok := keyCode(name)
	if !ok {
		k.status.SetText(i18n.T(i18n.KeyUnbindable, name))
		return
	}
	k.capture(code)
}

// installKeyCapture は設定ウィンドウのキーイベントを取り込みへつなぐ。
func (f *settingsForm) installKeyCapture() {
	dc, ok := f.win.Canvas().(desktop.Canvas)
	if !ok {
		return
	}
	dc.SetOnKeyDown(func(ev *fyne.KeyEvent) {
		if f.keyEditor != nil {
			f.keyEditor.keyPressed(ev.Name)
		}
	})
}
