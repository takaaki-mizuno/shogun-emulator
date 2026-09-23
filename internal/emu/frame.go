package emu

import (
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// FrameBuffer はエミュレーションゴルーチンから UI スレッドへ完成した
// フレームを渡す。
//
// PPU が持つフレームキュー（video.Queue）はエミュレーションゴルーチン
// からのみ触る。その内容をここへ複製し、複製した側を UI スレッドが読む。
// 複製するのは、表示が読んでいる間にエミュレーションが同じ領域へ
// 書き込むことを避けるためである。1 フレームは 120 KiB であり、
// 複製の費用は 1 フレームの予算（16.7 ミリ秒）に対して無視できる。
//
// 表示が取りに来る前に次のフレームが来たときは上書きする。エミュレーションを
// 表示で律速させない。
type FrameBuffer struct {
	mu sync.Mutex
	// ready は完成済みで未読のフレーム。
	ready *video.Frame
	// hasFrame は ready の内容が有効かを表す。
	hasFrame bool
	// dropped は表示が取る前に上書きしたフレーム数。
	dropped uint64
	// produced は書き込んだフレーム数。
	produced uint64
}

// NewFrameBuffer は受け渡しの場を作る。
func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{ready: video.NewFrame()}
}

// Put は完成したフレームを置く。エミュレーションゴルーチンが呼ぶ。
func (b *FrameBuffer) Put(f *video.Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hasFrame {
		b.dropped++
	}
	b.ready.CopyFrom(f)
	b.hasFrame = true
	b.produced++
}

// Take は未読のフレームを dst へ複製する。無いとき false を返す。
// UI スレッドが呼ぶ。
func (b *FrameBuffer) Take(dst *video.Frame) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.hasFrame {
		return false
	}
	dst.CopyFrom(b.ready)
	b.hasFrame = false
	return true
}

// Stats は書き込んだフレーム数と捨てたフレーム数を返す。
func (b *FrameBuffer) Stats() (produced, dropped uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.produced, b.dropped
}
