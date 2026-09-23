package emu

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// writeDebugROM はデバッガの検証に使う NROM の ROM を書く。
//
//	$8000  LDA #$42
//	$8002  LDX #$05
//	$8004  JSR $8010
//	$8007  INC $00
//	$8009  JMP $8000
//	$8010  LDY #$07
//	$8012  STA $0300
//	$8015  LDA $0300
//	$8018  RTS
func writeDebugROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	copy(prg[0x0000:], []uint8{
		0xA9, 0x42, // LDA #$42
		0xA2, 0x05, // LDX #$05
		0x20, 0x10, 0x80, // JSR $8010
		0xE6, 0x00, // INC $00
		0x4C, 0x00, 0x80, // JMP $8000
	})
	copy(prg[0x0010:], []uint8{
		0xA0, 0x07, // LDY #$07
		0x8D, 0x00, 0x03, // STA $0300
		0xAD, 0x00, 0x03, // LDA $0300
		0x60, // RTS
	})
	prg[0x7FFC] = 0x00
	prg[0x7FFD] = 0x80

	data := make([]uint8, 0, 16+len(prg)+8*1024)
	data = append(data, []uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}...)
	data = append(data, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	path := filepath.Join(t.TempDir(), "debug.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// breakRecorder は止まった理由を集める。
type breakRecorder struct {
	mu    sync.Mutex
	infos []debug.BreakInfo
}

func (r *breakRecorder) add(info debug.BreakInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.infos = append(r.infos, info)
}

func (r *breakRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.infos)
}

// newDebugEmulator は一時停止した状態でデバッグ用の ROM を読み込む。
func newDebugEmulator(t *testing.T) (*Emulator, *breakRecorder) {
	t.Helper()
	cfg := stateTestConfig(t)
	cfg.SymbolsDir = t.TempDir()
	cfg.TraceDir = t.TempDir()
	rec := &breakRecorder{}
	cfg.OnBreak = rec.add
	e := New(cfg)
	e.Start()
	t.Cleanup(e.Stop)
	if err := e.LoadROM(writeDebugROM(t)); err != nil {
		t.Fatal(err)
	}
	e.SetPaused(true)
	// 電源投入から 1 命令ずつ進めても PC が $8000 から始まるように、
	// 進んだ分を捨てて電源を入れ直す。
	if err := e.Reset(true); err != nil {
		t.Fatal(err)
	}
	return e, rec
}

// pcOf は現在の PC を返す。
func pcOf(t *testing.T, e *Emulator) uint16 {
	t.Helper()
	var pc uint16
	e.WithMachine(func(n *nes.NES) { pc = n.CPU.PC })
	return pc
}

// addBreakpoint はブレークポイントを加える。
func addBreakpoint(t *testing.T, e *Emulator, b debug.Breakpoint) int {
	t.Helper()
	b.Enabled = true
	var id int
	e.WithDebugger(func(d *debug.Debugger) { id = d.AddBreakpoint(b) })
	return id
}

// runUntilBreak は実行を再開し、次のブレークポイントで止まるまで待つ。
//
// 止まったことを知らせの数で判定する。Status の Paused は再開の
// コマンドが処理される前にも true であり、判定に使えない。
func runUntilBreak(t *testing.T, e *Emulator, rec *breakRecorder) {
	t.Helper()
	before := rec.count()
	e.SetPaused(false)
	waitFor(t, "ブレークポイントで止まる", func() bool { return rec.count() > before })
	// 止めた後のコマンドが処理され終わるのを待つ。
	e.WithMachine(func(*nes.NES) {})
}

// TestExecBreakpointStopsBeforeExecution は実行ブレークポイントが命令を
// 実行する前に止まることを確かめる。
func TestExecBreakpointStopsBeforeExecution(t *testing.T) {
	e, rec := newDebugEmulator(t)
	addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakExec, AddrStart: 0x8012, AddrEnd: 0x8012})
	runUntilBreak(t, e, rec)

	if pc := pcOf(t, e); pc != 0x8012 {
		t.Errorf("止まった PC = $%04X, 期待 $8012", pc)
	}
	var stored uint8
	e.WithMachine(func(n *nes.NES) { stored = n.Peek(0x0300) })
	if stored != 0 {
		t.Error("STA を実行してから止まった")
	}
	if rec.count() == 0 {
		t.Error("止まったことが知らされない")
	}
	if !strings.Contains(e.Status().Break, "$8012") {
		t.Errorf("理由が表示されない: %q", e.Status().Break)
	}

	// 止まった位置から 1 命令進めると、同じブレークポイントで止まり直さずに進む。
	e.Step(StepInstruction)
	if pc := pcOf(t, e); pc != 0x8015 {
		t.Errorf("1 命令進めた後の PC = $%04X, 期待 $8015", pc)
	}
}

