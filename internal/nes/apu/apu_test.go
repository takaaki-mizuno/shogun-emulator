package apu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// TestStepTogglesEvenCycle は Step が APU サイクルの位相を反転することを
// 確かめる。APU の各部品は 2 CPU サイクルごとに動くため位相が必要になる。
//
// 電源投入直後の位相を true とし、2 番目の CPU サイクルが APU サイクルに
// なるようにする。この位相でのみ `01.len_ctr` と `08.irq_timing` が
// 同時に合格する。
func TestStepTogglesEvenCycle(t *testing.T) {
	a := New(region.NTSC)
	a.PowerOn()

	if !a.EvenCycle() {
		t.Error("電源投入時の位相が false である")
	}
	a.Step()
	if a.EvenCycle() {
		t.Error("1 サイクル後に位相が反転していない")
	}
	a.Step()
	if !a.EvenCycle() {
		t.Error("2 サイクル後に位相が戻っていない")
	}
	if a.Cycles() != 2 {
		t.Errorf("サイクル数 = %d, 期待 2", a.Cycles())
	}
}

// TestMuteAffectsOnlyOutput はミュートが合成だけに効き、チャンネルの状態を
// 変えないことを確かめる（設計書 05 編 §5.9）。
func TestMuteAffectsOnlyOutput(t *testing.T) {
	a := New(region.NTSC)
	a.PowerOn()
	a.WriteRegister(0x4015, 0x01)
	a.WriteRegister(0x4000, 0x3F) // 一定音量 15
	a.WriteRegister(0x4002, 0x80)
	a.WriteRegister(0x4003, 0x08)
	for range 100 {
		a.Step()
	}
	before := a.Inspect()
	if before.Pulse[0].Volume != 15 || before.Pulse[0].Period != 0x080 {
		t.Fatalf("Pulse 1 の値 = %+v", before.Pulse[0])
	}
	b := New(region.NTSC)
	b.PowerOn()
	// Triangle は周期 0 でも 15 を出し続けるため、全チャンネルをミュートする。
	b.SetMute(MutePulse1 | MutePulse2 | MuteTriangle | MuteNoise | MuteDMC)
	b.WriteRegister(0x4015, 0x01)
	b.WriteRegister(0x4000, 0x3F)
	b.WriteRegister(0x4002, 0x80)
	b.WriteRegister(0x4003, 0x08)
	for range 100 {
		b.Step()
	}
	var loud, silent float32
	for range 100 {
		a.Step()
		b.Step()
		loud += a.mix()
		silent += b.mix()
	}
	if silent != 0 || loud == 0 {
		t.Errorf("合成の和 = ミュートなし %v, ミュートあり %v", loud, silent)
	}
	ai, bi := a.Inspect(), b.Inspect()
	ai.Mute, bi.Mute = 0, 0
	if ai != bi {
		t.Errorf("ミュートでチャンネルの状態が変わった\n%+v\n%+v", ai, bi)
	}
}
