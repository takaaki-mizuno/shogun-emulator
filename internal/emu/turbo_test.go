package emu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// TestTurboTogglesFromPressStart は連射が押し始めのフレームを基準に、
// 半周期ごとに押下と離しを入れ替えることを確かめる（設計書 07 編 §7.5）。
func TestTurboTogglesFromPressStart(t *testing.T) {
	var ts turboState
	ts.rates[0][0] = 15 // A を 15 Hz（半周期 2 フレーム）
	pressA := [input.PortCount]uint8{input.ButtonA | input.ButtonB, 0}
	var got []bool
	for f := uint64(100); f < 108; f++ {
		b := ts.apply(f, pressA)
		got = append(got, b[0]&input.ButtonA != 0)
		if b[0]&input.ButtonB == 0 {
			t.Fatal("連射していない B が消えた")
		}
	}
	want := []bool{true, true, false, false, true, true, false, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("A の並び = %v, 期待 %v", got, want)
		}
	}
	// 離してから押し直すと、押し始めから数え直す。
	ts.apply(200, [input.PortCount]uint8{})
	if b := ts.apply(203, pressA); b[0]&input.ButtonA == 0 {
		t.Error("押し直した直後のフレームで A が入っていない")
	}
	if turboHalfPeriod(30) != 1 || turboHalfPeriod(1) != 30 {
		t.Errorf("半周期 = %d, %d", turboHalfPeriod(30), turboHalfPeriod(1))
	}
}
