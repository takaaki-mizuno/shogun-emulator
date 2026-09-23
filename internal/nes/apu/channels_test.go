package apu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// TestTriangleSequence は 32 ステップの波形が調査文書の表と一致する
// ことを確かめる。
func TestTriangleSequence(t *testing.T) {
	want := []uint8{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0,
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	for i, w := range want {
		if triangleSequence[i] != w {
			t.Errorf("%d 番目 = %d, 期待 %d", i, triangleSequence[i], w)
		}
	}
}

// TestTriangleLinearCounterOrder はリニアカウンタのクロックが
// 2 つの手順をこの順で行うことを確かめる。
func TestTriangleLinearCounterOrder(t *testing.T) {
	var tri triangleChannel
	tri.length.setEnabled(true)
	tri.writeLinear(0x05) // control なし、リロード値 5
	tri.writeTimerHigh(0) // リロードフラグを立てる

	tri.clockLinear()
	if tri.linearCounter != 5 {
		t.Fatalf("リロード後 = %d, 期待 5", tri.linearCounter)
	}
	if tri.reloadFlag {
		t.Error("control が false なのにリロードフラグが残っている")
	}
	for want := 4; want >= 0; want-- {
		tri.clockLinear()
		if int(tri.linearCounter) != want {
			t.Fatalf("カウンタ = %d, 期待 %d", tri.linearCounter, want)
		}
	}
}

// TestTriangleControlKeepsReloading は control が立っている間、
// 毎回リロードされることを確かめる。これが halt の実体である。
func TestTriangleControlKeepsReloading(t *testing.T) {
	var tri triangleChannel
	tri.length.setEnabled(true)
	tri.writeLinear(0x87) // control あり、リロード値 7
	tri.writeTimerHigh(0)

	for range 10 {
		tri.clockLinear()
		if tri.linearCounter != 7 {
			t.Fatalf("カウンタ = %d, 期待 7（control 中は毎回リロード）", tri.linearCounter)
		}
	}
	if !tri.reloadFlag {
		t.Error("control 中にリロードフラグがクリアされた")
	}
}

// TestTriangleHoldsLastValue は停止中に最後の値を出し続けることを
// 確かめる。0 にはしない。
func TestTriangleHoldsLastValue(t *testing.T) {
	var tri triangleChannel
	tri.length.setEnabled(true)
	tri.length.load(0)
	tri.writeLinear(0x7F) // control なし、リロード値 127
	tri.writeTimerHigh(0)
	tri.clockLinear()
	tri.timer.setPeriod(1)

	for range 20 {
		tri.stepTimer()
	}
	stopped := tri.output()

	// リニアカウンタを 0 にして止める
	tri.linearCounter = 0
	for range 100 {
		tri.stepTimer()
	}
	if tri.output() != stopped {
		t.Errorf("停止後の出力 = %d, 期待 %d（最後の値を保つ）", tri.output(), stopped)
	}
}

// TestTriangleSilenceUltrasonic は設定で超音波域を止められることを
// 確かめる。
func TestTriangleSilenceUltrasonic(t *testing.T) {
	for _, silence := range []bool{false, true} {
		var tri triangleChannel
		tri.silenceUltrasonic = silence
		tri.length.setEnabled(true)
		tri.length.load(0)
		tri.linearCounter = 100
		tri.timer.setPeriod(1) // 超音波域

		before := tri.seqPos
		for range 100 {
			tri.stepTimer()
		}
		moved := tri.seqPos != before
		if silence && moved {
			t.Error("止める設定なのにシーケンサが進んだ")
		}
		if !silence && !moved {
			t.Error("止めない設定なのにシーケンサが進まない")
		}
	}
}

// TestNoiseLFSRPeriod は LFSR の周期がモードによって変わることを
// 確かめる。
//
// モード 0 は 32767 ステップ、モード 1 は 93 ステップになる（初期値 1
// から始めたとき）。
func TestNoiseLFSRPeriod(t *testing.T) {
	tests := []struct {
		mode bool
		want int
	}{
		{false, 32767},
		{true, 93},
	}
	for _, tt := range tests {
		n := newNoiseChannel(region.NTSC)
		n.mode = tt.mode

		steps := 0
		for {
			n.clockLFSR()
			steps++
			if n.lfsr == 1 {
				break
			}
			if steps > 40000 {
				t.Fatalf("モード %v: 周期が 40000 を超えた", tt.mode)
			}
		}
		if steps != tt.want {
			t.Errorf("モード %v の周期 = %d, 期待 %d", tt.mode, steps, tt.want)
		}
	}
}

// TestNoisePeriodFromTable は周期が表の値の CPU サイクル数になることを
// 確かめる。
func TestNoisePeriodFromTable(t *testing.T) {
	n := newNoiseChannel(region.NTSC)
	n.writeMode(0x00) // 表の先頭は 4 CPU サイクル
	n.timer.reload()

	before := n.lfsr
	for range 3 {
		n.stepTimer()
	}
	if n.lfsr != before {
		t.Error("3 サイクルでシフトレジスタが動いた")
	}
	n.stepTimer()
	if n.lfsr == before {
		t.Error("4 サイクルでシフトレジスタが動かない")
	}
}

// TestNoiseOutputConditions は出力が 0 になる条件を確かめる。
func TestNoiseOutputConditions(t *testing.T) {
	n := newNoiseChannel(region.NTSC)
	n.env.write(0x1F) // 定音量 15
	n.length.setEnabled(true)
	n.length.load(0)

	n.lfsr = 1 // bit 0 が 1
	if got := n.output(); got != 0 {
		t.Errorf("bit 0 が 1 のとき出力 = %d, 期待 0", got)
	}
	n.lfsr = 2
	if got := n.output(); got != 15 {
		t.Errorf("bit 0 が 0 のとき出力 = %d, 期待 15", got)
	}
	n.length.value = 0
	if got := n.output(); got != 0 {
		t.Errorf("レングス 0 のとき出力 = %d, 期待 0", got)
	}
}

// TestDMCOutputLevelClamps は出力レベルが 0-127 を出ないことを確かめる。
func TestDMCOutputLevelClamps(t *testing.T) {
	d := newDMCChannel(region.NTSC)
	d.silence = false
	d.bitsRemaining = 8

	// 上限。bit 0 が 1 なら +2 だが 126 以上では変えない。
	d.outputLevel = 126
	d.shiftReg = 0xFF
	d.clockOutput()
	if d.outputLevel != 126 {
		t.Errorf("126 から %d へ変わった", d.outputLevel)
	}

	// 下限。bit 0 が 0 なら -2 だが 1 以下では変えない。
	d.outputLevel = 1
	d.shiftReg = 0x00
	d.bitsRemaining = 8
	d.clockOutput()
	if d.outputLevel != 1 {
		t.Errorf("1 から %d へ変わった", d.outputLevel)
	}
}

// TestDMCShiftsRight はシフトレジスタが右へ進むことを確かめる。
// 各バイトの最下位ビットから再生される。
func TestDMCShiftsRight(t *testing.T) {
	d := newDMCChannel(region.NTSC)
	d.silence = false
	d.bitsRemaining = 8
	d.outputLevel = 64
	d.shiftReg = 0x01 // 最下位が 1、残りが 0

	d.clockOutput()
	if d.outputLevel != 66 {
		t.Errorf("最初のビットで %d になった。期待 66", d.outputLevel)
	}
	d.clockOutput()
	if d.outputLevel != 64 {
		t.Errorf("2 番目のビットで %d になった。期待 64", d.outputLevel)
	}
}

// TestDMCSampleAddressAndLength はアドレスと長さの計算を確かめる。
func TestDMCSampleAddressAndLength(t *testing.T) {
	d := newDMCChannel(region.NTSC)
	d.writeAddr(0x00)
	if d.sampleAddr != 0xC000 {
		t.Errorf("アドレス = %#04x, 期待 $C000", d.sampleAddr)
	}
	d.writeAddr(0xFF)
	if want := uint16(0xC000 + 255*64); d.sampleAddr != want {
		t.Errorf("アドレス = %#04x, 期待 %#04x", d.sampleAddr, want)
	}

	d.writeLength(0x00)
	if d.sampleLength != 1 {
		t.Errorf("長さ = %d, 期待 1", d.sampleLength)
	}
	d.writeLength(0xFF)
	if want := uint16(255*16 + 1); d.sampleLength != want {
		t.Errorf("長さ = %d, 期待 %d", d.sampleLength, want)
	}
}

// TestDMCSilenceDoesNotChangeLevel は silence 中に出力レベルが
// 変わらないことを確かめる。
func TestDMCSilenceDoesNotChangeLevel(t *testing.T) {
	d := newDMCChannel(region.NTSC)
	d.silence = true
	d.bitsRemaining = 8
	d.outputLevel = 64
	d.shiftReg = 0xFF

	for range 4 {
		d.clockOutput()
	}
	if d.outputLevel != 64 {
		t.Errorf("silence 中に %d へ変わった", d.outputLevel)
	}
}
