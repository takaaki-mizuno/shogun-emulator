package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// newSettingsTestUI は一時ファイルへ保存する Store を持つ UI を作る。
func newSettingsTestUI(t *testing.T, overrides ...config.Override) *UI {
	t.Helper()
	u := newDebugTestUI(t)
	dir := t.TempDir()
	u.store = config.NewStore(config.Paths{Config: dir}, filepath.Join(dir, "config.json"),
		filepath.Join(dir, "keybindings.json"), config.Default(), overrides, config.DefaultKeybindings())
	u.cfg = u.store.Config()
	u.screen = newScreen(u.emu.Frames, u.pal, u.cfg.Video)
	return u
}

// TestSettingsSaveAppliesAndPersists は設定画面の保存がファイルへ書かれ、
// 即時の項目が画面とエミュレータへ反映されることを確かめる（設計書 11 編 §11.3.2）。
func TestSettingsSaveAppliesAndPersists(t *testing.T) {
	u := newSettingsTestUI(t)
	f := u.newSettingsForm()
	f.win = u.app.NewWindow("設定")
	f.win.SetContent(f.content())

	f.edit.Video.Scale = 5
	f.edit.Video.AspectRatioCorrection = true
	f.edit.Audio.MasterVolume = 0.5
	f.edit.Emulation.Region = config.RegionPAL
	p1 := f.keys.Player(1)
	p1.Bindings[config.ButtonA] = append(p1.Bindings[config.ButtonA], config.Key("KeyQ"))
	p1.Turbo[config.ButtonB] = 15
	f.save()

	if u.cfg.Video.Scale != 5 || u.screen.scale != 5 || !u.screen.aspect {
		t.Errorf("映像の設定が反映されていない: cfg %d, screen %d", u.cfg.Video.Scale, u.screen.scale)
	}
	if got := u.emu.Settings(); got.Emulation.Region != config.RegionPAL || got.Audio.MasterVolume != 0.5 {
		t.Errorf("エミュレータへ渡っていない: %+v %+v", got.Emulation, got.Audio)
	}
	if acts := u.bindings["KeyQ"]; len(acts) != 1 || acts[0] != config.ActionP1A {
		t.Errorf("キーバインドが反映されていない: %v", acts)
	}
	saved, _, err := config.Load(u.store.File)
	if err != nil || saved.Video.Scale != 5 || saved.Emulation.Region != config.RegionPAL {
		t.Errorf("設定ファイルに保存されていない: %v %+v", err, saved.Video)
	}
	keys, _, err := config.LoadKeybindings(u.store.KeysFile, KnownKeyCode)
	if err != nil || keys.Player(1).Turbo[config.ButtonB] != 15 {
		t.Errorf("キーバインドファイルに保存されていない: %v", err)
	}
}

// TestSettingsOverrideNotWrittenBack は引数で上書きした項目が、設定画面の保存で
// ファイルへ書き戻されないことを確かめる（設計書 11 編 §11.1）。
func TestSettingsOverrideNotWrittenBack(t *testing.T) {
	u := newSettingsTestUI(t, config.Override{Source: "--scale", Apply: func(c *config.Config) { c.Video.Scale = 7 }})
	if u.cfg.Video.Scale != 7 {
		t.Fatalf("使う設定に上書きが入っていない: %d", u.cfg.Video.Scale)
	}
	f := u.newSettingsForm()
	if f.edit.Video.Scale != config.Default().Video.Scale {
		t.Errorf("写しに上書きが混ざっている: %d", f.edit.Video.Scale)
	}
	note := f.overrideNote(func(c *config.Config) any { return c.Video.Scale })
	if !strings.Contains(note, "--scale") || !strings.Contains(note, "7") {
		t.Errorf("上書きの表示 = %q", note)
	}
	if f.overrideNote(func(c *config.Config) any { return c.Video.Filter }) != "" {
		t.Error("上書きしていない項目に表示が出た")
	}
	f.edit.Video.Scale = 4
	f.save()
	saved, _, _ := config.Load(u.store.File)
	if saved.Video.Scale != 4 || u.cfg.Video.Scale != 7 {
		t.Errorf("保存 %d, 使う設定 %d, 期待 4, 7", saved.Video.Scale, u.cfg.Video.Scale)
	}
}

