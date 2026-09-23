package audio

import "math"

// onePoleHighPass は 1 次のハイパスフィルタ。
//
// 実機は DAC の後にアナログのフィルタを持つ。その構成を近似する。
type onePoleHighPass struct {
	// alpha は係数。カットオフ周波数とサンプリングレートから決まる。
	alpha float32
	prevIn,
	prevOut float32
}

// newHighPass はカットオフ周波数 hz のハイパスフィルタを作る。
func newHighPass(hz, sampleRate float64) onePoleHighPass {
	// RC 回路の離散化。alpha = RC / (RC + dt)
	rc := 1 / (2 * math.Pi * hz)
	dt := 1 / sampleRate
	return onePoleHighPass{alpha: float32(rc / (rc + dt))}
}

// process は 1 サンプル通す。
func (f *onePoleHighPass) process(in float32) float32 {
	out := f.alpha * (f.prevOut + in - f.prevIn)
	f.prevIn = in
	f.prevOut = out
	return out
}

// reset は状態を消す。
func (f *onePoleHighPass) reset() {
	f.prevIn = 0
	f.prevOut = 0
}

// onePoleLowPass は 1 次のローパスフィルタ。
//
// ダウンサンプルの前の折り返し防止も兼ねる。
type onePoleLowPass struct {
	alpha   float32
	prevOut float32
}

// newLowPass はカットオフ周波数 hz のローパスフィルタを作る。
func newLowPass(hz, sampleRate float64) onePoleLowPass {
	// alpha = dt / (RC + dt)
	rc := 1 / (2 * math.Pi * hz)
	dt := 1 / sampleRate
	return onePoleLowPass{alpha: float32(dt / (rc + dt))}
}

// process は 1 サンプル通す。
func (f *onePoleLowPass) process(in float32) float32 {
	f.prevOut += f.alpha * (in - f.prevOut)
	return f.prevOut
}

// reset は状態を消す。
func (f *onePoleLowPass) reset() { f.prevOut = 0 }
