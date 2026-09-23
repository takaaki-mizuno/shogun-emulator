package ppu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// newStateWriter と newStateReader は直列化の補助。
func newStateWriter() *state.Writer         { return state.NewWriter() }
func newStateReader(b []byte) *state.Reader { return state.NewReader(b) }

// TestMultiplexDecisionTable は優先度の決定表の全行を検証する。
func TestMultiplexDecisionTable(t *testing.T) {
	p, _ := newTestPPU(t)

	tests := []struct {
		name   string
		bg     uint8
		sp     uint8
		spAttr uint8
		want   uint8
	}{
		{"両方透明なら backdrop", 0, 0, 0x00, 0x00},
		{"背景が透明ならスプライト", 0, 0x06, 0x00, 0x16},
		{"スプライトが透明なら背景", 0x05, 0, 0x00, 0x05},
		{"両方不透明で優先度 0 ならスプライト", 0x05, 0x06, 0x00, 0x16},
		{"両方不透明で優先度 1 なら背景", 0x05, 0x06, 0x20, 0x05},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.multiplex(tt.bg, tt.sp, tt.spAttr); got != tt.want {
				t.Errorf("multiplex(%02X, %02X, %02X) = $%02X, 期待 $%02X",
					tt.bg, tt.sp, tt.spAttr, got, tt.want)
			}
		})
	}
}

// TestReverseBits は水平反転のためのビット反転を確かめる。
func TestReverseBits(t *testing.T) {
	tests := []struct{ in, want uint8 }{
		{0x00, 0x00},
		{0xFF, 0xFF},
		{0x01, 0x80},
		{0x80, 0x01},
		{0b1011_0010, 0b0100_1101},
	}
	for _, tt := range tests {
		if got := reverseBits(tt.in); got != tt.want {
			t.Errorf("reverseBits(%08b) = %08b, 期待 %08b", tt.in, got, tt.want)
		}
	}
}

// TestSpritePatternAddress8x8 は 8x8 スプライトのパターンアドレスを確かめる。
func TestSpritePatternAddress8x8(t *testing.T) {
	p, _ := newTestPPU(t)
	p.ctrl = Control(0x08) // スプライトのパターンテーブルを $1000 にする
	p.spriteCount = 1
	p.scanline = 20
	p.secondary[0] = 18 // Y
	p.secondary[1] = 0x42
	p.secondary[2] = 0x00

	// 行 2（20 - 18）
	if got := p.spritePatternAddress(0, 0); got != 0x1000|0x42<<4|2 {
		t.Errorf("下位プレーン = $%04X, 期待 $%04X", got, 0x1000|0x42<<4|2)
	}
	if got := p.spritePatternAddress(0, 8); got != 0x1000|0x42<<4|2|8 {
		t.Errorf("上位プレーン = $%04X", got)
	}

	// 垂直反転では行が反転する
	p.secondary[2] = 0x80
	if got := p.spritePatternAddress(0, 0); got != 0x1000|0x42<<4|5 {
		t.Errorf("垂直反転の下位プレーン = $%04X, 期待 $%04X", got, 0x1000|0x42<<4|5)
	}
}

// TestSpritePatternAddress8x16 は 8x16 スプライトのパターンアドレスを確かめる。
//
// タイル番号の bit 0 がパターンテーブルを選び、bit 7-1 が上半分を選ぶ。
func TestSpritePatternAddress8x16(t *testing.T) {
	p, _ := newTestPPU(t)
	p.ctrl = Control(0x20) // 8x16
	p.spriteCount = 1
	p.secondary[0] = 10 // Y
	p.secondary[1] = 0x05
	p.secondary[2] = 0x00

	// 上半分（行 0-7）はタイル $04、パターンテーブルは bit 0 = 1 なので $1000
	p.scanline = 10
	if got := p.spritePatternAddress(0, 0); got != 0x1000|0x04<<4|0 {
		t.Errorf("上半分 = $%04X, 期待 $%04X", got, 0x1000|0x04<<4)
	}
	// 下半分（行 8-15）は次のタイル $05
	p.scanline = 18
	if got := p.spritePatternAddress(0, 0); got != 0x1000|0x05<<4|0 {
		t.Errorf("下半分 = $%04X, 期待 $%04X", got, 0x1000|0x05<<4)
	}
}

