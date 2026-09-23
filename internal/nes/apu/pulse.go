package apu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// dutySequences はデューティごとの波形。
//
// 実機のカウンタは 0 で初期化されて下向きに数えるため、この表を
// 0, 7, 6, 5, 4, 3, 2, 1 の順に読むと出力波形になる。表そのものは
// docs/research/04_apu.md の 4.4 節の「ルックアップテーブル（内部）」である。
var dutySequences = [4][8]uint8{
	{0, 0, 0, 0, 0, 0, 0, 1}, // 12.5%
	{0, 0, 0, 0, 0, 0, 1, 1}, // 25%
	{0, 0, 0, 0, 1, 1, 1, 1}, // 50%
	{1, 1, 1, 1, 1, 1, 0, 0}, // 25% 反転
}

// minPulsePeriod はこれ未満の周期でミュートする境界。
const minPulsePeriod = 8

// maxPulsePeriod は目標周期がこれを超えるとミュートする境界。
const maxPulsePeriod = 0x7FF

// pulseChannel は矩形波チャンネル。
type pulseChannel struct {
	duty   uint8
	seqPos uint8
	// timer は APU サイクルごとにクロックする。
	timer  divider
	env    envelope
	swp    sweep
	length lengthCounter
}

// newPulseChannel はチャンネルを作る。ones が Pulse 1 と 2 を分ける。
func newPulseChannel(ones bool) pulseChannel {
	return pulseChannel{swp: sweep{ones: ones}}
}

// writeControl は $4000 / $4004 を反映する。
//
// デューティを変えてもシーケンサの現在位置は変わらない。
func (p *pulseChannel) writeControl(v uint8) {
	p.duty = v >> 6 & 0x03
	p.length.halt = v&0x20 != 0
	p.env.write(v)
}

// writeSweep は $4001 / $4005 を反映する。
func (p *pulseChannel) writeSweep(v uint8) { p.swp.write(v) }

// writeTimerLow は $4002 / $4006 を反映する。
func (p *pulseChannel) writeTimerLow(v uint8) {
	p.timer.setPeriod(p.timer.period&0x0700 | uint16(v))
}

// writeTimerHigh は $4003 / $4007 を反映する。
//
// シーケンサを先頭へ戻し、エンベロープを再スタートする。タイマーの
// 分周器はリセットしない。
func (p *pulseChannel) writeTimerHigh(v uint8) {
	p.timer.setPeriod(uint16(v&0x07)<<8 | p.timer.period&0x00FF)
	p.length.load(v >> 3)
	p.seqPos = 0
	p.env.restart()
}

// stepTimer は APU サイクルごとに呼ぶ。
func (p *pulseChannel) stepTimer() {
	if p.timer.clock() {
		// カウンタは下向きに数える。
		p.seqPos = (p.seqPos + 7) & 7
	}
}

// targetPeriod はスイープが目指す周期を返す。
//
// スイープが無効でも継続的に計算する。オーバーフローによるミュートが
// スイープの有効・無効に関わらず起こるためである。
func (p *pulseChannel) targetPeriod() int {
	change := int(p.timer.period >> p.swp.shift)
	if p.swp.negate {
		if p.swp.ones {
			// Pulse 1 は 1 の補数を加える。
			change = -change - 1
		} else {
			// Pulse 2 は 2 の補数を加える。
			change = -change
		}
	}
	t := int(p.timer.period) + change
	if t < 0 {
		t = 0
	}
	return t
}

// muted はスイープユニットがチャンネルを消音しているかを返す。
func (p *pulseChannel) muted() bool {
	return p.timer.period < minPulsePeriod || p.targetPeriod() > maxPulsePeriod
}

// clockSweep は half frame ごとに呼ぶ。
func (p *pulseChannel) clockSweep() {
	zero := p.swp.div.counter == 0
	if zero && p.swp.enabled && p.swp.shift != 0 && !p.muted() {
		p.timer.setPeriod(uint16(p.targetPeriod()))
	}
	if zero || p.swp.reload {
		p.swp.div.reload()
		p.swp.reload = false
		return
	}
	p.swp.div.counter--
}

// output はミキサーへ出す音量を返す。
func (p *pulseChannel) output() uint8 {
	if dutySequences[p.duty][p.seqPos] == 0 || p.muted() || !p.length.active() {
		return 0
	}
	return p.env.volume()
}

func (p *pulseChannel) saveState(w *state.Writer) {
	end := w.Section("pulse")
	w.U8(p.duty)
	w.U8(p.seqPos)
	p.timer.saveState(w)
	p.env.saveState(w)
	p.swp.saveState(w)
	p.length.saveState(w)
	end()
}

func (p *pulseChannel) loadState(r *state.Reader) {
	end := r.RequireSection("pulse")
	p.duty = r.U8()
	p.seqPos = r.U8() & 7
	p.timer.loadState(r)
	p.env.loadState(r)
	p.swp.loadState(r)
	p.length.loadState(r)
	end()
}
