// Package region はリージョン（NTSC / PAL / Dendy）ごとの定数を保持する。
//
// エミュレーションコードに 341・262・3 のような値を直接書かない。すべて
// Region のフィールドを経由する。これにより、後からリージョンを追加しても
// 散らばった定数を探し回る必要がない。
//
// 値の出典は docs/research/07_timing_and_synchronization.md の 2 節、
// docs/research/03_ppu.md の 2 節、docs/research/04_apu.md の 3・4.6・4.7 節。
package region

// Region は 1 つのリージョンの定数をまとめる。
type Region struct {
	Name string

	// --- クロック ---

	// MasterClockHz はマスタークロックの周波数。
	MasterClockHz float64
	// CPUClockDivider はマスタークロックを CPU クロックへ落とす分周比。
	CPUClockDivider int
	// PPUDotsNum / PPUDotsDen は「CPU 1 サイクルあたりの PPU ドット数」を
	// 既約分数で表す。NTSC は 3/1、PAL は 16/5（= 3.2）、Dendy は 3/1。
	//
	// 浮動小数点で累積すると誤差が入り決定論が保てないため、有理数で持つ。
	PPUDotsNum int
	PPUDotsDen int

	// --- スキャンライン構成 ---

	// VisibleScanlines は PPU のカウンタ上の可視スキャンライン数。3 機種とも 240。
	//
	// PAL の「画の高さ 239」は、常に黒いボーダーが画の上 1 ピクセルを覆うため
	// 表示される高さが 239 になることを指す。スキャンラインの数ではない。
	VisibleScanlines int
	// PostRenderScanlines は可視領域と VBlank の間の行数。
	PostRenderScanlines int
	// VBlankScanlines は垂直ブランキングの行数。
	VBlankScanlines int
	// PreRenderScanlines はプリレンダー行の数。3 機種とも 1。
	PreRenderScanlines int
	// DotsPerScanline は 1 スキャンラインのドット数。3 機種とも 341。
	DotsPerScanline int
	// PictureHeight は実際に表示される画の高さ。ボーダーの描画範囲に使う。
	PictureHeight int

	// --- 挙動の差分 ---

	// EmphasisBitShift は赤・緑・青のエンファシスに対応する PPUMASK のビット位置。
	// NTSC は赤 5・緑 6・青 7。PAL と Dendy は赤と緑が入れ替わる。
	EmphasisBitShift [3]uint8
	// ForcedOAMRefresh は PAL の強制 OAM リフレッシュを行うかどうか。
	ForcedOAMRefresh bool
	// DMCDMARegisterConflict は DMC DMA の停止中にレジスタを多重に読むかどうか。
	// 2A07（PAL）ではこの問題が修正されている。
	DMCDMARegisterConflict bool
	// BlackBorder はボーダーが常に黒で、画の左右 2 ピクセルと上 1 ピクセルを
	// 侵食するかどうか。
	BlackBorder bool

	// --- テーブル ---

	// NoisePeriods は Noise チャンネルの周期表。単位は CPU サイクル。
	NoisePeriods [16]uint16
	// DMCRates は DMC チャンネルのレート表。単位は CPU サイクル。
	DMCRates [16]uint16
	// FrameCounterSteps はフレームカウンタのステップ境界。単位は APU サイクル。
	// 添字 0 が 4 ステップモード、1 が 5 ステップモード。
	// 最後の要素はラップ位置を表す。
	FrameCounterSteps [2][]uint32
}

// TotalScanlines は 1 フレームのスキャンライン数を返す。
func (r *Region) TotalScanlines() int {
	return r.VisibleScanlines + r.PostRenderScanlines + r.VBlankScanlines + r.PreRenderScanlines
}

// PostRenderScanline はポストレンダー行の番号を返す。
func (r *Region) PostRenderScanline() int { return r.VisibleScanlines }

// VBlankStartScanline は VBlank フラグが立つスキャンラインの番号を返す。
func (r *Region) VBlankStartScanline() int {
	return r.VisibleScanlines + r.PostRenderScanlines
}

// PreRenderScanline はプリレンダー行の番号を返す。
func (r *Region) PreRenderScanline() int { return r.TotalScanlines() - 1 }

// CPUClockHz は CPU クロックの周波数を返す。
func (r *Region) CPUClockHz() float64 {
	return r.MasterClockHz / float64(r.CPUClockDivider)
}

// DotsPerFrame は 1 フレームのドット数を返す。
// skipDot が true のとき、奇数フレームの 1 ドットスキップを反映する。
func (r *Region) DotsPerFrame(skipDot bool) float64 {
	n := float64(r.TotalScanlines() * r.DotsPerScanline)
	if skipDot {
		n -= 0.5 // 奇数フレームだけ 1 ドット短いため、平均で 0.5 ドット短くなる
	}
	return n
}

