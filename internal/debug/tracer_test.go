package debug

import (
	"bytes"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// record は PC だけを変えた記録を作る。NOP（$EA）を並べる。
func record(pc uint16) cpu.TraceRecord {
	return cpu.TraceRecord{PC: pc, Bytes: [3]uint8{0xEA}, Cycles: uint64(pc)}
}

// TestTracerKeepsLatest はリングが最新の命令を保持することを確かめる。
func TestTracerKeepsLatest(t *testing.T) {
	tr := NewTracer(4)
	for pc := uint16(0x8000); pc < 0x8006; pc++ {
		tr.Record(record(pc))
	}
	if tr.Len() != 4 {
		t.Fatalf("Len = %d, 期待 4", tr.Len())
	}
	var buf bytes.Buffer
	if _, err := tr.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("行数 = %d, 期待 4", len(lines))
	}
	// 古い順に 8002 から 8005 まで並ぶ。
	for i, want := range []string{"8002", "8003", "8004", "8005"} {
		if !strings.HasPrefix(lines[i], want) {
			t.Errorf("%d 行目 = %q, 期待 %s で始まる", i, lines[i], want)
		}
	}
	if got := tr.Last(2); len(got) != 2 || got[0].PC != 0x8004 || got[1].PC != 0x8005 {
		t.Errorf("Last(2) = %+v", got)
	}
}

// TestTracerStreams は常時の出力へ 1 行ずつ書くことを確かめる。
func TestTracerStreams(t *testing.T) {
	tr := NewTracer(2)
	var buf bytes.Buffer
	if err := tr.SetOutput(&buf); err != nil {
		t.Fatal(err)
	}
	for pc := uint16(0xC000); pc < 0xC003; pc++ {
		tr.Record(record(pc))
	}
	if err := tr.Flush(); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(buf.String(), "\n"); n != 3 {
		t.Errorf("出力の行数 = %d, 期待 3", n)
	}
}

// TestTracerDefaultMemory は既定のリングが 32 MiB に収まることを確かめる。
func TestTracerDefaultMemory(t *testing.T) {
	const limit = 32 * 1024 * 1024
	if n := DefaultTraceRingSize * traceRecordBytes; n > limit {
		t.Errorf("既定のリング = %d バイト, 上限 %d", n, limit)
	}
}
