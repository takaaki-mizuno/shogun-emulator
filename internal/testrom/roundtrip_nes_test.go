package testrom_test

import (
	"os"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// newMachine は往復テスト用に ROM からエミュレータを作る。
func newMachine(t testing.TB, romPath string) testrom.StatefulMachine {
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
	n.PowerOn(nes.Deterministic())
	return machine{n}
}

// machine は NES を testrom.StatefulMachine に合わせる。
type machine struct{ *nes.NES }

func (m machine) Peek(addr uint16) uint8 { return m.NES.Peek(addr) }

// TestRoundtripNestest は nestest.nes でセーブステートの往復を検証する。
//
// 復元した経路とそのまま進めた経路で状態が一致しなければ、保存漏れが
// あることになる。
func TestRoundtripNestest(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	testrom.Roundtrip(t, newMachine, romPath, 60, 10)
}

// TestRoundtripSpriteROM はスプライトを使う ROM で往復を検証する。
//
// スプライト評価の途中の状態が保存されていなければ、復元した側で
// オーバーフローフラグの結果が変わる。
func TestRoundtripSpriteROM(t *testing.T) {
	romPath := testrom.RequireROM(t, "sprite_overflow_tests/1.Basics.nes")
	testrom.Roundtrip(t, newMachine, romPath, 120, 10)
}

// TestRoundtripSpriteROMEveryFrame は各フレームで往復を検証する。
func TestRoundtripSpriteROMEveryFrame(t *testing.T) {
	romPath := testrom.RequireROM(t, "sprite_hit_tests_2005.10.05/01.basics.nes")
	frames := testrom.RoundtripFrames(testing.Short())
	if frames > 90 {
		frames = 90
	}
	testrom.RoundtripEveryFrame(t, newMachine, romPath, frames)
}

// TestRoundtripNestestEveryFrame は各フレームで往復を検証する。
//
// 1 フレームだけ進めて比較するため、差分が現れた原因のフレームを
// 特定できる。
func TestRoundtripNestestEveryFrame(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	frames := testrom.RoundtripFrames(testing.Short())
	// この段階では PPU が描画しないため、短い範囲で足りる。
	if frames > 120 {
		frames = 120
	}
	testrom.RoundtripEveryFrame(t, newMachine, romPath, frames)
}

// TestSaveStateSectionNames はステートに各コンポーネントのセクションが
// 含まれることを確かめる。
//
// 設計書の「保存する状態」の表と実装の対応を機械的に確かめる。
//
// バスの中の入れ子のセクション（DMA と入力）はここに現れない。バスの
// セクションは RAM の内容から始まり、本体全体がセクションの並びでは
// ないためである。入れ子の内容は internal/nes/bus のテストが確かめる。
func TestSaveStateSectionNames(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	m := newMachine(t, romPath)
	testrom.RunFrames(m, 5)

	names := state.SectionNames(m.SaveState())
	want := []string{
		"header",
		"nes",
		"nes.cpu",
		"nes.bus",
		"nes.ppu",
		"nes.apu",
		"nes.mapper000",
		"nes.mapper000.common",
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("セクション %q が無い。実際: %v", w, names)
		}
	}
}

// TestLoadStateRejectsDifferentROM は別の ROM のステートを拒むことを
// 確かめる。ROM ハッシュが違うステートを読み込んで原因の分からない
// 誤動作を起こさないためである。
func TestLoadStateRejectsDifferentROM(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	data, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	a, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	a.PowerOn(nes.Deterministic())
	blob := a.SaveState()

	// PRG の内容を 1 バイト変えた ROM を作る
	other := make([]uint8, len(data))
	copy(other, data)
	other[16] ^= 0xFF
	rom2, err := cart.LoadROM(other)
	if err != nil {
		t.Fatal(err)
	}
	b, err := nes.New(rom2, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	b.PowerOn(nes.Deterministic())

	if err := b.LoadState(blob); err == nil {
		t.Error("別の ROM のステートを受け入れてしまった")
	}
}

// TestLoadStateRejectsDifferentRegion は別のリージョンのステートを
// 拒むことを確かめる。
func TestLoadStateRejectsDifferentRegion(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	data, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}

	a, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	a.PowerOn(nes.Deterministic())
	blob := a.SaveState()

	rom2, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := nes.New(rom2, region.PAL)
	if err != nil {
		t.Fatal(err)
	}
	b.PowerOn(nes.Deterministic())

	if err := b.LoadState(blob); err == nil {
		t.Error("別のリージョンのステートを受け入れてしまった")
	}
}

// TestLoadStateRejectsTruncated は途中で切れたステートを拒むことを確かめる。
func TestLoadStateRejectsTruncated(t *testing.T) {
	romPath := testrom.RequireROM(t, "other/nestest.nes")
	data, err := os.ReadFile(romPath)
	if err != nil {
		t.Fatal(err)
	}
	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	n.PowerOn(nes.Deterministic())
	blob := n.SaveState()

	if err := n.LoadState(blob[:len(blob)/2]); err == nil {
		t.Error("切り詰めたステートを受け入れてしまった")
	}
}

// TestRoundtripDPCM は DPCM を再生する ROM で往復を検証する。
//
// DMC のシフトレジスタと残りビット数が保存されていなければ、復元した側で
// 再生が途切れる。
func TestRoundtripDPCM(t *testing.T) {
	romPath := testrom.RequireROM(t, "apu_mixer/dmc.nes")
	testrom.Roundtrip(t, newMachine, romPath, 200, 20)
}

// TestRoundtripDPCMEveryFrame は各フレームで往復を検証する。
func TestRoundtripDPCMEveryFrame(t *testing.T) {
	romPath := testrom.RequireROM(t, "apu_mixer/dmc.nes")
	frames := testrom.RoundtripFrames(testing.Short())
	if frames > 240 {
		frames = 240
	}
	testrom.RoundtripEveryFrame(t, newMachine, romPath, frames)
}
