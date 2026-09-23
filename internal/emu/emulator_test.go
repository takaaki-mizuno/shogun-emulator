package emu

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// testConfig は待たずに動かす設定を返す。
//
// 壁時計で待つと 1 フレームに 16 ミリ秒かかり、テストが遅くなる。
func testConfig() Config {
	return Config{
		Emulation: config.EmulationConfig{Region: config.RegionNTSC, RAMInitPattern: "zero"},
		Input:     config.InputConfig{Port1Device: config.DeviceStandard},
		NewPacer:  func(*region.Region) Pacer { return NewNoPacer() },
	}
}

// writeTestROM は画面を塗るだけの NROM の ROM をファイルへ書き、パスを返す。
func writeTestROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)

	// $2001 に $08（BG 有効）を書いてから無限ループする。
	code := []uint8{
		0xA9, 0x08, // LDA #$08
		0x8D, 0x01, 0x20, // STA $2001
		0x4C, 0x05, 0x80, // JMP $8005
	}
	copy(prg, code)
	// リセットベクタを $8000 に向ける
	prg[0x7FFC] = 0x00
	prg[0x7FFD] = 0x80

	data := make([]uint8, 0, 16+len(prg)+8*1024)
	header := []uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	data = append(data, header...)
	data = append(data, prg...)
	data = append(data, make([]uint8, 8*1024)...)

	path := filepath.Join(t.TempDir(), "loop.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadROMAndRun は ROM を読み込むとフレームが出てくることを確かめる。
func TestLoadROMAndRun(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()

	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}

	s := e.Status()
	if !s.Loaded {
		t.Error("読み込み後に Loaded が false である")
	}
	if s.MapperName != "NROM" {
		t.Errorf("マッパー名 = %q, 期待 \"NROM\"", s.MapperName)
	}
	if s.ROMName != "loop" {
		t.Errorf("ROM 名 = %q, 期待 \"loop\"", s.ROMName)
	}

	f := video.NewFrame()
	waitFor(t, "フレームが出てくる", func() bool { return e.Frames.Take(f) })
}

// waitFor は条件が満たされるまで待つ。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%sのを待ったが起こらなかった", what)
}

// TestPauseStopsProgress は一時停止でフレーム数が進まなくなることを確かめる。
func TestPauseStopsProgress(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "フレームが進む", func() bool { return e.Status().Frames > 2 })

	e.Pause()
	// 一時停止が反映されるまで待つ
	waitFor(t, "一時停止が反映される", func() bool { return e.Status().Paused })

	before := e.Status().Frames
	time.Sleep(20 * time.Millisecond)
	if after := e.Status().Frames; after != before {
		t.Errorf("一時停止中にフレームが %d から %d へ進んだ", before, after)
	}

	e.Resume()
	waitFor(t, "再開後にフレームが進む", func() bool { return e.Status().Frames > before })
}

// TestFrameAdvance はコマ送りが 1 フレームだけ進めることを確かめる。
func TestFrameAdvance(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}
	e.Pause()
	waitFor(t, "一時停止が反映される", func() bool { return e.Status().Paused })

	before := e.Status().Frames
	e.FrameAdvance()
	waitFor(t, "1 フレーム進む", func() bool { return e.Status().Frames == before+1 })

	time.Sleep(20 * time.Millisecond)
	if after := e.Status().Frames; after != before+1 {
		t.Errorf("コマ送りの後に %d フレームまで進んだ（期待 %d）", after, before+1)
	}
}

// TestStepInstruction は 1 命令だけ進めることを確かめる。
func TestStepInstruction(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}
	e.Pause()
	waitFor(t, "一時停止が反映される", func() bool { return e.Status().Paused })

	var before, after uint64
	e.WithMachine(func(m *nes.NES) { before = m.Cycles() })
	e.StepInstruction()
	// 命令 1 つ分のサイクルだけ進んでいること
	e.WithMachine(func(m *nes.NES) { after = m.Cycles() })
	if after <= before {
		t.Errorf("サイクルが進んでいない: %d から %d", before, after)
	}
	if after-before > 8 {
		t.Errorf("1 命令で %d サイクル進んだ", after-before)
	}
}

