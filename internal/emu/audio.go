package emu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/audio"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// audioPipeline は APU の出力を音声デバイスへ運ぶ経路。
//
// APU → リサンプラ → リングバッファ → oto の順に流れる。リングの
// 高水位が書き込み側を待たせ、これが進行の駆動になる。
type audioPipeline struct {
	out       *audio.Output
	resampler *audio.Resampler
	ring      *audio.Ring

	// enabled が false のとき音を出さない。
	enabled bool
	// muteOnFastForward は早送り時に消音するかどうか。
	muteOnFastForward bool
	// profile はフィルタの構成。
	profile string
	// volume は消音を解いたときに戻す音量。
	volume float64
}

// newAudioPipeline は音声の経路を作る。
//
// リサンプラはリージョンが決まってから作る。入力のサンプリングレートが
// CPU クロックであり、リージョンによって異なるためである。
//
// 初期化に失敗したときはエラーを返す。呼び出し側は音声を無効にして
// 続行する（設計書 01 編 §1.9）。
func newAudioPipeline(cfg config.AudioConfig, appName string) (*audioPipeline, error) {
	out, err := audio.NewOutput(audio.Config{
		BufferMilliseconds:  cfg.BufferMilliseconds,
		HighWaterMultiplier: cfg.RingHighWaterMultiplier,
		Volume:              cfg.MasterVolume,
		ApplicationName:     appName,
	})
	if err != nil {
		return nil, err
	}

	return &audioPipeline{
		out:               out,
		ring:              out.Ring(),
		enabled:           true,
		muteOnFastForward: cfg.MuteOnFastForward,
		profile:           cfg.FilterProfile,
		volume:            cfg.MasterVolume,
	}, nil
}

// setRegion はリージョンに合わせてリサンプラを作り直す。
func (p *audioPipeline) setRegion(r *region.Region) {
	p.resampler = audio.NewResampler(r.CPUClockHz(), audio.SampleRate, p.profile, p.ring)
}

// WriteSample は APU から 1 CPU サイクルぶんの出力を受け取る。
//
// エミュレーションゴルーチンから呼ばれる。リングが高水位のときは
// ここで待つ。これが進行の駆動である。
func (p *audioPipeline) WriteSample(v float32) {
	if p.resampler == nil {
		return
	}
	p.resampler.WriteSample(v)
}

// setSpeed は速度倍率を反映する。
//
// 倍率が待たない境界に達したら、リングの待ちを外す。待ったままだと
// 出力レートが進行の上限を決めてしまい、早送りにならない。
func (p *audioPipeline) setSpeed(speed float64) {
	uncapped := speed >= UncappedSpeed
	if p.resampler == nil {
		return
	}
	p.resampler.SetSpeed(speed)
	p.ring.SetBlocking(!uncapped)
	p.resampler.SetMuted(uncapped && p.muteOnFastForward)
}

// setMuted は消音を切り替える。
//
// 出力の音量を 0 にする。サンプルの生成は止めない。止めると進行の
// 駆動が失われ、消音中だけ速く進むことになる。
func (p *audioPipeline) setMuted(muted bool) {
	if muted {
		p.out.SetVolume(0)
		return
	}
	p.out.SetVolume(p.volume)
}

// setPaused は一時停止を反映する。
//
// 一時停止中はサンプルを生成しないため、リングは自然に空になり、
// 無音が出る。待ちを外しておかないと、再開の指示を受け取る前に
// エミュレーションゴルーチンが高水位で止まったままになる。
func (p *audioPipeline) setPaused(paused bool) {
	p.ring.SetBlocking(!paused)
}

// reset はフィルタとリングを初期化する。
//
// セーブステートの読み込み後に呼ぶ。前の音から続けると不連続が
// クリック音になる。リングを高水位まで埋め直して音切れを防ぐ。
func (p *audioPipeline) reset() {
	if p.resampler == nil {
		return
	}
	p.resampler.Reset()
	p.ring.Clear()
	p.ring.Prime(func() int16 { return 0 })
}

// start は再生を始める。
func (p *audioPipeline) start() { p.out.Start() }

// close は再生を止める。
func (p *audioPipeline) close() {
	if p.out != nil {
		p.out.Close()
	}
}

// volumes は設定からチャンネル別の音量を作る。
//
// 指定の無いチャンネルは等倍とする。
func volumes(m map[string]float64) apu.ChannelVolumes {
	v := apu.DefaultVolumes()
	get := func(name string, dst *float32) {
		if x, ok := m[name]; ok {
			*dst = float32(x)
		}
	}
	get("pulse1", &v.Pulse1)
	get("pulse2", &v.Pulse2)
	get("triangle", &v.Triangle)
	get("noise", &v.Noise)
	get("dmc", &v.DMC)
	return v
}