// TestConditionalBreakpoint は条件式が成り立つときだけ止まることを
// 確かめる（完了判定の条件 `A == $42 && X < $10`）。
func TestConditionalBreakpoint(t *testing.T) {
	e, rec := newDebugEmulator(t)
	id := addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakExec, AddrStart: 0x8007, AddrEnd: 0x8007})
	var err error
	e.WithDebugger(func(d *debug.Debugger) { err = d.SetBreakpointCondition(id, "A == $42 && X < $10") })
	if err != nil {
		t.Fatal(err)
	}
	runUntilBreak(t, e, rec)
	if pc := pcOf(t, e); pc != 0x8007 {
		t.Errorf("止まった PC = $%04X, 期待 $8007", pc)
	}

	// 成り立たない条件では止まらない。
	e.WithDebugger(func(d *debug.Debugger) { err = d.SetBreakpointCondition(id, "A == $42 && X > $10") })
	if err != nil {
		t.Fatal(err)
	}
	before := rec.count()
	runFrames(t, e, 5)
	if rec.count() != before {
		t.Errorf("条件が成り立たないのに止まった: %q", e.Status().Break)
	}
}

// TestWriteAndReadBreakpoints は書き込みと読み出しのブレークポイントが
// アクセスした命令の後で止まることを確かめる。
func TestWriteAndReadBreakpoints(t *testing.T) {
	e, rec := newDebugEmulator(t)
	wid := addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakWrite, AddrStart: 0x0300, AddrEnd: 0x0300})
	runUntilBreak(t, e, rec)
	if pc := pcOf(t, e); pc != 0x8015 {
		t.Errorf("書き込みで止まった PC = $%04X, 期待 $8015（STA の次）", pc)
	}

	e.WithDebugger(func(d *debug.Debugger) { d.RemoveBreakpoint(wid) })
	addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakRead, AddrStart: 0x0300, AddrEnd: 0x03FF})
	runUntilBreak(t, e, rec)
	if pc := pcOf(t, e); pc != 0x8018 {
		t.Errorf("読み出しで止まった PC = $%04X, 期待 $8018（LDA の次）", pc)
	}
}

// TestStepOverAndOut はステップオーバーとステップアウトを確かめる。
func TestStepOverAndOut(t *testing.T) {
	e, _ := newDebugEmulator(t)
	e.RunTo(0x8004)
	waitFor(t, "JSR まで進む", func() bool { return e.Status().Paused && pcOf(t, e) == 0x8004 })

	e.Step(StepOver)
	waitFor(t, "JSR をまたぐ", func() bool { return e.Status().Paused && pcOf(t, e) == 0x8007 })

	e.RunTo(0x8012)
	waitFor(t, "サブルーチンの中まで進む", func() bool { return e.Status().Paused && pcOf(t, e) == 0x8012 })
	e.Step(StepOut)
	waitFor(t, "サブルーチンから戻る", func() bool { return e.Status().Paused && pcOf(t, e) == 0x8007 })
}

// TestStepCycleAdvancesThreeDots は 1 CPU サイクルずつ進み、PPU が
// 3 ドット進むことを確かめる（完了判定）。
func TestStepCycleAdvancesThreeDots(t *testing.T) {
	e, _ := newDebugEmulator(t)
	var startCycles uint64
	var startLine, startDot int
	e.WithMachine(func(n *nes.NES) {
		startCycles = n.Cycles()
		startLine, startDot = n.PPU.Scanline(), n.PPU.Dot()
	})

	e.Step(StepCycle)
	waitFor(t, "命令の途中で止まる", func() bool { return e.Status().MidInstruction })
	pos, ok := e.GatePosition()
	if !ok {
		t.Fatal("止まった位置が取れない")
	}
	if pos.Cycles != startCycles+1 {
		t.Errorf("サイクル = %d, 期待 %d", pos.Cycles, startCycles+1)
	}
	if got := dotsBetween(startLine, startDot, pos.Scanline, pos.Dot); got != 3 {
		t.Errorf("PPU が %d ドット進んだ, 期待 3", got)
	}

	// もう 1 サイクル進める。
	prev := pos
	e.Step(StepCycle)
	waitFor(t, "次のサイクルで止まる", func() bool {
		p, ok := e.GatePosition()
		return ok && p.Cycles == prev.Cycles+1
	})
	pos, _ = e.GatePosition()
	if got := dotsBetween(prev.Scanline, prev.Dot, pos.Scanline, pos.Dot); got != 3 {
		t.Errorf("2 サイクル目に PPU が %d ドット進んだ, 期待 3", got)
	}

	// 命令の途中でも状態を読める。
	var cycles uint64
	if !e.WithMachine(func(n *nes.NES) { cycles = n.Cycles() }) || cycles != pos.Cycles {
		t.Errorf("待ちの中で読んだサイクル = %d, 期待 %d", cycles, pos.Cycles)
	}
}

