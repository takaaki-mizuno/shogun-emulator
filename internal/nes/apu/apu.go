// Package apu は APU（音源とフレームカウンタ）を実装する。
//
// 1 CPU サイクルを単位として進める。APU サイクル（2 CPU サイクル）を
// 単位とする処理は evenCycle で判定する。CPU サイクル単位とするのは、
// Triangle のタイマーが CPU クロックで動くこと、DMC の DMA 要求が
// CPU サイクル単位で起こること、$4017 の書き込みによるリセットが
// 3 または 4 CPU サイクル後であることによる。
package apu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// Bus は DMC がサンプルを読むために使う。
type Bus interface {
	// RequestDMCFetch は DMC のサンプル 1 バイトの読み出しを要求する。
	//
	// バスは CPU を停止させ、その中の get サイクルで addr を読み、
	// 結果を CompleteDMCFetch で返す。読み出しを要求した時点では
	// 読まない。実機でも読むのは停止の終わりの 1 サイクルである。
	//
	// reload は、再生中にサンプルバッファが空になったことへの応答かを
	// 表す。チャンネルを有効にした直後の最初の読み出しでは false になる。
	RequestDMCFetch(addr uint16, reload bool)
}

// Output はミキサーの出力を受け取る先。
//
// 1 CPU サイクルごとに 1 回呼ばれる。nil のときは呼ばない。音を出さない
// 実行（テスト ROM ランナー）で、リサンプラを用意させないためである。
type Output interface {
	// WriteSample は 1 CPU サイクル分のミキサー出力を受け取る。
	WriteSample(v float32)
}

// APU は音声処理ユニット。
type APU struct {
	region *region.Region
	bus    Bus
	out    Output

	// OnFrameStep はフレームカウンタが 1/4 または 1/2 フレームの信号を
	// 出したときに呼ばれる。nil のとき呼ばない。デバッガのログが使う。
	OnFrameStep func(quarter, half bool)

	pulse1, pulse2 pulseChannel
	triangle       triangleChannel
	noise          noiseChannel
	dmc            dmcChannel
	frame          frameCounter

	// cycles は電源投入からの CPU サイクル数。
	cycles uint64
	// evenCycle は偶数サイクルかを表す。APU の各部品は 2 CPU サイクル
	// ごとに動くため、位相を保持する必要がある。
	evenCycle bool

	// volumes はチャンネル別の音量。1.0 が等倍。
	volumes ChannelVolumes
	// mute はミュートするチャンネルのビット。出力段の設定であり保存しない。
	mute uint8
}

// ChannelVolumes はチャンネル別の音量。設定から渡す。
type ChannelVolumes struct {
	Pulse1, Pulse2, Triangle, Noise, DMC float32
}

// DefaultVolumes は全チャンネルを等倍にした音量を返す。
func DefaultVolumes() ChannelVolumes {
	return ChannelVolumes{Pulse1: 1, Pulse2: 1, Triangle: 1, Noise: 1, DMC: 1}
}

// New は APU を作る。
func New(r *region.Region) *APU {
	return &APU{
		region:   r,
		pulse1:   newPulseChannel(true),
		pulse2:   newPulseChannel(false),
		noise:    newNoiseChannel(r),
		dmc:      newDMCChannel(r),
		frame:    newFrameCounter(r),
		volumes:  DefaultVolumes(),
		triangle: triangleChannel{},
	}
}

// SetBus は DMC のサンプル読み出し先を設定する。
func (a *APU) SetBus(b Bus) {
	a.bus = b
	a.dmc.setBus(b)
}

// SetOutput はミキサーの出力先を設定する。
func (a *APU) SetOutput(o Output) { a.out = o }

// CompleteDMCFetch はバスが読み終えたサンプルを受け取る。
//
// バスの get サイクルから呼ばれる。ここで残りバイト数が減り、
// ループと割り込みの判定が行われる。
func (a *APU) CompleteDMCFetch(v uint8) { a.dmc.completeFetch(v) }

