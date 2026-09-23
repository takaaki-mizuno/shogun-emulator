package debug

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// memPeeker は map を読む Peeker を作る。
func memPeeker(mem map[uint16]uint8) Peeker {
	return func(a uint16) uint8 { return mem[a] }
}

// TestCallStackTracksJSRAndRTS は JSR で積み、RTS で降ろすことを確かめる。
func TestCallStackTracksJSRAndRTS(t *testing.T) {
	mem := map[uint16]uint8{
		0x8000: opcodeJSR, 0x8001: 0x00, 0x8002: 0x90, // JSR $9000
		0x9000: opcodeJSR, 0x9001: 0x00, 0x9002: 0xA0, // JSR $A000
		0xA000: opcodeRTS,
		0x9003: opcodeRTS,
	}
	peek := memPeeker(mem)
	var c CallStack
	c.OnExec(0x8000, peek, 0xFD)
	c.OnExec(0x9000, peek, 0xFB)
	if got := c.Frames(); len(got) != 2 || got[1].To != 0xA000 || got[0].To != 0x9000 {
		t.Fatalf("2 段積んだ後 = %+v", got)
	}
	c.OnExec(0xA000, peek, 0xF9) // RTS。戻った後の S は $FB
	if got := c.Frames(); len(got) != 1 || got[0].To != 0x9000 {
		t.Fatalf("1 段戻った後 = %+v", got)
	}
	c.OnExec(0x9003, peek, 0xFB)
	if got := c.Frames(); len(got) != 0 {
		t.Fatalf("戻り切った後 = %+v", got)
	}
}

// TestCallStackDistinguishesInterrupts は割り込みで入った段を区別し、
// RTI で降ろすことを確かめる。
func TestCallStackDistinguishesInterrupts(t *testing.T) {
	// 割り込まれた位置 $8123 がスタックの $01FC-$01FD に積まれている。
	mem := map[uint16]uint8{0x01FC: 0x23, 0x01FD: 0x81, 0xC000: opcodeRTI}
	peek := memPeeker(mem)
	var c CallStack
	c.OnInterrupt(cpu.InterruptNMI, 0xC000, peek, 0xFA)
	got := c.Frames()
	if len(got) != 1 || got[0].Kind != FrameNMI || got[0].From != 0x8123 || got[0].To != 0xC000 {
		t.Fatalf("NMI の段 = %+v", got)
	}
	c.OnExec(0xC000, peek, 0xFA)
	if len(c.Frames()) != 0 {
		t.Errorf("RTI で降りない: %+v", c.Frames())
	}
}

// TestCallStackDiscardsUnbalancedFrames は RTS をジャンプに使うような
// 釣り合わない積み方でも、戻り先より浅い段を捨てることを確かめる。
func TestCallStackDiscardsUnbalancedFrames(t *testing.T) {
	mem := map[uint16]uint8{
		0x8000: opcodeJSR, 0x8001: 0x00, 0x8002: 0x90,
		0x9000: opcodeJSR, 0x9001: 0x00, 0x9002: 0xA0,
		0xA000: opcodeRTS,
	}
	peek := memPeeker(mem)
	var c CallStack
	c.OnExec(0x8000, peek, 0xFD)
	c.OnExec(0x9000, peek, 0xFB)
	// スタックを 2 段分捨ててから RTS する。戻った後の S は $FD。
	c.OnExec(0xA000, peek, 0xFB)
	if len(c.Frames()) != 0 {
		t.Errorf("釣り合わない段が残った: %+v", c.Frames())
	}
}
