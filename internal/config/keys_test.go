package config

import (
	"slices"
	"testing"
)

// TestDefaultKeybindingsMatchSpec は既定の割り当てが設計書 07 編 §7.4.4 の
// 表どおりであることを確かめる。
func TestDefaultKeybindingsMatchSpec(t *testing.T) {
	m := DefaultKeybindings().Resolve()

	tests := []struct {
		code string
		want Action
	}{
		{"ArrowUp", ActionP1Up},
		{"ArrowDown", ActionP1Down},
		{"ArrowLeft", ActionP1Left},
		{"ArrowRight", ActionP1Right},
		{"KeyX", ActionP1A},
		{"KeyZ", ActionP1B},
		{"Enter", ActionP1Start},
		{"ShiftRight", ActionP1Select},
		{"KeyW", ActionP2Up},
		{"KeyS", ActionP2Down},
		{"KeyA", ActionP2Left},
		{"KeyD", ActionP2Right},
		{"KeyG", ActionP2A},
		{"KeyF", ActionP2B},
		{"KeyR", ActionP2Start},
		{"KeyT", ActionP2Select},
		{"Space", ActionPause},
		{"Period", ActionFrameAdvance},
		{"Tab", ActionFastForward},
		{"Backquote", ActionSlowMotion},
		{"F1", ActionReset},
		{"F5", ActionSaveState},
		{"F7", ActionLoadState},
		{"Backspace", ActionRewind},
		{"F12", ActionScreenshot},
		{"F11", ActionToggleFullscreen},
	}
	for _, tt := range tests {
		got := m[tt.code]
		if !slices.Contains(got, tt.want) {
			t.Errorf("%s に %v が割り当てられていない: %v", tt.code, tt.want, got)
		}
	}
}

// TestResolveKeepsAllActionsForOneKey は同じキーに複数のアクションを
// 割り当てたときすべてを返すことを確かめる。
func TestResolveKeepsAllActionsForOneKey(t *testing.T) {
	k := &Keybindings{
		Players: []PlayerBindings{
			{Player: 1, Bindings: map[string][]Binding{ButtonA: {Key("KeyZ")}}},
			{Player: 2, Bindings: map[string][]Binding{ButtonB: {Key("KeyZ")}}},
		},
		Hotkeys: map[string][]Binding{"pause": {Key("KeyZ")}},
	}
	got := k.Resolve()["KeyZ"]
	want := []Action{ActionP1A, ActionP2B, ActionPause}
	if !slices.Equal(got, want) {
		t.Errorf("KeyZ = %v, 期待 %v", got, want)
	}
}

// TestResolveIsOrderedByAction は並びがアクションの定義順であることを
// 確かめる。map をたどる順序に依存すると適用の順序が実行ごとに変わる。
func TestResolveIsOrderedByAction(t *testing.T) {
	k := &Keybindings{
		Hotkeys: map[string][]Binding{
			"mute":       {Key("KeyQ")},
			"pause":      {Key("KeyQ")},
			"screenshot": {Key("KeyQ")},
		},
	}
	for range 20 {
		got := k.Resolve()["KeyQ"]
		want := []Action{ActionPause, ActionScreenshot, ActionMute}
		if !slices.Equal(got, want) {
			t.Fatalf("KeyQ = %v, 期待 %v", got, want)
		}
	}
}

// TestResolveDedupes は同じアクションへ同じキーを二重に割り当てても
// 1 回だけ返すことを確かめる。
func TestResolveDedupes(t *testing.T) {
	k := &Keybindings{
		Hotkeys: map[string][]Binding{"pause": {Key("Space"), Key("Space")}},
	}
	if got := k.Resolve()["Space"]; len(got) != 1 {
		t.Errorf("Space = %v, 期待 1 個", got)
	}
}

