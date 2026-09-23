package apu

import "testing"

// TestDutySequenceOutputOrder はデューティごとの出力波形が調査文書の
// 表と一致することを確かめる。
//
// 実機のカウンタは下向きに数えるため、内部の表をそのまま並べても
// 出力波形にはならない。
func TestDutySequenceOutputOrder(t *testing.T) {
	want := [4][8]uint8{
		{0, 1, 0, 0, 0, 0, 0, 0}, // 12.5%
		{0, 1, 1, 0, 0, 0, 0, 0}, // 25%
		{0, 1, 1, 1, 1, 0, 0, 0}, // 50%
		{1, 0, 0, 1, 1, 1, 1, 1}, // 25% 反転
	}
	for duty := range 4 {
		p := newPulseChannel(true)
		p.duty = uint8(duty)
		p.length.setEnabled(true)
		p.length.load(0)
		p.timer.setPeriod(8) // ミュートされない最小の周期
		p.env.write(0x1F)    // 定音量 15

		var got [8]uint8
		for i := range 8 {
			if p.output() != 0 {
				got[i] = 1
			}
			// 周期 8 なので 9 回クロックでシーケンサが 1 段進む
			for range 9 {
				p.stepTimer()
			}
		}
		if got != want[duty] {
			t.Errorf("デューティ %d: 出力 %v, 期待 %v", duty, got, want[duty])
		}
	}
}

// TestPulseSweepComplement は Pulse 1 と Pulse 2 で目標周期の計算が
// 違うことを確かめる。両者の唯一の違いがこれである。
func TestPulseSweepComplement(t *testing.T) {
	// 周期 20、シフト 0、negate。変化量は Pulse 1 が -21、Pulse 2 が -20。
	p1 := newPulseChannel(true)
	p1.timer.setPeriod(20)
	p1.swp.negate = true
	p1.swp.shift = 0
	if got := p1.targetPeriod(); got != 20-21+1-1 {
		// 20 + (-20 - 1) = -1 → 0 にクランプ
		if got != 0 {
			t.Errorf("Pulse 1 の目標周期 = %d, 期待 0（負をクランプ）", got)
		}
	}

	p1.timer.setPeriod(100)
	if got := p1.targetPeriod(); got != 100-101 {
		if got != 0 {
			t.Errorf("Pulse 1 の目標周期 = %d", got)
		}
	}

	p2 := newPulseChannel(false)
	p2.timer.setPeriod(100)
	p2.swp.negate = true
	p2.swp.shift = 0
	if got := p2.targetPeriod(); got != 0 {
		t.Errorf("Pulse 2 の目標周期 = %d, 期待 0", got)
	}

	// シフト 1 で違いが見える。100 >> 1 = 50。
	p1.swp.shift = 1
	p2.swp.shift = 1
	if got, want := p1.targetPeriod(), 100-50-1; got != want {
		t.Errorf("Pulse 1 の目標周期 = %d, 期待 %d（1 の補数）", got, want)
	}
	if got, want := p2.targetPeriod(), 100-50; got != want {
		t.Errorf("Pulse 2 の目標周期 = %d, 期待 %d（2 の補数）", got, want)
	}
}

// TestPulseMuteConditions はミュートの条件を確かめる。
//
// スイープが無効でもオーバーフローによる消音が起こる。これが多くの
// ゲームが Pulse の最低オクターブを使わない理由である。
func TestPulseMuteConditions(t *testing.T) {
	p := newPulseChannel(true)
	p.length.setEnabled(true)
	p.length.load(0)
	p.env.write(0x1F)

	// 周期 8 未満はミュート
	p.timer.setPeriod(7)
	if !p.muted() {
		t.Error("周期 7 でミュートされていない")
	}
	p.timer.setPeriod(8)
	if p.muted() {
		t.Error("周期 8 でミュートされている")
	}

	// negate が false、シフト 0、周期 $400 以上で目標周期が $7FF を超える
	p.swp.enabled = false
	p.swp.negate = false
	p.swp.shift = 0
	p.timer.setPeriod(0x400)
	if !p.muted() {
		t.Error("周期 $400 でミュートされていない（スイープ無効でも起こる）")
	}
	if got := p.targetPeriod(); got != 0x800 {
		t.Errorf("目標周期 = %#x, 期待 $800", got)
	}
}

// TestPulseTimerHighResetsSequencerNotDivider は $4003 の書き込みが
// シーケンサを戻し、タイマーの分周器を戻さないことを確かめる。
//
// 分周器まで戻すと、ビブラートでタイマー上位を書くたびに位相がずれる。
func TestPulseTimerHighResetsSequencerNotDivider(t *testing.T) {
	p := newPulseChannel(true)
	p.length.setEnabled(true)
	p.timer.setPeriod(100)
	p.timer.reload()

	for range 40 {
		p.stepTimer()
	}
	p.seqPos = 3
	counter := p.timer.counter

	p.writeTimerHigh(0x00)
	if p.seqPos != 0 {
		t.Errorf("シーケンサ位置 = %d, 期待 0", p.seqPos)
	}
	if p.timer.counter != counter {
		t.Errorf("分周器のカウンタが %d から %d へ変わった", counter, p.timer.counter)
	}
	if !p.env.start {
		t.Error("エンベロープが再スタートしていない")
	}
}

// TestPulseSweepUpdatesPeriod は half frame クロックで周期が
// 目標周期へ移ることを確かめる。
//
// 分周器の周期が 0 のときは half frame ごとに更新される。シフト 1 の
// 減算では周期が毎回半分になる。
func TestPulseSweepUpdatesPeriod(t *testing.T) {
	p := newPulseChannel(false)
	p.timer.setPeriod(0x100)
	p.swp.write(0x89) // 有効、分周器の周期 0、negate、シフト 1

	p.clockSweep()
	if p.timer.period != 0x80 {
		t.Errorf("1 回目の周期 = %#x, 期待 $80", p.timer.period)
	}
	p.clockSweep()
	if p.timer.period != 0x40 {
		t.Errorf("2 回目の周期 = %#x, 期待 $40", p.timer.period)
	}
}

// TestPulseSweepDividerPeriod は分周器の周期だけ待ってから周期が
// 更新されることを確かめる。
func TestPulseSweepDividerPeriod(t *testing.T) {
	p := newPulseChannel(false)
	p.timer.setPeriod(0x100)
	p.swp.write(0xA9) // 有効、分周器の周期 2、negate、シフト 1

	// reload が立っているため、1 回目はカウンタを載せ直すだけ。
	p.clockSweep()
	if p.timer.period != 0x80 {
		t.Errorf("1 回目の周期 = %#x, 期待 $80", p.timer.period)
	}
	// 周期 2 なので、次の更新までに 3 回のクロックが要る。
	for i := range 2 {
		p.clockSweep()
		if p.timer.period != 0x80 {
			t.Fatalf("%d 回目で周期が %#x へ変わった", i+2, p.timer.period)
		}
	}
	p.clockSweep()
	if p.timer.period != 0x40 {
		t.Errorf("4 回目の周期 = %#x, 期待 $40", p.timer.period)
	}
}

// TestPulseSweepDoesNotUpdateWhenMuted はミュート中に周期が
// 変わらないことを確かめる。
func TestPulseSweepDoesNotUpdateWhenMuted(t *testing.T) {
	p := newPulseChannel(false)
	p.timer.setPeriod(4) // 8 未満でミュート
	p.swp.write(0x81)    // 有効、シフト 1、加算
	p.clockSweep()
	p.clockSweep()
	if p.timer.period != 4 {
		t.Errorf("ミュート中に周期が %d へ変わった", p.timer.period)
	}
}
