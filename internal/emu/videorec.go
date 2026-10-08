package emu

import (
	"errors"

	"github.com/takaakimizuno/shogun-emulator/internal/audio"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
	"github.com/takaakimizuno/shogun-emulator/internal/video/mp4rec"
)

// VideoStatus は録画の状態。
type VideoStatus struct {
	// Recording は録画中かを表す。
	Recording bool
	// Frames は記録したフレーム数。
	Frames uint64
	// FrameRate は記録のフレームレート。経過時間の表示に使う。
	FrameRate float64
	// Error は書き込みの失敗で録画が止まったときのエラーの文言。次に録画を
	// 始めたとき・StopRecordingVideo がそのエラーを返したときに消える
	// （設計書 08 編 §8.8.3）。
	Error string
}

var errAlreadyRecordingVideo = errors.New("emu: すでに録画している")

// videoRecorder は録画の状態（設計書 08 編 §8.8.2）。エミュレーションゴルーチン
// だけが触る。
type videoRecorder struct {
	w         *mp4rec.Writer
	resampler *audio.Resampler
	// pcm は現在のフレームの間にリサンプラが作った音声。フレームが
	// 完成するたびに Writer へ渡して空にする。
	pcm       []int16
	frameRate float64
	// delivered はリサンプラが作ったサンプルの累計。APU の出力が録画へ
	// 届いていることをテストで確かめるために持つ。エミュレーション
	// ゴルーチンだけが触るため、ロックは要らない。
	delivered uint64
}

// WriteSample は APU の出力を録画専用のリサンプラへ渡す（apu.Output）。
func (r *videoRecorder) WriteSample(v float32) { r.resampler.WriteSample(v) }

// pcmSink はリサンプラの出力を受け取る（audio.Sink）。
type pcmSink struct{ r *videoRecorder }

// WriteSample はリサンプラの出力を溜める。待たないため常に true を返す。
func (s pcmSink) WriteSample(v int16) bool {
	s.r.pcm = append(s.r.pcm, v)
	s.r.delivered++
	return true
}

// apuTee は APU の出力を 2 つへ分ける。音声出力の経路と録画である。
type apuTee struct{ a, b apu.Output }

func (t apuTee) WriteSample(v float32) {
	t.a.WriteSample(v)
	t.b.WriteSample(v)
}

// frameRate はリージョンの平均のフレームレートを返す。NTSC は描画中の
// 奇数フレームで 1 ドット短くなるため、2 種類の平均とする。
func frameRate(r *region.Region) float64 {
	return (r.FrameRateHz(false) + r.FrameRateHz(true)) / 2
}

// StartRecordingVideo は次のフレームから録画を始める（設計書 08 編 §8.8.4）。
func (e *Emulator) StartRecordingVideo(path string, pal *video.Palette, scale int, overscan video.Overscan) error {
	return e.apply(cmdStartVideo{path: path, pal: pal, scale: scale, overscan: overscan, done: make(chan error, 1)})
}

// StopRecordingVideo は録画を止めてファイルを閉じる。録画していないときは何もしない。
// 書き込みの失敗で録画が止まっていたときは、そのエラーを返して消す
// （設計書 08 編 §8.8.3）。
func (e *Emulator) StopRecordingVideo() error {
	return e.apply(cmdStopVideo{done: make(chan error, 1)})
}

// startVideo はエミュレーションゴルーチンで録画を始める。
func (e *Emulator) startVideo(c cmdStartVideo) error {
	if e.machine == nil {
		return errNoROM
	}
	if e.video != nil {
		return errAlreadyRecordingVideo
	}
	r := e.machine.Region
	rate := frameRate(r)
	w, err := mp4rec.Create(c.path, c.pal, mp4rec.Options{
		Scale: c.scale, FrameRate: rate, Overscan: c.overscan, PictureHeight: r.PictureHeight,
	})
	if err != nil {
		return err
	}
	rec := &videoRecorder{w: w, frameRate: rate}
	// 前の録画の失敗は、新しい録画を始めた時点で知らせ終えたものとする。
	e.videoErr = nil
	// 速度倍率を 1 に固定し、消音を適用しないリサンプラ（設計書 08 編 §8.8.2）。
	rec.resampler = audio.NewResampler(r.CPUClockHz(), mp4rec.SampleRate, e.cfg.Audio.FilterProfile, pcmSink{rec})
	e.video = rec
	e.machine.APU.SetOutput(e.apuOutput())
	e.setVideoStatus()
	return nil
}

// recordFrame は完成したフレームを録画へ渡す。失敗したときは録画を止め、
// ファイルを消し、エラーを覚えておく。覚えたエラーは Status.Video.Error に
// 載せ、次の StopRecordingVideo で返す（設計書 08 編 §8.8.3）。
func (e *Emulator) recordFrame(f *video.Frame) {
	pcm := e.video.pcm
	// Writer は受け取った PCM を自分の領域へ写すため、同じ配列を使い回せる。
	e.video.pcm = e.video.pcm[:0]
	if err := e.video.w.WriteFrame(f, pcm); err != nil {
		// 失敗は Status.Video.Error と StopRecordingVideo の戻り値で伝える。
		// ここで警告としても出すと、headless では同じ文言が 2 回出る。
		e.videoErr = err
		_ = e.closeVideo(true) // 中止は常に nil を返す
		return
	}
	e.setVideoStatus()
}

// stopVideo は StopRecordingVideo の処理。録画を終え、途中の失敗で止まって
// いたときはそのエラーを返して消す。
func (e *Emulator) stopVideo(abort bool) error {
	err := e.closeVideo(abort)
	if e.videoErr != nil {
		err = errors.Join(e.videoErr, err)
		e.videoErr = nil
		e.setVideoStatus()
	}
	return err
}

// closeVideo は録画を終える。abort のときはファイルを消す。録画していない
// ときは何もしない。
func (e *Emulator) closeVideo(abort bool) error {
	if e.video == nil {
		return nil
	}
	w := e.video.w
	e.video = nil
	if e.machine != nil {
		e.machine.APU.SetOutput(e.apuOutput())
	}
	e.setVideoStatus()
	if abort {
		w.Abort()
		return nil
	}
	return w.Close()
}

// setVideoStatus は録画の状態を Status へ写す。
func (e *Emulator) setVideoStatus() {
	var st VideoStatus
	if e.video != nil {
		st = VideoStatus{Recording: true, Frames: e.video.w.Frames(), FrameRate: e.video.frameRate}
	} else if e.videoErr != nil {
		st.Error = e.videoErr.Error()
	}
	e.statusMu.Lock()
	e.status.Video = st
	e.statusMu.Unlock()
}
