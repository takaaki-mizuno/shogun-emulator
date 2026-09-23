package emu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// writePollingROM はコントローラを毎フレーム読む NROM の ROM を書く。
//
// 読んだ 8 bit を $10 へ積み、フレーム数を $11 で数える。入力が変われば
// RAM の内容が変わるため、状態のハッシュで入力の再現を確かめられる。
func writePollingROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	code := []uint8{
		0xA9, 0x01, // LDA #$01
		0x8D, 0x16, 0x40, // STA $4016   strobe を立てる
		0xA9, 0x00, // LDA #$00
		0x8D, 0x16, 0x40, // STA $4016   strobe を下ろす
		0xA2, 0x08, // LDX #$08
		0xAD, 0x16, 0x40, // LDA $4016   ボタンを 1 つ読む
		0x4A,       // LSR A
		0x26, 0x10, // ROL $10
		0xCA,       // DEX
		0xD0, 0xF7, // BNE -9
		0xE6, 0x11, // INC $11
		0x2C, 0x02, 0x20, // BIT $2002
		0x10, 0xFB, // BPL -5      VBlank を待つ
		0x4C, 0x00, 0x80, // JMP $8000
	}
	copy(prg, code)
	prg[0x7FFC] = 0x00
	prg[0x7FFD] = 0x80

	data := make([]uint8, 0, 16+len(prg)+8*1024)
	data = append(data, []uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	data = append(data, prg...)
	data = append(data, make([]uint8, 8*1024)...)

	path := filepath.Join(t.TempDir(), "poll.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// stateTestConfig は保存先を一時ディレクトリにした設定を返す。
func stateTestConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	cfg := testConfig()
	cfg.Paths = config.PathsConfig{StateDir: dir, SaveDir: dir, MovieDir: dir}
	cfg.PatchesDir = filepath.Join(dir, "patches")
	cfg.SymbolsDir = filepath.Join(dir, "symbols")
	cfg.State = config.StateConfig{
		Slots:                SlotCount,
		RewindEnabled:        true,
		RewindSeconds:        2,
		RewindIntervalFrames: 10,
		SaveScreenshot:       true,
	}
	cfg.Movie = config.MovieConfig{
		ChecksumIntervalFrames: 10,
		StopOnDesync:           true,
		VerifyChecksums:        true,
	}
	return cfg
}

// runFrames は一時停止したまま n フレーム進める。
//
// 一時停止して進めるのは、テストの各段階で進んだフレーム数を確実に
// 揃えるためである。
func runFrames(t *testing.T, e *Emulator, n int) {
	t.Helper()
	e.StepFrames(n)
	// コマンドは順に処理される。完了を待つコマンドを 1 つ送ることで、
	// 進め終わったことを確かめてから次へ進む。
	if !e.WithMachine(func(*nes.NES) {}) {
		t.Fatal("エミュレーションが停止している")
	}
}

// hashOf は現在の状態のハッシュを返す。
func hashOf(t *testing.T, e *Emulator) [8]uint8 {
	t.Helper()
	var h [8]uint8
	if !e.WithMachine(func(n *nes.NES) { h = n.StateHash() }) {
		t.Fatal("本体を参照できない")
	}
	return h
}

// framesOf は進んだフレーム数を返す。
func framesOf(t *testing.T, e *Emulator) uint64 {
	t.Helper()
	var f uint64
	if !e.WithMachine(func(n *nes.NES) { f = n.Frames() }) {
		t.Fatal("本体を参照できない")
	}
	return f
}

// newPausedEmulator は一時停止した状態で ROM を読み込んだ Emulator を返す。
func newPausedEmulator(t *testing.T) *Emulator {
	t.Helper()
	e := New(stateTestConfig(t))
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(writePollingROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	return e
}

// TestSaveAndLoadSlot はスロットへの保存と復元を確かめる。
func TestSaveAndLoadSlot(t *testing.T) {
	e := newPausedEmulator(t)
	runFrames(t, e, 30)

	if err := e.SaveSlot(0); err != nil {
		t.Fatalf("保存できない: %v", err)
	}
	saved := hashOf(t, e)

	runFrames(t, e, 30)
	if hashOf(t, e) == saved {
		t.Fatal("進めても状態が変わらない。テストが成立していない")
	}

	if err := e.LoadSlot(0); err != nil {
		t.Fatalf("復元できない: %v", err)
	}
	if got := hashOf(t, e); got != saved {
		t.Errorf("復元後のハッシュ = %x, 期待 %x", got, saved)
	}
}

// TestLoadEmptySlot は空のスロットからの復元がエラーになることを
// 確かめる。
func TestLoadEmptySlot(t *testing.T) {
	e := newPausedEmulator(t)
	if err := e.LoadSlot(3); err == nil {
		t.Error("空のスロットを読み込めてしまう")
	}
}

// TestSlotInfos はスロットの一覧に保存時刻とフレーム数と
// スクリーンショットが入ることを確かめる。
func TestSlotInfos(t *testing.T) {
	e := newPausedEmulator(t)
	runFrames(t, e, 20)
	if err := e.SaveSlot(2); err != nil {
		t.Fatal(err)
	}

	infos, err := e.SlotInfos()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != SlotCount {
		t.Fatalf("スロット数 = %d, 期待 %d", len(infos), SlotCount)
	}
	for _, info := range infos {
		if info.Index == 2 {
			if !info.Exists {
				t.Fatal("保存したスロットが空と判定された")
			}
			if info.Frames == 0 {
				t.Error("フレーム数が記録されていない")
			}
			if info.SavedAt.IsZero() {
				t.Error("保存時刻が記録されていない")
			}
			if len(info.Screenshot) == 0 {
				t.Error("スクリーンショットが入っていない")
			}
			if string(info.Screenshot[1:4]) != "PNG" {
				t.Errorf("スクリーンショットが PNG でない（%x）", info.Screenshot[:4])
			}
			continue
		}
		if info.Exists {
			t.Errorf("スロット %d が空でない", info.Index)
		}
	}
}

// TestSlotSelection はスロットの選択が巡回することを確かめる。
func TestSlotSelection(t *testing.T) {
	e := New(testConfig())
	if got := e.Slot(); got != 0 {
		t.Errorf("初期のスロット = %d, 期待 0", got)
	}
	if got := e.NextSlot(); got != 1 {
		t.Errorf("次のスロット = %d, 期待 1", got)
	}
	if got := e.PrevSlot(); got != 0 {
		t.Errorf("前のスロット = %d, 期待 0", got)
	}
	if got := e.PrevSlot(); got != SlotCount-1 {
		t.Errorf("先頭の前 = %d, 期待 %d", got, SlotCount-1)
	}
	e.SetSlot(5)
	if got := e.Slot(); got != 5 {
		t.Errorf("SetSlot(5) の後 = %d", got)
	}
	e.SetSlot(SlotCount)
	if got := e.Slot(); got != 5 {
		t.Errorf("範囲外の指定で変わった（%d）", got)
	}
}

// TestRewindReturnsToSameState は巻き戻した後に前方再生した結果が、
// 巻き戻さなかった場合と一致することを確かめる。
func TestRewindReturnsToSameState(t *testing.T) {
	e := newPausedEmulator(t)

	runFrames(t, e, 50)
	want := hashOf(t, e)
	wantFrames := framesOf(t, e)

	runFrames(t, e, 30)
	if hashOf(t, e) == want {
		t.Fatal("進めても状態が変わらない。テストが成立していない")
	}

	if err := e.Rewind(30); err != nil {
		t.Fatalf("巻き戻せない: %v", err)
	}
	if got := framesOf(t, e); got != wantFrames {
		t.Errorf("巻き戻した先のフレーム = %d, 期待 %d", got, wantFrames)
	}
	if got := hashOf(t, e); got != want {
		t.Errorf("巻き戻した先のハッシュ = %x, 期待 %x", got, want)
	}

	// 巻き戻した後に前へ進めた結果も、巻き戻さなかった場合と一致する。
	runFrames(t, e, 30)
	after := hashOf(t, e)

	other := newPausedEmulator(t)
	runFrames(t, other, 80)
	if got := hashOf(t, other); got != after {
		t.Errorf("巻き戻しを挟むと結果が変わる（%x と %x）", after, got)
	}
}

// TestRewindStopsAtLimit は保持している範囲を越えて要求したとき、
// 限界で止まることを確かめる。
func TestRewindStopsAtLimit(t *testing.T) {
	e := newPausedEmulator(t)
	runFrames(t, e, 40)

	if err := e.Rewind(100000); err != nil {
		t.Fatalf("巻き戻せない: %v", err)
	}
	if got := framesOf(t, e); got > 40 {
		t.Errorf("フレームが進んだ（%d）", got)
	}
	// 保持している最も古いステートは 0 フレーム目である。
	if got := framesOf(t, e); got != 0 {
		t.Errorf("限界まで戻っていない（%d フレーム目）", got)
	}
}

// TestRewindDisabled は設定で無効にしたとき記録しないことを確かめる。
func TestRewindDisabled(t *testing.T) {
	cfg := stateTestConfig(t)
	cfg.State.RewindEnabled = false
	e := New(cfg)
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writePollingROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	runFrames(t, e, 30)

	if err := e.Rewind(10); err == nil {
		t.Error("無効にしても巻き戻せてしまう")
	}
	if n := e.RewindBytes(); n != 0 {
		t.Errorf("無効にしても %d バイト保持している", n)
	}
}

// TestRewindSixtySecondsFitsBudget は 60 秒分を保持したときの量を測る。
//
// 圧縮前で 14 MiB に収まることを確かめる（設計書 08 編 §8.6）。
// 3606 フレームを進めるため、-short のときは飛ばす。
func TestRewindSixtySecondsFitsBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("-short のため飛ばす")
	}
	cfg := stateTestConfig(t)
	cfg.State.RewindSeconds = 60
	e := New(cfg)
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writePollingROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)

	// 60 秒分を埋める。
	const frames = 3606
	for done := 0; done < frames; done += 600 {
		runFrames(t, e, min(600, frames-done))
	}

	compressed := e.RewindBytes()
	var raw int
	if !e.WithMachine(func(n *nes.NES) { raw = len(n.SaveState()) }) {
		t.Fatal("本体を参照できない")
	}
	states := frames / cfg.State.RewindIntervalFrames
	uncompressed := raw * states
	t.Logf("ステート 1 個 %d バイト × %d 個 = 圧縮前 %.1f MiB、圧縮後 %.1f MiB",
		raw, states, float64(uncompressed)/(1024*1024), float64(compressed)/(1024*1024))

	const budget = 14 * 1024 * 1024
	if uncompressed > budget {
		t.Errorf("圧縮前 %d バイトが予算 %d バイトを超えた", uncompressed, budget)
	}
	if compressed > uncompressed {
		t.Errorf("圧縮後 %d バイトが圧縮前 %d バイトより大きい", compressed, uncompressed)
	}
}

