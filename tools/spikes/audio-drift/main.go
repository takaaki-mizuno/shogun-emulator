// ドリフト実験: 「1フレーム = 固定サンプル数」方式が本当に壊れるかを再現し、
// 充填率フィードバック制御で直るかを測る。
package main

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
)

const (
	sampleRate     = 48000
	channels       = 2
	bytesPerSample = 2 * channels
	targetMS       = 50 // 目標バッファ 50ms
)

type ring struct {
	mu   sync.Mutex
	buf  []int16
	r, w int
	size int

	underrunSamples int64 // 音切れした（無音を出した）サンプル数
	underrunEvents  int64 // 音切れが「発生した回数」= プチッと鳴る回数
	overflowDrops   int64 // バッファ満杯で捨てたサンプル数
	inUnderrun      bool
}

func newRing(samples int) *ring {
	return &ring{buf: make([]int16, samples*channels), size: samples * channels}
}

func (r *ring) fillLocked() int {
	d := r.w - r.r
	if d < 0 {
		d += r.size
	}
	return d / channels // サンプル単位
}

func (r *ring) Fill() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fillLocked()
}

func (r *ring) write(l, rr int16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := (r.w + channels) % r.size
	if next == r.r {
		r.overflowDrops++
		return
	}
	r.buf[r.w] = l
	r.buf[r.w+1] = rr
	r.w = next
}

func (r *ring) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	return n, nil
}

var ctx *oto.Context

func main() {
	c, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate: sampleRate, ChannelCount: channels, Format: oto.FormatSignedInt16LE,
	})
	if err != nil {
		fmt.Println("NewContext:", err)
		return
	}
	<-ready
	ctx = c

	secs := 60
	fmt.Printf("目標バッファ %d ms / 各方式 %d 秒間\n\n", targetMS, secs)
	runPull("D1: 重負荷・高水位 = 目標x1（スラックなし）", secs, 200000, 1)
	runPull("D2: 重負荷・高水位 = 目標x2", secs, 200000, 2)
	runPull("D3: 重負荷・高水位 = 目標x3", secs, 200000, 3)
}

func run(name string, secs int, feedback bool) {
	rb := newRing(sampleRate * targetMS * 6 / 1000) // 目標の 6 倍のリング
	player := ctx.NewPlayer(rb)
	player.SetBufferSize(sampleRate * bytesPerSample * targetMS / 1000)

	target := sampleRate * targetMS / 1000 // 目標充填サンプル数
	// プライミング
	ph := 0.0
	for i := 0; i < target; i++ {
		v := int16(math.Sin(ph) * 6000)
		ph += 2 * math.Pi * 440 / sampleRate
		rb.write(v, v)
	}

	stop := make(chan struct{})
	var produced int64
	var ratioMin, ratioMax float64 = 2, 0
	fillLog := []int{}

	go func() {
		// NES の 1 フレーム = 60.0988 Hz 相当。理想の 1 フレームぶんのサンプル数
		samplesPerFrameIdeal := float64(sampleRate) / 60.0988
		var accum float64
		var integ float64 // PI 制御の積分項
		frameNS := 1.0e9 / 60.0988
		ticker := time.NewTicker(time.Duration(frameNS) * time.Nanosecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ratio := 1.0
				if feedback {
					fill := rb.Fill()
					e := float64(target-fill) / float64(target) // 正なら足りない
					integ += e
					// アンチワインドアップ
					if integ > 20 {
						integ = 20
					} else if integ < -20 {
						integ = -20
					}
					// 足りない → たくさん作る（ratio > 1）
					ratio = 1 + 0.02*e + 0.0008*integ
					if ratio < 0.995 {
						ratio = 0.995
					} else if ratio > 1.005 {
						ratio = 1.005
					}
					if ratio < ratioMin {
						ratioMin = ratio
					}
					if ratio > ratioMax {
						ratioMax = ratio
					}
				}
				accum += samplesPerFrameIdeal * ratio
				n := int(accum)
				accum -= float64(n)
				for i := 0; i < n; i++ {
					v := int16(math.Sin(ph) * 6000)
					ph += 2 * math.Pi * 440 / sampleRate
					if ph > 2*math.Pi {
						ph -= 2 * math.Pi
					}
					rb.write(v, v)
					atomic.AddInt64(&produced, 1)
				}
			}
		}
	}()

	player.Play()
	t0 := time.Now()
	// 5 秒ごとに充填率を記録
	for time.Since(t0) < time.Duration(secs)*time.Second {
		time.Sleep(5 * time.Second)
		fillLog = append(fillLog, rb.Fill())
	}
	close(stop)
	elapsed := time.Since(t0)
	player.Close()

	rb.mu.Lock()
	us, ue, od := rb.underrunSamples, rb.underrunEvents, rb.overflowDrops
	rb.mu.Unlock()
	prod := atomic.LoadInt64(&produced)

	fmt.Printf("%s\n", name)
	fmt.Printf("  生成レート  : %.1f samples/s（理想 %d）\n", float64(prod)/elapsed.Seconds(), sampleRate)
	fmt.Printf("  音切れ      : %d 回 / 合計 %d サンプル (%.1f ms)\n", ue, us,
		float64(us)/sampleRate*1000)
	fmt.Printf("  満杯で破棄  : %d サンプル (%.1f ms)\n", od, float64(od)/sampleRate*1000)
	fmt.Printf("  充填推移(5s毎, 目標%d): %v\n", target, fillLog)
	if feedback {
		fmt.Printf("  補正比の範囲: %.5f 〜 %.5f（= %+.3f%% 〜 %+.3f%%）\n",
			ratioMin, ratioMax, (ratioMin-1)*100, (ratioMax-1)*100)
		cents := 1200 * math.Log2(ratioMax)
		fmt.Printf("  最大ピッチ変化: %.2f セント（人間の検知限 約10-20セント）\n", cents)
	}
	if ue == 0 && od == 0 {
		fmt.Printf("  判定: 合格（音切れ・破棄ともゼロ）\n")
	} else {
		fmt.Printf("  判定: 不合格\n")
	}
	fmt.Println()
}
