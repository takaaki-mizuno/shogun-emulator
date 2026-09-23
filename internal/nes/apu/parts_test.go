package apu

import "testing"

// TestDividerPeriodIsPlusOne は分周器が P+1 回に 1 度クロックを出すことを
// 確かめる。周期の数え方を間違えると全チャンネルの音程がずれる。
func TestDividerPeriodIsPlusOne(t *testing.T) {
	for period := range uint16(8) {
		d := divider{period: period}
		d.reload()

		clocks := 0
		const steps = 100
		for range steps {
			if d.clock() {
				clocks++
			}
		}
		// 最初のリロード済みの状態から数えるため、出力は
		// おおむね steps / (period+1) 回になる。
		want := steps / int(period+1)
		if clocks != want {
			t.Errorf("周期 %d: %d 回クロックした（期待 %d 回）", period, clocks, want)
		}
	}
}

// TestDividerClocksImmediatelyAtZero はカウンタが 0 のときのクロックで
// 出力が出て、カウンタが周期に戻ることを確かめる。
func TestDividerClocksImmediatelyAtZero(t *testing.T) {
	d := divider{period: 3}
	if !d.clock() {
		t.Fatal("カウンタ 0 でクロックが出ない")
	}
	if d.counter != 3 {
		t.Errorf("リロード後のカウンタ = %d, 期待 3", d.counter)
	}
	for i := range 3 {
		if d.clock() {
			t.Errorf("%d 回目で余分なクロックが出た", i+1)
		}
	}
	if !d.clock() {
		t.Error("4 回目でクロックが出ない")
	}
}

// TestDividerSetPeriodKeepsCounter は周期の変更でカウンタが変わらない
// ことを確かめる。ビブラートで位相がずれないための挙動である。
func TestDividerSetPeriodKeepsCounter(t *testing.T) {
	d := divider{period: 10}
	d.reload()
	d.clock()
	before := d.counter
	d.setPeriod(200)
	if d.counter != before {
		t.Errorf("カウンタが %d から %d へ変わった", before, d.counter)
	}
}

// TestLengthTableValues はレングステーブルが調査文書の表と一致することを
// 確かめる。
func TestLengthTableValues(t *testing.T) {
	tests := []struct {
		index uint8
		want  uint8
	}{
		{0x00, 10}, {0x01, 254}, {0x02, 20}, {0x03, 2},
		{0x0F, 14}, {0x10, 12}, {0x11, 16}, {0x18, 192},
		{0x1F, 30},
	}
	for _, tt := range tests {
		if got := lengthTable[tt.index]; got != tt.want {
			t.Errorf("lengthTable[%#02x] = %d, 期待 %d", tt.index, got, tt.want)
		}
	}
}

// TestLengthCounterLoadRequiresEnabled は無効なチャンネルへのロードが
// 効かないことを確かめる。
func TestLengthCounterLoadRequiresEnabled(t *testing.T) {
	var l lengthCounter
	l.load(0x00)
	if l.value != 0 {
		t.Errorf("無効なのにロードされた: %d", l.value)
	}

	l.setEnabled(true)
	l.load(0x00)
	if l.value != 10 {
		t.Errorf("ロード後の値 = %d, 期待 10", l.value)
	}

	l.setEnabled(false)
	if l.value != 0 {
		t.Errorf("無効にしたのに値が %d である", l.value)
	}
	// 無効のあいだは有効にしても値は戻らない
	l.setEnabled(true)
	if l.value != 0 {
		t.Errorf("有効にしただけで値が %d になった", l.value)
	}
}

// TestLengthCounterHalt は halt 中に減らないことを確かめる。
func TestLengthCounterHalt(t *testing.T) {
	var l lengthCounter
	l.setEnabled(true)
	l.load(0x03) // 2

	l.halt = true
	for range 10 {
		l.clock()
	}
	if l.value != 2 {
		t.Errorf("halt 中に %d まで減った", l.value)
	}

	l.halt = false
	l.clock()
	l.clock()
	if l.value != 0 {
		t.Errorf("2 回のクロック後の値 = %d, 期待 0", l.value)
	}
	l.clock()
	if l.value != 0 {
		t.Errorf("0 から further 減った: %d", l.value)
	}
}

