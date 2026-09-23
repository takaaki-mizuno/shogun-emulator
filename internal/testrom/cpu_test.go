package testrom_test

import (
	"errors"
	"os"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// cpuTestROMs は CPU だけで判定できるテスト ROM。
//
// PPU・APU・DMA・マッパーの実装を前提とする ROM はここに含めない。
// どの ROM をどの段階で検証するかは設計書 12 編 §12.6.1 の表に従う。
var cpuTestROMs = []string{
	// 命令の挙動
	"instr_misc/rom_singles/01-abs_x_wrap.nes",
	"instr_misc/rom_singles/02-branch_wrap.nes",
	// レジスタ空間でのコード実行
	"cpu_exec_space/test_cpu_exec_space_apu.nes",
	// リセット
	"cpu_reset/registers.nes",
	"cpu_reset/ram_after_reset.nes",
}

// TestCPUTestROMs は blargg 形式のテスト ROM を実行して判定する。
func TestCPUTestROMs(t *testing.T) {
	for _, rom := range cpuTestROMs {
		t.Run(rom, func(t *testing.T) {
			runBlarggROM(t, rom)
		})
	}
}

// TestCPUInstructionROMSingles は 256 個の opcode を命令群ごとに検証する。
//
// まとめた `instr_test-v5/all_instrs.nes` はマッパー 1 を必要とするため、
// 分割された ROM を使う。検証する内容は同じである。
func TestCPUInstructionROMSingles(t *testing.T) {
	names := listNESFiles(t, testrom.ROMPath(t, "instr_test-v5/rom_singles"))
	if len(names) == 0 {
		t.Skip("instr_test-v5/rom_singles が無い。go run ./tools/fetch-test-roms で取得する")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			runBlarggROM(t, "instr_test-v5/rom_singles/"+name)
		})
	}
}

// runBlarggROM は 1 つのテスト ROM を実行し、期待した結果コードかを確かめる。
func runBlarggROM(t *testing.T, rom string) {
	t.Helper()
	path := testrom.RequireROM(t, rom)
	want := testrom.Expect(t, rom)

	m := newMachine(t, path)
	res, err := testrom.RunBlargg(m, want.Timeout)
	if errors.Is(err, testrom.ErrTimeout) {
		t.Fatalf("%s: %d フレームまでに結果が出なかった", rom, want.Timeout)
	}
	if err != nil {
		t.Fatalf("%s: %v", rom, err)
	}
	if res.Code != want.Expect {
		t.Fatalf("%s: %s（期待した結果コード %d）", rom, res, want.Expect)
	}
	t.Logf("%s: %s", rom, res)
}

// listNESFiles はディレクトリ内の `.nes` の名前を並べて返す。
func listNESFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > 4 && name[len(name)-4:] == ".nes" {
			out = append(out, name)
		}
	}
	return out
}
