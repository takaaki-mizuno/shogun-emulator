package region

import (
	"math"
	"testing"
)

// gcd は最大公約数を返す。既約分数の検証に使う。
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// TestPPUDotRatioIsReduced は PPU ドット比が既約分数であることを確かめる。
// 約分されていないと、分数カウンタの周期が必要以上に長くなる。
func TestPPUDotRatioIsReduced(t *testing.T) {
	for _, r := range All {
		if r.PPUDotsDen == 0 {
			t.Fatalf("%s: PPUDotsDen が 0", r.Name)
		}
		if g := gcd(r.PPUDotsNum, r.PPUDotsDen); g != 1 {
			t.Errorf("%s: PPU ドット比 %d/%d が約分されていない（GCD %d）",
				r.Name, r.PPUDotsNum, r.PPUDotsDen, g)
		}
	}
}

// TestScanlineLayout はスキャンライン構成が調査結果と一致することを確かめる。
func TestScanlineLayout(t *testing.T) {
	tests := []struct {
		r         *Region
		total     int
		vblank    int
		preRender int
	}{
		{NTSC, 262, 241, 261},
		{PAL, 312, 241, 311},
		{Dendy, 312, 291, 311},
	}
	for _, tt := range tests {
		if got := tt.r.TotalScanlines(); got != tt.total {
			t.Errorf("%s: TotalScanlines = %d, 期待 %d", tt.r.Name, got, tt.total)
		}
		if got := tt.r.VBlankStartScanline(); got != tt.vblank {
			t.Errorf("%s: VBlankStartScanline = %d, 期待 %d", tt.r.Name, got, tt.vblank)
		}
		if got := tt.r.PreRenderScanline(); got != tt.preRender {
			t.Errorf("%s: PreRenderScanline = %d, 期待 %d", tt.r.Name, got, tt.preRender)
		}
	}
}

// TestVisibleScanlinesAre240 は 3 機種とも可視スキャンラインが 240 であることを
// 確かめる。PAL の「画の高さ 239」はボーダーが上 1 行を覆うためであり、
// スキャンラインの数ではない。
func TestVisibleScanlinesAre240(t *testing.T) {
	for _, r := range All {
		if r.VisibleScanlines != 240 {
			t.Errorf("%s: VisibleScanlines = %d, 期待 240", r.Name, r.VisibleScanlines)
		}
	}
	if PAL.PictureHeight != 239 {
		t.Errorf("PAL: PictureHeight = %d, 期待 239", PAL.PictureHeight)
	}
	if NTSC.PictureHeight != 240 {
		t.Errorf("NTSC: PictureHeight = %d, 期待 240", NTSC.PictureHeight)
	}
}

// TestCPUCyclesPerFrame は Region から計算した 1 フレームの CPU サイクル数が
// 調査結果の値と一致することを確かめる。
//
// この検証は、クロック分周比・PPU ドット比・スキャンライン構成の 3 つが
// 互いに矛盾していないことを一度に確かめる。
func TestCPUCyclesPerFrame(t *testing.T) {
	tests := []struct {
		r       *Region
		skipDot bool
		want    float64
	}{
		{NTSC, true, 29780.5},
		{NTSC, false, 29780.0 + 2.0/3.0},
		{PAL, false, 33247.5},
		{Dendy, false, 35464.0},
	}
	for _, tt := range tests {
		got := tt.r.CPUCyclesPerFrame(tt.skipDot)
		if math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("%s (skipDot=%v): CPUCyclesPerFrame = %g, 期待 %g",
				tt.r.Name, tt.skipDot, got, tt.want)
		}
	}
}

// TestCPUClockHz は CPU クロックが調査結果の値と一致することを確かめる。
func TestCPUClockHz(t *testing.T) {
	tests := []struct {
		r    *Region
		want float64
		tol  float64
	}{
		{NTSC, 1789772.7, 0.5},
		{PAL, 1662607.0, 0.5},
		{Dendy, 1773447.5, 0.5},
	}
	for _, tt := range tests {
		got := tt.r.CPUClockHz()
		if math.Abs(got-tt.want) > tt.tol {
			t.Errorf("%s: CPUClockHz = %g, 期待 %g ± %g", tt.r.Name, got, tt.want, tt.tol)
		}
	}
}

// TestFrameRateHz はフレームレートが調査結果の値と一致することを確かめる。
func TestFrameRateHz(t *testing.T) {
	tests := []struct {
		r       *Region
		skipDot bool
		want    float64
	}{
		{NTSC, true, 60.0988},
		{PAL, false, 50.0070},
		{Dendy, false, 50.0070},
	}
	for _, tt := range tests {
		got := tt.r.FrameRateHz(tt.skipDot)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("%s: FrameRateHz = %g, 期待 %g", tt.r.Name, got, tt.want)
		}
	}
}

