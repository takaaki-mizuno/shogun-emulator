package emu

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// rewindDefaultInterval はステートを保存するフレーム間隔。
//
// 毎フレーム保存すると 60 秒で 144 MiB を要する。10 フレーム間隔なら
// 圧縮して数 MiB に収まる。間の フレームは記録した入力で前方再生する
// （設計書 08 編 §8.6）。
const rewindDefaultInterval = 10

// 巻き戻しが行えないときの理由。
var (
	errNoROM          = errors.New("emu: ROM が読み込まれていない")
	errRewindDisabled = errors.New("emu: 巻き戻しが無効である")
	errRewindEmpty    = errors.New("emu: 巻き戻せる記録がまだ無い")
)

// frameInput は 1 フレーム分の入力。
type frameInput [input.PortCount]uint8

// compressedState は圧縮したステートと、それを取ったフレーム番号。
type compressedState struct {
	frame uint64
	data  []uint8
	// valid は中身が入っていることを表す。
	valid bool
}

// rewindBuffer は巻き戻しのための記録。
//
// ステートと入力をリングバッファで保持する。ステートのロードと記録した
// 入力での前方再生が成立するのは、決定論が保証されているためである。
type rewindBuffer struct {
	// interval はステートを保存するフレーム間隔。
	interval int
	// capacity は保持するフレーム数。
	capacity int

	// inputs はフレーム番号を capacity で割った余りを添字とする。
	inputs []frameInput
	// states はステートのリングバッファ。
	states []compressedState
	// next は次に書き込む states の位置。
	next int

	// first と last は保持している範囲。last は記録済みの最後のフレーム。
	first uint64
	last  uint64
	// count は記録したフレーム数。
	count uint64
}

// newRewindBuffer は巻き戻しの記録を作る。
//
// seconds が 0 以下のとき nil を返す。設定で無効にしたときに保存の費用を
// かけないためである。
func newRewindBuffer(seconds int, framesPerSecond int, interval int) *rewindBuffer {
	if seconds <= 0 || framesPerSecond <= 0 {
		return nil
	}
	if interval <= 0 {
		interval = rewindDefaultInterval
	}
	capacity := seconds * framesPerSecond
	return &rewindBuffer{
		interval: interval,
		capacity: capacity,
		inputs:   make([]frameInput, capacity),
		states:   make([]compressedState, capacity/interval+1),
	}
}

// record はフレーム frame の開始時の入力とステートを記録する。
//
// snapshot はステートを取る関数。間隔に当たらないフレームでは呼ばない。
func (b *rewindBuffer) record(frame uint64, in frameInput, snapshot func() []uint8) {
	if b == nil {
		return
	}
	b.inputs[int(frame%uint64(b.capacity))] = in
	if b.count == 0 {
		b.first = frame
	}
	b.last = frame
	b.count++
	if frame >= uint64(b.capacity) {
		b.first = frame - uint64(b.capacity) + 1
	}

	if frame%uint64(b.interval) != 0 {
		return
	}
	b.states[b.next] = compressedState{frame: frame, data: compressState(snapshot()), valid: true}
	b.next = (b.next + 1) % len(b.states)
}

// input はフレーム frame の入力を返す。
func (b *rewindBuffer) input(frame uint64) frameInput {
	return b.inputs[int(frame%uint64(b.capacity))]
}

// stateAtOrBefore は frame 以前で最も新しいステートを返す。
func (b *rewindBuffer) stateAtOrBefore(frame uint64) (compressedState, bool) {
	var best compressedState
	found := false
	for _, s := range b.states {
		if !s.valid || s.frame > frame || s.frame < b.first {
			continue
		}
		if !found || s.frame > best.frame {
			best = s
			found = true
		}
	}
	return best, found
}

// earliest は巻き戻せる最も古いフレームを返す。
//
// 入力を保持していても、その手前のステートが無ければ復元できない。
func (b *rewindBuffer) earliest() (uint64, bool) {
	var best uint64
	found := false
	for _, s := range b.states {
		if !s.valid || s.frame < b.first {
			continue
		}
		if !found || s.frame < best {
			best = s.frame
			found = true
		}
	}
	return best, found
}

// truncate は frame より後の記録を捨てる。
//
// 巻き戻した後に前へ進めると、そこから先は別の入力になる。
func (b *rewindBuffer) truncate(frame uint64) {
	if b == nil {
		return
	}
	for i, s := range b.states {
		if s.valid && s.frame > frame {
			b.states[i] = compressedState{}
		}
	}
	b.last = frame
	// next を、次のステートが上書きされても古いものを壊さない位置へ戻す。
	b.next = 0
	for i, s := range b.states {
		if !s.valid {
			b.next = i
			break
		}
	}
}

