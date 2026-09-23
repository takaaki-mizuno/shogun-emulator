package emu

// StepKind はステップ実行の単位（設計書 09 編 §9.5）。
type StepKind uint8

// ステップ実行の単位。
const (
	// StepFrame は次のフレームの先頭まで進める。
	StepFrame StepKind = iota
	// StepInstruction は 1 命令実行する。
	StepInstruction
	// StepCycle は 1 CPU サイクル進める。命令の途中で止まる。
	StepCycle
	// StepOver は JSR のときは対応する RTS まで実行する。
	StepOver
	// StepOut は現在のサブルーチンから RTS するまで実行する。
	StepOut
	// StepScanline は次のスキャンラインの先頭まで実行する。
	StepScanline
	// RunToCursor は指定アドレスに達するまで実行する。
	RunToCursor
)

// opcodeJSR は JSR の opcode。
const opcodeJSR = 0x20

// Step は一時停止したまま指定の単位だけ進める。
//
// StepCycle は命令の途中で止まる。止まっている間にもう一度呼ぶと、
// 次の CPU サイクルへ進む。
func (e *Emulator) Step(kind StepKind) {
	if kind == StepCycle && e.gate.active.Load() {
		e.gate.step()
		return
	}
	e.send(cmdStep{kind: kind, count: 1})
}

// RunTo は addr に達するまで実行する。
func (e *Emulator) RunTo(addr uint16) {
	e.send(cmdStep{kind: RunToCursor, count: 1, addr: addr})
}

// startStep はエミュレーションゴルーチンでステップ実行を始める。
//
// 短い単位（命令・サイクル・フレーム）はコマンドの処理の中で進める。
// 終わりが読めない単位（ステップオーバー・ステップアウト・スキャンライン・
// カーソル位置まで）は、止める条件を置いて実行を再開する。コマンドの処理の
// 中で待ち続けると、停止の操作を受け付けられなくなるためである。
func (e *Emulator) startStep(v cmdStep) {
	if e.machine == nil {
		return
	}
	e.paused = true
	e.setPaused(true)
	e.clearBreak()
	// 止まっている位置の実行ブレークポイントで、進めずに止まり直さない
	// ようにする。
	e.dbg.SkipExecAt(e.machine.CPU.PC)

	switch v.kind {
	case StepInstruction, StepFrame:
		e.runSteps(v.kind, max(v.count, 1))
	case StepCycle:
		e.stepCycles(max(v.count, 1))
	case StepOver:
		pc, s := e.machine.CPU.PC, e.machine.CPU.S
		if e.machine.Peek(pc) != opcodeJSR {
			e.runSteps(StepInstruction, 1)
			return
		}
		ret := pc + 3
		// JSR の前のスタックポインタへ戻り、戻り先に着いたところで止まる。
		e.runUntil(func() bool {
			c := e.machine.CPU
			return c.PC == ret && c.S == s
		})
	case StepOut:
		s := e.machine.CPU.S
		// RTS で 2 バイト（RTI なら 3 バイト）降りた命令境界で止まる。
		e.runUntil(func() bool { return stackAbove(e.machine.CPU.S, s, 2) })
	case StepScanline:
		line := e.machine.PPU.Scanline()
		e.runUntil(func() bool { return e.machine.PPU.Scanline() != line })
	case RunToCursor:
		addr := v.addr
		e.runUntil(func() bool { return e.machine.CPU.PC == addr })
	}
}

// stackAbove は s が base より少なくとも n 大きい（浅い）かを返す。
//
// スタックポインタは 8 bit で折り返す。差を符号付きで見て、浅くなった
// 方向だけを数える。
func stackAbove(s, base uint8, n int) bool {
	d := int(int8(s - base))
	return d >= n
}

// runUntil は cond が成り立つ命令境界まで実行を再開する。
func (e *Emulator) runUntil(cond func() bool) {
	e.until = cond
	e.paused = false
	e.setPaused(false)
	e.pacer.Reset()
}

// checkUntil は止める条件が成り立ったかを調べ、成り立てば止める。
func (e *Emulator) checkUntil() {
	if e.until == nil || !e.until() {
		return
	}
	e.until = nil
	e.paused = true
	e.setPaused(true)
	e.updateStatus()
}