// TestEmphasisBitShift は PAL と Dendy で赤と緑が入れ替わることを確かめる。
func TestEmphasisBitShift(t *testing.T) {
	if NTSC.EmphasisBitShift != [3]uint8{5, 6, 7} {
		t.Errorf("NTSC: EmphasisBitShift = %v, 期待 [5 6 7]", NTSC.EmphasisBitShift)
	}
	for _, r := range []*Region{PAL, Dendy} {
		if r.EmphasisBitShift != [3]uint8{6, 5, 7} {
			t.Errorf("%s: EmphasisBitShift = %v, 期待 [6 5 7]", r.Name, r.EmphasisBitShift)
		}
	}
}

// TestNoisePeriodsAreEven はノイズ周期がすべて偶数であることを確かめる。
// APU サイクルが 2 CPU サイクルであるため、周期は偶数になる。
func TestNoisePeriodsAreEven(t *testing.T) {
	for _, r := range All {
		for i, p := range r.NoisePeriods {
			if p%2 != 0 {
				t.Errorf("%s: NoisePeriods[%d] = %d が奇数", r.Name, i, p)
			}
		}
	}
}

// TestDMCRatesAreEven は DMC レートがすべて偶数であることを確かめる。
func TestDMCRatesAreEven(t *testing.T) {
	for _, r := range All {
		for i, p := range r.DMCRates {
			if p%2 != 0 {
				t.Errorf("%s: DMCRates[%d] = %d が奇数", r.Name, i, p)
			}
		}
	}
}

// TestNoisePeriodsAreDescendingInFrequency は周期表が降順になっていることを
// 確かめる。転記ミスの検出に使う。
func TestNoisePeriodsAreDescending(t *testing.T) {
	for _, r := range All {
		for i := 1; i < len(r.NoisePeriods); i++ {
			if r.NoisePeriods[i] <= r.NoisePeriods[i-1] {
				t.Errorf("%s: NoisePeriods が昇順でない: [%d]=%d, [%d]=%d",
					r.Name, i-1, r.NoisePeriods[i-1], i, r.NoisePeriods[i])
			}
		}
		for i := 1; i < len(r.DMCRates); i++ {
			if r.DMCRates[i] >= r.DMCRates[i-1] {
				t.Errorf("%s: DMCRates が降順でない: [%d]=%d, [%d]=%d",
					r.Name, i-1, r.DMCRates[i-1], i, r.DMCRates[i])
			}
		}
	}
}

// TestFrameCounterSteps はフレームカウンタのステップ境界が昇順で、
// 4 ステップと 5 ステップの要素数が正しいことを確かめる。
func TestFrameCounterSteps(t *testing.T) {
	for _, r := range All {
		for mode := range 2 {
			steps := r.FrameCounterSteps[mode]
			wantLen := 5
			if mode == 1 {
				wantLen = 6
			}
			if len(steps) != wantLen {
				t.Errorf("%s mode %d: 要素数 %d, 期待 %d", r.Name, mode, len(steps), wantLen)
			}
			for i := 1; i < len(steps); i++ {
				if steps[i] <= steps[i-1] {
					t.Errorf("%s mode %d: 昇順でない: [%d]=%d, [%d]=%d",
						r.Name, mode, i-1, steps[i-1], i, steps[i])
				}
			}
		}
	}
}

// TestFrameIRQPeriod は 4 ステップモードのフレーム IRQ の周期が調査結果と
// 一致することを確かめる。NTSC で 29830 CPU サイクル、PAL で 33254。
func TestFrameIRQPeriod(t *testing.T) {
	tests := []struct {
		r    *Region
		want uint32
	}{
		{NTSC, 29830},
		{PAL, 33254},
	}
	for _, tt := range tests {
		steps := tt.r.FrameCounterSteps[0]
		// 最後の要素がラップ位置（APU サイクル）。CPU サイクルはその 2 倍。
		got := steps[len(steps)-1] * 2
		if got != tt.want {
			t.Errorf("%s: フレーム IRQ の周期 = %d CPU サイクル, 期待 %d", tt.r.Name, got, tt.want)
		}
	}
}

// TestByName は名前からリージョンを引けることを確かめる。
func TestByName(t *testing.T) {
	tests := []struct {
		name string
		want *Region
		ok   bool
	}{
		{"ntsc", NTSC, true},
		{"NTSC", NTSC, true},
		{"Ntsc", NTSC, true},
		{"pal", PAL, true},
		{"PAL", PAL, true},
		{"dendy", Dendy, true},
		{"Dendy", Dendy, true},
		{"unknown", nil, false},
		{"", nil, false},
	}
	for _, tt := range tests {
		got, ok := ByName(tt.name)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ByName(%q) = %v, %v; 期待 %v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
