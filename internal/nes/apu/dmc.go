package apu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// maxDMCLevel は出力レベルの上限。7 bit 符号なし。
const maxDMCLevel = 127

// dmcChannel はデルタ変調チャンネル。
//
// 出力レベルはチャンネルの有効・無効に関係なくミキサーへ送る。
// $4011 への書き込みで直接動かせるため、PCM の再生にも使われる。
type dmcChannel struct {
	region *region.Region
	bus    Bus

	rateIndex uint8
	// timer は CPU サイクルごとにクロックする。
	timer     divider
	loop      bool
	irqEnable bool
	irqFlag   bool

	// メモリリーダー
	sampleAddr     uint16
	sampleLength   uint16
	currentAddr    uint16
	bytesRemaining uint16
	sampleBuffer   uint8
	bufferFilled   bool
	// fetchPending はバスへ読み出しを要求してから受け取るまで true。
	fetchPending bool
	// firstFetch はサンプルを始めてから最初の読み出しかを表す。
	//
	// $4015 でチャンネルを有効にした直後の読み出しと、再生中に
	// バッファが空になったことへの応答とでは、CPU を止める位置が違う。
	firstFetch bool

	// 出力ユニット
	shiftReg      uint8
	bitsRemaining uint8
	outputLevel   uint8
	silence       bool
}

// newDMCChannel はチャンネルを作る。
func newDMCChannel(r *region.Region) dmcChannel {
	return dmcChannel{region: r, silence: true, bitsRemaining: 8}
}

// setBus はサンプルの読み出し先を設定する。
func (d *dmcChannel) setBus(b Bus) { d.bus = b }

// writeControl は $4010 を反映する。
func (d *dmcChannel) writeControl(v uint8) {
	d.irqEnable = v&0x80 != 0
	d.loop = v&0x40 != 0
	d.rateIndex = v & 0x0F
	// 表の値は CPU サイクル数である。分周器は P+1 で数えるため 1 を引く。
	d.timer.setPeriod(d.region.DMCRates[d.rateIndex] - 1)
	if !d.irqEnable {
		d.irqFlag = false
	}
}

// writeLoad は $4011 を反映する。出力レベルを直接書き換える。
func (d *dmcChannel) writeLoad(v uint8) { d.outputLevel = v & 0x7F }

// writeAddr は $4012 を反映する。サンプルアドレス = $C000 + A*64。
func (d *dmcChannel) writeAddr(v uint8) { d.sampleAddr = 0xC000 | uint16(v)<<6 }

// writeLength は $4013 を反映する。サンプル長 = L*16 + 1 バイト。
func (d *dmcChannel) writeLength(v uint8) { d.sampleLength = uint16(v)<<4 | 1 }

// setEnabled は $4015 の DMC ビットを反映する。
//
// 0 を書くと残りバイト数が 0 になる。1 を書いたときは、残りバイト数が
// 0 のときだけサンプルを再スタートする。再生中のサンプルは中断しない。
func (d *dmcChannel) setEnabled(v bool) {
	d.irqFlag = false
	if !v {
		d.bytesRemaining = 0
		return
	}
	if d.bytesRemaining == 0 {
		d.restart()
	}
}

// restart はサンプルの先頭から読み直す。
func (d *dmcChannel) restart() {
	d.currentAddr = d.sampleAddr
	d.bytesRemaining = d.sampleLength
	d.firstFetch = true
}

// active は残りバイト数があるかを返す。$4015 の bit 4 になる。
func (d *dmcChannel) active() bool { return d.bytesRemaining > 0 }

// stepTimer は CPU サイクルごとに呼ぶ。
func (d *dmcChannel) stepTimer() {
	d.fillBuffer()
	if d.timer.clock() {
		d.clockOutput()
	}
}

// fillBuffer はサンプルバッファが空なら読み出しを要求する。
//
// ここでは読まない。バスが CPU を停止させ、その中の get サイクルで
// 読んで completeFetch を呼ぶ。残りバイト数が減るのもそのときである。
func (d *dmcChannel) fillBuffer() {
	if d.bufferFilled || d.fetchPending || d.bytesRemaining == 0 || d.bus == nil {
		return
	}
	d.fetchPending = true
	d.bus.RequestDMCFetch(d.currentAddr, !d.firstFetch)
	d.firstFetch = false
}

// completeFetch はバスが読み終えたサンプルを受け取る。
func (d *dmcChannel) completeFetch(v uint8) {
	if !d.fetchPending {
		return
	}
	d.fetchPending = false
	d.sampleBuffer = v
	d.bufferFilled = true

	if d.currentAddr == 0xFFFF {
		d.currentAddr = 0x8000
	} else {
		d.currentAddr++
	}

	if d.bytesRemaining > 0 {
		d.bytesRemaining--
	}
	if d.bytesRemaining != 0 {
		return
	}
	switch {
	case d.loop:
		d.restart()
	case d.irqEnable:
		d.irqFlag = true
	}
}

// clockOutput は出力ユニットを 1 段進める。
//
// 3 つの手順の順序を守る。出力サイクルは中断せず、必ず 8 bit を
// 出し切ってから次のサイクルが始まる。
func (d *dmcChannel) clockOutput() {
	if !d.silence {
		if d.shiftReg&1 != 0 {
			// 範囲を出るときは変更しない。
			if d.outputLevel <= maxDMCLevel-2 {
				d.outputLevel += 2
			}
		} else if d.outputLevel >= 2 {
			d.outputLevel -= 2
		}
	}
	d.shiftReg >>= 1

	d.bitsRemaining--
	if d.bitsRemaining == 0 {
		d.startOutputCycle()
	}
}

// startOutputCycle は新しい出力サイクルを始める。
func (d *dmcChannel) startOutputCycle() {
	d.bitsRemaining = 8
	if !d.bufferFilled {
		d.silence = true
		return
	}
	d.silence = false
	d.shiftReg = d.sampleBuffer
	d.bufferFilled = false
}

// clearIRQ は $4015 への書き込みで割り込みフラグを落とす。
func (d *dmcChannel) clearIRQ() { d.irqFlag = false }

func (d *dmcChannel) saveState(w *state.Writer) {
	end := w.Section("dmc")
	w.U8(d.rateIndex)
	d.timer.saveState(w)
	w.Bool(d.loop)
	w.Bool(d.irqEnable)
	w.Bool(d.irqFlag)
	w.U16(d.sampleAddr)
	w.U16(d.sampleLength)
	w.U16(d.currentAddr)
	w.U16(d.bytesRemaining)
	w.U8(d.sampleBuffer)
	w.Bool(d.bufferFilled)
	w.Bool(d.fetchPending)
	w.Bool(d.firstFetch)
	w.U8(d.shiftReg)
	w.U8(d.bitsRemaining)
	w.U8(d.outputLevel)
	w.Bool(d.silence)
	end()
}

func (d *dmcChannel) loadState(r *state.Reader) {
	end := r.RequireSection("dmc")
	d.rateIndex = r.U8()
	d.timer.loadState(r)
	d.loop = r.Bool()
	d.irqEnable = r.Bool()
	d.irqFlag = r.Bool()
	d.sampleAddr = r.U16()
	d.sampleLength = r.U16()
	d.currentAddr = r.U16()
	d.bytesRemaining = r.U16()
	d.sampleBuffer = r.U8()
	d.bufferFilled = r.Bool()
	d.fetchPending = r.Bool()
	d.firstFetch = r.Bool()
	d.shiftReg = r.U8()
	d.bitsRemaining = r.U8()
	d.outputLevel = r.U8()
	d.silence = r.Bool()
	end()
}
