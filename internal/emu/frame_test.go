package emu

import (
	"sync"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// TestFrameBufferHandsOverContent は置いたフレームの内容が取り出せることを
// 確かめる。
func TestFrameBufferHandsOverContent(t *testing.T) {
	b := NewFrameBuffer()
	dst := video.NewFrame()

	if b.Take(dst) {
		t.Error("空のときに取り出せてしまった")
	}

	src := video.NewFrame()
	src.Set(1, 2, 0x25)
	b.Put(src)

	if !b.Take(dst) {
		t.Fatal("置いたフレームを取り出せない")
	}
	if got := dst.At(1, 2); got != 0x25 {
		t.Errorf("(1,2) = %#x, 期待 $25", got)
	}
	if b.Take(dst) {
		t.Error("2 回取り出せてしまった")
	}
}

// TestFrameBufferDropsUnreadFrames は取り出す前の上書きを数えることを
// 確かめる。表示が遅れてもエミュレーションを待たせない。
func TestFrameBufferDropsUnreadFrames(t *testing.T) {
	b := NewFrameBuffer()
	src := video.NewFrame()
	for i := range 5 {
		src.Clear(uint16(i))
		b.Put(src)
	}

	produced, dropped := b.Stats()
	if produced != 5 {
		t.Errorf("書き込み数 = %d, 期待 5", produced)
	}
	if dropped != 4 {
		t.Errorf("捨てた数 = %d, 期待 4", dropped)
	}

	dst := video.NewFrame()
	if !b.Take(dst) {
		t.Fatal("取り出せない")
	}
	if got := dst.At(0, 0); got != 4 {
		t.Errorf("最後に置いたフレームが読めない: %#x", got)
	}
}

// TestFrameBufferIsSafeForConcurrentUse は両側から同時に触っても
// 壊れないことを確かめる。-race で検証する。
func TestFrameBufferIsSafeForConcurrentUse(t *testing.T) {
	b := NewFrameBuffer()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		f := video.NewFrame()
		for i := range 200 {
			f.Clear(uint16(i & 0x3F))
			b.Put(f)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		dst := video.NewFrame()
		for range 200 {
			b.Take(dst)
		}
	}()
	wg.Wait()
}
