package testrom_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// pad は押下状態をテストから差し替えられる供給元。
type pad struct{ buttons [input.PortCount]uint8 }

func (p *pad) Buttons(port int) uint8 { return p.buttons[port] }

// press は 1P のボタンを押してから離す。
//
// 押す長さをフレーム単位にするのは、テスト ROM がフレームに 1 回
// ポーリングするためである。1 フレームでは取りこぼす ROM がある。
func (p *pad) press(m *nes.NES, buttons uint8, holdFrames, releaseFrames int) {
	p.buttons[0] = buttons
	m.RunFrames(holdFrames)
	p.buttons[0] = 0
	m.RunFrames(releaseFrames)
}

// newInputMachine はコントローラを繋いだ本体を作る。
func newInputMachine(t *testing.T, romName string) (*nes.NES, *pad) {
	t.Helper()
	data, err := os.ReadFile(testrom.RequireROM(t, romName))
	if err != nil {
		t.Fatalf("ROM を読めない: %v", err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatalf("エミュレータを組み立てられない: %v", err)
	}
	p := &pad{}
	n.ConnectStandardControllers(p)
	n.PowerOn(nes.Deterministic())
	return n, p
}

// TestReadJoy3TestButtons は `read_joy3/test_buttons` を実行する。
//
// この ROM は押すボタンを 1 つずつ指示し、指示どおりに押されたときだけ
// 次へ進む。8 個すべてを順に押して最後まで進めば、ビットの並びと
// 読み出しの順序が実機と一致している。
func TestReadJoy3TestButtons(t *testing.T) {
	n, p := newInputMachine(t, "read_joy3/test_buttons.nes")
	n.RunFrames(60)

	// $4016 から読まれる順に押す。
	order := []uint8{
		input.ButtonA, input.ButtonB, input.ButtonSelect, input.ButtonStart,
		input.ButtonUp, input.ButtonDown, input.ButtonLeft, input.ButtonRight,
	}
	names := []string{"A", "B", "Select", "Start", "Up", "Down", "Left", "Right"}
	for i, b := range order {
		p.press(n, b, 20, 20)
		if got := testrom.ScreenText(n); !strings.Contains(got, names[i]) {
			t.Fatalf("%s を押したのに画面に現れない:\n%s", names[i], got)
		}
	}

	got := testrom.ScreenText(n)
	if !strings.Contains(got, "Passed") {
		t.Errorf("合格の表示が出ない:\n%s", got)
	}
}

// TestReadJoy3ThoroughTest は `read_joy3/thorough_test` を実行する。
//
// $4016 の読み出しが DMC DMA の停止サイクルと重なる場面を含む。
// DMC DMA が CPU を止める挙動はフェーズ 7 で実装するため、この段階では
// 競合の起きない経路だけを通る。
//
// 2200 フレームほどかかる。DMC がサンプルを読み進める速さで進行するため、
// APU を実装する前より長い。
func TestReadJoy3ThoroughTest(t *testing.T) {
	n, _ := newInputMachine(t, "read_joy3/thorough_test.nes")
	n.RunFrames(2400)
	if got := testrom.ScreenText(n); !strings.Contains(got, "Passed") {
		t.Errorf("合格の表示が出ない:\n%s", got)
	}
}

// TestAllpadsLowLevelProbing は `allpads` の低レベルの測定結果を検証する。
//
// この ROM はリセット後、オープンバスとコントローラのデータ線の振る舞いを
// 測って表示する。文字を ASCII と対応しないタイルで描くため画面を文字として
// 読めない。フレームハッシュで照合する。期待値を記録する際に、画面の内容を
// ROM 自身の説明（説明画面の 4 ページ目）と突き合わせて確認してある。
//
//	PPU readback  00 FF の繰り返し
//	PPU latch     先頭 4 つが 3F
//	APU open bus  40 40 3F
//	1P・2P        4L=40 3L=A0 4H=41 3H=A1 → 「...0000S」
//
// 最後の行が標準コントローラの署名である。D0 が 4L と 4H で違う（シリアル）、
// D1-D4 が常に 0、D5-D7 が 4L と 3L で違う（未接続）ことを表す。上位 3 bit が
// 読み出しに使った命令によって変わるのは、そこがオープンバスだからであり、
// 設計書 02 編 §2.4.1 の合成が実機と一致していることを示す。
func TestAllpadsLowLevelProbing(t *testing.T) {
	n, _ := newInputMachine(t, "allpads/allpads.nes")
	n.RunFrames(180)
	// 画面の指示どおりリセットを掛けると測定結果の画面へ入る。
	// 説明のページは操作しなくても順に送られる。
	n.Reset()
	n.RunFrames(320)

	f := lastFrame(t, n)
	if dir := os.Getenv("SHOGUN_FRAME_PNG"); dir != "" {
		if err := testrom.WritePNG(filepath.Join(dir, "allpads-probing.png"), f); err != nil {
			t.Fatalf("PNG を書けない: %v", err)
		}
	}
	checkFrameHash(t, "allpads/allpads.nes#low-level-probing", f)
}

// heldPad は常に同じボタンを押している供給元。
type heldPad struct{ buttons uint8 }

func (p heldPad) Buttons(int) uint8 { return p.buttons }

// newHeldInputMachine はボタンを押したままのコントローラを繋いだ本体を
// testrom.StatefulMachine として作る。
func newHeldInputMachine(t testing.TB, romPath string) testrom.StatefulMachine {
	t.Helper()
	data, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatalf("ROM を読めない: %v", err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatalf("エミュレータを組み立てられない: %v", err)
	}
	// 押す内容を固定する。復元した側と進めた側で同じ値を読む必要がある。
	n.ConnectStandardControllers(heldPad{buttons: input.ButtonA | input.ButtonRight})
	n.PowerOn(nes.Deterministic())
	return machine{n}
}

// TestRoundtripInputROM は入力を読む ROM でセーブステートの往復を検証する。
//
// シフトレジスタと strobe が保存されていなければ、復元した側で読み出しの
// 途中の位置が変わる。
func TestRoundtripInputROM(t *testing.T) {
	romPath := testrom.RequireROM(t, "read_joy3/test_buttons.nes")
	testrom.Roundtrip(t, newHeldInputMachine, romPath, 90, 30)
}

// TestRoundtripInputROMEveryFrame は各フレームで往復を検証する。
func TestRoundtripInputROMEveryFrame(t *testing.T) {
	romPath := testrom.RequireROM(t, "read_joy3/test_buttons.nes")
	frames := testrom.RoundtripFrames(testing.Short())
	if frames > 90 {
		frames = 90
	}
	testrom.RoundtripEveryFrame(t, newHeldInputMachine, romPath, frames)
}