// TestSpritePatternAddress8x16VerticalFlipSwapsTiles は 8x16 の垂直反転で
// 副タイルの位置が入れ替わることを確かめる。
//
// それぞれを反転するだけでは足りない。
func TestSpritePatternAddress8x16VerticalFlip(t *testing.T) {
	p, _ := newTestPPU(t)
	p.ctrl = Control(0x20)
	p.spriteCount = 1
	p.secondary[0] = 10
	p.secondary[1] = 0x05
	p.secondary[2] = 0x80 // 垂直反転

	// 反転すると上端の行に下半分のタイルの最終行が来る
	p.scanline = 10
	if got := p.spritePatternAddress(0, 0); got != 0x1000|0x05<<4|7 {
		t.Errorf("反転した上端 = $%04X, 期待 $%04X", got, 0x1000|0x05<<4|7)
	}
	// 下端の行には上半分のタイルの先頭行が来る
	p.scanline = 25
	if got := p.spritePatternAddress(0, 0); got != 0x1000|0x04<<4|0 {
		t.Errorf("反転した下端 = $%04X, 期待 $%04X", got, 0x1000|0x04<<4)
	}
}

// TestSecondaryOAMClearedToFF は dot 1-64 で secondary OAM が $FF で
// 埋められることを確かめる。
func TestSecondaryOAMClearedToFF(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)
	for i := range p.secondary {
		p.secondary[i] = 0
	}

	stepTo(p, 0, 1)
	for range 64 {
		p.Step()
	}
	for i, v := range p.secondary {
		if v != 0xFF {
			t.Fatalf("secondary[%d] = $%02X, 期待 $FF", i, v)
		}
	}
}

// TestOAMDataReadsFFDuringClear は secondary OAM の初期化中に $2004 が
// $FF を返すことを確かめる。
func TestOAMDataReadsFFDuringClear(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)
	p.oam[0] = 0x12
	p.oamAddr = 0

	stepTo(p, 0, 10)
	if got := p.ReadRegister(regOAMData); got != 0xFF {
		t.Errorf("$2004 = $%02X, 期待 $FF", got)
	}
}

// TestEvaluationFindsSpritesInRange は範囲内のスプライトを集めることを
// 確かめる。
func TestEvaluationFindsSpritesInRange(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)

	// 走査線 20 に掛かるスプライトを 3 個、掛からないものを 1 個置く
	setSprite(p, 0, 20, 0x11, 0x00, 10)
	setSprite(p, 1, 100, 0x22, 0x00, 20) // 範囲外
	setSprite(p, 2, 15, 0x33, 0x00, 30)  // 20-15 = 5 で範囲内
	setSprite(p, 3, 13, 0x44, 0x00, 40)  // 20-13 = 7 で範囲内

	stepTo(p, 20, 257)

	if p.spriteCount != 3 {
		t.Fatalf("spriteCount = %d, 期待 3", p.spriteCount)
	}
	// OAM の順に並ぶ
	wantY := []uint8{20, 15, 13}
	for i, y := range wantY {
		if got := p.secondary[i*4]; got != y {
			t.Errorf("secondary[%d] の Y = %d, 期待 %d", i, got, y)
		}
	}
	if !p.sprite0OnNext {
		t.Error("スプライト 0 が範囲内なので sprite0OnNext が立つこと")
	}
}

// TestEvaluationStopsAtEightSprites は 9 個目以降が secondary OAM へ
// 入らないことを確かめる。
func TestEvaluationStopsAtEightSprites(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)

	// 走査線 20 に掛かるスプライトを 10 個置く
	for i := range 10 {
		setSprite(p, i, 20, uint8(i), 0x00, uint8(i*8))
	}

	stepTo(p, 20, 257)

	if p.spriteCount != 8 {
		t.Errorf("spriteCount = %d, 期待 8", p.spriteCount)
	}
	if !p.status.SpriteOverflow() {
		t.Error("9 個目があるのでオーバーフローフラグが立つこと")
	}
}

// TestOverflowFlagNotSetWithEightSprites はちょうど 8 個のときに
// オーバーフローフラグが立たないことを確かめる。
func TestOverflowFlagNotSetWithEightSprites(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)

	for i := range 8 {
		setSprite(p, i, 20, uint8(i), 0x00, uint8(i*8))
	}
	// 残りは画面外
	for i := 8; i < 64; i++ {
		setSprite(p, i, 0xF0, 0, 0, 0)
	}

	stepTo(p, 20, 257)

	if p.spriteCount != 8 {
		t.Fatalf("spriteCount = %d, 期待 8", p.spriteCount)
	}
	if p.status.SpriteOverflow() {
		t.Error("8 個ちょうどではオーバーフローフラグが立たないこと")
	}
}

