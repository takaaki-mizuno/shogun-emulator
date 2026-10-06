package agent

import (
	"fmt"
	"testing"
)

// フェーズ 15 の性能の確認（計画 §3.18）。結果は計画の末尾に記録する。

// newBenchSession は NMI が動き始めた Instance を作る。
func newBenchSession(b *testing.B) *agentSession {
	t := &testing.T{}
	s := newSession(t)
	s.t = t
	s.warm()
	return s
}

// BenchmarkStep3600 は headless で exec.step {frames: 3600} の所要時間を測る。
func BenchmarkStep3600(b *testing.B) {
	s := newBenchSession(b)
	for b.Loop() {
		s.must("exec.step", map[string]any{"frames": 3600, "observe": false}, nil)
	}
}

// BenchmarkStepWithImage は毎フレーム画像を付けたときの 1 往復を測る。
func BenchmarkStepWithImage(b *testing.B) {
	s := newBenchSession(b)
	for b.Loop() {
		s.must("exec.step", map[string]any{"observe": map[string]any{"include": []string{"image"}}}, nil)
	}
}

// BenchmarkStepWatchFreeze はウォッチ 32 件と Freeze 16 件を置いたときの
// exec.step {frames: 60} を測る。
func BenchmarkStepWatchFreeze(b *testing.B) {
	s := newBenchSession(b)
	for i := range 32 {
		s.must("debug.watch.add", map[string]any{"loc": fmt.Sprintf("$%04X", 0x0300+i)}, nil)
	}
	for i := range 16 {
		s.must("mem.freeze", map[string]any{"loc": fmt.Sprintf("$%04X", 0x0400+i), "value": i}, nil)
	}
	for b.Loop() {
		s.must("exec.step", map[string]any{"frames": 60}, nil)
	}
}

// BenchmarkStep60Plain は比較のため何も置かずに exec.step {frames: 60} を測る。
func BenchmarkStep60Plain(b *testing.B) {
	s := newBenchSession(b)
	for b.Loop() {
		s.must("exec.step", map[string]any{"frames": 60}, nil)
	}
}
