package audio

// フィルタプロファイル。設定 `audio.filterProfile` の値。
const (
	// ProfileNES は NES 本体相当。90 Hz と 440 Hz のハイパス、14 kHz のローパス。
	ProfileNES = "nes"
	// ProfileFamicom はファミコン相当。37 Hz のハイパスのみ。
	ProfileFamicom = "famicom"
	// ProfileNone はフィルタなし。
	ProfileNone = "none"
)

// Sink はリサンプルした結果を受け取る先。
type Sink interface {
	// WriteSample は 1 サンプルを書く。左右は同じ値である。
	//
	// 書き込み側を待たせることで進行を律速する実装があるため、
	// 戻り値で続行できるかを返す。
	WriteSample(v int16) bool
}

// Resampler は CPU クロックのサンプル列を出力レートへ変換する。
//
// APU の出力は CPU クロック（約 1.79 MHz）で変化する。これを 48 kHz へ
// 落とす。ローパスが折り返しの防止を兼ね、ダウンサンプルは線形補間で行う。
type Resampler struct {
	hp1, hp2 onePoleHighPass
	lp       onePoleLowPass
	useHP1   bool
	useHP2   bool
	useLP    bool

	// step は出力 1 サンプルあたりの入力サンプル数。
	step float64
	// baseStep は速度倍率 1.0 のときの step。
	baseStep float64
	// accum は次の出力サンプルまでの残り。
	accum float64
	// prev は直前の入力サンプル。線形補間に使う。
	prev float32

	// muted が true のとき 0 を出力する。
	muted bool

	sink Sink
}

// NewResampler はリサンプラを作る。
//
// inputRate は入力のサンプリングレート（CPU クロック）、outputRate は
// 出力のサンプリングレートである。
func NewResampler(inputRate, outputRate float64, profile string, sink Sink) *Resampler {
	r := &Resampler{
		baseStep: inputRate / outputRate,
		sink:     sink,
	}
	r.step = r.baseStep
	r.setProfile(profile, inputRate)
	return r
}

// setProfile はフィルタの構成を決める。
//
// `none` はフィルタを通さない。直流成分が残るため波形が片側に寄る。
// 実機の音を求める用途ではなく、APU の出力をそのまま見るためにある。
//
// フィルタは入力レートで動かす。ダウンサンプルの前に掛けることで、
// ローパスが折り返しの防止になる。
func (r *Resampler) setProfile(profile string, inputRate float64) {
	switch profile {
	case ProfileFamicom:
		r.hp1 = newHighPass(37, inputRate)
		r.useHP1 = true
		r.useHP2 = false
		r.useLP = false
	case ProfileNone:
		r.useHP1 = false
		r.useHP2 = false
		r.useLP = false
	default:
		r.hp1 = newHighPass(90, inputRate)
		r.hp2 = newHighPass(440, inputRate)
		r.lp = newLowPass(14000, inputRate)
		r.useHP1 = true
		r.useHP2 = true
		r.useLP = true
	}
}

// SetSpeed は速度倍率を反映する。
//
// 倍率を step に掛ける。速く進めるほど 1 出力サンプルあたりの入力が
// 増え、音の高さが変わる。実機を速回ししたときと同じ変化である。
func (r *Resampler) SetSpeed(speed float64) {
	if speed <= 0 {
		speed = 1
	}
	r.step = r.baseStep * speed
}

// SetMuted は出力を 0 にするかを設定する。
func (r *Resampler) SetMuted(v bool) { r.muted = v }

// Reset はフィルタと補間の状態を消す。
//
// セーブステートを読み込んだ直後に呼ぶ。前の内容から続けると、
// 不連続がクリック音になる。
func (r *Resampler) Reset() {
	r.hp1.reset()
	r.hp2.reset()
	r.lp.reset()
	r.accum = 0
	r.prev = 0
}

// WriteSample は入力を 1 サンプル受け取る。
//
// 出力すべき点に達したとき sink へ書く。sink が false を返したとき
// false を返す。呼び出し側は進行を止める。
func (r *Resampler) WriteSample(v float32) bool {
	v = r.filter(v)

	r.accum++
	if r.accum < r.step {
		r.prev = v
		return true
	}
	r.accum -= r.step

	// 出力点は直前のサンプルと今のサンプルの間にある。
	t := float32(1 - r.accum/r.step)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	out := r.prev + (v-r.prev)*t
	r.prev = v

	if r.muted {
		out = 0
	}
	return r.sink.WriteSample(toInt16(out))
}

// filter はフィルタを通す。
func (r *Resampler) filter(v float32) float32 {
	if r.useHP1 {
		v = r.hp1.process(v)
	}
	if r.useHP2 {
		v = r.hp2.process(v)
	}
	if r.useLP {
		v = r.lp.process(v)
	}
	return v
}

// outputScale はミキサーの出力（0.0〜1.0）を 16 bit へ移す係数。
//
// ミキサーの最大値は約 1.0 である。ハイパスを通した後の振れ幅は
// それより小さい。余裕を見て上限の 8 割に収める。
const outputScale = 0.8 * 32767

// toInt16 は実数のサンプルを 16 bit 符号付きにする。
func toInt16(v float32) int16 {
	s := float64(v) * outputScale
	if s > 32767 {
		return 32767
	}
	if s < -32768 {
		return -32768
	}
	return int16(s)
}
