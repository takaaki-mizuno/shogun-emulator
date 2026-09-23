package audio

import (
	"encoding/binary"
	"sync"
)

// bytesPerFrame は 1 サンプル（左右 2 チャンネル × 16 bit）のバイト数。
const bytesPerFrame = 4

// minHighWaterMultiplier は高水位を出力バッファの何倍にするかの下限。
//
// 等倍では余裕がなく、処理の一時的な遅れがそのまま音切れになる。
const minHighWaterMultiplier = 2

// Ring はエミュレーションゴルーチンと再生スレッドの間のリングバッファ。
//
// 書き込み側は高水位に達するとブロックする。再生スレッドが消費したときに
// 起きる。これによりエミュレーションの進行速度が出力デバイスの実クロックに
// 追従する。壁時計を参照しない。
type Ring struct {
	mu   sync.Mutex
	cond *sync.Cond

	buf []int16
	// r と w は読み書きの位置。サンプル単位。
	r, w int
	// count は溜まっているサンプル数。
	count int
	// high は高水位。サンプル数。
	high int

	closed bool
	// blocking が false のとき、高水位でも待たずに捨てる。
	blocking bool

	// underruns は空のまま読まれた回数。
	underruns uint64
	// dropped は満杯で捨てたサンプル数。
	dropped uint64
	// written は書き込んだサンプル数。
	written uint64
}

// NewRing はリングバッファを作る。
//
// bufferSamples は出力デバイスのバッファのサンプル数、multiplier は
// 高水位をその何倍にするかである。容量は高水位の 2 倍とする。
func NewRing(bufferSamples, multiplier int) *Ring {
	if multiplier < minHighWaterMultiplier {
		multiplier = minHighWaterMultiplier
	}
	if bufferSamples < 1 {
		bufferSamples = 1
	}
	high := bufferSamples * multiplier
	r := &Ring{
		buf:      make([]int16, high*2),
		high:     high,
		blocking: true,
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// SetBlocking は高水位で待つかを設定する。
//
// 速度倍率が高いときは待たない。待つと、出力レートが進行の上限を決めて
// しまい、早送りにならない。
func (r *Ring) SetBlocking(v bool) {
	r.mu.Lock()
	r.blocking = v
	r.mu.Unlock()
	r.cond.Broadcast()
}

// WriteSample は 1 サンプル書く。エミュレーションゴルーチンが呼ぶ。
//
// 高水位に達しているあいだ待つ。閉じられたとき false を返す。
func (r *Ring) WriteSample(v int16) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.count >= r.high && r.blocking && !r.closed {
		r.cond.Wait()
	}
	if r.closed {
		return false
	}
	if r.count >= len(r.buf) {
		r.dropped++
		return true
	}
	r.buf[r.w] = v
	r.w = (r.w + 1) % len(r.buf)
	r.count++
	r.written++
	return true
}

// Read は再生スレッドが呼ぶ。io.Reader を満たす。
//
// バッファが空のときは無音を返す。待たない。待つと再生スレッドが
// 止まり、出力デバイスのバッファが枯れる。
func (r *Ring) Read(p []byte) (int, error) {
	frames := len(p) / bytesPerFrame
	if frames == 0 {
		return 0, nil
	}

	r.mu.Lock()
	for i := range frames {
		var v int16
		if r.count > 0 {
			v = r.buf[r.r]
			r.r = (r.r + 1) % len(r.buf)
			r.count--
		} else {
			r.underruns++
		}
		// 左右に同じ値を入れる。NES はモノラルである。
		off := i * bytesPerFrame
		binary.LittleEndian.PutUint16(p[off:], uint16(v))
		binary.LittleEndian.PutUint16(p[off+2:], uint16(v))
	}
	r.mu.Unlock()

	// 消費したことを書き込み側へ知らせる。
	r.cond.Broadcast()
	return frames * bytesPerFrame, nil
}

// Prime は再生を始める前にリングを高水位まで埋める。
//
// 埋めずに始めると、起動時にバッファ 1 杯分の無音が出力される。
func (r *Ring) Prime(gen func() int16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.count < r.high && !r.closed {
		r.buf[r.w] = gen()
		r.w = (r.w + 1) % len(r.buf)
		r.count++
	}
}

// Clear は内容を捨てる。セーブステートの読み込み後に呼ぶ。
func (r *Ring) Clear() {
	r.mu.Lock()
	r.r, r.w, r.count = 0, 0, 0
	r.mu.Unlock()
	r.cond.Broadcast()
}

// Fill は溜まっているサンプル数と高水位を返す。デバッグ表示に使う。
func (r *Ring) Fill() (count, high int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count, r.high
}

// Stats は書き込み数・アンダーラン数・捨てた数を返す。
func (r *Ring) Stats() (written, underruns, dropped uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.written, r.underruns, r.dropped
}

// Close は書き込み側の待ちを解いて閉じる。
func (r *Ring) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	r.cond.Broadcast()
}