// TestNoEvaluationOnPreRenderScanline はプリレンダー行で評価を行わない
// ことを確かめる。走査線 0 にスプライトが描かれない理由である。
func TestNoEvaluationOnPreRenderScanline(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)
	for i := range 4 {
		setSprite(p, i, 0, uint8(i), 0x00, uint8(i*8))
	}

	stepTo(p, p.region.PreRenderScanline(), 257)
	if p.spriteCount != 0 {
		t.Errorf("プリレンダー行で spriteCount = %d、評価してはならない", p.spriteCount)
	}
}

// TestSprite0HitConditions はスプライト 0 ヒットの各条件を確かめる。
func TestSprite0HitConditions(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(p *PPU)
		bg, sp  uint8
		spIndex int
		wantHit bool
	}{
		{"背景とスプライトが不透明ならヒット", nil, 1, 1, 0, true},
		{"背景が透明ならヒットしない", nil, 0, 1, 0, false},
		{"スプライトが透明ならヒットしない", nil, 1, 0, 0, false},
		{"ユニット 0 以外ではヒットしない", nil, 1, 1, 3, false},
		{"背景が無効ならヒットしない",
			func(p *PPU) { p.mask = Mask(0x10) }, 1, 1, 0, false},
		{"スプライトが無効ならヒットしない",
			func(p *PPU) { p.mask = Mask(0x08) }, 1, 1, 0, false},
		{"x = 255 ではヒットしない",
			func(p *PPU) { p.dot = 256 }, 1, 1, 0, false},
		{"左端クリッピング中の x < 8 ではヒットしない",
			func(p *PPU) { p.mask = Mask(0x18); p.dot = 4 }, 1, 1, 0, false},
		{"左端を表示しているなら x < 8 でもヒットする",
			func(p *PPU) { p.mask = Mask(0x1E); p.dot = 4 }, 1, 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := newTestPPU(t)
			p.mask = Mask(0x1E)
			p.dot = 100
			p.sprite0OnCurrent = true
			if tt.setup != nil {
				tt.setup(p)
			}

			p.detectSprite0Hit(tt.bg, tt.sp, tt.spIndex)

			if got := p.status.Sprite0Hit(); got != tt.wantHit {
				t.Errorf("Sprite0Hit = %v, 期待 %v", got, tt.wantHit)
			}
		})
	}
}

// TestSprite0HitNeedsSprite0OnCurrentLine はスプライト 0 が現在行に
// 掛かっていないときヒットしないことを確かめる。
func TestSprite0HitNeedsSprite0OnCurrentLine(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x1E)
	p.dot = 100
	p.sprite0OnCurrent = false

	p.detectSprite0Hit(1, 1, 0)
	if p.status.Sprite0Hit() {
		t.Error("スプライト 0 が現在行に無いのにヒットした")
	}
}

// TestSprite0HitSetOnlyOncePerFrame は同じフレームで 2 回立てないことを
// 確かめる。
func TestSprite0HitIsStickyWithinFrame(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x1E)
	p.dot = 100
	p.sprite0OnCurrent = true

	p.detectSprite0Hit(1, 1, 0)
	if !p.status.Sprite0Hit() {
		t.Fatal("1 回目でヒットしていない")
	}
	// 2 回目は何もしない（フラグは立ったまま）
	p.detectSprite0Hit(1, 1, 0)
	if !p.status.Sprite0Hit() {
		t.Error("フラグが消えた")
	}
}

// TestOAMRefreshBugCopiesEightBytes は OAM のリフレッシュバグを確かめる。
func TestOAMRefreshBugCopiesEightBytes(t *testing.T) {
	p, _ := newTestPPU(t)
	var warnings int
	p.Warn = func(string, ...any) { warnings++ }

	for i := range 16 {
		p.oam[i] = uint8(i)
	}
	for i := range 8 {
		p.oam[0x28+i] = uint8(0xA0 + i)
	}
	p.oamAddr = 0x2A

	p.refreshOAMOnRenderStart()

	for i := range 8 {
		if got := p.oam[i]; got != uint8(0xA0+i) {
			t.Errorf("oam[%d] = $%02X, 期待 $%02X", i, got, 0xA0+i)
		}
	}
	if warnings == 0 {
		t.Error("リフレッシュバグの記録が残っていない")
	}
}