// SetVolumes はチャンネル別の音量を設定する。
func (a *APU) SetVolumes(v ChannelVolumes) { a.volumes = v }

// SetSilenceUltrasonicTriangle は超音波域の Triangle を止めるかを設定する。
func (a *APU) SetSilenceUltrasonicTriangle(v bool) { a.triangle.silenceUltrasonic = v }

// PowerOn は電源投入時の状態にする。
//
// 設計書 05 編 §5.11 の表に従う。電源投入とリセットの直後は、最初の
// コードが実行される 10 CPU サイクル前に $4017 へ 0 が書かれた状態と
// 等価にする。
func (a *APU) PowerOn() {
	r := a.region
	vol := a.volumes
	mute := a.mute
	ultrasonic := a.triangle.silenceUltrasonic
	bus, out := a.bus, a.out
	onStep := a.OnFrameStep

	*a = *New(r)
	a.volumes = vol
	a.mute = mute
	a.triangle.silenceUltrasonic = ultrasonic
	a.out = out
	a.OnFrameStep = onStep
	a.SetBus(bus)

	// APU サイクルの初期位相。1 CPU サイクル目の step で反転するため、
	// 偶数番目の CPU サイクルが APU サイクルになる。この位相でのみ
	// `01.len_ctr` と `08.irq_timing` が同時に合格する。
	a.evenCycle = true
	a.writeStatus(0)
	a.frame.reset()
}

// Reset はリセットを掛ける。
//
// レジスタの内容は変えない。$4015 へ 0 を書いたのと同じ効果を与え、
// DMC の出力レベルの最下位ビットだけを残す。
func (a *APU) Reset() {
	a.writeStatus(0)
	a.dmc.outputLevel &= 1
	a.triangle.seqPos = 0
	a.frame.reset()
}

// Step は 1 CPU サイクル進める。バスから 1 CPU サイクルごとに 1 回呼ばれる。
func (a *APU) Step() {
	a.cycles++
	a.evenCycle = !a.evenCycle

	// フレームカウンタの信号を先に処理する。
	if k := a.frame.step(a.evenCycle); k.quarter || k.half {
		if k.quarter {
			a.clockQuarterFrame()
		}
		if k.half {
			a.clockHalfFrame()
		}
		if a.OnFrameStep != nil {
			a.OnFrameStep(k.quarter, k.half)
		}
	}

	// Triangle・Noise・DMC は CPU サイクルごと、Pulse は APU サイクルごと。
	a.triangle.stepTimer()
	a.noise.stepTimer()
	a.dmc.stepTimer()
	if a.evenCycle {
		a.pulse1.stepTimer()
		a.pulse2.stepTimer()
	}

	if a.out != nil {
		a.out.WriteSample(a.mix())
	}
}

// clockQuarterFrame はエンベロープと Triangle のリニアカウンタを進める。
func (a *APU) clockQuarterFrame() {
	a.pulse1.env.clock()
	a.pulse2.env.clock()
	a.noise.env.clock()
	a.triangle.clockLinear()
}

// clockHalfFrame はレングスカウンタとスイープを進める。
func (a *APU) clockHalfFrame() {
	a.pulse1.length.clock()
	a.pulse2.length.clock()
	a.triangle.length.clock()
	a.noise.length.clock()
	a.pulse1.clockSweep()
	a.pulse2.clockSweep()
}

// Cycles は電源投入からの CPU サイクル数を返す。
func (a *APU) Cycles() uint64 { return a.cycles }

// EvenCycle は偶数サイクルかを返す。
func (a *APU) EvenCycle() bool { return a.evenCycle }

// FrameIRQ はフレームカウンタの IRQ フラグを返す。デバッガの表示に使う。
func (a *APU) FrameIRQ() bool { return a.frame.irqFlag }

// DMCIRQ は DMC の IRQ フラグを返す。デバッガの表示に使う。
func (a *APU) DMCIRQ() bool { return a.dmc.irqFlag }