// reset は記録を空にする。
func (b *rewindBuffer) reset() {
	if b == nil {
		return
	}
	clear(b.states)
	b.next = 0
	b.first = 0
	b.last = 0
	b.count = 0
}

// bytes は保持しているステートの合計サイズを返す。計測に使う。
func (b *rewindBuffer) bytes() int {
	if b == nil {
		return 0
	}
	n := 0
	for _, s := range b.states {
		if s.valid {
			n += len(s.data)
		}
	}
	return n
}

// compressState はステートを圧縮する。
//
// 巻き戻しのリングは 300 個以上のステートを保持する。圧縮しないと
// 60 秒で 14 MiB を要する。
func compressState(data []uint8) []uint8 {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.BestSpeed)
	if err != nil {
		// flate.BestSpeed は常に受け付けられる。
		return append([]uint8(nil), data...)
	}
	if _, err := w.Write(data); err != nil {
		w.Close()
		return append([]uint8(nil), data...)
	}
	if err := w.Close(); err != nil {
		return append([]uint8(nil), data...)
	}
	return buf.Bytes()
}

// decompressState は圧縮したステートを戻す。
func decompressState(data []uint8) ([]uint8, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	return io.ReadAll(r)
}

// RewindBytes は巻き戻しのために保持しているステートの合計サイズを返す。
//
// 計測に使う。エミュレーションゴルーチンが触る値を読むため、命令境界で
// 取り出す。
func (e *Emulator) RewindBytes() int {
	var n int
	e.WithMachine(func(*nes.NES) { n = e.rewind.bytes() })
	return n
}

// resetRewind は巻き戻しの記録を作り直す。
//
// ROM を読み込んだときと取り外したときに呼ぶ。別の ROM のステートが
// リングに残っていると、遡った先で復元に失敗する。
func (e *Emulator) resetRewind() {
	if !e.cfg.State.RewindEnabled || e.machine == nil {
		e.rewind = nil
		return
	}
	// フレームレートはリージョンから求める。PAL は 1 秒あたりのフレーム数が
	// 少なく、同じ秒数でも保持するフレーム数が変わる。
	fps := int(e.machine.Region.FrameRateHz(false) + 0.5)
	e.rewind = newRewindBuffer(e.cfg.State.RewindSeconds, fps, e.cfg.State.RewindIntervalFrames)
}

// rewindFrames は frames フレーム分だけ遡る。
//
// 目標フレームの直前に保存したステートをロードし、記録した入力で
// 前方再生する。遡れる限界を越えて要求されたときは限界で止まる。
func (e *Emulator) rewindFrames(frames int) error {
	if e.machine == nil {
		return errNoROM
	}
	if e.rewind == nil {
		return errRewindDisabled
	}
	if frames <= 0 {
		return nil
	}
	cur := e.machine.Frames()
	target := uint64(0)
	if uint64(frames) < cur {
		target = cur - uint64(frames)
	}
	oldest, ok := e.rewind.earliest()
	if !ok {
		return errRewindEmpty
	}
	if target < oldest {
		target = oldest
	}
	if target >= cur {
		return nil
	}

	st, ok := e.rewind.stateAtOrBefore(target)
	if !ok {
		return errRewindEmpty
	}
	data, err := decompressState(st.data)
	if err != nil {
		return err
	}
	if err := e.machine.LoadState(data); err != nil {
		return err
	}
	e.afterLoadState()
	e.dbg.StateLoaded()

	// 記録した入力で目標まで前方再生する。映像と音声は出さない。
	// 音声を出したままにすると、遡る間の波形がリングに溜まり、
	// 高水位で待つ経路（設計書 02 編 §2.6）で進行が止まる。
	e.silent = true
	e.machine.APU.SetOutput(nil)
	// 前方再生ではラッチを直接与える。遅らせたフレームの開始の処理を
	// 走らせない。
	e.framePending = false
	for f := st.frame; f < target; f++ {
		e.latch.set(e.rewind.input(f))
		for !e.stepOnce() {
		}
	}
	e.machine.APU.SetOutput(e.apuOutput())
	e.silent = false
	// 前方再生の途中で当たったブレークポイントは、遡る操作の副産物で
	// ある。止まらずに捨てる。
	e.dbg.ClearHit()
	if e.audio != nil {
		e.audio.reset()
	}
	// 目標フレームの入力を戻しておく。遡った直後に進めたときの 1 フレームは
	// 記録した入力ではなく、現在の入力を使う。
	e.rewind.truncate(target)
	if e.recorder != nil {
		e.recorder.TruncateTo(target)
	}
	if e.journal.rec != nil && target >= e.journal.base {
		e.journal.rec.TruncateTo(target - e.journal.base)
	}
	e.frameBoundary()
	e.updateStatus()
	return nil
}
