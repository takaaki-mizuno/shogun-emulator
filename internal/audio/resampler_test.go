package audio

import (
	"math"
	"testing"
)

// collectSink は書かれたサンプルを集める。
type collectSink struct{ out []int16 }

func (s *collectSink) WriteSample(v int16) bool {
	s.out = append(s.out, v)
	return true
}

// cpuRate は NTSC の CPU クロック。リサンプラの入力レートである。
const cpuRate = 1789773.0

// TestResamplerOutputRate は入力の数に対して出力の数が比で決まることを
// 確かめる。
func TestResamplerOutputRate(t *testing.T) {
	s := &collectSink{}
	r := NewResampler(cpuRate, SampleRate, ProfileNone, s)

	const inputs = int(cpuRate) // 1 秒ぶん
	for range inputs {
		r.WriteSample(0)
	}

	want := SampleRate
	diff := len(s.out) - want
	if diff < -2 || diff > 2 {
		t.Errorf("出力数 = %d, 期待 %d 付近", len(s.out), want)
	}
}

// TestResamplerSpeedChangesOutputRate は速度倍率が出力数に反映される
// ことを確かめる。倍速では 1 秒ぶんの入力から半分の出力が出る。
func TestResamplerSpeedChangesOutputRate(t *testing.T) {
	s := &collectSink{}
	r := NewResampler(cpuRate, SampleRate, ProfileNone, s)
	r.SetSpeed(2)

	for range int(cpuRate) {
		r.WriteSample(0)
	}
	want := SampleRate / 2
	if d := len(s.out) - want; d < -2 || d > 2 {
		t.Errorf("2 倍速の出力数 = %d, 期待 %d 付近", len(s.out), want)
	}
}

// TestResamplerMutedOutputsZero は消音時に 0 が出ることを確かめる。
func TestResamplerMutedOutputsZero(t *testing.T) {
	s := &collectSink{}
	r := NewResampler(cpuRate, SampleRate, ProfileNone, s)
	r.SetMuted(true)

	for range 10000 {
		r.WriteSample(0.5)
	}
	if len(s.out) == 0 {
		t.Fatal("出力が無い")
	}
	for i, v := range s.out {
		if v != 0 {
			t.Fatalf("%d 個目が %d である。消音を期待", i, v)
		}
	}
}

// TestHighPassRemovesDC はハイパスが直流成分を落とすことを確かめる。
//
// APU の出力は 0 を中心にしない。落とさないと波形が片側に寄り、
// 振幅を使い切れない。
func TestHighPassRemovesDC(t *testing.T) {
	f := newHighPass(90, cpuRate)
	var out float32
	for range int(cpuRate) / 10 { // 0.1 秒ぶん
		out = f.process(0.5)
	}
	if math.Abs(float64(out)) > 0.01 {
		t.Errorf("直流を入れ続けた出力 = %v, 期待 0 付近", out)
	}
}

// TestLowPassPassesDC はローパスが直流成分を通すことを確かめる。
func TestLowPassPassesDC(t *testing.T) {
	f := newLowPass(14000, cpuRate)
	var out float32
	for range int(cpuRate) / 10 {
		out = f.process(0.5)
	}
	if math.Abs(float64(out)-0.5) > 0.01 {
		t.Errorf("直流を入れ続けた出力 = %v, 期待 0.5 付近", out)
	}
}

// TestLowPassAttenuatesHighFrequency はローパスが高い周波数を
// 減衰させることを確かめる。ダウンサンプルの折り返しを防ぐ。
func TestLowPassAttenuatesHighFrequency(t *testing.T) {
	f := newLowPass(14000, cpuRate)
	// 入力レートの半分の周波数（交互に +1 と -1）
	var peak float32
	for i := range 1000 {
		v := float32(1)
		if i%2 == 1 {
			v = -1
		}
		out := f.process(v)
		if a := float32(math.Abs(float64(out))); a > peak {
			peak = a
		}
	}
	if peak > 0.2 {
		t.Errorf("高い周波数の振幅が %v 残っている", peak)
	}
}

// TestResamplerResetClearsState はリセットで状態が消えることを確かめる。
//
// セーブステートを読み込んだ直後に前の内容から続けると、不連続が
// クリック音になる。
func TestResamplerResetClearsState(t *testing.T) {
	s := &collectSink{}
	r := NewResampler(cpuRate, SampleRate, ProfileNES, s)
	for range 1000 {
		r.WriteSample(1)
	}
	r.Reset()
	if r.accum != 0 || r.prev != 0 {
		t.Errorf("リセット後に accum = %v, prev = %v", r.accum, r.prev)
	}
	if r.hp1.prevOut != 0 || r.hp2.prevOut != 0 || r.lp.prevOut != 0 {
		t.Error("フィルタの状態が残っている")
	}
}

// TestToInt16Clamps は範囲を超える値が飽和することを確かめる。
func TestToInt16Clamps(t *testing.T) {
	if got := toInt16(100); got != 32767 {
		t.Errorf("大きい値 = %d, 期待 32767", got)
	}
	if got := toInt16(-100); got != -32768 {
		t.Errorf("小さい値 = %d, 期待 -32768", got)
	}
	if got := toInt16(0); got != 0 {
		t.Errorf("0 = %d", got)
	}
}
