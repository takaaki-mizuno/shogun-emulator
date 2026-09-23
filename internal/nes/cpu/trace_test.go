package cpu

import "testing"

// TestTraceRecordMatchesTraceLine は整形を後回しにした記録から作った
// 行が、State.TraceLine と一致することを確かめる。
//
// すべての opcode とアドレッシングモードについて、メモリの内容を
// 変えながら比べる。
func TestTraceRecordMatchesTraceLine(t *testing.T) {
	for code := range 256 {
		for _, seed := range []uint8{0x00, 0x37, 0xFE} {
			b := newFakeBus()
			// メモリを seed から決まる値で埋める。間接アドレッシングの
			// ポインタが様々な位置を指すようにする。
			for i := range b.mem {
				b.mem[i] = uint8(i*7) ^ seed
			}
			pc := uint16(0x8123)
			b.mem[pc] = uint8(code)
			b.mem[pc+1] = seed ^ 0x5A
			b.mem[pc+2] = seed ^ 0x03

			c := New(b)
			c.PC = pc
			c.X = seed ^ 0x11
			c.Y = seed ^ 0x22
			c.A = 0x33

			want := c.State(12, 34).TraceLine()
			got := c.TraceRecord(12, 34).Line()
			if got != want {
				t.Fatalf("opcode $%02X（seed $%02X）\n記録: %s\n期待: %s", code, seed, got, want)
			}
		}
	}
}

// TestTraceRecordSize は 1 行分が 32 バイトに収まることを確かめる。
func TestTraceRecordSize(t *testing.T) {
	if size := traceRecordSize(); size > 32 {
		t.Errorf("TraceRecord の大きさ = %d バイト, 上限 32", size)
	}
}

// TestModeValuesMatch は公開したモードの値が内部の値と一致することを
// 確かめる。Info は値をそのまま変換している。
func TestModeValuesMatch(t *testing.T) {
	pairs := map[addrMode]Mode{
		modeImplied: ModeImplied, modeAccumulator: ModeAccumulator,
		modeImmediate: ModeImmediate, modeZeroPage: ModeZeroPage,
		modeZeroPageX: ModeZeroPageX, modeZeroPageY: ModeZeroPageY,
		modeAbsolute: ModeAbsolute, modeAbsoluteX: ModeAbsoluteX,
		modeAbsoluteY: ModeAbsoluteY, modeIndirectX: ModeIndirectX,
		modeIndirectY: ModeIndirectY, modeRelative: ModeRelative,
		modeIndirect: ModeIndirect, modeJSR: ModeJSR,
	}
	for in, want := range pairs {
		if Mode(in) != want {
			t.Errorf("内部 %d が公開 %d と一致しない", in, want)
		}
	}
}
