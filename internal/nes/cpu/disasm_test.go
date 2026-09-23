package cpu

import "testing"

// TestDisassembleOperandForms は各アドレッシングモードの表記を確かめる。
//
// 期待値は nestest.log の命令欄の形に合わせてある。実効アドレスと
// その位置の値を注記する形まで一致させる必要がある。
func TestDisassembleOperandForms(t *testing.T) {
	tests := []struct {
		name    string
		program []uint8
		// mem は事前に置くメモリ。アドレスから値の並び。
		mem      map[uint16][]uint8
		x, y     uint8
		wantText string
		wantLen  int
	}{
		{
			name:     "implied",
			program:  []uint8{0xEA},
			wantText: "NOP",
			wantLen:  1,
		},
		{
			name:     "accumulator",
			program:  []uint8{0x4A},
			wantText: "LSR A",
			wantLen:  1,
		},
		{
			name:     "immediate",
			program:  []uint8{0xA2, 0x00},
			wantText: "LDX #$00",
			wantLen:  2,
		},
		{
			name:     "zero page",
			program:  []uint8{0x86, 0x00},
			mem:      map[uint16][]uint8{0x0000: {0x00}},
			wantText: "STX $00 = 00",
			wantLen:  2,
		},
		{
			name:     "zero page,X",
			program:  []uint8{0x95, 0x00},
			mem:      map[uint16][]uint8{0x0055: {0x00}},
			x:        0x55,
			wantText: "STA $00,X @ 55 = 00",
			wantLen:  2,
		},
		{
			name:     "zero page,Y",
			program:  []uint8{0xB6, 0x80},
			mem:      map[uint16][]uint8{0x007F: {0x97}},
			y:        0xFF,
			wantText: "LDX $80,Y @ 7F = 97",
			wantLen:  2,
		},
		{
			name:     "absolute",
			program:  []uint8{0xAD, 0x47, 0x06},
			mem:      map[uint16][]uint8{0x0647: {0x52}},
			wantText: "LDA $0647 = 52",
			wantLen:  3,
		},
		{
			name:     "absolute（JMP は注記しない）",
			program:  []uint8{0x4C, 0xF5, 0xC5},
			wantText: "JMP $C5F5",
			wantLen:  3,
		},
		{
			name:     "absolute,X",
			program:  []uint8{0x9D, 0x00, 0x06},
			mem:      map[uint16][]uint8{0x0655: {0x00}},
			x:        0x55,
			wantText: "STA $0600,X @ 0655 = 00",
			wantLen:  3,
		},
		{
			name:     "absolute,Y",
			program:  []uint8{0x19, 0x00, 0x04},
			mem:      map[uint16][]uint8{0x0400: {0xAA}},
			y:        0x00,
			wantText: "ORA $0400,Y @ 0400 = AA",
			wantLen:  3,
		},
		{
			name:    "(d,X)",
			program: []uint8{0xA1, 0x80},
			mem: map[uint16][]uint8{
				0x0080: {0x00, 0x02},
				0x0200: {0x5A},
			},
			x:        0x00,
			wantText: "LDA ($80,X) @ 80 = 0200 = 5A",
			wantLen:  2,
		},
		{
			name:    "(d),Y",
			program: []uint8{0xB1, 0x97},
			mem: map[uint16][]uint8{
				0x0097: {0xFF, 0xFF},
				0x0033: {0xA3},
			},
			y:        0x34,
			wantText: "LDA ($97),Y = FFFF @ 0033 = A3",
			wantLen:  2,
		},
		{
			name:     "relative",
			program:  []uint8{0xD0, 0x05},
			wantText: "BNE $C007",
			wantLen:  2,
		},
		{
			name:    "absolute indirect",
			program: []uint8{0x6C, 0x00, 0x02},
			mem: map[uint16][]uint8{
				0x0200: {0x7E, 0xDB},
			},
			wantText: "JMP ($0200) = DB7E",
			wantLen:  3,
		},
		{
			name:    "absolute indirect のページ境界バグ",
			program: []uint8{0x6C, 0xFF, 0x02},
			mem: map[uint16][]uint8{
				0x02FF: {0x00},
				0x0200: {0x03},
				0x0300: {0xFF},
			},
			wantText: "JMP ($02FF) = 0300",
			wantLen:  3,
		},
		{
			name:     "JSR",
			program:  []uint8{0x20, 0xD2, 0xC7},
			wantText: "JSR $C7D2",
			wantLen:  3,
		},
	}

	const base = 0xC000
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := map[uint16]uint8{}
			for i, v := range tt.program {
				mem[base+uint16(i)] = v
			}
			for addr, vs := range tt.mem {
				for i, v := range vs {
					mem[addr+uint16(i)] = v
				}
			}
			peek := func(addr uint16) uint8 { return mem[addr] }

			text, n := Disassemble(peek, base, tt.x, tt.y)
			if text != tt.wantText {
				t.Errorf("表記 = %q, 期待 %q", text, tt.wantText)
			}
			if n != tt.wantLen {
				t.Errorf("長さ = %d, 期待 %d", n, tt.wantLen)
			}
		})
	}
}

// TestTraceLineMatchesNestestFirstLine はトレース行の桁位置が
// nestest.log と一致することを確かめる。
//
// 期待値は nestest.log の 1 行目をそのまま書き写したものである。
func TestTraceLineMatchesNestestFirstLine(t *testing.T) {
	const want = "C000  4C F5 C5  JMP $C5F5                       " +
		"A:00 X:00 Y:00 P:24 SP:FD PPU:  0, 21 CYC:7"

	s := State{
		PC:          0xC000,
		A:           0x00,
		X:           0x00,
		Y:           0x00,
		P:           0x24,
		S:           0xFD,
		Bytes:       [3]uint8{0x4C, 0xF5, 0xC5},
		ByteLen:     3,
		Disasm:      "JMP $C5F5",
		Official:    true,
		PPUScanline: 0,
		PPUDot:      21,
		Cycles:      7,
	}

	got := s.TraceLine()
	if got != want {
		t.Errorf("トレース行が一致しない\n実際: %q\n期待: %q", got, want)
	}
	// レジスタの欄は 48 桁目から始まる
	if i := indexOf(got, "A:"); i != 48 {
		t.Errorf("\"A:\" の桁位置 = %d, 期待 48", i)
	}
}

// TestTraceLineMarksUnofficial は非公式命令に * が付くことを確かめる。
//
// 期待値は nestest.log の非公式命令の行の形に合わせてある。
func TestTraceLineMarksUnofficial(t *testing.T) {
	const want = "C6BD  04 A9    *NOP $A9 = 00                    " +
		"A:AA X:97 Y:4E P:EF SP:F9 PPU:128, 89 CYC:14579"

	s := State{
		PC:          0xC6BD,
		A:           0xAA,
		X:           0x97,
		Y:           0x4E,
		P:           0xEF,
		S:           0xF9,
		Bytes:       [3]uint8{0x04, 0xA9},
		ByteLen:     2,
		Disasm:      "NOP $A9 = 00",
		Official:    false,
		PPUScanline: 128,
		PPUDot:      89,
		Cycles:      14579,
	}

	if got := s.TraceLine(); got != want {
		t.Errorf("トレース行が一致しない\n実際: %q\n期待: %q", got, want)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
