package main

import (
	"fmt"
	"testing"
	"time"
)

// 原因の切り分け: time.Ticker はそもそも 60.0988 Hz を刻めるのか？
func TestTickerAccuracy(t *testing.T) {
	for _, hz := range []float64{60.0988, 60.0, 240.0} {
		ns := 1.0e9 / hz
		d := time.Duration(ns) * time.Nanosecond
		n := 0
		tk := time.NewTicker(d)
		t0 := time.Now()
		for range tk.C {
			n++
			if time.Since(t0) > 5*time.Second {
				break
			}
		}
		tk.Stop()
		el := time.Since(t0)
		actual := float64(n) / el.Seconds()
		fmt.Printf("要求 %.4f Hz (%v/tick) → 実測 %.4f Hz  誤差 %+.3f%%\n",
			hz, d, actual, (actual/hz-1)*100)
	}
}

// Sleep ベースはどうか
func TestSleepAccuracy(t *testing.T) {
	ns := 1.0e9 / 60.0988
	d := time.Duration(ns) * time.Nanosecond
	n := 0
	t0 := time.Now()
	for time.Since(t0) < 5*time.Second {
		time.Sleep(d)
		n++
	}
	el := time.Since(t0)
	fmt.Printf("time.Sleep %v × %d → 実測 %.4f Hz  誤差 %+.3f%%\n",
		d, n, float64(n)/el.Seconds(), (float64(n)/el.Seconds()/60.0988-1)*100)
}
