package testrom_test

import (
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// dmaScreenROMs は結果を画面に表示する DMA のテスト ROM。
//
// この一覧に `dmc_dma_during_read4/dma_4016_read` を含めない。未達である。
// 画面に何も出ないまま止まる。DMC DMA を実装する前から同じであり、
// このフェーズで生じたものではない。
//
// `dma_2007_read` と `double_2007_read` も含めない。この 2 つは合否を
// 表示せず、読み出した値とその検査和だけを出す。比べる相手の値が
// 手元にない。
//
// `sprdma_and_dmc_dma` も含めない。OAM DMA の最中に DMC DMA が
// 割り込む回数とその位置を 1 サイクルの精度で測るテストで、未達である。
var dmaScreenROMs = []struct {
	rom    string
	frames int
}{
	{"dmc_dma_during_read4/dma_2007_write.nes", 2000},
	{"dmc_dma_during_read4/read_write_2007.nes", 2000},
}

// TestDMAScreenROMs は DMA のテスト ROM を判定する。
func TestDMAScreenROMs(t *testing.T) {
	for _, tt := range dmaScreenROMs {
		t.Run(tt.rom, func(t *testing.T) {
			path := testrom.RequireROM(t, tt.rom)
			m := newMachine(t, path)
			testrom.RunFrames(m, tt.frames)

			peeker, ok := m.(testrom.ScreenPeeker)
			if !ok {
				t.Fatal("PPU のアドレス空間を読めない")
			}
			text := testrom.ScreenText(peeker)
			if !strings.Contains(text, "Passed") {
				t.Errorf("合格の表示が無い。画面:\n%s", text)
			}
		})
	}
}

// TestDMATestROMs は結果を `$6000` へ書く DMA のテスト ROM を判定する。
func TestDMATestROMs(t *testing.T) {
	// DMC DMA が CPU を止めることを前提とする。フェーズ 2 から引き継いだ。
	runBlarggROM(t, "cpu_interrupts_v2/rom_singles/4-irq_and_dma.nes")
}

// TestReadJoy3ConflictsHappen は DMC DMA と $4016 の読み出しが競合し、
// コントローラのレポートが壊れることを確かめる。
//
// `count_errors` は 1000 回の読み出しのうち何回が壊れたかを表示する。
// 実機の NTSC 機では 0 にならない。0 のままなら競合が起きていない。
func TestReadJoy3ConflictsHappen(t *testing.T) {
	n, _ := newInputMachine(t, "read_joy3/count_errors.nes")
	n.RunFrames(3000)

	text := testrom.ScreenText(n)
	if !strings.Contains(text, "Conflicts:") {
		t.Fatalf("結果が表示されない。画面:\n%s", text)
	}
	if strings.Contains(text, "Conflicts: 0/") {
		t.Errorf("競合が 1 回も起きていない。画面:\n%s", text)
	}
	if n.Bus.ConflictCount() == 0 {
		t.Error("停止中の読み直しが 1 回も起きていない")
	}
}
