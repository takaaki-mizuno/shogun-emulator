package emu

import (
	"context"
	"errors"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// StopReason は進行が止まった理由（設計書 14 編 §14.8.4 の元になる値）。
type StopReason uint8

// 進行が止まった理由。
const (
	// StopFramesDone は指定したフレーム数を進めたことを表す。
	StopFramesDone StopReason = iota
	// StopStepDone はフレーム以外の単位のステップを終えたことを表す。
	StopStepDone
	// StopBreakpoint はブレークポイントで止まったことを表す。
	StopBreakpoint
	// StopCancelled は取り消し、ROM の差し替え、リセット、ロードなどで
	// 進行が打ち切られたことを表す。
	StopCancelled
	// StopCPUHalted は STP などで CPU が止まっていることを表す。
	StopCPUHalted
	// StopCondition は進行の条件（StepOptions.FrameCond・InstrCond）が
	// 成り立ったことを表す。
	StopCondition
)

var stopReasonNames = []string{"frames_done", "step_done", "breakpoint", "cancelled", "cpu_halted", "condition"}

// String は理由の名前を返す。
func (r StopReason) String() string {
	if int(r) < len(stopReasonNames) {
		return stopReasonNames[r]
	}
	return "unknown"
}

// StepResult は進行が止まったときの結果。
type StepResult struct {
	Reason StopReason
	// Frame と Cycles は止まった位置の累積フレーム数とサイクル数。
	Frame  uint64
	Cycles uint64
	// PC は止まった位置のプログラムカウンタ。
	PC uint16
	// MidInstruction は命令の途中で止まっていることを表す。
	MidInstruction bool
	// Break はブレークポイントで止まったときの情報。それ以外では nil。
	Break *debug.BreakInfo
}

// errStopped は エミュレーションゴルーチンが止まっているときのエラー。
var errStopped = errors.New("emu: エミュレーションが停止している")

// StepAndWait は一時停止したまま kind の単位で count 回進め、止まるまで待つ。
//
// GUI の Step と違い、止まった理由と位置を返す。ctx を取り消すと進行の
// 停止を要求し、止まった位置の結果（理由は StopCancelled）を返す。
func (e *Emulator) StepAndWait(ctx context.Context, kind StepKind, count int) (StepResult, error) {
	return e.stepAndWait(ctx, cmdStep{kind: kind, count: count})
}

// StepOptions は StepWith の指定。
type StepOptions struct {
	Kind  StepKind
	Count int
	// Addr は RunToCursor の目標アドレス。
	Addr uint16
	// Input はポート 1 と 2 の入力。nil のとき変えない。エージェントの
	// 入力の経路（SetAgentInput）が有効なときだけ使う。
	Input *[2]uint8
	// Cond は止める条件。StepFrame でだけ使う。エミュレーションゴルーチン
	// から呼ばれ、本体を副作用なしに読むこと。
	Cond func(*nes.NES) bool
	// CondEachInstruction は Cond を命令境界ごとに判定することを表す。
	// false のときフレームの開始ごとに判定する。
	CondEachInstruction bool
	// NoBreak はブレークポイントで止まらないことを表す。
	NoBreak bool
}

// StepWith は指定に従って進め、止まるまで待つ（Agent Interface の進行）。
func (e *Emulator) StepWith(ctx context.Context, o StepOptions) (StepResult, error) {
	c := cmdStep{kind: o.Kind, count: o.Count, addr: o.Addr, input: o.Input,
		instr: o.CondEachInstruction, noBreak: o.NoBreak}
	if o.Cond != nil {
		cond := o.Cond
		c.cond = func() bool { return cond(e.machine) }
	}
	return e.stepAndWait(ctx, c)
}

// SetAgentInput はエージェントの入力の経路を切り替える（設計書 14 編
// §14.4.2）。有効な間、入力は StepWith の Input だけで決まり、キーボードと
// 連射を使わない。フレームの開始時の処理を、そのフレームの最初の命令の
// 直前まで遅らせる。止まっている間に次の進行の入力を受け取り、その
// フレームの入力としてラッチするためである。
func (e *Emulator) SetAgentInput(on bool) { e.send(cmdAgentInput{on: on}) }

// setAgentInput はエミュレーションゴルーチンで経路を切り替える。
func (e *Emulator) setAgentInput(on bool) {
	if on {
		if e.agentInput == nil {
			e.agentInput = &[2]uint8{}
		}
		return
	}
	if e.framePending {
		// 遅らせていたフレームの開始をキーボードの入力で行う。
		e.framePending = false
		e.agentInput = nil
		e.beginFrame()
		return
	}
	e.agentInput = nil
}

// frameBoundary はフレームの境界の処理を行う。エージェントの入力の
// 経路では、フレームの開始時の処理を最初の命令の直前まで遅らせる。
func (e *Emulator) frameBoundary() {
	if e.agentInput != nil {
		e.framePending = true
		return
	}
	e.beginFrame()
}

// RunToAndWait は addr に達するまで実行し、止まるまで待つ。
func (e *Emulator) RunToAndWait(ctx context.Context, addr uint16) (StepResult, error) {
	return e.stepAndWait(ctx, cmdStep{kind: RunToCursor, count: 1, addr: addr})
}

// stepAndWait は進行のコマンドを送り、結果を待つ。
func (e *Emulator) stepAndWait(ctx context.Context, c cmdStep) (StepResult, error) {
	done := make(chan StepResult, 1)
	if c.kind == StepCycle && e.gate.active.Load() {
		// 命令の途中で止まっている。待ちの中で次のサイクルへ進める。
		// コマンドで送ると待ちが解かれ、命令の残りが一度に実行される。
		select {
		case e.gate.req <- gateRequest{steps: max(c.count, 1), result: done}:
		case <-e.done:
			return StepResult{}, errStopped
		}
	} else {
		c.done = done
		if !e.send(c) {
			return StepResult{}, errStopped
		}
	}
	select {
	case r := <-done:
		return r, nil
	case <-e.done:
		return StepResult{}, errStopped
	case <-ctx.Done():
	}

	// 取り消された。コマンドの処理の中で進めている間はフラグで、
	// 条件を置いて走っている間は一時停止のコマンドで止める。
	e.cancelStep.Store(true)
	defer e.cancelStep.Store(false)
	e.send(cmdPause{paused: true})
	select {
	case r := <-done:
		return r, nil
	case <-e.done:
		return StepResult{}, errStopped
	default:
	}
	// 一時停止のコマンドが処理されるまで待ってから、もう一度見る。
	if !e.WithMachine(func(*nes.NES) {}) {
		return StepResult{}, errStopped
	}
	select {
	case r := <-done:
		return r, nil
	default:
		// 待ちの中への要求が受け取られないまま止まった。止まっている
		// 位置を結果として返す。
		st := e.Status()
		return StepResult{Reason: StopCancelled, Frame: st.Frames, Cycles: st.Cycles, MidInstruction: st.MidInstruction}, nil
	}
}

// beginPending は進行の結果の知らせ先を置く。エミュレーションゴルーチンで呼ぶ。
func (e *Emulator) beginPending(done chan StepResult) {
	e.finishStep(StopCancelled)
	e.pending = done
}

// cancelPending は結果を待たれている進行を打ち切り、一時停止する。
//
// 止める条件を置いて走っている進行では、条件も捨てる。待っている者には
// 打ち切りを知らせたのに走り続けると、エージェントの知らない間に状態が
// 進むためである。待つ者がいないときは何もしない。
func (e *Emulator) cancelPending() {
	if e.pending == nil {
		return
	}
	e.until = nil
	e.paused = true
	e.setPaused(true)
	e.finishStep(StopCancelled)
}

// finishStep は結果を待っている者がいれば、reason で知らせる。
//
// 待っている者がいないときは何もしない。GUI の操作は結果を待たないため、
// 挙動は変わらない。
func (e *Emulator) finishStep(reason StopReason) {
	e.stepCond, e.ignoreBreaks = nil, false
	if e.pending == nil {
		return
	}
	r := e.stepResult(reason)
	e.pending <- r
	e.pending = nil
}

// finishStepBreak はブレークポイントで止まったことを知らせる。
func (e *Emulator) finishStepBreak(info debug.BreakInfo) {
	e.stepCond, e.ignoreBreaks = nil, false
	if e.pending == nil {
		return
	}
	r := e.stepResult(StopBreakpoint)
	r.Break = &info
	e.pending <- r
	e.pending = nil
}

// stepResult は現在の位置から結果を作る。
func (e *Emulator) stepResult(reason StopReason) StepResult {
	r := StepResult{Reason: reason, MidInstruction: e.gate.active.Load() || e.debug.midBreak}
	m := e.machine
	if m == nil {
		return r
	}
	if (reason == StopFramesDone || reason == StopStepDone) && m.CPU.Halted() {
		r.Reason = StopCPUHalted
	}
	r.Frame, r.Cycles, r.PC = m.Frames(), m.Cycles(), m.CPU.PC
	return r
}