// CPUCyclesPerFrame は 1 フレームの CPU サイクル数を返す。
func (r *Region) CPUCyclesPerFrame(skipDot bool) float64 {
	dotsPerCycle := float64(r.PPUDotsNum) / float64(r.PPUDotsDen)
	return r.DotsPerFrame(skipDot) / dotsPerCycle
}

// FrameRateHz はフレームレートを返す。
func (r *Region) FrameRateHz(skipDot bool) float64 {
	return r.CPUClockHz() / r.CPUCyclesPerFrame(skipDot)
}

// 共通のノイズ周期・DMC レート表。
//
// docs/research/04_apu.md の 4.6・4.7 節の表から転記した。
var (
	noisePeriodsNTSC = [16]uint16{4, 8, 16, 32, 64, 96, 128, 160, 202, 254, 380, 508, 762, 1016, 2034, 4068}
	noisePeriodsPAL  = [16]uint16{4, 8, 14, 30, 60, 88, 118, 148, 188, 236, 354, 472, 708, 944, 1890, 3778}

	dmcRatesNTSC = [16]uint16{428, 380, 340, 320, 286, 254, 226, 214, 190, 160, 142, 128, 106, 84, 72, 54}
	dmcRatesPAL  = [16]uint16{398, 354, 316, 298, 276, 236, 210, 198, 176, 148, 132, 118, 98, 78, 66, 50}
)

// フレームカウンタのステップ境界。単位は APU サイクル。
//
// docs/research/04_apu.md の 3.1・3.2 節の表から転記した。各列の最後の値は
// カウントが 0 に戻る位置を表す。
var (
	frameStepsNTSC = [2][]uint32{
		{3728, 7456, 11185, 14914, 14915},
		{3728, 7456, 11185, 14914, 18640, 18641},
	}
	frameStepsPAL = [2][]uint32{
		{4156, 8313, 12469, 16626, 16627},
		{4156, 8313, 12469, 16626, 20782, 20783},
	}
)

// NTSC は RP2A03 + RP2C02 の構成。
var NTSC = &Region{
	Name:            "NTSC",
	MasterClockHz:   21477272.0, // 236.25 MHz / 11
	CPUClockDivider: 12,
	PPUDotsNum:      3,
	PPUDotsDen:      1,

	VisibleScanlines:    240,
	PostRenderScanlines: 1,
	VBlankScanlines:     20,
	PreRenderScanlines:  1,
	DotsPerScanline:     341,
	PictureHeight:       240,

	EmphasisBitShift:       [3]uint8{5, 6, 7}, // 赤, 緑, 青
	ForcedOAMRefresh:       false,
	DMCDMARegisterConflict: true,
	BlackBorder:            false,

	NoisePeriods:      noisePeriodsNTSC,
	DMCRates:          dmcRatesNTSC,
	FrameCounterSteps: frameStepsNTSC,
}

// PAL は RP2A07 + RP2C07 の構成。
var PAL = &Region{
	Name:            "PAL",
	MasterClockHz:   26601712.5,
	CPUClockDivider: 16,
	PPUDotsNum:      16, // 3.2 = 16/5
	PPUDotsDen:      5,

	VisibleScanlines:    240,
	PostRenderScanlines: 1,
	VBlankScanlines:     70,
	PreRenderScanlines:  1,
	DotsPerScanline:     341,
	PictureHeight:       239, // ボーダーが画の上 1 行を覆う

	EmphasisBitShift:       [3]uint8{6, 5, 7}, // 赤と緑が入れ替わる
	ForcedOAMRefresh:       true,
	DMCDMARegisterConflict: false,
	BlackBorder:            true,

	NoisePeriods:      noisePeriodsPAL,
	DMCRates:          dmcRatesPAL,
	FrameCounterSteps: frameStepsPAL,
}

// Dendy は UMC UA6527P + UA6538 の構成。
var Dendy = &Region{
	Name:            "Dendy",
	MasterClockHz:   26601712.5,
	CPUClockDivider: 15,
	PPUDotsNum:      3,
	PPUDotsDen:      1,

	VisibleScanlines:    240,
	PostRenderScanlines: 51, // PAL の行数を NTSC 相当の VBlank で埋め合わせる
	VBlankScanlines:     20,
	PreRenderScanlines:  1,
	DotsPerScanline:     341,
	PictureHeight:       239,

	EmphasisBitShift:       [3]uint8{6, 5, 7},
	ForcedOAMRefresh:       false,
	DMCDMARegisterConflict: false,
	BlackBorder:            true,

	NoisePeriods:      noisePeriodsPAL,
	DMCRates:          dmcRatesPAL,
	FrameCounterSteps: frameStepsNTSC,
}

// All は対応するリージョンの一覧。
var All = []*Region{NTSC, PAL, Dendy}

// ByName は名前からリージョンを返す。名前の大小は区別しない。
func ByName(name string) (*Region, bool) {
	switch lower(name) {
	case "ntsc":
		return NTSC, true
	case "pal":
		return PAL, true
	case "dendy":
		return Dendy, true
	}
	return nil, false
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
