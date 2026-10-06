package emu

import (
	"context"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// TestStepAndWaitFrames は進めたフレーム数と理由が返ることを確かめる。
func TestStepAndWaitFrames(t *testing.T) {
	e, _ := newDebugEmulator(t)
	before := e.Status().Frames
	r, err := e.StepAndWait(context.Background(), StepFrame, 5)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopFramesDone {
		t.Errorf("理由 = %v, 期待 frames_done", r.Reason)
	}
	if r.Frame != before+5 {
		t.Errorf("フレーム = %d, 期待 %d", r.Frame, before+5)
	}
}

// TestStepAndWaitInstruction は 1 命令の進行が step_done を返すことを確かめる。
func TestStepAndWaitInstruction(t *testing.T) {
	e, _ := newDebugEmulator(t)
	r, err := e.StepAndWait(context.Background(), StepInstruction, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopStepDone || r.PC != 0x8002 {
		t.Errorf("結果 = %+v, 期待 step_done と PC $8002", r)
	}
}

// TestStepAndWaitBreakpoint はブレークポイントで止まった理由と情報が返る
// ことを確かめる。
func TestStepAndWaitBreakpoint(t *testing.T) {
	e, _ := newDebugEmulator(t)
	addBreakpoint(t, e, debug.Breakpoint{Kind: debug.BreakExec, AddrStart: 0x8012, AddrEnd: 0x8012})
	r, err := e.StepAndWait(context.Background(), StepFrame, 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopBreakpoint || r.Break == nil || r.PC != 0x8012 {
		t.Errorf("結果 = %+v, 期待 breakpoint と PC $8012", r)
	}
}

// TestStepAndWaitUntilKinds は条件を置いて走る単位でも結果が返ることを
// 確かめる。
func TestStepAndWaitUntilKinds(t *testing.T) {
	e, _ := newDebugEmulator(t)
	ctx := context.Background()
	// $8000 LDA, $8002 LDX, $8004 JSR
	for range 2 {
		if _, err := e.StepAndWait(ctx, StepInstruction, 1); err != nil {
			t.Fatal(err)
		}
	}
	r, err := e.StepAndWait(ctx, StepOver, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopStepDone || r.PC != 0x8007 {
		t.Errorf("ステップオーバー = %+v, 期待 PC $8007", r)
	}
	r, err = e.RunToAndWait(ctx, 0x8010)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopStepDone || r.PC != 0x8010 {
		t.Errorf("カーソルまで = %+v, 期待 PC $8010", r)
	}
	r, err = e.StepAndWait(ctx, StepScanline, 1)
	if err != nil || r.Reason != StopStepDone {
		t.Errorf("スキャンライン = %+v, %v", r, err)
	}
}

// TestStepAndWaitCycle はサイクル単位の進行が命令の途中で止まったことを
// 返すことを確かめる。
func TestStepAndWaitCycle(t *testing.T) {
	e, _ := newDebugEmulator(t)
	ctx := context.Background()
	r, err := e.StepAndWait(ctx, StepCycle, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopStepDone || !r.MidInstruction {
		t.Errorf("1 サイクル目 = %+v, 期待 命令の途中", r)
	}
	r2, err := e.StepAndWait(ctx, StepCycle, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Cycles != r.Cycles+1 {
		t.Errorf("サイクル数 %d → %d, 期待 1 進む", r.Cycles, r2.Cycles)
	}
	// 待ちを解いて命令境界へ戻す。
	r3, err := e.StepAndWait(ctx, StepInstruction, 1)
	if err != nil || r3.MidInstruction {
		t.Errorf("命令単位 = %+v, %v", r3, err)
	}
}

// TestStepAndWaitCancel は取り消すと止まった位置の結果が返ることを確かめる。
func TestStepAndWaitCancel(t *testing.T) {
	e, _ := newDebugEmulator(t)
	// 到達しないアドレスまで走らせ、取り消す。
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r, err := e.RunToAndWait(ctx, 0x9000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reason != StopCancelled {
		t.Errorf("理由 = %v, 期待 cancelled", r.Reason)
	}
	if !e.Status().Paused {
		t.Error("取り消した後に一時停止していない")
	}
	// 取り消しの後も進行の要求を受け付ける。
	r, err = e.StepAndWait(context.Background(), StepFrame, 1)
	if err != nil || r.Reason != StopFramesDone {
		t.Errorf("取り消しの後の進行 = %+v, %v", r, err)
	}
}

// TestStepAndWaitCancelledByReset はリセットで進行が打ち切られたことを
// 返すことを確かめる。
func TestStepAndWaitCancelledByReset(t *testing.T) {
	e, _ := newDebugEmulator(t)
	got := make(chan StepResult, 1)
	go func() {
		r, _ := e.RunToAndWait(context.Background(), 0x9000)
		got <- r
	}()
	// 走り始めるのを待ってからリセットする。
	waitFor(t, "走り始める", func() bool { return !e.Status().Paused })
	if err := e.Reset(false); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r.Reason != StopCancelled {
			t.Errorf("理由 = %v, 期待 cancelled", r.Reason)
		}
		e.WithMachine(func(*nes.NES) {})
		if !e.Status().Paused {
			t.Error("打ち切った後も走り続けている")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("リセットしても結果が返らない")
	}
}