// TestRewindMemoryStaysBounded は保持するステートの量が設定した時間分に
// 収まることを確かめる。
func TestRewindMemoryStaysBounded(t *testing.T) {
	cfg := stateTestConfig(t)
	cfg.State.RewindSeconds = 1
	e := New(cfg)
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(writePollingROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)

	// 保持する時間の 3 倍を進める。リングが循環しても増え続けない。
	runFrames(t, e, 180)
	n := e.RewindBytes()
	if n == 0 {
		t.Fatal("ステートを保持していない")
	}
	// 1 秒あたり 6 個（10 フレーム間隔）のステートに収まる。
	const limit = 8 * 64 * 1024
	if n > limit {
		t.Errorf("保持している量 = %d バイト, 上限 %d バイト", n, limit)
	}
}

// TestRewindThenUsesCurrentInput は巻き戻した後に前へ進めたとき、
// 記録した入力ではなく現在の入力を使うことを確かめる。
func TestRewindThenUsesCurrentInput(t *testing.T) {
	e := newPausedEmulator(t)
	runFrames(t, e, 40)
	if err := e.Rewind(20); err != nil {
		t.Fatalf("巻き戻せない: %v", err)
	}
	branch := hashOf(t, e)

	// 記録した区間と違うボタンを押して進める。
	e.Input.Set(0, 0xFF)
	runFrames(t, e, 20)
	withInput := hashOf(t, e)

	// 同じところまで戻し、何も押さずに進める。
	if err := e.Rewind(20); err != nil {
		t.Fatalf("巻き戻せない: %v", err)
	}
	if got := hashOf(t, e); got != branch {
		t.Fatalf("2 回目の巻き戻し先が違う（%x と %x）", got, branch)
	}
	e.Input.Set(0, 0)
	runFrames(t, e, 20)

	if hashOf(t, e) == withInput {
		t.Error("押したボタンが結果に反映されていない。記録した入力を使っている")
	}
}
