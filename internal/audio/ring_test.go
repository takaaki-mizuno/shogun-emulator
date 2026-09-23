package audio

import (
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

// TestRingRoundTrip は書いた値が順に読めることを確かめる。
func TestRingRoundTrip(t *testing.T) {
	r := NewRing(4, 2) // 高水位 8、容量 16

	for i := range 8 {
		if !r.WriteSample(int16(i + 1)) {
			t.Fatalf("%d 個目の書き込みに失敗した", i)
		}
	}

	p := make([]byte, 8*bytesPerFrame)
	n, err := r.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(p) {
		t.Fatalf("読み出したバイト数 = %d, 期待 %d", n, len(p))
	}
	for i := range 8 {
		off := i * bytesPerFrame
		l := int16(binary.LittleEndian.Uint16(p[off:]))
		right := int16(binary.LittleEndian.Uint16(p[off+2:]))
		if l != int16(i+1) || right != l {
			t.Errorf("%d 個目 = (%d, %d), 期待 (%d, %d)", i, l, right, i+1, i+1)
		}
	}
}

// TestRingBlocksAtHighWater は高水位で書き込みが待つことを確かめる。
//
// この待ちがエミュレーションの進行速度を出力デバイスに追従させる。
func TestRingBlocksAtHighWater(t *testing.T) {
	r := NewRing(4, 2) // 高水位 8

	for range 8 {
		r.WriteSample(1)
	}

	done := make(chan struct{})
	go func() {
		r.WriteSample(2)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("高水位を超えて書き込めてしまった")
	case <-time.After(20 * time.Millisecond):
	}

	// 消費すると書き込みが進む
	r.Read(make([]byte, bytesPerFrame))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("消費しても書き込みが進まない")
	}
}

// TestRingDoesNotBlockWhenDisabled は待たない設定で進むことを確かめる。
//
// 速度倍率が高いときに使う。
func TestRingDoesNotBlockWhenDisabled(t *testing.T) {
	r := NewRing(4, 2)
	r.SetBlocking(false)

	done := make(chan struct{})
	go func() {
		for range 100 {
			r.WriteSample(1)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("待たない設定なのに進まない")
	}
}

// TestRingReadReturnsSilenceWhenEmpty は空のとき無音を返すことを確かめる。
//
// 再生スレッドを待たせると出力デバイスのバッファが枯れる。
func TestRingReadReturnsSilenceWhenEmpty(t *testing.T) {
	r := NewRing(4, 2)
	p := make([]byte, 4*bytesPerFrame)
	n, err := r.Read(p)
	if err != nil || n != len(p) {
		t.Fatalf("読み出し = (%d, %v)", n, err)
	}
	for i, b := range p {
		if b != 0 {
			t.Fatalf("%d バイト目が %d である。無音を期待", i, b)
		}
	}
	if _, under, _ := r.Stats(); under != 4 {
		t.Errorf("アンダーラン数 = %d, 期待 4", under)
	}
}

// TestRingHighWaterMinimum は高水位の下限が働くことを確かめる。
//
// 等倍では余裕がなく、処理の遅れがそのまま音切れになる。
func TestRingHighWaterMinimum(t *testing.T) {
	r := NewRing(10, 1)
	if _, high := r.Fill(); high != 20 {
		t.Errorf("高水位 = %d, 期待 20（2 倍に丸める）", high)
	}
}

// TestRingPrimeFillsToHighWater は再生前に高水位まで埋まることを確かめる。
func TestRingPrimeFillsToHighWater(t *testing.T) {
	r := NewRing(4, 2)
	r.Prime(func() int16 { return 0 })
	count, high := r.Fill()
	if count != high {
		t.Errorf("充填数 = %d, 高水位 = %d", count, high)
	}
}

// TestRingCloseReleasesWriter は閉じると待っている書き込みが解けることを
// 確かめる。終了時にエミュレーションゴルーチンが止まらなくなるのを防ぐ。
func TestRingCloseReleasesWriter(t *testing.T) {
	r := NewRing(4, 2)
	for range 8 {
		r.WriteSample(1)
	}

	result := make(chan bool, 1)
	go func() { result <- r.WriteSample(2) }()

	time.Sleep(10 * time.Millisecond)
	r.Close()

	select {
	case ok := <-result:
		if ok {
			t.Error("閉じた後の書き込みが成功を返した")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("閉じても書き込みが解けない")
	}
}

// TestRingConcurrentUse は両側から同時に触っても壊れないことを確かめる。
//
// 読み出し側は書き込みが終わるまで読み続ける。途中でやめると、書き込み側が
// 高水位で待ったまま進まなくなる。これは不具合ではなく、待ちによって
// 進行を律速するという設計そのものである。
func TestRingConcurrentUse(t *testing.T) {
	r := NewRing(64, 2)
	var wg sync.WaitGroup
	writeDone := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(writeDone)
		for i := range 5000 {
			r.WriteSample(int16(i))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		p := make([]byte, 32*bytesPerFrame)
		for {
			r.Read(p)
			select {
			case <-writeDone:
				return
			default:
			}
		}
	}()
	wg.Wait()

	if written, _, dropped := r.Stats(); written != 5000 || dropped != 0 {
		t.Errorf("書き込み数 = %d（期待 5000）、捨てた数 = %d（期待 0）", written, dropped)
	}
	r.Close()
}
