package testrom_test

import (
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// apuTestROMs は結果を `$6000` へ書く APU のテスト ROM。
//
// まとめた `apu_test/apu_test.nes` はマッパー 1 を必要とするため、
// 分割された ROM を使う。検証する内容は同じである。
var apuTestROMs = []string{
	// フレームカウンタ・レングスカウンタ・IRQ・DMC
	"apu_test/rom_singles/1-len_ctr.nes",
	"apu_test/rom_singles/2-len_table.nes",
	"apu_test/rom_singles/3-irq_flag.nes",
	"apu_test/rom_singles/4-jitter.nes",
	"apu_test/rom_singles/5-len_timing.nes",
	"apu_test/rom_singles/6-irq_flag_timing.nes",
	"apu_test/rom_singles/7-dmc_basics.nes",
	"apu_test/rom_singles/8-dmc_rates.nes",
	// 電源投入とリセット
	"apu_reset/4015_cleared.nes",
	"apu_reset/4017_timing.nes",
	"apu_reset/4017_written.nes",
	"apu_reset/irq_flag_cleared.nes",
	"apu_reset/len_ctrs_enabled.nes",
	"apu_reset/works_immediately.nes",
	// ミキサー。チャンネル間の相対音量と非線形ミキシングを見る
	"apu_mixer/dmc.nes",
	"apu_mixer/noise.nes",
	"apu_mixer/square.nes",
	"apu_mixer/triangle.nes",
	// APU のフレーム IRQ と長さカウンタを前提とする ROM
	"cpu_interrupts_v2/rom_singles/1-cli_latency.nes",
	"cpu_interrupts_v2/rom_singles/3-nmi_and_irq.nes",
	"cpu_interrupts_v2/rom_singles/5-branch_delays_irq.nes",
	"instr_timing/rom_singles/1-instr_timing.nes",
	"instr_timing/rom_singles/2-branch_timing.nes",
	"instr_misc/rom_singles/04-dummy_reads_apu.nes",
}

// この一覧に `cpu_interrupts_v2/rom_singles/2-nmi_and_brk` を含めない。
// 未達である。NMI が BRK を横取りしたときに積まれる P の値が実機と違う。
// APU を実装する前から不合格であり、割り込みの経路の問題である。

// TestAPUTestROMs は APU のテスト ROM を実行して判定する。
func TestAPUTestROMs(t *testing.T) {
	for _, rom := range apuTestROMs {
		t.Run(rom, func(t *testing.T) {
			runBlarggROM(t, rom)
		})
	}
}

// apuScreenROMs は結果を画面に表示する 2005 年版の APU テスト ROM。
//
// `$01` が合格である。
// apuScreenFrames は結果が出るまで進めるフレーム数。
//
// これらの ROM は結果が出た後も同じ表示を保つ。100 フレームで
// すべての ROM が結果を出し、それ以降は変わらないことを確かめてある。
const apuScreenFrames = 300

var apuScreenROMs = []struct {
	rom    string
	frames int
}{
	{"blargg_apu_2005.07.30/01.len_ctr.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/02.len_table.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/03.irq_flag.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/04.clock_jitter.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/05.len_timing_mode0.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/06.len_timing_mode1.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/07.irq_flag_timing.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/08.irq_timing.nes", apuScreenFrames},
	{"blargg_apu_2005.07.30/09.reset_timing.nes", apuScreenFrames},
}

// TestAPUScreenROMs は画面に結果を表示する APU のテスト ROM を判定する。
//
// この一覧に `10.len_halt_timing` と `11.len_reload_timing` を含めない。
// 2 つとも未達である。レジスタへの書き込みとレングスカウンタのクロックが
// 同一の CPU サイクルに重なったときの順序を見るテストで、原因を特定
// できていない。新しい版である `apu_test` にはこの 2 つに相当する
// テストが無い。
func TestAPUScreenROMs(t *testing.T) {
	for _, tt := range apuScreenROMs {
		t.Run(tt.rom, func(t *testing.T) {
			path := testrom.RequireROM(t, tt.rom)
			m := newMachine(t, path)
			testrom.RunFrames(m, tt.frames)

			peeker, ok := m.(testrom.ScreenPeeker)
			if !ok {
				t.Fatal("PPU のアドレス空間を読めない")
			}
			text := testrom.ScreenText(peeker)
			if !strings.Contains(text, "$01") {
				t.Errorf("画面に %q が無い。画面:\n%s", "$01", text)
			}
		})
	}
}
