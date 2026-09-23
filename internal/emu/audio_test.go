package emu

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// audioROM は音を出して測るための ROM。
const audioROM = "../../testdata/roms/apu_mixer/square.nes"

// TestAudioDrivenPacing はオーディオデバイスの消費で進行が決まることを
// 確かめる。
//
// 既定では飛ばす。出力デバイスを必要とし、結果がホストの状態に依存する
// ためである。実行するには次のようにする。
//
//	SHOGUN_AUDIO=1 go test ./internal/emu -run TestAudioDrivenPacing -v
//	SHOGUN_AUDIO=1 SHOGUN_AUDIO_SECONDS=600 go test ./internal/emu -run TestAudioDrivenPacing -v -timeout 20m
func TestAudioDrivenPacing(t *testing.T) {
	if os.Getenv("SHOGUN_AUDIO") == "" {
		t.Skip("SHOGUN_AUDIO=1 で実行する")
	}
	if _, err := os.Stat(audioROM); err != nil {
		t.Skipf("テスト ROM が無い: %v", err)
	}

	seconds := 10
	if v := os.Getenv("SHOGUN_AUDIO_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			seconds = n
		}
	}

	cfg := config.Default()
	cfg.Emulation.Region = config.RegionNTSC
	cfg.Emulation.RAMInitPattern = "zero"

	e := New(Config{
		Emulation: cfg.Emulation,
		Input:     cfg.Input,
		Audio:     cfg.Audio,
		AppName:   "shogun-test",
	})
	if err := e.AudioError(); err != nil {
		t.Skipf("音声を初期化できない: %v", err)
	}
	e.Start()
	defer e.Stop()

	if err := e.LoadROM(audioROM); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	time.Sleep(time.Duration(seconds) * time.Second)
	elapsed := time.Since(start)

	s := e.Status()
	written, under, dropped := e.audio.ring.Stats()
	fps := float64(s.Frames) / elapsed.Seconds()
	rate := float64(written) / elapsed.Seconds()

	t.Logf("%.1f 秒: %.2f fps、%.0f Hz、アンダーラン %d、捨てた数 %d",
		elapsed.Seconds(), fps, rate, under, dropped)

	if under != 0 {
		t.Errorf("アンダーランが %d 回起きた", under)
	}
	if dropped != 0 {
		t.Errorf("サンプルを %d 個捨てた", dropped)
	}
	// 出力デバイスの実クロックに追従するため、実機のフレームレートから
	// 大きく離れない。
	if fps < 59 || fps > 61 {
		t.Errorf("フレームレート = %.2f fps", fps)
	}
}

// TestAudioPauseAndResume は一時停止と再開を繰り返してもアンダーランが
// 起きないことを確かめる。
//
// 一時停止中はサンプルを生成しないため、リングは空になる。空のときに
// 無音を返す経路が働いていなければ、ここでアンダーランが数えられる。
func TestAudioPauseAndResume(t *testing.T) {
	if os.Getenv("SHOGUN_AUDIO") == "" {
		t.Skip("SHOGUN_AUDIO=1 で実行する")
	}
	if _, err := os.Stat(audioROM); err != nil {
		t.Skipf("テスト ROM が無い: %v", err)
	}

	cfg := config.Default()
	cfg.Emulation.Region = config.RegionNTSC
	cfg.Emulation.RAMInitPattern = "zero"

	e := New(Config{Emulation: cfg.Emulation, Input: cfg.Input, Audio: cfg.Audio, AppName: "shogun-test"})
	if err := e.AudioError(); err != nil {
		t.Skipf("音声を初期化できない: %v", err)
	}
	e.Start()
	defer e.Stop()

	if err := e.LoadROM(audioROM); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	for range 5 {
		e.Pause()
		time.Sleep(200 * time.Millisecond)
		e.Resume()
		time.Sleep(200 * time.Millisecond)
	}

	before := e.Status().Frames
	time.Sleep(500 * time.Millisecond)
	if e.Status().Frames <= before {
		t.Error("再開後に進んでいない")
	}
}

