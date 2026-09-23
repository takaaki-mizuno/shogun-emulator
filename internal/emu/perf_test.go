package emu

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// perfROM は速度の測定に使う ROM。
//
// 画面全体を毎フレーム描き替えるため、描画の負荷が高い側に振れる。
const perfROM = "../../testdata/roms/full_palette/full_palette.nes"

// TestFrameRate は進行の速度を測る。
//
// 既定では飛ばす。時間のかかる測定であり、結果がホストの負荷に依存する
// ためである。実行するには次のようにする。
//
//	SHOGUN_PERF=1 go test ./internal/emu -run TestFrameRate -v
//	SHOGUN_PERF_SECONDS=600 SHOGUN_PERF=1 go test ./internal/emu -run TestFrameRate -v -timeout 20m
func TestFrameRate(t *testing.T) {
	if os.Getenv("SHOGUN_PERF") == "" {
		t.Skip("SHOGUN_PERF=1 で実行する")
	}
	if _, err := os.Stat(perfROM); err != nil {
		t.Skipf("テスト ROM が無い: %v", err)
	}

	seconds := 10
	if v := os.Getenv("SHOGUN_PERF_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			seconds = n
		}
	}

	paced := measure(t, NewWallClockPacer, time.Duration(seconds)*time.Second)
	t.Logf("壁時計で待つ: %.2f fps（実機は %.2f fps）", paced, region.NTSC.FrameRateHz(true))

	free := measure(t, func(*region.Region) Pacer { return NewNoPacer() }, 3*time.Second)
	t.Logf("待たない: %.2f fps（実時間の %.1f 倍）", free, free/region.NTSC.FrameRateHz(true))

	// 実機のフレームレートから 1% 以上離れていれば進行の制御が効いていない。
	want := region.NTSC.FrameRateHz(true)
	if paced < want*0.99 || paced > want*1.01 {
		t.Errorf("フレームレートが %.2f fps である。期待 %.2f fps 付近", paced, want)
	}
}

// measure は d の間に完成したフレーム数から 1 秒あたりの枚数を求める。
func measure(t *testing.T, pacer func(*region.Region) Pacer, d time.Duration) float64 {
	t.Helper()
	e := New(Config{
		Emulation: config.EmulationConfig{Region: config.RegionNTSC, RAMInitPattern: "zero"},
		Input:     config.InputConfig{Port1Device: config.DeviceStandard},
		NewPacer:  pacer,
	})
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(perfROM); err != nil {
		t.Fatal(err)
	}

	// 表示側の読み出しも同時に行う。受け渡しの費用を測定に含める。
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		f := video.NewFrame()
		tk := time.NewTicker(time.Second / 200)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				e.Frames.Take(f)
			}
		}
	}()

	before, _ := e.Frames.Stats()
	start := time.Now()
	time.Sleep(d)
	after, _ := e.Frames.Stats()
	elapsed := time.Since(start)

	close(stop)
	<-done
	return float64(after-before) / elapsed.Seconds()
}