// TestEnvelopeDecays はエンベロープが 15 から 0 へ減衰することを確かめる。
func TestEnvelopeDecays(t *testing.T) {
	var e envelope
	e.write(0x00) // 周期 0、ループ無し、定音量でない
	e.restart()

	e.clock() // start の処理。decay = 15、分周器リロード
	if e.decayLevel != 15 {
		t.Fatalf("再スタート後の decay = %d, 期待 15", e.decayLevel)
	}
	if e.volume() != 15 {
		t.Errorf("音量 = %d, 期待 15", e.volume())
	}

	// 周期 0 なので 1 クロックごとに 1 段減る
	for want := 14; want >= 0; want-- {
		e.clock()
		if int(e.decayLevel) != want {
			t.Fatalf("decay = %d, 期待 %d", e.decayLevel, want)
		}
	}
	// 0 で止まる（ループ無し）
	e.clock()
	if e.decayLevel != 0 {
		t.Errorf("0 の後に %d になった", e.decayLevel)
	}
}

// TestEnvelopeLoop はループ時に 0 の次が 15 に戻ることを確かめる。
func TestEnvelopeLoop(t *testing.T) {
	var e envelope
	e.write(0x20) // ループ
	e.restart()
	e.clock()
	for range 15 {
		e.clock()
	}
	if e.decayLevel != 0 {
		t.Fatalf("decay = %d, 期待 0", e.decayLevel)
	}
	e.clock()
	if e.decayLevel != 15 {
		t.Errorf("ループ後の decay = %d, 期待 15", e.decayLevel)
	}
}

// TestEnvelopePeriod は分周器の周期が V+1 quarter frame であることを
// 確かめる。
func TestEnvelopePeriod(t *testing.T) {
	var e envelope
	e.write(0x03) // V = 3。周期 4
	e.restart()
	e.clock() // start

	// 4 回目のクロックで初めて 1 段減る
	for i := range 3 {
		e.clock()
		if e.decayLevel != 15 {
			t.Fatalf("%d 回目で decay が %d になった", i+1, e.decayLevel)
		}
	}
	e.clock()
	if e.decayLevel != 14 {
		t.Errorf("4 回目の decay = %d, 期待 14", e.decayLevel)
	}
}

// TestEnvelopeConstantKeepsDecaying は定音量モードでも減衰が進むことを
// 確かめる。定音量フラグは出力の選択だけを行う。
func TestEnvelopeConstantKeepsDecaying(t *testing.T) {
	var e envelope
	e.write(0x15) // 定音量、V = 5
	e.restart()
	e.clock()
	if e.volume() != 5 {
		t.Errorf("定音量の音量 = %d, 期待 5", e.volume())
	}

	before := e.decayLevel
	for range 6 {
		e.clock()
	}
	if e.decayLevel == before {
		t.Error("定音量モードで decay が更新されていない")
	}
	if e.volume() != 5 {
		t.Errorf("定音量なのに音量が %d に変わった", e.volume())
	}
}

// TestSweepWriteSetsReload は $4001 の書き込みが reload フラグを
// 立てることを確かめる。
func TestSweepWriteSetsReload(t *testing.T) {
	var s sweep
	s.write(0x8A) // 有効、周期 0、negate、シフト 2
	if !s.enabled || !s.negate || s.shift != 2 || !s.reload {
		t.Errorf("書き込みの反映が誤っている: %+v", s)
	}
	if s.div.period != 0 {
		t.Errorf("分周器の周期 = %d, 期待 0", s.div.period)
	}

	s.write(0x70) // 無効、周期 7、シフト 0
	if s.enabled || s.div.period != 7 {
		t.Errorf("書き込みの反映が誤っている: %+v", s)
	}
}
