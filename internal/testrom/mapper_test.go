package testrom_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// newMapperMachine は設定を指定して本体を組み立てる。
func newMapperMachine(t *testing.T, romName string, o nes.Options) *nes.NES {
	t.Helper()
	data, err := os.ReadFile(testrom.RequireROM(t, romName))
	if err != nil {
		t.Fatalf("ROM を読めない: %v", err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	n, err := nes.NewWithOptions(rom, region.NTSC, o)
	if err != nil {
		t.Fatalf("エミュレータを組み立てられない: %v", err)
	}
	n.PowerOn(nes.Deterministic())
	return n
}

// holyMapperelROMs は対応するマッパーの Holy Mapperel。
//
// この ROM は基板の構成を自分で判別し、PRG と CHR の全バンクへの到達、
// RAM の読み書き、ミラーリング、IRQ、WRAM 保護を順に試す。結果を
// 画面の「DETAILED TEST RESULT」に 16 進 4 桁で出す。
var holyMapperelROMs = []string{
	"M0_P32K_C8K_V",
	"M0_P32K_CR8K_V",
	"M0_P32K_CR32K_V",
	"M1_P128K",
	"M1_P128K_C128K",
	"M1_P128K_C128K_S8K",
	"M1_P128K_C128K_W8K",
	"M1_P128K_C32K",
	"M1_P128K_C32K_S8K",
	"M1_P128K_C32K_W8K",
	"M1_P128K_CR8K",
	"M1_P512K_CR8K_S32K",
	"M1_P512K_CR8K_S8K",
	"M1_P512K_S32K",
	"M1_P512K_S8K",
	"M2_P128K_V",
	"M2_P128K_CR8K_V",
	"M3_P32K_C32K_H",
	"M4_P128K",
	"M4_P128K_CR8K",
	"M4_P128K_CR32K",
	"M4_P256K_C256K",
	"M7_P128K",
	"M7_P128K_CR8K",
	"M66_P64K_C16K_V",
}

// holyMapperelFrames は結果が出るまでに進めるフレーム数。
//
// 32 KiB の WRAM を持つ構成が最も時間を要する。すべての構成で結果が
// 出る値を選ぶ。
const holyMapperelFrames = 1500

// holyMapperelPass は全項目に合格したときの表示。
const holyMapperelPass = "DETAILED TEST RESULT: 0000"

// TestHolyMapperel は対応マッパーの基板を Holy Mapperel で検証する。
func TestHolyMapperel(t *testing.T) {
	for _, name := range holyMapperelROMs {
		t.Run(name, func(t *testing.T) {
			n := newMapperMachine(t, "holy-mapperel/testroms/"+name+".nes", nes.DefaultOptions())
			n.RunFrames(holyMapperelFrames)
			text := testrom.ScreenTextCompactFont(n)
			if !strings.Contains(text, holyMapperelPass) {
				t.Errorf("合格の表示が無い。画面:\n%s", text)
			}
		})
	}
}

// unsupportedMapperROMs は対応していないマッパーの ROM。
//
// 読み込みの時点でエラーになることを確かめる。動かないまま黙って
// 起動すると、利用者には原因の分からない故障に見える。
var unsupportedMapperROMs = []struct {
	rom  string
	name string
}{
	{"holy-mapperel/testroms/M9_P128K_C64K.nes", "MMC2"},
	{"holy-mapperel/testroms/M10_P128K_C64K_S8K.nes", "MMC4"},
	{"holy-mapperel/testroms/M11_P64K_C64K_V.nes", "Color Dreams"},
	{"holy-mapperel/testroms/M34_P128K_H.nes", "BNROM/NINA-001"},
	{"holy-mapperel/testroms/M69_P128K_C64K_S8K.nes", "Sunsoft FME-7"},
}

// TestUnsupportedMapperError は未対応マッパーの ROM がエラーになり、
// メッセージに番号と名称が入ることを確かめる。
func TestUnsupportedMapperError(t *testing.T) {
	for _, tt := range unsupportedMapperROMs {
		t.Run(tt.rom, func(t *testing.T) {
			data, err := os.ReadFile(testrom.RequireROM(t, tt.rom))
			if err != nil {
				t.Fatalf("ROM を読めない: %v", err)
			}
			rom, err := cart.LoadROM(data)
			if err != nil {
				t.Fatalf("ROM を解析できない: %v", err)
			}
			_, err = cart.New(rom)
			if err == nil {
				t.Fatal("未対応のマッパーを受け入れてしまった")
			}
			if !strings.Contains(err.Error(), tt.name) {
				t.Errorf("エラーに名称 %q が無い: %v", tt.name, err)
			}
		})
	}
}

// mmc3TestROMs は結果を $6000 へ書く MMC3 のテスト ROM。
//
// `6-MMC3_alt` は NEC 版の挙動を検証する ROM である。既定の sharp では
// 不合格になるのが正しい。
var mmc3TestROMs = []struct {
	rom     string
	variant string
}{
	{"mmc3_test_2/rom_singles/1-clocking.nes", cart.MMC3IRQSharp},
	{"mmc3_test_2/rom_singles/2-details.nes", cart.MMC3IRQSharp},
	{"mmc3_test_2/rom_singles/3-A12_clocking.nes", cart.MMC3IRQSharp},
	{"mmc3_test_2/rom_singles/4-scanline_timing.nes", cart.MMC3IRQSharp},
	{"mmc3_test_2/rom_singles/5-MMC3.nes", cart.MMC3IRQSharp},
	{"mmc3_test_2/rom_singles/6-MMC3_alt.nes", cart.MMC3IRQNEC},
}

// TestMMC3TestROMs は mmc3_test_2 を実行する。
func TestMMC3TestROMs(t *testing.T) {
	for _, tt := range mmc3TestROMs {
		t.Run(tt.rom, func(t *testing.T) {
			want := testrom.Expect(t, tt.rom)
			o := nes.DefaultOptions()
			o.Cart.MMC3IRQVariant = tt.variant
			n := newMapperMachine(t, tt.rom, o)
			res, err := testrom.RunBlargg(machine{n}, want.Timeout)
			if errors.Is(err, testrom.ErrTimeout) {
				t.Fatalf("%d フレームまでに結果が出なかった", want.Timeout)
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Code != want.Expect {
				t.Fatalf("%s（期待した結果コード %d）", res, want.Expect)
			}
			t.Logf("%s: %s", tt.variant, res)
		})
	}
}

// mmc3IRQTestROMs は結果を画面に出す MMC3 の IRQ テスト ROM。
var mmc3IRQTestROMs = []struct {
	rom     string
	variant string
}{
	{"mmc3_irq_tests/1.Clocking.nes", cart.MMC3IRQSharp},
	{"mmc3_irq_tests/2.Details.nes", cart.MMC3IRQSharp},
	{"mmc3_irq_tests/3.A12_clocking.nes", cart.MMC3IRQSharp},
	{"mmc3_irq_tests/4.Scanline_timing.nes", cart.MMC3IRQSharp},
	{"mmc3_irq_tests/5.MMC3_rev_A.nes", cart.MMC3IRQNEC},
	{"mmc3_irq_tests/6.MMC3_rev_B.nes", cart.MMC3IRQSharp},
}

// mmc3IRQTestFrames は結果が出るまでに進めるフレーム数。
const mmc3IRQTestFrames = 600

// TestMMC3IRQTests は mmc3_irq_tests を実行する。
//
// この ROM は $6000 へ結果を書かない。合否は画面の表示で判定する。
func TestMMC3IRQTests(t *testing.T) {
	for _, tt := range mmc3IRQTestROMs {
		t.Run(tt.rom, func(t *testing.T) {
			o := nes.DefaultOptions()
			o.Cart.MMC3IRQVariant = tt.variant
			n := newMapperMachine(t, tt.rom, o)
			n.RunFrames(mmc3IRQTestFrames)
			text := testrom.ScreenTextCompactFont(n)
			if !strings.Contains(text, "PASSED") {
				t.Errorf("合格の表示が無い（%s）。画面:\n%s", tt.variant, text)
			}
		})
	}
}

// mapperBlarggROMs はマッパーを必要とする blargg 形式のテスト ROM。
//
// 分割された ROM でフェーズ 2 までに検証した内容を、まとめた ROM で
// もう一度通す。まとめた ROM はマッパー 1 を使う。
// この一覧に `cpu_interrupts_v2/cpu_interrupts` を含めない。まとめた
// ROM は 5 つの副試験を順に実行し、2 番目の `2-nmi_and_brk` で止まる。
// この副試験はフェーズ 2 から未達である（設計書 12 編 §12.6.1）。
//
// `cpu_dummy_reads` も含めない。結果を `$6000` へ書かず画面に表示する。
var mapperBlarggROMs = []string{
	"instr_test-v5/all_instrs.nes",
	"instr_test-v5/official_only.nes",
	"instr_misc/instr_misc.nes",
	"instr_timing/instr_timing.nes",
}

// TestMapperBlarggROMs はマッパーを必要とするテスト ROM を実行する。
func TestMapperBlarggROMs(t *testing.T) {
	for _, rom := range mapperBlarggROMs {
		t.Run(rom, func(t *testing.T) {
			runBlarggROM(t, rom)
		})
	}
}

// TestCPUDummyReads は `cpu_dummy_reads` を実行する。CNROM を必要とする。
//
// この ROM は結果を `$6000` へ書かず画面に表示する。
func TestCPUDummyReads(t *testing.T) {
	n := newMapperMachine(t, "cpu_dummy_reads/cpu_dummy_reads.nes", nes.DefaultOptions())
	n.RunFrames(120)
	text := testrom.ScreenText(n)
	if !strings.Contains(text, "Passed") {
		t.Errorf("合格の表示が無い。画面:\n%s", text)
	}
}

// mapperFrameCases は描画で確かめるマッパーの ROM。
//
// 合否を文字で出さない ROM である。画面を目で確かめたうえで、
// フレームハッシュを golden に記録して維持する。
var mapperFrameCases = []struct {
	rom    string
	frames int
}{
	// MMC1 の WRAM 無効化ビットを走査線カウンタとして使う道具。
	// 表題と操作の説明が出る。
	{"MMC1_A12/mmc1_a12.nes", 300},
	// 240p Test Suite。マッパー 2 の CHR-RAM へ字形を書きながら描く。
	{"240pee/240pee.nes", 300},
}

// TestMapperFrameHashes は描画で確かめるマッパーの ROM を照合する。
func TestMapperFrameHashes(t *testing.T) {
	for _, tc := range mapperFrameCases {
		t.Run(tc.rom, func(t *testing.T) {
			n := newMapperMachine(t, tc.rom, nes.DefaultOptions())
			n.RunFrames(tc.frames)
			checkFrameHash(t, tc.rom, lastFrame(t, n))
		})
	}
}

// TestRoundtripMMC1 は MMC1 を使う ROM で往復を検証する。
//
// シリアルポートの途中の状態（shiftReg と lastWriteCycle）が保存されて
// いなければ、復元した側で次の書き込みが別のレジスタへ入る。
func TestRoundtripMMC1(t *testing.T) {
	romPath := testrom.RequireROM(t, "holy-mapperel/testroms/M1_P128K_C128K.nes")
	testrom.Roundtrip(t, newMachine, romPath, 120, 30)
}

// TestRoundtripMMC1EveryFrame は MMC1 を使う ROM で毎フレーム往復を
// 検証する。
func TestRoundtripMMC1EveryFrame(t *testing.T) {
	romPath := testrom.RequireROM(t, "holy-mapperel/testroms/M1_P128K_C128K.nes")
	testrom.RoundtripEveryFrame(t, newMachine, romPath, mapperRoundtripFrames())
}

// mapperRoundtripFrames は全フレーム往復テストで進めるフレーム数を返す。
//
// 1 フレームごとに電源投入からやり直すため、費用はフレーム数の 2 乗で
// 増える。マッパーの状態はバンク切り替えと IRQ に集中しており、起動
// 直後の範囲で出尽くす。
func mapperRoundtripFrames() int {
	if n := testrom.RoundtripFrames(testing.Short()); n < 120 {
		return n
	}
	return 120
}

// TestRoundtripMMC3 は MMC3 を使う ROM で往復を検証する。
//
// A12 フィルタの状態が保存されていなければ、復元直後のスキャンライン
// IRQ が 1 行ずれる。
func TestRoundtripMMC3(t *testing.T) {
	romPath := testrom.RequireROM(t, "mmc3_test_2/rom_singles/4-scanline_timing.nes")
	testrom.Roundtrip(t, newMachine, romPath, 120, 30)
}

// TestRoundtripMMC3EveryFrame は MMC3 を使う ROM で毎フレーム往復を
// 検証する。
func TestRoundtripMMC3EveryFrame(t *testing.T) {
	romPath := testrom.RequireROM(t, "mmc3_test_2/rom_singles/4-scanline_timing.nes")
	testrom.RoundtripEveryFrame(t, newMachine, romPath, mapperRoundtripFrames())
}

// TestMapperStateSections はマッパーのセクションがステートに含まれる
// ことを確かめる。
func TestMapperStateSections(t *testing.T) {
	// マッパー 3 は独自のレジスタを持たない。CHR のバンク番号は共通部分
	// が持つため、regs のセクションが無い。
	for _, tt := range []struct {
		rom     string
		section string
		regs    bool
	}{
		{"holy-mapperel/testroms/M1_P128K_C128K.nes", "nes.mapper001", true},
		{"holy-mapperel/testroms/M2_P128K_V.nes", "nes.mapper002", true},
		{"holy-mapperel/testroms/M3_P32K_C32K_H.nes", "nes.mapper003", false},
		{"holy-mapperel/testroms/M4_P128K.nes", "nes.mapper004", true},
		{"holy-mapperel/testroms/M7_P128K.nes", "nes.mapper007", true},
		{"holy-mapperel/testroms/M66_P64K_C16K_V.nes", "nes.mapper066", true},
	} {
		t.Run(tt.rom, func(t *testing.T) {
			m := newMachine(t, testrom.RequireROM(t, tt.rom))
			testrom.RunFrames(m, 5)
			names := state.SectionNames(m.SaveState())
			have := map[string]bool{}
			for _, n := range names {
				have[n] = true
			}
			want := []string{tt.section, tt.section + ".common"}
			if tt.regs {
				want = append(want, tt.section+".regs")
			}
			for _, w := range want {
				if !have[w] {
					t.Errorf("セクション %q が無い。実際: %v", w, names)
				}
			}
		})
	}
}

// TestMMC1RoundtripMidSerialWrite はシリアル書き込みの途中で保存・復元
// しても続きが成立することを確かめる。
func TestMMC1RoundtripMidSerialWrite(t *testing.T) {
	n := newMapperMachine(t, "holy-mapperel/testroms/M1_P128K_C128K.nes", nes.DefaultOptions())
	n.RunFrames(60)

	m, ok := n.Cart.(*cart.MMC1)
	if !ok {
		t.Fatalf("MMC1 ではない（%T）", n.Cart)
	}

	// PRG バンク 3 を選ぶ 5 bit のうち 2 bit だけを送る。
	// 1 サイクル以上空けて書く。連続サイクルの書き込みは無視される。
	for _, bit := range []uint8{1, 1} {
		n.Bus.Write(0xE000, bit)
		n.Bus.Write(0x0000, 0)
	}
	mid := n.SaveState()

	// 残りの 3 bit を送る。
	finish := func(n *nes.NES) {
		for _, bit := range []uint8{0, 0, 0} {
			n.Bus.Write(0xE000, bit)
			n.Bus.Write(0x0000, 0)
		}
	}
	finish(n)
	want := m.Info().PRGBanks[0].BankIndex

	other := newMapperMachine(t, "holy-mapperel/testroms/M1_P128K_C128K.nes", nes.DefaultOptions())
	if err := other.LoadState(mid); err != nil {
		t.Fatalf("ステートを復元できない: %v", err)
	}
	finish(other)
	got := other.Cart.Info().PRGBanks[0].BankIndex

	if got != want {
		t.Errorf("復元した側のバンク = %d, そのまま続けた側 = %d", got, want)
	}
	if want != 3 {
		t.Errorf("送った 5 bit がバンク 3 にならない（%d）", want)
	}
}

// simpleMapperROMs はレジスタの少ないマッパーの ROM。
//
// 往復テストの対象をフェーズ 8 で実装した 7 マッパーすべてに広げる
// ために使う（設計書 12 編 §12.4.2）。
var simpleMapperROMs = []string{
	"holy-mapperel/testroms/M0_P32K_C8K_V.nes",
	"holy-mapperel/testroms/M2_P128K_V.nes",
	"holy-mapperel/testroms/M3_P32K_C32K_H.nes",
	"holy-mapperel/testroms/M7_P128K.nes",
	"holy-mapperel/testroms/M66_P64K_C16K_V.nes",
}

// simpleMapperRoundtripFrames は簡単なマッパーの全フレーム往復テストで
// 進めるフレーム数。
//
// これらのマッパーはレジスタが 1 つか 2 つであり、状態の取りうる形が
// 少ない。起動直後の範囲で出尽くす。
const simpleMapperRoundtripFrames = 60

// TestRoundtripSimpleMappersEveryFrame はレジスタの少ないマッパーで
// 毎フレーム往復を検証する。
func TestRoundtripSimpleMappersEveryFrame(t *testing.T) {
	frames := min(simpleMapperRoundtripFrames, testrom.RoundtripFrames(testing.Short()))
	for _, rom := range simpleMapperROMs {
		t.Run(rom, func(t *testing.T) {
			romPath := testrom.RequireROM(t, rom)
			testrom.RoundtripEveryFrame(t, newMachine, romPath, frames)
		})
	}
}
