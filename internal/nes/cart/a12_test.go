package cart

import "testing"

// TestA12FilterShortLowIsIgnored は low が短いとき立ち上がりとして
// 数えないことを確かめる。
//
// スプライトのパターンフェッチの間には 4 ドットの low が現れる。
// これを数えると走査線あたりの立ち上がりが増え、IRQ が早まる。
func TestA12FilterShortLowIsIgnored(t *testing.T) {
	var f a12Filter
	f.notify(0x1000, 0) // high から始める
	f.notify(0x0000, 2)
	f.notify(0x0000, 2)
	if f.notify(0x1000, 2) {
		t.Errorf("low が %d ドットで立ち上がりと数えた", 4)
	}
}

// TestA12FilterLongLowRises は low が十分に続いたときだけ立ち上がりを
// 数えることを確かめる。
func TestA12FilterLongLowRises(t *testing.T) {
	for _, tt := range []struct {
		dots int
		want bool
	}{
		{a12LowDotsRequired - 1, false},
		{a12LowDotsRequired, true},
		{a12LowDotsRequired + 100, true},
	} {
		var f a12Filter
		f.notify(0x1000, 0)
		f.notify(0x0000, tt.dots)
		if got := f.notify(0x1000, 1); got != tt.want {
			t.Errorf("low %d ドットで rising = %v, 期待 %v", tt.dots, got, tt.want)
		}
	}
}

// TestA12FilterHighStaysHigh は high が続く間は数えないことを確かめる。
func TestA12FilterHighStaysHigh(t *testing.T) {
	var f a12Filter
	f.notify(0x0000, 100)
	if !f.notify(0x1000, 1) {
		t.Fatal("最初の立ち上がりを数えていない")
	}
	for i := range 4 {
		if f.notify(0x1FFF, 100) {
			t.Errorf("high が続いている間に %d 回目の立ち上がりを数えた", i+1)
		}
	}
}

// TestA12FilterResetsOnHigh は立ち上がりと認めなかった low の時間が
// 積み上がらないことを確かめる。
func TestA12FilterResetsOnHigh(t *testing.T) {
	var f a12Filter
	f.notify(0x1000, 0)
	// 短い low と high を繰り返す。
	for range 10 {
		f.notify(0x0000, 4)
		if f.notify(0x1000, 1) {
			t.Fatal("短い low の繰り返しで立ち上がりを数えた")
		}
	}
}
