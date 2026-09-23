package apu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// divider は周期 P+1 の分周器。
//
// 実機の各ユニットが共通して持つ部品である。型として分けるのは、
// 「周期 P に対して P+1 回に 1 度クロックを出す」という数え方を
// 1 か所に閉じるためである。
type divider struct {
	period  uint16
	counter uint16
}

// clock は 1 回クロックし、出力クロックを生成したとき true を返す。
func (d *divider) clock() bool {
	if d.counter == 0 {
		d.counter = d.period
		return true
	}
	d.counter--
	return false
}

// reload はカウンタを period にする。出力クロックは生成しない。
func (d *divider) reload() { d.counter = d.period }

// setPeriod は周期を変える。カウンタは変えない。
//
// 周期を変えてもカウンタをリセットしないのは実機の挙動である。
// タイマー周期の書き換えで位相がずれないことを、ビブラートを使う
// プログラムが前提にしている。
func (d *divider) setPeriod(p uint16) { d.period = p }

func (d *divider) saveState(w *state.Writer) {
	w.U16(d.period)
	w.U16(d.counter)
}

func (d *divider) loadState(r *state.Reader) {
	d.period = r.U16()
	d.counter = r.U16()
}

// lengthTable はレングスカウンタにロードされる値。
//
// docs/research/04_apu.md の 4.1 節の表から転記した。
var lengthTable = [32]uint8{
	10, 254, 20, 2, 40, 4, 80, 6, 160, 8, 60, 10, 14, 12, 26, 14,
	12, 16, 24, 18, 48, 20, 96, 22, 192, 24, 72, 26, 16, 28, 32, 30,
}

// lengthCounter は音の長さを数えるカウンタ。
//
// 0 になるとチャンネルが消音する。halt が立っている間は減らない。
type lengthCounter struct {
	value   uint8
	halt    bool
	enabled bool
}

// load は $4003・$4007・$400B・$400F の上位 5 bit から値をロードする。
//
// チャンネルが無効のときは何もしない。無効のあいだは以前の値も失われる。
func (l *lengthCounter) load(index uint8) {
	if !l.enabled {
		return
	}
	l.value = lengthTable[index&0x1F]
}

// setEnabled はチャンネルの有効・無効を設定する。
//
// 無効にすると値が 0 になり、再度有効にするまで変更できない。
// 有効にすること自体には即座の効果がない。
func (l *lengthCounter) setEnabled(v bool) {
	l.enabled = v
	if !v {
		l.value = 0
	}
}

// clock は half frame ごとに呼ばれる。
func (l *lengthCounter) clock() {
	if l.value > 0 && !l.halt {
		l.value--
	}
}

// active はチャンネルが鳴っているかを返す。
func (l *lengthCounter) active() bool { return l.value > 0 }

func (l *lengthCounter) saveState(w *state.Writer) {
	w.U8(l.value)
	w.Bool(l.halt)
	w.Bool(l.enabled)
}

func (l *lengthCounter) loadState(r *state.Reader) {
	l.value = r.U8()
	l.halt = r.Bool()
	l.enabled = r.Bool()
}

// envelope は音量を自動で減衰させるユニット。
type envelope struct {
	start      bool
	div        divider
	decayLevel uint8
	loop       bool
	constant   bool
	param      uint8
}

// write は $4000・$4004・$400C の下位 6 bit を反映する。
func (e *envelope) write(v uint8) {
	e.loop = v&0x20 != 0
	e.constant = v&0x10 != 0
	e.param = v & 0x0F
	e.div.setPeriod(uint16(e.param))
}

// restart は $4003・$4007・$400F の書き込みで start フラグを立てる。
func (e *envelope) restart() { e.start = true }

// clock は quarter frame ごとに呼ばれる。
//
// 定音量モードでも減衰の更新を続ける。定音量フラグは出力の選択だけを
// 行う。モードを切り替えたときに減衰の途中から鳴り出す挙動がこれに依る。
func (e *envelope) clock() {
	if e.start {
		e.start = false
		e.decayLevel = 15
		e.div.reload()
		return
	}
	if !e.div.clock() {
		return
	}
	switch {
	case e.decayLevel > 0:
		e.decayLevel--
	case e.loop:
		e.decayLevel = 15
	}
}

// volume は出力する音量を返す。
func (e *envelope) volume() uint8 {
	if e.constant {
		return e.param
	}
	return e.decayLevel
}

func (e *envelope) saveState(w *state.Writer) {
	w.Bool(e.start)
	e.div.saveState(w)
	w.U8(e.decayLevel)
	w.Bool(e.loop)
	w.Bool(e.constant)
	w.U8(e.param)
}

func (e *envelope) loadState(r *state.Reader) {
	e.start = r.Bool()
	e.div.loadState(r)
	e.decayLevel = r.U8()
	e.loop = r.Bool()
	e.constant = r.Bool()
	e.param = r.U8()
}

// sweep は Pulse の周期を自動で変えるユニット。
type sweep struct {
	enabled bool
	negate  bool
	shift   uint8
	div     divider
	reload  bool
	// ones は負にするときの補数の取り方。Pulse 1 は 1 の補数、
	// Pulse 2 は 2 の補数である。両者の唯一の違いがこれである。
	ones bool
}

// write は $4001・$4005 を反映する。
func (s *sweep) write(v uint8) {
	s.enabled = v&0x80 != 0
	s.div.setPeriod(uint16(v>>4) & 0x07)
	s.negate = v&0x08 != 0
	s.shift = v & 0x07
	s.reload = true
}

func (s *sweep) saveState(w *state.Writer) {
	w.Bool(s.enabled)
	w.Bool(s.negate)
	w.U8(s.shift)
	s.div.saveState(w)
	w.Bool(s.reload)
	w.Bool(s.ones)
}

func (s *sweep) loadState(r *state.Reader) {
	s.enabled = r.Bool()
	s.negate = r.Bool()
	s.shift = r.U8()
	s.div.loadState(r)
	s.reload = r.Bool()
	s.ones = r.Bool()
}
