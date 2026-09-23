package apu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// noiseChannel はノイズチャンネル。
type noiseChannel struct {
	region *region.Region

	// lfsr は 15 bit の線形帰還シフトレジスタ。
	lfsr uint16
	mode bool
	// timer は CPU サイクルごとにクロックする。周期は表から取る。
	timer  divider
	env    envelope
	length lengthCounter
}

// newNoiseChannel はチャンネルを作る。
func newNoiseChannel(r *region.Region) noiseChannel {
	return noiseChannel{region: r, lfsr: 1}
}

// writeControl は $400C を反映する。
func (n *noiseChannel) writeControl(v uint8) {
	n.length.halt = v&0x20 != 0
	n.env.write(v)
}

// writeMode は $400E を反映する。
func (n *noiseChannel) writeMode(v uint8) {
	n.mode = v&0x80 != 0
	// 表の値は CPU サイクル数である。分周器は P+1 で数えるため 1 を引く。
	n.timer.setPeriod(n.region.NoisePeriods[v&0x0F] - 1)
}

// writeLength は $400F を反映する。
func (n *noiseChannel) writeLength(v uint8) {
	n.length.load(v >> 3)
	n.env.restart()
}

// stepTimer は CPU サイクルごとに呼ぶ。
func (n *noiseChannel) stepTimer() {
	if n.timer.clock() {
		n.clockLFSR()
	}
}

// clockLFSR はシフトレジスタを 1 回進める。
//
// モードフラグが立っているとき bit 6、そうでないとき bit 1 との排他的
// 論理和を帰還にする。モードを立てると周期が 31 または 93 ステップになる。
func (n *noiseChannel) clockLFSR() {
	var other uint16
	if n.mode {
		other = n.lfsr >> 6 & 1
	} else {
		other = n.lfsr >> 1 & 1
	}
	fb := n.lfsr&1 ^ other
	n.lfsr >>= 1
	n.lfsr |= fb << 14
}

// output はミキサーへ出す音量を返す。
func (n *noiseChannel) output() uint8 {
	if n.lfsr&1 != 0 || !n.length.active() {
		return 0
	}
	return n.env.volume()
}

func (n *noiseChannel) saveState(w *state.Writer) {
	end := w.Section("noise")
	w.U16(n.lfsr)
	w.Bool(n.mode)
	n.timer.saveState(w)
	n.env.saveState(w)
	n.length.saveState(w)
	end()
}

func (n *noiseChannel) loadState(r *state.Reader) {
	end := r.RequireSection("noise")
	n.lfsr = r.U16()
	n.mode = r.Bool()
	n.timer.loadState(r)
	n.env.loadState(r)
	n.length.loadState(r)
	end()
}
