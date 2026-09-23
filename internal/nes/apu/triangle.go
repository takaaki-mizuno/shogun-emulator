package apu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// triangleSequence は 32 ステップの三角波。
var triangleSequence = [32]uint8{
	15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0,
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
}

// ultrasonicPeriod はこれ未満の周期を超音波域とみなす境界。
const ultrasonicPeriod = 2

// triangleChannel は三角波チャンネル。
type triangleChannel struct {
	linearCounter uint8
	linearReload  uint8
	reloadFlag    bool
	control       bool
	// timer は CPU サイクルごとにクロックする。Pulse と異なる。
	timer  divider
	seqPos uint8
	length lengthCounter

	// silenceUltrasonic が true のとき、超音波域でシーケンサを止める。
	//
	// 実機はこの範囲でも動作し、出力は 7 と 8 の中間に平均化される。
	// 止めると Mega Man 1・2 のポップノイズが消える代わりに精度が落ちる。
	silenceUltrasonic bool
}

// writeLinear は $4008 を反映する。
func (t *triangleChannel) writeLinear(v uint8) {
	t.control = v&0x80 != 0
	t.length.halt = t.control
	t.linearReload = v & 0x7F
}

// writeTimerLow は $400A を反映する。
func (t *triangleChannel) writeTimerLow(v uint8) {
	t.timer.setPeriod(t.timer.period&0x0700 | uint16(v))
}

// writeTimerHigh は $400B を反映する。リニアカウンタのリロードフラグを立てる。
func (t *triangleChannel) writeTimerHigh(v uint8) {
	t.timer.setPeriod(uint16(v&0x07)<<8 | t.timer.period&0x00FF)
	t.length.load(v >> 3)
	t.reloadFlag = true
}

// clockLinear は quarter frame ごとに呼ぶ。
//
// 2 つの手順の順序を守る。control が立っている間はリロードフラグが
// クリアされず、毎回のクロックでリロードされ続ける（= halt）。
func (t *triangleChannel) clockLinear() {
	if t.reloadFlag {
		t.linearCounter = t.linearReload
	} else if t.linearCounter > 0 {
		t.linearCounter--
	}
	if !t.control {
		t.reloadFlag = false
	}
}

// stepTimer は CPU サイクルごとに呼ぶ。
//
// シーケンサはリニアカウンタとレングスカウンタの両方が非ゼロのときだけ
// 進む。止まっているあいだは最後の値を出力し続ける。0 にはしない。
func (t *triangleChannel) stepTimer() {
	if t.linearCounter == 0 || !t.length.active() {
		return
	}
	if t.silenceUltrasonic && t.timer.period < ultrasonicPeriod {
		return
	}
	if t.timer.clock() {
		t.seqPos = (t.seqPos + 1) & 31
	}
}

// output はミキサーへ出す値を返す。
func (t *triangleChannel) output() uint8 {
	return triangleSequence[t.seqPos]
}

func (t *triangleChannel) saveState(w *state.Writer) {
	end := w.Section("triangle")
	w.U8(t.linearCounter)
	w.U8(t.linearReload)
	w.Bool(t.reloadFlag)
	w.Bool(t.control)
	t.timer.saveState(w)
	w.U8(t.seqPos)
	t.length.saveState(w)
	end()
}

func (t *triangleChannel) loadState(r *state.Reader) {
	end := r.RequireSection("triangle")
	t.linearCounter = r.U8()
	t.linearReload = r.U8()
	t.reloadFlag = r.Bool()
	t.control = r.Bool()
	t.timer.loadState(r)
	t.seqPos = r.U8() & 31
	t.length.loadState(r)
	end()
}
