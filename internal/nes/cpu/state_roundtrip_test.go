package cpu

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// TestCPUStateRoundtrip は設計書 03 編 §3.9 の「保存する状態」がすべて
// 往復することを確かめる。
//
// 割り込みの検出状態を省くと、復元直後に割り込みを取りこぼすか
// 二重に発生させる。
func TestCPUStateRoundtrip(t *testing.T) {
	c, _ := newTestCPU(t)

	// 既定値と異なる値をすべてのフィールドに入れる
	c.A, c.X, c.Y, c.S = 0x11, 0x22, 0x33, 0x44
	c.PC = 0x5566
	c.C, c.Z, c.I, c.D, c.V, c.N = true, false, true, true, false, true
	c.nmiLinePrev = false
	c.nmiPending = true
	c.pollNMI, c.pollIRQ = true, false
	c.pollNMIPrev, c.pollIRQPrev = false, true
	c.halted = true

	w := state.NewWriter()
	c.SaveState(w)
	blob := w.Data()

	// 別の CPU へ復元する
	restored, _ := newTestCPU(t)
	if err := restored.LoadState(state.NewReader(blob)); err != nil {
		t.Fatalf("復元に失敗した: %v", err)
	}

	// 直列化した結果を比べる。フィールドを直接比べないのは、直列化に
	// 含めていないフィールドを検出する必要があるためである。
	w2 := state.NewWriter()
	restored.SaveState(w2)
	if d := state.FirstDiff(blob, w2.Data()); d != "" {
		t.Errorf("往復で状態が一致しない: %s", d)
	}

	// 個別にも確かめる。差分の位置だけでは何が漏れたか分からない。
	checks := []struct {
		name      string
		got, want any
	}{
		{"A", restored.A, c.A},
		{"X", restored.X, c.X},
		{"Y", restored.Y, c.Y},
		{"S", restored.S, c.S},
		{"PC", restored.PC, c.PC},
		{"C", restored.C, c.C},
		{"Z", restored.Z, c.Z},
		{"I", restored.I, c.I},
		{"D", restored.D, c.D},
		{"V", restored.V, c.V},
		{"N", restored.N, c.N},
		{"nmiLinePrev", restored.nmiLinePrev, c.nmiLinePrev},
		{"nmiPending", restored.nmiPending, c.nmiPending},
		{"pollNMI", restored.pollNMI, c.pollNMI},
		{"pollIRQ", restored.pollIRQ, c.pollIRQ},
		{"pollNMIPrev", restored.pollNMIPrev, c.pollNMIPrev},
		{"pollIRQPrev", restored.pollIRQPrev, c.pollIRQPrev},
		{"halted", restored.halted, c.halted},
	}
	for _, ch := range checks {
		if ch.got != ch.want {
			t.Errorf("%s = %v, 期待 %v", ch.name, ch.got, ch.want)
		}
	}
}

// TestCPUStateRoundtripPreservesNMIDetection は復元後に NMI の検出が
// 正しく続くことを確かめる。
func TestCPUStateRoundtripPreservesNMIDetection(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(vectorNMI, 0x00, 0xA0)
	b.load(0x8000, 0xEA, 0xEA)

	// 線を下げたまま保存する。エッジは既に検出済み。
	b.nmiLine = false
	c.sampleInterruptLines()
	if !c.nmiPending {
		t.Fatal("エッジが検出されていない")
	}

	w := state.NewWriter()
	c.SaveState(w)

	restored := New(b)
	if err := restored.LoadState(state.NewReader(w.Data())); err != nil {
		t.Fatalf("復元に失敗した: %v", err)
	}

	// 線が下がったままでも、検出済みの NMI は 1 回だけ処理される
	restored.StepInstruction()
	if restored.PC != 0xA000 {
		t.Errorf("PC = $%04X, 期待 $A000（保持していた NMI が処理されること）", restored.PC)
	}

	b.load(0xA000, 0xEA)
	restored.StepInstruction()
	if restored.PC != 0xA001 {
		t.Errorf("PC = $%04X, 期待 $A001（レベルのままでは再発しないこと）", restored.PC)
	}
}
