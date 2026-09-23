package testrom_test

import (
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// TestPPUVBlankNMI は VBlank フラグと NMI のタイミングを検証する。
//
// 1 PPU クロックの精度が要求される。分割された ROM を個別に実行するのは、
// どのサブテストが失敗したかを特定するためである。
func TestPPUVBlankNMI(t *testing.T) {
	names := listNESFiles(t, testrom.ROMPath(t, "ppu_vbl_nmi/rom_singles"))
	if len(names) == 0 {
		t.Skip("ppu_vbl_nmi/rom_singles が無い。go run ./tools/fetch-test-roms で取得する")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			runBlarggROM(t, "ppu_vbl_nmi/rom_singles/"+name)
		})
	}
}

// ppuTestROMs は PPU のメモリとレジスタを検証するテスト ROM。
//
// スプライトと DMA を前提とする ROM はここに含めない。どの ROM を
// どの段階で検証するかは設計書 12 編 §12.6.1 の表に従う。
var ppuTestROMs = []string{
	"ppu_open_bus/ppu_open_bus.nes",
}

// TestPPUTestROMs は PPU のテスト ROM を実行する。
func TestPPUTestROMs(t *testing.T) {
	for _, rom := range ppuTestROMs {
		t.Run(rom, func(t *testing.T) {
			runBlarggROM(t, rom)
		})
	}
}

// screenTestROMs は結果を画面に表示するテスト ROM。
//
// 2005 年版の blargg のテストは `$6000` を使わず、結果コードを画面に
// 表示する。`$01` が合格である。命令のタイミングを測るテストは
// `PASSED` と表示する。
var screenTestROMs = []struct {
	rom    string
	frames int
	want   string
}{
	// PPU のメモリ
	{"blargg_ppu_tests_2005.09.15b/palette_ram.nes", 180, "$01"},
	{"blargg_ppu_tests_2005.09.15b/vram_access.nes", 180, "$01"},
	{"blargg_ppu_tests_2005.09.15b/vbl_clear_time.nes", 180, "$01"},
	// 分岐と命令のタイミング。APU を使わず画面に結果を出す。
	{"branch_timing_tests/1.Branch_Basics.nes", 300, "PASSED"},
	{"branch_timing_tests/2.Backward_Branch.nes", 300, "PASSED"},
	{"branch_timing_tests/3.Forward_Branch.nes", 300, "PASSED"},
	{"cpu_timing_test6/cpu_timing_test.nes", 1200, "PASSED"},
}

// TestScreenTestROMs は画面に結果を表示するテスト ROM を判定する。
func TestScreenTestROMs(t *testing.T) {
	for _, tt := range screenTestROMs {
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
			t.Logf("画面:\n%s", text)
		})
	}
}