// TestOAMRefreshBugSkippedWhenAddrIsSmall は oamAddr が 8 未満のとき
// 何も起きないことを確かめる。
func TestOAMRefreshBugSkippedWhenAddrIsSmall(t *testing.T) {
	p, _ := newTestPPU(t)
	p.oam[0] = 0x11
	p.oamAddr = 4

	p.refreshOAMOnRenderStart()

	if p.oam[0] != 0x11 {
		t.Error("oamAddr が 8 未満なのに OAM が書き換わった")
	}
}

// setSprite は OAM へ 1 個のスプライトを書く。
func setSprite(p *PPU, index int, y, tile, attr, x uint8) {
	p.oam[index*4+0] = y
	p.oam[index*4+1] = tile
	p.oam[index*4+2] = attr
	p.oam[index*4+3] = x
}

// TestOverflowDiagonalScanDiffersFromLinearScan は手順 3b の `m` の
// インクリメントがオーバーフローフラグの結果を変えることを確かめる。
//
// `n` だけを進める実装（期待どおりの動作）では偽陰性が起きない。実機は
// OAM を斜めに走査するため、範囲外のスプライトしか残っていなくても
// タイル番号や X 座標が Y 座標として評価されてフラグが立つ。
func TestOverflowDiagonalScanDiffersFromLinearScan(t *testing.T) {
	// 走査線 20 に掛かるスプライトを 8 個置く。
	// 9 個目以降は Y が範囲外だが、タイル番号を走査線に掛かる値にする。
	build := func(p *PPU) {
		for i := range 8 {
			setSprite(p, i, 20, uint8(i), 0x00, uint8(i*8))
		}
		for i := 8; i < 64; i++ {
			// Y は範囲外。タイル番号は範囲内の値。
			setSprite(p, i, 0xF0, 20, 0x00, 0xF0)
		}
	}

	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)
	build(p)
	stepTo(p, 20, 257)

	if !p.status.SpriteOverflow() {
		t.Error("斜めの走査でタイル番号が Y として評価され、フラグが立つこと")
	}
}

// TestOverflowNoFalsePositiveWhenAllOutOfRange は OAM の残りがどのバイトも
// 範囲外のときフラグが立たないことを確かめる。
func TestOverflowNoFalsePositiveWhenAllOutOfRange(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x18)
	for i := range 8 {
		setSprite(p, i, 20, 0xF0, 0xF0, 0xF0)
	}
	for i := 8; i < 64; i++ {
		setSprite(p, i, 0xF0, 0xF0, 0xF0, 0xF0)
	}

	stepTo(p, 20, 257)

	if p.status.SpriteOverflow() {
		t.Error("どのバイトも範囲外なのでフラグは立たないこと")
	}
}

// TestSprite0HitCanOccurAtDot2 はスプライト 0 ヒットが dot 2 から
// 発生しうることを確かめる。
func TestSprite0HitCanOccurAtDot2(t *testing.T) {
	p, _ := newTestPPU(t)
	p.mask = Mask(0x1E) // 左端も表示する
	p.sprite0OnCurrent = true
	p.dot = 2

	p.detectSprite0Hit(1, 1, 0)
	if !p.status.Sprite0Hit() {
		t.Error("dot 2 でヒットしないのは早すぎる判定である")
	}
}

// TestEvaluationRoundtripMidLine は評価の途中で保存・復元しても結果が
// 変わらないことを確かめる。
func TestEvaluationRoundtripMidLine(t *testing.T) {
	// dot 65-256 の各位置で保存・復元し、行の終わりの結果を比べる
	for _, dot := range []int{70, 100, 150, 200, 250} {
		run := func(save bool) (int, [secondarySize]uint8, bool) {
			p, c := newTestPPU(t)
			p.mask = Mask(0x18)
			for i := range 12 {
				setSprite(p, i, 20, uint8(i), 0x00, uint8(i*8))
			}
			stepTo(p, 20, dot)
			if save {
				w := newStateWriter()
				p.SaveState(w)
				restored := New(p.region, c)
				restored.PowerOn(false)
				if err := restored.LoadState(newStateReader(w.Data())); err != nil {
					t.Fatalf("復元に失敗した: %v", err)
				}
				p = restored
			}
			stepTo(p, 20, 257)
			return p.spriteCount, p.secondary, p.status.SpriteOverflow()
		}

		wantCount, wantSecondary, wantOverflow := run(false)
		gotCount, gotSecondary, gotOverflow := run(true)

		if gotCount != wantCount || gotSecondary != wantSecondary || gotOverflow != wantOverflow {
			t.Errorf("dot %d で保存・復元すると結果が変わる（count %d/%d, overflow %v/%v）",
				dot, gotCount, wantCount, gotOverflow, wantOverflow)
		}
	}
}
