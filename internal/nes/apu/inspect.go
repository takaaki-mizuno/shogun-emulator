package apu

// ミュートのビット。SetMute に渡す。
const (
	MutePulse1 uint8 = 1 << iota
	MutePulse2
	MuteTriangle
	MuteNoise
	MuteDMC
)

// SetMute はミュートするチャンネルをビットで指定する（設計書 05 編 §5.9）。
//
// ミュートは合成にしか影響せず、チャンネルの状態は変えない。デバッガの
// APU 状態ビューアが使う。
func (a *APU) SetMute(mask uint8) { a.mute = mask }

// Mute はミュートしているチャンネルのビットを返す。
func (a *APU) Mute() uint8 { return a.mute }

// PulseInspection は Pulse チャンネルの現在値。
type PulseInspection struct {
	Period        uint16
	Duty          uint8
	Volume        uint8 // エンベロープの出力
	Constant      bool  // 一定音量か
	EnvelopeLoop  bool
	EnvelopeParam uint8
	Length        uint8
	Halt          bool
	SweepEnabled  bool
	SweepNegate   bool
	SweepShift    uint8
	SweepPeriod   uint16
	Output        uint8
}

// TriangleInspection は Triangle チャンネルの現在値。
type TriangleInspection struct {
	Period        uint16
	LinearCounter uint8
	LinearReload  uint8
	Length        uint8
	Sequence      uint8
	Output        uint8
}

// NoiseInspection は Noise チャンネルの現在値。
type NoiseInspection struct {
	Period uint16
	Mode   bool // true で短周期
	LFSR   uint16
	Volume uint8
	Length uint8
	Output uint8
}

// DMCInspection は DMC チャンネルの現在値。
type DMCInspection struct {
	OutputLevel    uint8
	Rate           uint8
	SampleAddr     uint16
	SampleLength   uint16
	CurrentAddr    uint16
	BytesRemaining uint16
	Loop           bool
	IRQEnabled     bool
	IRQ            bool
}

// FrameInspection はフレームカウンタの現在値。
type FrameInspection struct {
	FiveStep   bool
	APUCycles  uint32
	IRQInhibit bool
	IRQ        bool
}

// Inspection はデバッガへ見せる APU の現在値。
type Inspection struct {
	Pulse    [2]PulseInspection
	Triangle TriangleInspection
	Noise    NoiseInspection
	DMC      DMCInspection
	Frame    FrameInspection
	Mute     uint8
}

// Inspect は各チャンネルの現在値を返す。状態を変えない。
func (a *APU) Inspect() Inspection {
	return Inspection{
		Pulse:    [2]PulseInspection{a.pulse1.inspect(), a.pulse2.inspect()},
		Triangle: a.triangle.inspect(),
		Noise:    a.noise.inspect(),
		DMC: DMCInspection{
			OutputLevel:    a.dmc.outputLevel,
			Rate:           a.dmc.rateIndex,
			SampleAddr:     a.dmc.sampleAddr,
			SampleLength:   a.dmc.sampleLength,
			CurrentAddr:    a.dmc.currentAddr,
			BytesRemaining: a.dmc.bytesRemaining,
			Loop:           a.dmc.loop,
			IRQEnabled:     a.dmc.irqEnable,
			IRQ:            a.dmc.irqFlag,
		},
		Frame: FrameInspection{
			FiveStep:   a.frame.mode == 1,
			APUCycles:  a.frame.apuCycles,
			IRQInhibit: a.frame.irqInhibit,
			IRQ:        a.frame.irqFlag,
		},
		Mute: a.mute,
	}
}

// inspect は Pulse の現在値を返す。
func (p *pulseChannel) inspect() PulseInspection {
	return PulseInspection{
		Period:        p.timer.period,
		Duty:          p.duty,
		Volume:        p.env.volume(),
		Constant:      p.env.constant,
		EnvelopeLoop:  p.env.loop,
		EnvelopeParam: p.env.param,
		Length:        p.length.value,
		Halt:          p.length.halt,
		SweepEnabled:  p.swp.enabled,
		SweepNegate:   p.swp.negate,
		SweepShift:    p.swp.shift,
		SweepPeriod:   p.swp.div.period,
		Output:        p.output(),
	}
}

// inspect は Triangle の現在値を返す。
func (t *triangleChannel) inspect() TriangleInspection {
	return TriangleInspection{
		Period:        t.timer.period,
		LinearCounter: t.linearCounter,
		LinearReload:  t.linearReload,
		Length:        t.length.value,
		Sequence:      t.seqPos,
		Output:        t.output(),
	}
}

// inspect は Noise の現在値を返す。
func (n *noiseChannel) inspect() NoiseInspection {
	return NoiseInspection{
		Period: n.timer.period,
		Mode:   n.mode,
		LFSR:   n.lfsr,
		Volume: n.env.volume(),
		Length: n.length.value,
		Output: n.output(),
	}
}
