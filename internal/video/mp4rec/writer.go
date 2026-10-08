package mp4rec

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
	"math"
	"sync"
	"sync/atomic"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// 倍率の範囲と圧縮の設定(設計書 08 編 §8.8.1)。
const (
	MinScale    = 1
	MaxScale    = 3
	jpegQuality = 90
	// queueFrames は書き出し用のゴルーチンへの受け渡しの列の長さ。満ちたときは
	// WriteFrame が待つ。フレームを捨てると動画が途切れるためである。
	queueFrames = 8
	// maxCarry は次のフレームへ回す音声の上限(0.1 秒)。リサンプラの出力が
	// 続けて多いときに、遅れがたまり続けないようにする。
	maxCarry = SampleRate / 10
)

// Options は動画の書き出しの設定。
type Options struct {
	// Scale は拡大率(MinScale–MaxScale)。最近傍で拡大する。
	Scale int
	// FrameRate はリージョンのフレームレート。
	FrameRate float64
	// Overscan は画面の設定と同じだけ削る量。
	Overscan video.Overscan
	// PictureHeight は表示する画の高さ。
	PictureHeight int
}

// job は書き出し用のゴルーチンへ渡す 1 フレーム分の材料。
//
// frame はポインタで持つ。video.Frame は 256x240 の uint16 の配列であり、
// channel の要素としてそのまま渡すと -race 付きビルドが「要素が大きすぎる」
// として拒むため、ヒープに確保したものを指す。
type job struct {
	frame *video.Frame
	dur   uint32
	pcm   []byte
}

// Writer は動画の書き出し。WriteFrame はエミュレーションゴルーチンから、
// 圧縮と書き込みは専用のゴルーチンで行う(設計書 08 編 §8.8.3)。
type Writer struct {
	pal   *video.Palette
	opts  Options
	mux   *muxer
	queue chan job
	done  chan struct{}

	mu  sync.Mutex
	err error

	// frames は atomic で持つ。Frames() を WriteFrame と別のゴルーチン
	// (UI の進捗表示など)から呼んでも競合しないようにするためである。
	frames    atomic.Uint64
	audioSent uint64
	carry     []int16
}

// Create はファイルを作り、書き出しを始める。pal は写して持つため、呼び出し側は
// この後パレットを書き換えてよい（書き出し用のゴルーチンと競合しない）。
func Create(path string, pal *video.Palette, opts Options) (*Writer, error) {
	if pal == nil {
		return nil, errors.New("mp4rec: パレットが無い")
	}
	if opts.Scale < MinScale || opts.Scale > MaxScale {
		return nil, fmt.Errorf("mp4rec: 倍率 %d は %d–%d の範囲にない", opts.Scale, MinScale, MaxScale)
	}
	if opts.FrameRate <= 0 || opts.PictureHeight <= 0 {
		return nil, fmt.Errorf("mp4rec: フレームレート %v・画の高さ %d が不正", opts.FrameRate, opts.PictureHeight)
	}
	rect := opts.Overscan.Rect(opts.PictureHeight)
	mux, err := newMuxer(path, rect.Dx()*opts.Scale, rect.Dy()*opts.Scale)
	if err != nil {
		return nil, err
	}
	// GUI は設定の保存でパレットをその場で書き換える。書き出し用の
	// ゴルーチンが同じ表を読み続けると競合するため、写したものを使う。
	palCopy := *pal
	w := &Writer{pal: &palCopy, opts: opts, mux: mux, queue: make(chan job, queueFrames), done: make(chan struct{})}
	go w.run()
	return w, nil
}

// Frames は受け取ったフレーム数を返す。どのゴルーチンから呼んでも安全。
func (w *Writer) Frames() uint64 { return w.frames.Load() }

// WriteFrame は 1 フレームの画と、そのフレームの間に作られた音声を受け取る。
// 書き出しが失敗していればそのエラーを返す。
func (w *Writer) WriteFrame(f *video.Frame, pcm []int16) error {
	if err := w.failed(); err != nil {
		return err
	}
	i := w.frames.Load()
	t0 := math.Round(float64(i) * videoTimescale / w.opts.FrameRate)
	t1 := math.Round(float64(i+1) * videoTimescale / w.opts.FrameRate)
	target := uint64(math.Round(float64(i+1) * SampleRate / w.opts.FrameRate))
	need := int(target - w.audioSent)

	// 足りない分は無音で埋め、多い分は次のフレームへ回す(設計書 08 編 §8.8.2)。
	w.carry = append(w.carry, pcm...)
	take := min(need, len(w.carry))
	out := make([]byte, need*pcmFrameBytes)
	for k, v := range w.carry[:take] {
		u := uint16(v)
		out[k*4], out[k*4+1], out[k*4+2], out[k*4+3] = byte(u), byte(u>>8), byte(u), byte(u>>8)
	}
	w.carry = append(w.carry[:0], w.carry[take:]...)
	if len(w.carry) > maxCarry {
		w.carry = w.carry[len(w.carry)-maxCarry:]
	}
	w.audioSent = target

	frame := video.NewFrame()
	frame.CopyFrom(f)
	j := job{frame: frame, dur: uint32(t1 - t0), pcm: out}
	w.queue <- j
	w.frames.Add(1)
	return nil
}

// run は受け取ったフレームを圧縮して書く。失敗した後は捨てる。
func (w *Writer) run() {
	defer close(w.done)
	var buf bytes.Buffer
	for j := range w.queue {
		if w.failed() != nil {
			continue
		}
		img := video.ScaleNearest(video.Image(j.frame, w.pal, w.opts.Overscan, w.opts.PictureHeight), w.opts.Scale)
		buf.Reset()
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			w.fail(err)
			continue
		}
		if err := w.mux.addFrame(buf.Bytes(), j.dur, j.pcm); err != nil {
			w.fail(err)
		}
	}
}

func (w *Writer) failed() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *Writer) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		w.err = err
	}
}

// Close は残りを書き終え、moov を書いて閉じる。書き出しが失敗していたときは
// ファイルを消してそのエラーを返す。
func (w *Writer) Close() error {
	close(w.queue)
	<-w.done
	if err := w.failed(); err != nil {
		w.mux.abort()
		return err
	}
	return w.mux.close()
}

// Abort は書き出しを止め、ファイルを消す。
func (w *Writer) Abort() {
	close(w.queue)
	<-w.done
	w.mux.abort()
}
