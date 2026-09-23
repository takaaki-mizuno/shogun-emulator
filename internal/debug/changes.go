package debug

import "sync"

// Region は変更追跡の対象の領域。
type Region uint8

// 追跡する領域（設計書 09 編 §9.2）。
const (
	RegionRAM Region = iota
	RegionCIRAM
	RegionOAM
	RegionPalette
	RegionPRGRAM

	regionCount
)

// DefaultDecayFrames は色が消えるまでのフレーム数の既定値。
//
// 1 フレームだけ色を変えると人の目に映らない。30 フレーム（約 0.5 秒）
// かけて薄くする。
const DefaultDecayFrames = 30

// ChangeTracker はメモリのどのバイトが最近書き換わったかを記録する。
//
// フレームが完成するたびに前のフレームの内容と比べ、値が変わった
// バイトの記録を更新する。書き込みのフックで記録しないのは、同じ値の
// 書き込みで色が付かないようにし、CPU のバスを通らない変更（OAM DMA
// など）も捉え、バスアクセスごとの費用をかけないためである。
//
// Update はエミュレーションゴルーチンから、Heat は UI スレッドから
// 呼ばれる。両者を mu で守る。
type ChangeTracker struct {
	mu sync.Mutex
	// lastWrite は変わったフレーム番号に 1 を足した値。0 は変わって
	// いないことを表す。
	lastWrite [regionCount][]uint32
	// prev は前のフレームの内容。
	prev         [regionCount][]uint8
	currentFrame uint32
	decayFrames  uint32
}

// NewChangeTracker は変更追跡を作る。decay が 0 のとき既定値を使う。
func NewChangeTracker(decay uint32) *ChangeTracker {
	if decay == 0 {
		decay = DefaultDecayFrames
	}
	return &ChangeTracker{decayFrames: decay}
}

// SetDecay は色が消えるまでのフレーム数を変える。
func (t *ChangeTracker) SetDecay(n uint32) {
	if n == 0 {
		n = DefaultDecayFrames
	}
	t.mu.Lock()
	t.decayFrames = n
	t.mu.Unlock()
}

// Update はフレームの終わりに現在の内容を取り込む。
//
// mem は領域ごとの現在の内容である。最初の呼び出しと、領域の大きさが
// 変わったとき（ROM の差し替え）は比較せずに取り込むだけにする。
func (t *ChangeTracker) Update(mem [regionCount][]uint8) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentFrame++
	stamp := t.currentFrame + 1
	for r, cur := range mem {
		if len(t.prev[r]) != len(cur) {
			t.prev[r] = append([]uint8(nil), cur...)
			t.lastWrite[r] = make([]uint32, len(cur))
			continue
		}
		prev := t.prev[r]
		last := t.lastWrite[r]
		for i, v := range cur {
			if v != prev[i] {
				prev[i] = v
				last[i] = stamp
			}
		}
	}
}

// Reset は記録を空にする。ROM を読み込んだときと巻き戻したときに呼ぶ。
func (t *ChangeTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = [regionCount][]uint8{}
	t.lastWrite = [regionCount][]uint32{}
}

// Heat は 0.0 から 1.0 を返す。1.0 が直前のフレームで変わったことを表す。
func (t *ChangeTracker) Heat(r Region, offset int) float32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.heatLocked(r, offset)
}

// HeatRow は領域の offset から n バイト分の値を dst へ書く。
//
// メモリビューアが 1 行ずつ取り出す。バイトごとにロックを取らないため
// である。
func (t *ChangeTracker) HeatRow(r Region, offset int, dst []float32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range dst {
		dst[i] = t.heatLocked(r, offset+i)
	}
}

// heatLocked は mu を持った状態で値を求める。
func (t *ChangeTracker) heatLocked(r Region, offset int) float32 {
	last := t.lastWrite[r]
	if offset < 0 || offset >= len(last) || last[offset] == 0 {
		return 0
	}
	age := t.currentFrame + 1 - last[offset]
	if age >= t.decayFrames {
		return 0
	}
	return 1 - float32(age)/float32(t.decayFrames)
}

// Bytes は記録に使っているメモリの量を返す。計測に使う。
func (t *ChangeTracker) Bytes() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for r := range t.lastWrite {
		n += len(t.lastWrite[r])*4 + len(t.prev[r])
	}
	return n
}
