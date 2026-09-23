// oto v3 スパイク: NES エミュレータ相当の負荷でオーディオを鳴らし、
// バッファのアンダーラン（音切れ）とレイテンシを測る。
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
	bytesPerSample = 2 * channels // 16bit stereo
)

// エミュレータが生成したサンプルを溜めるリングバッファ
type ring struct {
	mu        sync.Mutex
	buf       []int16 // インターリーブ済み
	r, w      int
	size      int
	underruns int64
	maxFill   int
	minFill   int
}

func newRing(samples int) *ring {
	return &ring{buf: make([]int16, samples*channels), size: samples * channels, minFill: 1 << 30}
}

func (r *ring) fill() int {
	d := r.w - r.r
	if d < 0 {
		d += r.size
	}
	return d
}

func (r *ring) write(l, rr int16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := (r.w + 2) % r.size
	if next == r.r {
		return // バッファ満杯（エミュレータが速すぎる）
	}
	r.buf[r.w] = l
	r.buf[r.w+1] = rr
	r.w = next
}

// oto から呼ばれる
func (r *ring) Read(p []byte) (int, error) {
	r.mu.Lock()
	f := r.fill()
	if f > r.maxFill {
		r.maxFill = f
	}
	if f < r.minFill {
		r.minFill = f
	}
	n := 0
	for n+3 < len(p) {
		if r.r == r.w {
			atomic.AddInt64(&r.underruns, 1)
			// 無音を出す
			p[n], p[n+1], p[n+2], p[n+3] = 0, 0, 0, 0
			n += 4
			continue
		}
		l := r.buf[r.r]
		rr := r.buf[r.r+1]
		r.r = (r.r + 2) % r.size
		p[n] = byte(l)
		p[n+1] = byte(l >> 8)
		p[n+2] = byte(rr)
		p[n+3] = byte(rr >> 8)
		n += 4
	}
	r.mu.Unlock()
	return n, nil
}

var gctx *oto.Context

func main() {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		fmt.Println("NewContext error:", err)
		return
	}
	<-ready
	gctx = ctx
	fmt.Printf("oto context: %d Hz, %d ch, 16bit LE\n\n", sampleRate, channels)
	for _, bufMS := range []int{20, 35, 50, 100} {
		run(bufMS)
	}
}

func run(bufMS int) {
	ctx := gctx

	// リングバッファは目標バッファの 4 倍を確保
	rb := newRing(sampleRate * bufMS * 4 / 1000)
	player := ctx.NewPlayer(rb)
	bufBytes := sampleRate * bytesPerSample * bufMS / 1000
	player.SetBufferSize(bufBytes)

	// エミュレータを模した生成ゴルーチン:
	// 1/60 秒ごとに 800 サンプル（= 48000/60）を生成し、そのあいだに CPU 負荷もかける
	stop := make(chan struct{})
	var generated int64
	go func() {
		phase := 0.0
		ticker := time.NewTicker(time.Second / 60)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// NES 1 フレーム相当の CPU 負荷を模擬（約 1.5 ms 相当のビジーループ）
				busy(1500)
				for i := 0; i < sampleRate/60; i++ {
					v := int16(math.Sin(phase) * 8000)
					phase += 2 * math.Pi * 440 / sampleRate
					if phase > 2*math.Pi {
						phase -= 2 * math.Pi
					}
					rb.write(v, v)
					atomic.AddInt64(&generated, 1)
				}
			}
		}
	}()

	// プライミング: 再生開始前にリングを目標バッファ量まで埋める
	primeSamples := sampleRate * bufMS / 1000
	ph := 0.0
	for i := 0; i < primeSamples; i++ {
		v := int16(math.Sin(ph) * 8000)
		ph += 2 * math.Pi * 440 / sampleRate
		rb.write(v, v)
	}

	t0 := time.Now()
	player.Play()
	// 定常状態のみを測るため、最初の 500ms 後にカウンタをリセット
	time.Sleep(500 * time.Millisecond)
	atomic.StoreInt64(&rb.underruns, 0)
	rb.mu.Lock()
	rb.minFill = 1 << 30
	rb.maxFill = 0
	rb.mu.Unlock()
	atomic.StoreInt64(&generated, 0)
	t0 = time.Now()
	time.Sleep(3 * time.Second)
	close(stop)
	elapsed := time.Since(t0)
	un := atomic.LoadInt64(&rb.underruns)
	buffered := player.BufferedSize()
	player.Close()
	_ = ctx

	gen := atomic.LoadInt64(&generated)
	fmt.Printf("bufMS=%3d : 生成 %d サンプル (%.0f/s, 理想 %d/s)  アンダーラン %d サンプル\n",
		bufMS, gen, float64(gen)/elapsed.Seconds(), sampleRate, un)
	fmt.Printf("           oto 内部バッファ残 %d B (= %.1f ms)  リング充填 min=%d max=%d (= %.1f〜%.1f ms)\n",
		buffered, float64(buffered)/float64(sampleRate*bytesPerSample)*1000,
		rb.minFill/channels, rb.maxFill/channels,
		float64(rb.minFill/channels)/sampleRate*1000, float64(rb.maxFill/channels)/sampleRate*1000)
	if un == 0 {
		fmt.Printf("           JUDGE: 合格（音切れなし）\n")
	} else {
		fmt.Printf("           JUDGE: アンダーラン発生（%.3f%% のサンプルが無音）\n",
			float64(un)/float64(gen)*100)
	}
	fmt.Println()
}

var sink float64

func busy(iters int) {
	s := 0.0
	for i := 0; i < iters; i++ {
		s += math.Sqrt(float64(i))
	}
	sink = s
}