// TestResetKeepsRAM はリセットで RAM の内容が変わらないことを確かめる。
func TestResetKeepsRAM(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}
	e.Pause()
	waitFor(t, "一時停止が反映される", func() bool { return e.Status().Paused })

	e.WithMachine(func(m *nes.NES) { m.Bus.Poke(0x0010, 0x5A) })
	if err := e.Reset(false); err != nil {
		t.Fatal(err)
	}
	var got uint8
	e.WithMachine(func(m *nes.NES) { got = m.Peek(0x0010) })
	if got != 0x5A {
		t.Errorf("リセット後の $0010 = $%02X, 期待 $5A", got)
	}

	if err := e.Reset(true); err != nil {
		t.Fatal(err)
	}
	e.WithMachine(func(m *nes.NES) { got = m.Peek(0x0010) })
	if got != 0x00 {
		t.Errorf("電源投入後の $0010 = $%02X, 期待 $00", got)
	}
}

// TestSaveAndLoadState は保存と復元でサイクル数が戻ることを確かめる。
//
// 一時停止してから保存するのは、保存した時点のサイクル数を別の
// コマンドで読み出すあいだにエミュレーションが進まないようにするためである。
func TestSaveAndLoadState(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "フレームが進む", func() bool { return e.Status().Frames > 2 })

	e.Pause()
	waitFor(t, "一時停止が反映される", func() bool { return e.Status().Paused })

	blob, err := e.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var savedCycles uint64
	e.WithMachine(func(m *nes.NES) { savedCycles = m.Cycles() })

	e.Resume()
	waitFor(t, "さらにフレームが進む", func() bool { return e.Status().Frames > 6 })
	e.Pause()
	waitFor(t, "一時停止が反映される", func() bool { return e.Status().Paused })

	if err := e.LoadState(blob); err != nil {
		t.Fatal(err)
	}
	var got uint64
	e.WithMachine(func(m *nes.NES) { got = m.Cycles() })
	if got != savedCycles {
		t.Errorf("復元後のサイクル数 = %d, 期待 %d", got, savedCycles)
	}
}

// TestCommandsBeforeLoadDoNotPanic は ROM を読む前の操作が失敗として
// 返ることを確かめる。
func TestCommandsBeforeLoadDoNotPanic(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()

	if err := e.Reset(false); err == nil {
		t.Error("ROM が無い状態のリセットが成功してしまった")
	}
	if _, err := e.SaveState(); err == nil {
		t.Error("ROM が無い状態の保存が成功してしまった")
	}
	e.Pause()
	e.Resume()
	e.FrameAdvance()
	e.SetSpeed(2)
}

// TestStopWithoutStart は起動していない Emulator を止められることを
// 確かめる。ROM を開かずに終了したときの経路である。
func TestStopWithoutStart(t *testing.T) {
	e := New(testConfig())
	e.Stop()
	if err := e.Reset(false); err == nil {
		t.Error("停止後のコマンドが成功してしまった")
	}
}

// TestUnload は ROM を取り外すと状態が空になることを確かめる。
func TestUnload(t *testing.T) {
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetSpeed(2)
	if err := e.Unload(); err != nil {
		t.Fatal(err)
	}
	s := e.Status()
	if s.Loaded {
		t.Error("取り外した後に Loaded が true である")
	}
	if s.Speed != 2 {
		t.Errorf("取り外しで速度倍率が %v になった。期待 2", s.Speed)
	}
}

// TestLoadROMRejectsBadFile は ROM でないファイルを拒むことを確かめる。
func TestLoadROMRejectsBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.nes")
	if err := os.WriteFile(path, []byte("これは ROM ではない"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(testConfig())
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(path); err == nil {
		t.Error("ROM でないファイルを受け入れてしまった")
	}
	if err := e.LoadROM(filepath.Join(t.TempDir(), "ない.nes")); err == nil {
		t.Error("存在しないファイルを受け入れてしまった")
	}
}

// TestUnknownRegionIsRejected は知らないリージョン名を拒むことを確かめる。
func TestUnknownRegionIsRejected(t *testing.T) {
	cfg := testConfig()
	cfg.Emulation.Region = "secam"
	e := New(cfg)
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writeTestROM(t)); err == nil {
		t.Error("知らないリージョン名を受け入れてしまった")
	}
}