// TestResolveIgnoresUnknownBindingType は種別が key でない割り当てを
// 無視することを確かめる。ゲームパッドの割り当てはフェーズ 12 で扱う。
func TestResolveIgnoresUnknownBindingType(t *testing.T) {
	k := &Keybindings{
		Hotkeys: map[string][]Binding{
			"pause": {{Type: "pad", Code: "Button0"}, {Type: BindingTypeKey, Code: ""}},
		},
	}
	if got := k.Resolve(); len(got) != 0 {
		t.Errorf("解決の結果が空でない: %v", got)
	}
}

// TestActionNames はアクションと名前の対応が往復することを確かめる。
func TestActionNames(t *testing.T) {
	for _, name := range HotkeyNames() {
		a, ok := HotkeyAction(name)
		if !ok {
			t.Fatalf("ホットキー %q を解決できない", name)
		}
		if a.String() != name {
			t.Errorf("%v.String() = %q, 期待 %q", a, a.String(), name)
		}
		if a.IsPlayerInput() {
			t.Errorf("%q がプレイヤー入力として扱われている", name)
		}
	}

	for _, player := range []int{1, 2} {
		for _, name := range PlayerButtonOrder() {
			a, ok := PlayerAction(player, name)
			if !ok {
				t.Fatalf("プレイヤー %d の %q を解決できない", player, name)
			}
			if !a.IsPlayerInput() {
				t.Errorf("%v がプレイヤー入力として扱われていない", a)
			}
			if a.Player() != player {
				t.Errorf("%v.Player() = %d, 期待 %d", a, a.Player(), player)
			}
			if got, _ := a.ButtonName(); got != name {
				t.Errorf("%v.ButtonName() = %q, 期待 %q", a, got, name)
			}
		}
	}
}

// TestPlayerActionRejectsUnknown は知らないボタン名とポート番号を
// 拒むことを確かめる。
func TestPlayerActionRejectsUnknown(t *testing.T) {
	if _, ok := PlayerAction(1, "turbo"); ok {
		t.Error("知らないボタン名を受け入れてしまった")
	}
	if _, ok := PlayerAction(3, ButtonA); ok {
		t.Error("ポート 3 を受け入れてしまった")
	}
}

// TestDefaultConfigValues は既定値が設計書 11 編 §11.3.1 の表どおりで
// あることを確かめる。
func TestDefaultConfigValues(t *testing.T) {
	c := Default()
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"version", c.Version, Version},
		{"emulation.region", c.Emulation.Region, RegionAuto},
		{"emulation.ramInitPattern", c.Emulation.RAMInitPattern, "random"},
		{"emulation.cpuPpuAlignment", c.Emulation.CPUPPUAlignment, 2},
		{"emulation.dmcDmaRegisterConflicts", c.Emulation.DMCDMARegisterConflicts, true},
		{"video.scale", c.Video.Scale, 3},
		{"video.filter", c.Video.Filter, FilterNearest},
		{"video.overscanTop", c.Video.OverscanTop, 8},
		{"video.overscanBottom", c.Video.OverscanBottom, 8},
		{"video.overscanLeft", c.Video.OverscanLeft, 0},
		{"audio.sampleRate", c.Audio.SampleRate, 48000},
		{"audio.bufferMilliseconds", c.Audio.BufferMilliseconds, 25},
		{"audio.ringHighWaterMultiplier", c.Audio.RingHighWaterMultiplier, 2},
		{"input.turboRateHz", c.Input.TurboRateHz, 15},
		{"state.slots", c.State.Slots, 10},
		{"state.rewindSeconds", c.State.RewindSeconds, 60},
		{"state.rewindIntervalFrames", c.State.RewindIntervalFrames, 10},
		{"movie.checksumIntervalFrames", c.Movie.ChecksumIntervalFrames, 60},
		{"ui.viewerLayout", c.UI.ViewerLayout, LayoutWindows},
		{"ui.language", c.UI.Language, "ja"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, 期待 %v", tt.name, tt.got, tt.want)
		}
	}
}
