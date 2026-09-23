package emu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// TestSetActionMapsToControllerBits はアクションがコントローラのビットへ
// 対応づけられることを確かめる。
func TestSetActionMapsToControllerBits(t *testing.T) {
	tests := []struct {
		action config.Action
		port   int
		bit    uint8
	}{
		{config.ActionP1A, 0, input.ButtonA},
		{config.ActionP1B, 0, input.ButtonB},
		{config.ActionP1Select, 0, input.ButtonSelect},
		{config.ActionP1Start, 0, input.ButtonStart},
		{config.ActionP1Up, 0, input.ButtonUp},
		{config.ActionP1Down, 0, input.ButtonDown},
		{config.ActionP1Left, 0, input.ButtonLeft},
		{config.ActionP1Right, 0, input.ButtonRight},
		{config.ActionP2A, 1, input.ButtonA},
		{config.ActionP2Right, 1, input.ButtonRight},
	}
	for _, tt := range tests {
		var s InputState
		if !s.SetAction(tt.action, true) {
			t.Fatalf("%v をプレイヤー入力として扱えない", tt.action)
		}
		if got := s.Buttons(tt.port); got != tt.bit {
			t.Errorf("%v: ポート %d = %#08b, 期待 %#08b", tt.action, tt.port, got, tt.bit)
		}
		// 他方のポートは変わらない
		if got := s.Buttons(1 - tt.port); got != 0 {
			t.Errorf("%v: ポート %d が %#08b に変わった", tt.action, 1-tt.port, got)
		}

		s.SetAction(tt.action, false)
		if got := s.Buttons(tt.port); got != 0 {
			t.Errorf("%v: 離した後に %#08b が残った", tt.action, got)
		}
	}
}

// TestSetActionRejectsHotkeys はホットキーのアクションを押下状態へ
// 反映しないことを確かめる。
func TestSetActionRejectsHotkeys(t *testing.T) {
	var s InputState
	for _, a := range []config.Action{config.ActionPause, config.ActionReset, config.ActionNone} {
		if s.SetAction(a, true) {
			t.Errorf("%v をプレイヤー入力として扱ってしまった", a)
		}
	}
	if got := s.Buttons(0); got != 0 {
		t.Errorf("押下状態が %#08b に変わった", got)
	}
}

// TestSetKeepsOtherButtons は 1 つのボタンの操作が他のボタンを
// 変えないことを確かめる。
func TestSetKeepsOtherButtons(t *testing.T) {
	var s InputState
	s.SetAction(config.ActionP1A, true)
	s.SetAction(config.ActionP1Right, true)
	if got, want := s.Buttons(0), input.ButtonA|input.ButtonRight; got != want {
		t.Errorf("押下状態 = %#08b, 期待 %#08b", got, want)
	}
	s.SetAction(config.ActionP1A, false)
	if got, want := s.Buttons(0), input.ButtonRight; got != want {
		t.Errorf("A を離した後 = %#08b, 期待 %#08b", got, want)
	}
}

// TestClearReleasesEverything はすべてのボタンを離すことを確かめる。
// ウィンドウがフォーカスを失ったときに押しっぱなしを残さない。
func TestClearReleasesEverything(t *testing.T) {
	var s InputState
	s.Set(0, 0xFF)
	s.Set(1, 0xFF)
	s.Clear()
	for port := range input.PortCount {
		if got := s.Buttons(port); got != 0 {
			t.Errorf("ポート %d = %#08b, 期待 0", port, got)
		}
	}
}

// TestPortOutOfRangeIsIgnored は範囲外のポート番号を無視することを確かめる。
func TestPortOutOfRangeIsIgnored(t *testing.T) {
	var s InputState
	s.Set(-1, 0xFF)
	s.Set(input.PortCount, 0xFF)
	s.SetButton(5, input.ButtonA, true)
	if got := s.Buttons(-1); got != 0 {
		t.Errorf("範囲外のポートが %#08b を返した", got)
	}
	if got := s.Buttons(input.PortCount); got != 0 {
		t.Errorf("範囲外のポートが %#08b を返した", got)
	}
}