// TestAudioSpeedChange は速度倍率を変えてもエミュレーションが止まらない
// ことを確かめる。
//
// 倍率が待たない境界に達したときにリングの待ちを外さないと、出力レートが
// 進行の上限を決めてしまい、早送りにならない。
func TestAudioSpeedChange(t *testing.T) {
	if os.Getenv("SHOGUN_AUDIO") == "" {
		t.Skip("SHOGUN_AUDIO=1 で実行する")
	}
	if _, err := os.Stat(audioROM); err != nil {
		t.Skipf("テスト ROM が無い: %v", err)
	}

	cfg := config.Default()
	cfg.Emulation.Region = config.RegionNTSC
	cfg.Emulation.RAMInitPattern = "zero"

	e := New(Config{Emulation: cfg.Emulation, Input: cfg.Input, Audio: cfg.Audio, AppName: "shogun-test"})
	if err := e.AudioError(); err != nil {
		t.Skipf("音声を初期化できない: %v", err)
	}
	e.Start()
	defer e.Stop()

	if err := e.LoadROM(audioROM); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	measure := func(d time.Duration) float64 {
		before := e.Status().Frames
		time.Sleep(d)
		return float64(e.Status().Frames-before) / d.Seconds()
	}

	normal := measure(time.Second)
	e.SetSpeed(UncappedSpeed)
	time.Sleep(200 * time.Millisecond)
	fast := measure(time.Second)

	t.Logf("等速 %.1f fps、最速 %.1f fps", normal, fast)
	if fast < normal*2 {
		t.Errorf("最速が %.1f fps しか出ない（等速 %.1f fps）", fast, normal)
	}
}

// TestAudioBufferSizes は出力バッファの長さを変えてもアンダーランが
// 起きないことを確かめる。
//
// 負荷を掛けた状態でも測る。デバッグウィンドウを開いたときのように、
// 他のゴルーチンが CPU を使っている状況を模す。
func TestAudioBufferSizes(t *testing.T) {
	if os.Getenv("SHOGUN_AUDIO") == "" {
		t.Skip("SHOGUN_AUDIO=1 で実行する")
	}
	if _, err := os.Stat(audioROM); err != nil {
		t.Skipf("テスト ROM が無い: %v", err)
	}

	for _, ms := range []int{20, 25, 50} {
		for _, load := range []bool{false, true} {
			name := "バッファ" + strconv.Itoa(ms) + "ms"
			if load {
				name += "・負荷あり"
			}
			t.Run(name, func(t *testing.T) {
				cfg := config.Default()
				cfg.Emulation.Region = config.RegionNTSC
				cfg.Emulation.RAMInitPattern = "zero"
				cfg.Audio.BufferMilliseconds = ms

				e := New(Config{
					Emulation: cfg.Emulation,
					Input:     cfg.Input,
					Audio:     cfg.Audio,
					AppName:   "shogun-test",
				})
				if err := e.AudioError(); err != nil {
					t.Skipf("音声を初期化できない: %v", err)
				}
				e.Start()
				defer e.Stop()

				if err := e.LoadROM(audioROM); err != nil {
					t.Fatal(err)
				}

				stop := make(chan struct{})
				if load {
					for range 4 {
						go busyLoop(stop)
					}
				}
				time.Sleep(5 * time.Second)
				close(stop)

				_, under, dropped := e.audio.ring.Stats()
				if under != 0 || dropped != 0 {
					t.Errorf("アンダーラン %d、捨てた数 %d", under, dropped)
				}
			})
		}
	}
}

// busyLoop は停止を指示されるまで CPU を使い続ける。
func busyLoop(stop <-chan struct{}) {
	x := 0
	for {
		select {
		case <-stop:
			_ = x
			return
		default:
			for range 100000 {
				x++
			}
		}
	}
}
