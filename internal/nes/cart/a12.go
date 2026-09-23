package cart

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// a12LowDotsRequired は立ち上がりと認める前に A12 が low である必要の
// ある PPU ドット数。
//
// MMC3 のフィルタは A12 の信号を積分回路に通してから読む。回路の時定数に
// より、短い low は立ち上がりとして現れない。3 CPU サイクルに相当する
// 9 ドットとする。
//
// ドット単位で測るのは、スプライトのパターンフェッチの間に現れる 4 ドットの
// low を確実に無視するためである。CPU サイクル単位では、この 4 ドットが
// 位相によって 1 サイクルにも 2 サイクルにも見え、しきい値 3 をまたぐ
// ことがある。またいだ走査線だけ余分な立ち上がりを数え、IRQ が早まる。
const a12LowDotsRequired = 10

// a12Filter は PPU アドレスバスの A12 の立ち上がりを数える。
//
// 独立した型にするのは、「low が 3 サイクル以上続いた後の立ち上がり」と
// いう条件を PPU を動かさずに検証できるようにするためである。
type a12Filter struct {
	// prevHigh は直前の通知で A12 が high だったかを表す。
	prevHigh bool
	// lowDots は A12 が low のまま経過した PPU のドット数。
	lowDots int
}

// notify はアドレスの通知を受け、立ち上がりを検出したかを返す。
//
// dots は前回の通知からの経過ドット数である。
func (f *a12Filter) notify(addr uint16, dots int) (rising bool) {
	high := addr&0x1000 != 0
	if !high {
		f.lowDots += dots
		f.prevHigh = false
		return false
	}
	rising = !f.prevHigh && f.lowDots >= a12LowDotsRequired
	// high になった時点で low の継続は途切れる。立ち上がりと認めなかった
	// ときも 0 に戻す。戻さないと、スプライトのフェッチの間に現れる短い
	// low が積み上がり、走査線あたり 3 回の立ち上がりを数える。
	f.lowDots = 0
	f.prevHigh = true
	return rising
}

// saveState はフィルタの状態を書く。
//
// 保存しないと、復元直後のスキャンライン IRQ が 1 行ずれる。
func (f *a12Filter) saveState(w *state.Writer) {
	w.Bool(f.prevHigh)
	w.Int(f.lowDots)
}

// loadState はフィルタの状態を読む。
func (f *a12Filter) loadState(r *state.Reader) {
	f.prevHigh = r.Bool()
	f.lowDots = r.Int()
}
