package apu

// pulseTable と tndTable は非線形ミキシングの表。
//
// 各チャンネルが独自の非線形 DAC を持つため、チャンネル間で相互作用が
// ある。DMC のレベルが上がると Triangle と Noise の音量が下がる。
// この挙動を Triangle の音量調整に使うプログラムがある。
//
// 式は docs/research/04_apu.md の 5.1 節のもの。毎サンプル除算を
// 行わないために起動時に 1 回だけ計算する。
var (
	pulseTable [31]float32
	tndTable   [203]float32
)

func init() {
	// n == 0 の要素は 0 とする。式が 0 除算になるためである。
	for n := 1; n < len(pulseTable); n++ {
		pulseTable[n] = float32(95.52 / (8128.0/float64(n) + 100))
	}
	for n := 1; n < len(tndTable); n++ {
		tndTable[n] = float32(163.67 / (24329.0/float64(n) + 100))
	}
}

// mix は 5 チャンネルの出力を 0.0〜1.0 の値に合成する。
//
// チャンネル別の音量は合成の前に各チャンネルの値へ掛ける。合成した後に
// 掛けると、音量を下げたチャンネルが他のチャンネルへ与える非線形の
// 影響まで一緒に変わってしまう。
func (a *APU) mix() float32 {
	p1 := scaleLevel(a.pulse1.output(), a.volumes.Pulse1)
	p2 := scaleLevel(a.pulse2.output(), a.volumes.Pulse2)
	tr := scaleLevel(a.triangle.output(), a.volumes.Triangle)
	ns := scaleLevel(a.noise.output(), a.volumes.Noise)
	dm := scaleLevel(a.dmc.outputLevel, a.volumes.DMC)
	if a.mute != 0 {
		p1, p2, tr, ns, dm = a.muted(p1, p2, tr, ns, dm)
	}

	return pulseTable[p1+p2] + tndTable[3*tr+2*ns+dm]
}

// scaleLevel はチャンネルの出力に音量を掛ける。
//
// 表の添字は整数であるため、掛けた結果を丸める。音量 1.0 のときは
// 元の値をそのまま返す。
func scaleLevel(v uint8, vol float32) uint8 {
	if vol == 1 {
		return v
	}
	if vol <= 0 {
		return 0
	}
	scaled := float32(v) * vol
	if scaled > float32(v) {
		// 音量を上げる指定でも表の範囲を超えない。
		scaled = float32(v)
	}
	return uint8(scaled + 0.5)
}

// muted はミュートしたチャンネルの値を 0 にする。
func (a *APU) muted(p1, p2, tr, ns, dm uint8) (uint8, uint8, uint8, uint8, uint8) {
	levels := [5]*uint8{&p1, &p2, &tr, &ns, &dm}
	for i, l := range levels {
		if a.mute&(1<<i) != 0 {
			*l = 0
		}
	}
	return p1, p2, tr, ns, dm
}
