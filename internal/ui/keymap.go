package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// fyneKeyToCode は Fyne のキー名を W3C の KeyboardEvent.code へ変換する。
//
// 物理キーの位置を表す名前を使うのは、キーボードレイアウト（JIS、US、
// AZERTY）が変わっても設定ファイルの内容が通用するようにするためである。
// 変換表はこのファイルにのみ存在する。
//
// 表に無いキーは無視する。押されても何も起こらない。
var fyneKeyToCode = map[fyne.KeyName]string{
	// 矢印キー
	fyne.KeyUp:    "ArrowUp",
	fyne.KeyDown:  "ArrowDown",
	fyne.KeyLeft:  "ArrowLeft",
	fyne.KeyRight: "ArrowRight",

	// 英字キー
	fyne.KeyA: "KeyA",
	fyne.KeyB: "KeyB",
	fyne.KeyC: "KeyC",
	fyne.KeyD: "KeyD",
	fyne.KeyE: "KeyE",
	fyne.KeyF: "KeyF",
	fyne.KeyG: "KeyG",
	fyne.KeyH: "KeyH",
	fyne.KeyI: "KeyI",
	fyne.KeyJ: "KeyJ",
	fyne.KeyK: "KeyK",
	fyne.KeyL: "KeyL",
	fyne.KeyM: "KeyM",
	fyne.KeyN: "KeyN",
	fyne.KeyO: "KeyO",
	fyne.KeyP: "KeyP",
	fyne.KeyQ: "KeyQ",
	fyne.KeyR: "KeyR",
	fyne.KeyS: "KeyS",
	fyne.KeyT: "KeyT",
	fyne.KeyU: "KeyU",
	fyne.KeyV: "KeyV",
	fyne.KeyW: "KeyW",
	fyne.KeyX: "KeyX",
	fyne.KeyY: "KeyY",
	fyne.KeyZ: "KeyZ",

	// 数字キー
	fyne.Key0: "Digit0",
	fyne.Key1: "Digit1",
	fyne.Key2: "Digit2",
	fyne.Key3: "Digit3",
	fyne.Key4: "Digit4",
	fyne.Key5: "Digit5",
	fyne.Key6: "Digit6",
	fyne.Key7: "Digit7",
	fyne.Key8: "Digit8",
	fyne.Key9: "Digit9",

	// ファンクションキー
	fyne.KeyF1:  "F1",
	fyne.KeyF2:  "F2",
	fyne.KeyF3:  "F3",
	fyne.KeyF4:  "F4",
	fyne.KeyF5:  "F5",
	fyne.KeyF6:  "F6",
	fyne.KeyF7:  "F7",
	fyne.KeyF8:  "F8",
	fyne.KeyF9:  "F9",
	fyne.KeyF10: "F10",
	fyne.KeyF11: "F11",
	fyne.KeyF12: "F12",

	// 編集キー
	fyne.KeyReturn:    "Enter",
	fyne.KeyEnter:     "NumpadEnter",
	fyne.KeyTab:       "Tab",
	fyne.KeySpace:     "Space",
	fyne.KeyBackspace: "Backspace",
	fyne.KeyDelete:    "Delete",
	fyne.KeyInsert:    "Insert",
	fyne.KeyEscape:    "Escape",
	fyne.KeyHome:      "Home",
	fyne.KeyEnd:       "End",
	fyne.KeyPageUp:    "PageUp",
	fyne.KeyPageDown:  "PageDown",

	// 記号キー
	fyne.KeyBackTick:     "Backquote",
	fyne.KeyPeriod:       "Period",
	fyne.KeyComma:        "Comma",
	fyne.KeyMinus:        "Minus",
	fyne.KeyEqual:        "Equal",
	fyne.KeySlash:        "Slash",
	fyne.KeyBackslash:    "Backslash",
	fyne.KeyLeftBracket:  "BracketLeft",
	fyne.KeyRightBracket: "BracketRight",
	fyne.KeySemicolon:    "Semicolon",
	fyne.KeyApostrophe:   "Quote",

	// 修飾キー
	desktop.KeyShiftLeft:    "ShiftLeft",
	desktop.KeyShiftRight:   "ShiftRight",
	desktop.KeyControlLeft:  "ControlLeft",
	desktop.KeyControlRight: "ControlRight",
	desktop.KeyAltLeft:      "AltLeft",
	desktop.KeyAltRight:     "AltRight",
	desktop.KeySuperLeft:    "MetaLeft",
	desktop.KeySuperRight:   "MetaRight",
	desktop.KeyCapsLock:     "CapsLock",
	desktop.KeyMenu:         "ContextMenu",
	desktop.KeyPrintScreen:  "PrintScreen",
}

// keyCode は Fyne のキー名に対応する code を返す。
func keyCode(name fyne.KeyName) (string, bool) {
	code, ok := fyneKeyToCode[name]
	return code, ok
}

// knownCodes は変換表にあるキーコードの集合。
var knownCodes = func() map[string]bool {
	m := map[string]bool{}
	for _, code := range fyneKeyToCode {
		m[code] = true
	}
	return m
}()

// KnownKeyCode はキーコードが変換表にあるかを返す。キーバインドファイルの
// 読み込みで、知らないキーコードを除くために使う（設計書 11 編 §11.4）。
func KnownKeyCode(code string) bool { return knownCodes[code] }
