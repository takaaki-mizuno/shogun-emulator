package emu

import (
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// TestFramePeriodMatchesRegion はフレーム周期がリージョンの
// フレームレートと一致することを確かめる。
func TestFramePeriodMatchesRegion(t *testing.T) {
	tests := []struct {
		r    *region.Region
		want time.Duration
	}{
		{region.NTSC, 16_639 * time.Microsecond},
		{region.PAL, 19_997 * time.Microsecond},
		{region.Dendy, 19_997 * time.Microsecond},
	}
	for _, tt := range tests {
		got := framePeriod(tt.r)
		diff := got - tt.want
		if diff < 0 {
			diff = -diff
		}
		if diff > 10*time.Microsecond {
			t.Errorf("%s のフレーム周期 = %v, 期待 %v 付近", tt.r.Name, got, tt.want)
		}
	}
}

// TestWallClockPacerWaits は待ちがフレーム周期に近いことを確かめる。
func TestWallClockPacerWaits(t *testing.T) {
	p := NewWallClockPacer(region.NTSC)
	p.WaitFrame(1) // 1 回目は基準を作るだけで待たない

	start := time.Now()
	for range 3 {
		p.WaitFrame(1)
	}
	elapsed := time.Since(start)
	want := 3 * framePeriod(region.NTSC)
	if elapsed < want*8/10 {
		t.Errorf("3 フレームの待ちが %v しかない。期待 %v 付近", elapsed, want)
	}
	if elapsed > want*3 {
		t.Errorf("3 フレームの待ちが %v もある。期待 %v 付近", elapsed, want)
	}
}

// TestWallClockPacerSpeed は速度倍率が待ち時間に反映されることを確かめる。
func TestWallClockPacerSpeed(t *testing.T) {
	p := NewWallClockPacer(region.NTSC)
	p.WaitFrame(2)

	start := time.Now()
	for range 4 {
		p.WaitFrame(2)
	}
	elapsed := time.Since(start)
	want := 2 * framePeriod(region.NTSC) // 4 フレームを 2 倍速で
	if elapsed > want*3 {
		t.Errorf("2 倍速の 4 フレームで %v 待った。期待 %v 付近", elapsed, want)
	}
}

// TestWallClockPacerDoesNotWaitAtHighSpeed は高い速度倍率では待たない
// ことを確かめる。
func TestWallClockPacerDoesNotWaitAtHighSpeed(t *testing.T) {
	p := NewWallClockPacer(region.NTSC)
	start := time.Now()
	for range 100 {
		p.WaitFrame(UncappedSpeed)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("100 フレームで %v 待った。待たないことを期待", elapsed)
	}
}

// TestWallClockPacerDoesNotCatchUp は大きく遅れた後に早送りに
// ならないことを確かめる。
//
// 遅れを取り戻そうとして待ちを省き続けると、処理が重い瞬間の後に
// 画面が早送りになる。
func TestWallClockPacerDoesNotCatchUp(t *testing.T) {
	period := framePeriod(region.NTSC)
	p := NewWallClockPacer(region.NTSC)
	p.WaitFrame(1)

	// フレームの処理が 5 フレーム分かかったことにする
	time.Sleep(5 * period)
	p.WaitFrame(1)

	start := time.Now()
	p.WaitFrame(1)
	if elapsed := time.Since(start); elapsed < period*8/10 {
		t.Errorf("遅れの後の待ちが %v しかない。期待 %v 付近", elapsed, period)
	}
}

// TestNoPacerDoesNotWait は待たない Pacer が待たないことを確かめる。
func TestNoPacerDoesNotWait(t *testing.T) {
	p := NewNoPacer()
	p.Reset()
	start := time.Now()
	for range 1000 {
		p.WaitFrame(1)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("1000 フレームで %v 待った", elapsed)
	}
}