// TestKeyEditorCapture はキーの取り込みを確かめる。取り込み中はホットキーを止める
// （設計書 10 編 §10.8.1）。
func TestKeyEditorCapture(t *testing.T) {
	u := newSettingsTestUI(t)
	f := u.newSettingsForm()
	f.win = u.app.NewWindow("設定")
	f.win.SetContent(f.content())
	k := f.keyEditor
	p1 := f.keys.Player(1)
	list := &sliceRef{get: func() []config.Binding { return p1.Bindings[config.ButtonA] },
		set: func(b []config.Binding) { p1.Bindings[config.ButtonA] = b }}

	k.startCapture("A", list)
	if !u.keyCapture {
		t.Fatal("取り込み中にホットキーが止まっていない")
	}
	u.onKeyDown(&fyne.KeyEvent{Name: fyne.KeyF5})
	if u.pressed["F5"] {
		t.Error("取り込み中にメインウィンドウがキーを処理した")
	}
	k.keyPressed(fyne.KeyName("NotAKey"))
	if !strings.Contains(k.status.Text, "割り当てられない") || k.capture == nil {
		t.Errorf("変換表に無いキーの扱い: %q", k.status.Text)
	}
	k.keyPressed(fyne.KeyF5)
	if u.keyCapture || len(p1.Bindings[config.ButtonA]) != 2 || p1.Bindings[config.ButtonA][1].Code != "F5" {
		t.Errorf("取り込んだ割り当て = %v", p1.Bindings[config.ButtonA])
	}
	// F5 はセーブステートにも割り当ててあるため重複として強調する。
	if !f.keys.Duplicates()["F5"] {
		t.Error("重複を検出していない")
	}

	k.startCapture("A", list)
	k.keyPressed(fyne.KeyEscape)
	if u.keyCapture || len(p1.Bindings[config.ButtonA]) != 2 {
		t.Error("Escape で取り込みをやめられない")
	}

	f.resetDefaults()
	if len(f.keys.Player(1).Bindings[config.ButtonA]) != 1 {
		t.Error("既定値に戻していない")
	}
}

// TestSettingsResetDefaultsKeepsHistory は既定値に戻してもウィンドウ状態と
// 最近使った ROM を残すことを確かめる。
func TestSettingsResetDefaultsKeepsHistory(t *testing.T) {
	u := newSettingsTestUI(t)
	u.addRecent("/roms/a.nes")
	f := u.newSettingsForm()
	f.win = u.app.NewWindow("設定")
	f.win.SetContent(f.content())
	f.edit.Video.Scale = 6
	f.resetDefaults()
	if f.edit.Video.Scale != config.Default().Video.Scale || len(f.edit.UI.RecentROMs) != 1 {
		t.Errorf("既定値に戻した写し: scale %d, recent %v", f.edit.Video.Scale, f.edit.UI.RecentROMs)
	}
}

// TestWindowStatesSavedAndRestored はビューアのサイズと表示状態を保存し、
// 起動時に開き直すことを確かめる（設計書 10 編 §10.7）。
func TestWindowStatesSavedAndRestored(t *testing.T) {
	u := newSettingsTestUI(t)
	u.status = newStatusBar(u.emu.Frames)
	u.buildContent()
	u.update(func(c *config.Config) {
		c.UI.Windows[windowPattern] = config.WindowState{Width: 420, Height: 330, Visible: true}
		c.UI.Windows[windowMemory] = config.WindowState{Width: 300, Height: 300, Visible: true}
	})

	u.reopenViewers()
	if !u.host.IsVisible(u.patterns()) {
		t.Fatal("前回開いていたパターンテーブルが開き直されていない")
	}
	if got := u.host.Size(u.patterns()); got.Width != 420 || got.Height != 330 {
		t.Errorf("保存したサイズ = %v, 期待 420x330", got)
	}
	if len(u.host.Visible()) != 1 {
		t.Errorf("開いたビューアの数 = %d（メモリビューアは開き直さない）", len(u.host.Visible()))
	}

	u.saveWindowStates()
	if st := u.cfg.UI.Windows[windowPattern]; !st.Visible {
		t.Error("終了時に開いていたビューアが開いたものとして記録されていない")
	}
	u.host.Hide(u.patterns())
	if st := u.cfg.UI.Windows[windowPattern]; st.Visible || st.Width == 0 {
		t.Errorf("閉じたときの記録 = %+v", st)
	}
	saved, _, _ := config.Load(u.store.File)
	if st := saved.UI.Windows[windowPattern]; st.Visible || st.Width == 0 {
		t.Errorf("設定ファイルの記録 = %+v", st)
	}
}

// TestPaletteFileAppliedImmediately はパレットファイルの差し替えがすぐに
// 反映され、読めないときは組み込みのパレットに戻ることを確かめる（計画 §3.14）。
func TestPaletteFileAppliedImmediately(t *testing.T) {
	u := newSettingsTestUI(t)
	dir := t.TempDir()
	pal := make([]byte, 192)
	pal[0x0F*3] = 0xFF // $0F を赤にする
	path := filepath.Join(dir, "red.pal")
	if err := writeFile(path, pal); err != nil {
		t.Fatal(err)
	}
	old := u.cfg.Clone()
	u.update(func(c *config.Config) { c.Video.PaletteFile = path })
	u.applySettings(old, u.store.Keys())
	if got := u.pal.Color(0x0F); got.R != 0xFF || got.G != 0 {
		t.Errorf("差し替えたパレットの $0F = %v", got)
	}
	old = u.cfg.Clone()
	u.update(func(c *config.Config) { c.Video.PaletteFile = filepath.Join(dir, "none.pal") })
	u.applySettings(old, u.store.Keys())
	if got := u.pal.Color(0x0F); got.R == 0xFF && got.G == 0 {
		t.Error("読めないパレットで組み込みのパレットに戻っていない")
	}
}
