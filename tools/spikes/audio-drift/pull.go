package main

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// 方式C: バックプレッシャ（プル）モデル
// オーディオデバイスの消費が唯一のクロック源。壁時計のティッカーを一切使わない。
// エミュレータはリングが高水位に達したらブロックし、Read が消費したら起きる。
type pullRing struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  []int16
	r, w int
	size int
	high int // 高水位（サンプル数）

	underrunSamples int64
	underrunEvents  int64
	inUnderrun      bool
	fillSum         int64
	fillN           int64
	fillMin         int
	fillMax         int
	closed          bool
}

func newPullRing(capSamples, highSamples int) *pullRing {
	r := &pullRing{buf: make([]int16, capSamples*channels), size: capSamples * channels,
		high: highSamples, fillMin: 1 << 30}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *pullRing) fillLocked() int {
	d := r.w - r.r
	if d < 0 {
		d += r.size
	}
	return d / channels
}

// エミュレータ側: 高水位を超えていたらブロックする（= オーディオに歩調を合わせる）
func (r *pullRing) writeBlocking(l, rr int16) bool {
	r.mu.Lock()
	for r.fillLocked() >= r.high && !r.closed {
		r.cond.Wait()
	}
	if r.closed {
		r.mu.Unlock()
		return false
	}
	next := (r.w + channels) % r.size
	r.buf[r.w], r.buf[r.w+1] = l, rr
	r.w = next
	r.mu.Unlock()
	return true
}

func (r *pullRing) Read(p []byte) (int, error) {
	r.mu.Lock()
	f := r.fillLocked()
	r.fillSum += int64(f)
	r.fillN++
	if f < r.fillMin {
		r.fillMin = f
	}
	if f > r.fillMax {
		r.fillMax = f
	}
	n := 0
	for n+3 < len(p) {
		if r.r == r.w {
			if !r.inUnderrun {
				r.underrunEvents++
				r.inUnderrun = true
			}
			r.underrunSamples++
			p[n], p[n+1], p[n+2], p[n+3] = 0, 0, 0, 0
			n += 4
			continue
		}
		r.inUnderrun = false
		l, rr := r.buf[r.r], r.buf[r.r+1]
		r.r = (r.r + channels) % r.size
		p[n], p[n+1] = byte(l), byte(l>>8)
		p[n+2], p[n+3] = byte(rr), byte(rr>>8)
		n += 4
	}
	r.cond.Broadcast() // 場所が空いたのでエミュレータを起こす
	r.mu.Unlock()
	return n, nil
}

func (r *pullRing) close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}

func runPull(name string, secs int, cpuWorkPerFrame int, highMul int) {
	target := sampleRate * targetMS / 1000
	high := target * highMul
	rb := newPullRing(high*2, high) // 高水位 = 目標バッファ量 × highMul
	player := ctx.NewPlayer(rb)
	player.SetBufferSize(sampleRate * bytesPerSample * targetMS / 1000)

	var producedFrames int64
	var produced int64
	stop := make(chan struct{})

	// プライミング: 再生開始前に高水位まで埋める（これが無いと起動時に 50ms の無音が出る）
	{
		ph := 0.0
		for i := 0; i < high; i++ {
			v := int16(math.Sin(ph) * 6000)
			ph += 2 * math.Pi * 440 / sampleRate
			rb.writeBlocking(v, v)
		}
	}

	go func() {
		ph := 0.0
		var accum float64
		samplesPerFrame := float64(sampleRate) / 60.0988
		for {
			select {
			case <-stop:
				return
			default:
			}
			// NES 1 フレーム分のエミュレーションを模擬（CPU 負荷）
			busyWork(cpuWorkPerFrame)
			accum += samplesPerFrame
			n := int(accum)
			accum -= float64(n)
			for i := 0; i < n; i++ {
				v := int16(math.Sin(ph) * 6000)
				ph += 2 * math.Pi * 440 / sampleRate
				if ph > 2*math.Pi {
					ph -= 2 * math.Pi
				}
				if !rb.writeBlocking(v, v) { // ← ここでオーディオに同期する
					return
				}
				atomic.AddInt64(&produced, 1)
			}
			atomic.AddInt64(&producedFrames, 1)
		}
	}()

	player.Play()
	t0 := time.Now()
	time.Sleep(time.Duration(secs) * time.Second)
	close(stop)
	rb.close()
	el := time.Since(t0)
	player.Close()

	rb.mu.Lock()
	ue, us := rb.underrunEvents, rb.underrunSamples
	avg := float64(rb.fillSum) / float64(max64(rb.fillN, 1))
	fmin, fmax := rb.fillMin, rb.fillMax
	rb.mu.Unlock()

	frames := atomic.LoadInt64(&producedFrames)
	fmt.Printf("%s\n", name)
	fmt.Printf("  生成レート  : %.1f samples/s（理想 %d）\n",
		float64(atomic.LoadInt64(&produced))/el.Seconds(), sampleRate)
	fmt.Printf("  エミュ fps  : %.3f（NES の実 fps は 60.0988）\n", float64(frames)/el.Seconds())
	fmt.Printf("  音切れ      : %d 回 / 合計 %d サンプル (%.1f ms)\n", ue, us, float64(us)/sampleRate*1000)
	fmt.Printf("  充填(高水位%d=目標%dx%d) : 平均 %.0f  最小 %d  最大 %d\n", high, target, highMul, avg, fmin, fmax)
	if ue == 0 {
		fmt.Printf("  判定: 合格（音切れゼロ）\n\n")
	} else {
		fmt.Printf("  判定: 不合格\n\n")
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var bwSink float64

func busyWork(iters int) {
	s := 0.0
	for i := 0; i < iters; i++ {
		s += math.Sqrt(float64(i&1023) + 1)
	}
	bwSink = s
}