// IRQAsserted はフレーム IRQ と DMC IRQ のいずれかが立っているかを返す。
func (a *APU) IRQAsserted() bool { return a.frame.irqFlag || a.dmc.irqFlag }

// ReadStatus は $4015 を読む。
//
// フレーム割り込みフラグをクリアする。DMC 側のフラグはクリアしない。
func (a *APU) ReadStatus() uint8 {
	v := a.PeekStatus()
	a.frame.clearIRQ()
	return v
}

// PeekStatus は副作用を起こさずに $4015 を読む。
func (a *APU) PeekStatus() uint8 {
	var v uint8
	if a.pulse1.length.active() {
		v |= 0x01
	}
	if a.pulse2.length.active() {
		v |= 0x02
	}
	if a.triangle.length.active() {
		v |= 0x04
	}
	if a.noise.length.active() {
		v |= 0x08
	}
	if a.dmc.active() {
		v |= 0x10
	}
	if a.frame.irqFlag {
		v |= 0x40
	}
	if a.dmc.irqFlag {
		v |= 0x80
	}
	return v
}

// WriteRegister は $4000-$4013 と $4015・$4017 へ書く。
func (a *APU) WriteRegister(addr uint16, v uint8) {
	switch addr {
	case 0x4000:
		a.pulse1.writeControl(v)
	case 0x4001:
		a.pulse1.writeSweep(v)
	case 0x4002:
		a.pulse1.writeTimerLow(v)
	case 0x4003:
		a.pulse1.writeTimerHigh(v)

	case 0x4004:
		a.pulse2.writeControl(v)
	case 0x4005:
		a.pulse2.writeSweep(v)
	case 0x4006:
		a.pulse2.writeTimerLow(v)
	case 0x4007:
		a.pulse2.writeTimerHigh(v)

	case 0x4008:
		a.triangle.writeLinear(v)
	case 0x400A:
		a.triangle.writeTimerLow(v)
	case 0x400B:
		a.triangle.writeTimerHigh(v)

	case 0x400C:
		a.noise.writeControl(v)
	case 0x400E:
		a.noise.writeMode(v)
	case 0x400F:
		a.noise.writeLength(v)

	case 0x4010:
		a.dmc.writeControl(v)
	case 0x4011:
		a.dmc.writeLoad(v)
	case 0x4012:
		a.dmc.writeAddr(v)
	case 0x4013:
		a.dmc.writeLength(v)

	case 0x4015:
		a.writeStatus(v)
	case 0x4017:
		a.frame.write(v, a.evenCycle)
	}
}

// writeStatus は $4015 への書き込みを反映する。
func (a *APU) writeStatus(v uint8) {
	a.pulse1.length.setEnabled(v&0x01 != 0)
	a.pulse2.length.setEnabled(v&0x02 != 0)
	a.triangle.length.setEnabled(v&0x04 != 0)
	a.noise.length.setEnabled(v&0x08 != 0)
	a.dmc.setEnabled(v&0x10 != 0)
}

// SaveState は状態を書く。
func (a *APU) SaveState(w *state.Writer) {
	end := w.Section("apu")
	w.U64(a.cycles)
	w.Bool(a.evenCycle)
	a.pulse1.saveState(w)
	a.pulse2.saveState(w)
	a.triangle.saveState(w)
	a.noise.saveState(w)
	a.dmc.saveState(w)
	a.frame.saveState(w)
	end()
}

// LoadState は状態を読む。
func (a *APU) LoadState(r *state.Reader) error {
	end := r.RequireSection("apu")
	a.cycles = r.U64()
	a.evenCycle = r.Bool()
	a.pulse1.loadState(r)
	a.pulse2.loadState(r)
	a.triangle.loadState(r)
	a.noise.loadState(r)
	a.dmc.loadState(r)
	a.frame.loadState(r)
	end()
	return r.Err()
}
