package ui

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// TestKeymapCoversDefaultBindings は既定の割り当てに使うキーが
// すべて変換表にあることを確かめる。
//
// 変換表に無いキーは無視される。既定の割り当てに使うキーが漏れていると、
// 設定を変えていない利用者が操作できない。
func TestKeymapCoversDefaultBindings(t *testing.T) {
	codes := map[string]bool{}
	for _, code := range fyneKeyToCode {
		codes[code] = true
	}
	for code := range config.DefaultKeybindings().Resolve() {
		if !codes[code] {
			t.Errorf("既定の割り当てに使う %q が変換表に無い", code)
		}
	}
}

// TestKeymapIsInjective は 2 つの Fyne のキー名が同じ code に
// ならないことを確かめる。重なると別の物理キーが区別できなくなる。
func TestKeymapIsInjective(t *testing.T) {
	seen := map[string]fyne.KeyName{}
	for name, code := range fyneKeyToCode {
		if other, ok := seen[code]; ok {
			t.Errorf("%q に %q と %q の 2 つが対応している", code, other, name)
		}
		seen[code] = name
	}
}

// TestKeyCodeIgnoresUnknownKeys は変換表に無いキーを無視することを確かめる。
func TestKeyCodeIgnoresUnknownKeys(t *testing.T) {
	if _, ok := keyCode(fyne.KeyUnknown); ok {
		t.Error("知らないキーを受け入れてしまった")
	}
	if _, ok := keyCode(fyne.KeyName("存在しないキー")); ok {
		t.Error("知らないキーを受け入れてしまった")
	}
}

// TestKeymapNamesAreW3CCodes は変換後の名前が W3C の code の形で
// あることを確かめる。設定ファイルに保存される値である。
func TestKeymapNamesAreW3CCodes(t *testing.T) {
	tests := []struct {
		key  fyne.KeyName
		want string
	}{
		{fyne.KeyUp, "ArrowUp"},
		{fyne.KeyZ, "KeyZ"},
		{fyne.Key0, "Digit0"},
		{fyne.KeyReturn, "Enter"},
		{fyne.KeyBackTick, "Backquote"},
		{fyne.KeyPeriod, "Period"},
		{fyne.KeyBackspace, "Backspace"},
	}
	for _, tt := range tests {
		got, ok := keyCode(tt.key)
		if !ok || got != tt.want {
			t.Errorf("%q = %q（%v）, 期待 %q", tt.key, got, ok, tt.want)
		}
	}
}