// dotsBetween は PPU の位置 a から b までのドット数を返す。
func dotsBetween(la, da, lb, db int) int {
	return (lb*341 + db) - (la*341 + da)
}

// TestSaveStateWhileMidInstruction は命令の途中で保存を要求したとき、
// 命令を終えてから保存することを確かめる。
func TestSaveStateWhileMidInstruction(t *testing.T) {
	e, _ := newDebugEmulator(t)
	e.Step(StepCycle)
	waitFor(t, "命令の途中で止まる", func() bool { return e.Status().MidInstruction })

	data, err := e.SaveState()
	if err != nil {
		t.Fatalf("保存できない: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ステートが空である")
	}
	if e.Status().MidInstruction {
		t.Error("保存の後も命令の途中のままである")
	}
	// 命令境界にいる。最初の命令 LDA #$42 を終えて $8002 にいる。
	if pc := pcOf(t, e); pc != 0x8002 {
		t.Errorf("保存した位置の PC = $%04X, 期待 $8002", pc)
	}
}

// TestPPUPositionBreakpoint は指定したスキャンラインとドットで止まる
// ことを確かめる（完了判定）。
func TestPPUPositionBreakpoint(t *testing.T) {
	e, _ := newDebugEmulator(t)
	addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakPPUPosition, Scanline: 100, Dot: 50})
	e.SetPaused(false)
	waitFor(t, "PPU 位置で止まる", func() bool { return e.Status().MidInstruction })

	pos, ok := e.GatePosition()
	if !ok {
		t.Fatal("止まった位置が取れない")
	}
	if pos.Scanline != 100 || pos.Dot < 50 || pos.Dot > 53 {
		t.Errorf("止まった位置 = %d, %d, 期待 100, 50-53", pos.Scanline, pos.Dot)
	}

	// 再開すると次のフレームの同じ位置で再び止まる。
	e.SetPaused(false)
	waitFor(t, "次のフレームで止まる", func() bool {
		p, ok := e.GatePosition()
		return ok && p.Cycles > pos.Cycles
	})
}

// TestStepScanline は次のスキャンラインまで進むことを確かめる。
func TestStepScanline(t *testing.T) {
	e, _ := newDebugEmulator(t)
	var before int
	e.WithMachine(func(n *nes.NES) { before = n.PPU.Scanline() })
	e.Step(StepScanline)
	waitFor(t, "スキャンラインが変わる", func() bool {
		var line int
		e.WithMachine(func(n *nes.NES) { line = n.PPU.Scanline() })
		return e.Status().Paused && line != before
	})
}

// TestHooksAreNilWithoutBreakpoints はブレークポイントもビューアも
// 無いとき、フックが設定されないことを確かめる。
func TestHooksAreNilWithoutBreakpoints(t *testing.T) {
	e, _ := newDebugEmulator(t)
	check := func(label string, wantNil bool) {
		t.Helper()
		var h nes.Hooks
		e.WithMachine(func(n *nes.NES) { h = n.Hooks() })
		isNil := h.OnCPURead == nil && h.OnCPUWrite == nil && h.OnBeforeExec == nil &&
			h.OnCycle == nil && h.OnInterrupt == nil && h.OnSprite0Hit == nil && h.OnFrameComplete == nil
		if isNil != wantNil {
			t.Errorf("%s: フックがすべて nil = %v, 期待 %v（%+v）", label, isNil, wantNil, h)
		}
	}
	check("最初", true)

	id := addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakWrite, AddrStart: 0x0300, AddrEnd: 0x0300})
	check("ブレークポイントを置いた後", false)

	e.WithDebugger(func(d *debug.Debugger) { d.RemoveBreakpoint(id) })
	check("取り除いた後", true)

	e.WithDebugger(func(d *debug.Debugger) { d.SetFeatures(debug.Features{ChangeTracking: true}) })
	check("ビューアを開いた後", false)
	e.WithDebugger(func(d *debug.Debugger) { d.SetFeatures(debug.Features{}) })
	check("ビューアを閉じた後", true)
}

// TestPokeRefusedWhileRecording はムービーの記録中にメモリを編集できない
// ことを確かめる。
func TestPokeRefusedWhileRecording(t *testing.T) {
	e, _ := newDebugEmulator(t)
	if err := e.Poke(debug.SpaceRAM, 0x10, 0x55, false); err != nil {
		t.Fatalf("記録していないのに編集できない: %v", err)
	}
	if err := e.StartRecordingMovie(filepath.Join(t.TempDir(), "m.movie")); err != nil {
		t.Fatal(err)
	}
	if err := e.Poke(debug.SpaceRAM, 0x10, 0x66, false); err == nil {
		t.Error("記録中に編集できてしまう")
	}
}
