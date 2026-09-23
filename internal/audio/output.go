package audio

import (
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// SampleRate は出力のサンプリングレート。
//
// 固定する。oto.NewContext はプロセスで 1 回しか呼べず、レートは
// コンテキストに属するためである。48000 Hz とするのは、APU の出力から
// 落とす段が 1 つで済むことによる。
const SampleRate = 48000

// ChannelCount は出力のチャンネル数。NES はモノラルだが、左右に同じ値を
// 入れて出す。モノラルで出せない環境があるためである。
const ChannelCount = 2

// contextOnce は oto.NewContext を 1 回に限るための状態。
var (
	contextMu    sync.Mutex
	sharedCtx    *oto.Context
	contextReady chan struct{}
)

// Output はオーディオデバイスへの出力。
type Output struct {
	ring   *Ring
	player *oto.Player

	// bufferMS は出力デバイスのバッファ長。
	bufferMS int
	// volume は音量。0.0〜1.0。
	volume float64
}

// Config は Output の設定。
type Config struct {
	// BufferMilliseconds は出力デバイスのバッファ長。
	BufferMilliseconds int
	// HighWaterMultiplier はリングの高水位を出力バッファの何倍にするか。
	HighWaterMultiplier int
	// Volume は音量。0.0〜1.0。
	Volume float64
	// ApplicationName は音量調整の UI に出す名前。
	ApplicationName string
}

// NewOutput はオーディオデバイスへの出力を開く。
//
// 初期化に失敗したときはエラーを返す。呼び出し側は音声を無効にして
// 続行する（設計書 01 編 §1.9）。
func NewOutput(cfg Config) (*Output, error) {
	ctx, err := context(cfg)
	if err != nil {
		return nil, err
	}

	bufferSamples := SampleRate * cfg.BufferMilliseconds / 1000
	ring := NewRing(bufferSamples, cfg.HighWaterMultiplier)

	o := &Output{
		ring:     ring,
		bufferMS: cfg.BufferMilliseconds,
		volume:   cfg.Volume,
	}
	o.player = ctx.NewPlayer(ring)
	o.player.SetBufferSize(bufferSamples * bytesPerFrame)
	o.player.SetVolume(cfg.Volume)
	return o, nil
}

// context は共有のオーディオコンテキストを返す。
//
// oto.NewContext の 2 回目の呼び出しは失敗する。プロセスで 1 回だけ作り、
// 以降は同じものを返す。
func context(cfg Config) (*oto.Context, error) {
	contextMu.Lock()
	defer contextMu.Unlock()

	if sharedCtx != nil {
		<-contextReady
		return sharedCtx, sharedCtx.Err()
	}

	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      SampleRate,
		ChannelCount:    ChannelCount,
		Format:          oto.FormatSignedInt16LE,
		BufferSize:      time.Duration(cfg.BufferMilliseconds) * time.Millisecond,
		ApplicationName: cfg.ApplicationName,
	})
	if err != nil {
		return nil, err
	}
	<-ready
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sharedCtx = ctx
	contextReady = ready
	return ctx, nil
}

// Ring は書き込み先のリングバッファを返す。
func (o *Output) Ring() *Ring { return o.ring }

// Start は再生を始める。
//
// リングを無音で高水位まで埋めてから始める。埋めずに始めると、起動時に
// バッファ 1 杯分の無音が出力される。
func (o *Output) Start() {
	o.ring.Prime(func() int16 { return 0 })
	o.player.Play()
}

// SetVolume は音量を変える。
func (o *Output) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	o.volume = v
	o.player.SetVolume(v)
}

// SetBufferMilliseconds は出力デバイスのバッファ長を変える。
func (o *Output) SetBufferMilliseconds(ms int) {
	if ms <= 0 {
		return
	}
	o.bufferMS = ms
	o.player.SetBufferSize(SampleRate * ms / 1000 * bytesPerFrame)
}

// Close は再生を止めてリングを閉じる。
//
// オーディオコンテキストは閉じない。プロセスで 1 つしか作れないため、
// 作り直しに備えて残す。
func (o *Output) Close() error {
	o.ring.Close()
	return o.player.Close()
}
