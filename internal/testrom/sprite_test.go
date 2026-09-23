package testrom_test

import (
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// spriteBlarggROMs は `$6000` で結果を返すスプライト関連のテスト ROM。
var spriteBlarggROMs = []string{
	"oam_read/oam_read.nes",
	"oam_stress/oam_stress.nes",
	// フェーズ 2 から引き継いだ ROM（設計書 12 編 §12.6.1）
	"cpu_dummy_writes/cpu_dummy_writes_oam.nes",
}

// TestSpriteBlarggROMs はスプライト関連のテスト ROM を実行する。
func TestSpriteBlarggROMs(t *testing.T) {
	for _, rom := range spriteBlarggROMs {
		t.Run(rom, func(t *testing.T) {
			runBlarggROM(t, rom)
		})
	}
}

// spriteScreenROMs は結果を画面に表示するスプライト関連のテスト ROM。
//
// スプライト 0 ヒットは 1 ピクセル単位で背景との重なりを見るため、
// 背景のシフタの位相も同時に検証される。
var spriteScreenROMs = []struct {
	rom    string
	frames int
	want   string
}{
	{"sprite_hit_tests_2005.10.05/01.basics.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/02.alignment.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/03.corners.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/04.flip.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/05.left_clip.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/06.right_edge.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/07.screen_bottom.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/08.double_height.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/09.timing_basics.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/10.timing_order.nes", 240, "PASSED"},
	{"sprite_hit_tests_2005.10.05/11.edge_timing.nes", 240, "PASSED"},
	{"sprite_overflow_tests/1.Basics.nes", 240, "PASSED"},
	{"sprite_overflow_tests/2.Details.nes", 240, "PASSED"},
	{"sprite_overflow_tests/3.Timing.nes", 240, "PASSED"},
	{"sprite_overflow_tests/4.Obscure.nes", 240, "PASSED"},
	{"sprite_overflow_tests/5.Emulator.nes", 240, "PASSED"},
	// フェーズ 3 から引き継いだ ROM（設計書 12 編 §12.6.1）
	{"blargg_ppu_tests_2005.09.15b/sprite_ram.nes", 180, "$01"},
}

// TestSpriteScreenROMs は画面に結果を表示するスプライトのテスト ROM を判定する。
func TestSpriteScreenROMs(t *testing.T) {
	for _, tt := range spriteScreenROMs {
		t.Run(tt.rom, func(t *testing.T) {
			path := testrom.RequireROM(t, tt.rom)
			m := newMachine(t, path)
			testrom.RunFrames(m, tt.frames)

			peeker, ok := m.(testrom.ScreenPeeker)
			if !ok {
				t.Fatal("PPU のアドレス空間を読めない")
			}
			text := testrom.ScreenText(peeker)
			if !strings.Contains(text, tt.want) {
				t.Errorf("画面に %q が無い。画面:\n%s", tt.want, text)
			}
		})
	}
}
