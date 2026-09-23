package debug

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// FrameKind はコールスタックの 1 段がどのように積まれたか。
type FrameKind uint8

// 積まれ方の種類。
const (
	// FrameJSR は JSR による呼び出し。
	FrameJSR FrameKind = iota
	// FrameNMI は NMI による割り込み。
	FrameNMI
	// FrameIRQ は IRQ による割り込み。
	FrameIRQ
	// FrameBRK は BRK 命令。
	FrameBRK
)

// String は積まれ方の名前を返す。
func (k FrameKind) String() string {
	switch k {
	case FrameJSR:
		return "JSR"
	case FrameNMI:
		return "NMI"
	case FrameIRQ:
		return "IRQ"
	case FrameBRK:
		return "BRK"
	}
	return "?"
}

// CallFrame はコールスタックの 1 段。
type CallFrame struct {
	Kind FrameKind
	// From は呼び出した位置（割り込みでは割り込まれた命令の位置）。
	From uint16
	// To は呼び出し先。
	To uint16
	// S は呼び出す前のスタックポインタ。戻りの判定に使う。
	S uint8
}

// String は表示用の 1 行を返す。
func (f CallFrame) String() string {
	return fmt.Sprintf("%s $%04X ← $%04X", f.Kind, f.To, f.From)
}

// maxCallDepth は保持する段数の上限。
//
// JSR と RTS が釣り合わないプログラム（RTS を間接ジャンプに使うもの）で
// 際限なく積み上がらないようにする。
const maxCallDepth = 256

// CallStack は JSR・RTS・割り込み・RTI を追ってコールスタックを推定する。
//
// スタックを直接書き換えるプログラムでは実際の戻り先と一致しない。
// 戻りの判定をスタックポインタで行い、釣り合わない段は捨てる。表示では
// 推定であることを示す（設計書 09 編 §9.4）。
type CallStack struct {
	frames []CallFrame
}

// 観測する opcode。
const (
	opcodeJSR = 0x20
	opcodeRTS = 0x60
	opcodeRTI = 0x40
)

// OnExec は命令を実行する直前に呼ぶ。
func (c *CallStack) OnExec(pc uint16, peek Peeker, s uint8) {
	switch peek(pc) {
	case opcodeJSR:
		target := uint16(peek(pc+2))<<8 | uint16(peek(pc+1))
		c.push(CallFrame{Kind: FrameJSR, From: pc, To: target, S: s})
	case opcodeRTS:
		// JSR で 2 バイト積まれている。戻った後の S は今より 2 大きい。
		c.popTo(s + 2)
	case opcodeRTI:
		// 割り込みで 3 バイト積まれている。
		c.popTo(s + 3)
	}
}

// OnInterrupt は割り込みシーケンスを終えたときに呼ぶ。
//
// 割り込まれた位置はスタックに積まれた戻り先から求める。s は
// シーケンスを終えた後のスタックポインタである。
func (c *CallStack) OnInterrupt(k cpu.Interrupt, pc uint16, peek Peeker, s uint8) {
	if k == cpu.InterruptReset {
		c.frames = c.frames[:0]
		return
	}
	lo := peek(0x0100 | uint16(s+2))
	hi := peek(0x0100 | uint16(s+3))
	from := uint16(hi)<<8 | uint16(lo)
	kind := FrameIRQ
	switch k {
	case cpu.InterruptNMI:
		kind = FrameNMI
	case cpu.InterruptBRK:
		kind = FrameBRK
		// BRK は次の命令の 1 バイト先を積む。命令の位置へ戻して表示する。
		from -= 2
	}
	c.push(CallFrame{Kind: kind, From: from, To: pc, S: s + 3})
}

// push は 1 段積む。
func (c *CallStack) push(f CallFrame) {
	if len(c.frames) >= maxCallDepth {
		c.frames = c.frames[1:]
	}
	c.frames = append(c.frames, f)
}

// popTo は戻った後のスタックポインタ s より浅い段を捨てる。
//
// スタックは下へ伸びる。積む前の S が s 以下の段は、この戻りで
// 抜けたことになる。
func (c *CallStack) popTo(s uint8) {
	for len(c.frames) > 0 && c.frames[len(c.frames)-1].S <= s {
		c.frames = c.frames[:len(c.frames)-1]
	}
}

// Frames は外側から内側の順に写しを返す。
func (c *CallStack) Frames() []CallFrame { return append([]CallFrame(nil), c.frames...) }

// Reset は空にする。
func (c *CallStack) Reset() { c.frames = c.frames[:0] }
