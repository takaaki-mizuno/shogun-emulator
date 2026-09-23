package cpu

import "testing"

// officialTable は docs/research/02_cpu_6502.md の 6.1 節の表を
// opcode・ニモニック・バイト数・サイクル数の形で書き写したもの。
//
// cycles は「ページ境界をまたがず、分岐が成立しない」場合の値とする。
var officialTable = []struct {
	code     uint8
	mnemonic string
	bytes    int
	cycles   int
}{
	// ロード / ストア
	{0xA9, "LDA", 2, 2}, {0xA5, "LDA", 2, 3}, {0xB5, "LDA", 2, 4}, {0xAD, "LDA", 3, 4},
	{0xBD, "LDA", 3, 4}, {0xB9, "LDA", 3, 4}, {0xA1, "LDA", 2, 6}, {0xB1, "LDA", 2, 5},
	{0xA2, "LDX", 2, 2}, {0xA6, "LDX", 2, 3}, {0xB6, "LDX", 2, 4}, {0xAE, "LDX", 3, 4},
	{0xBE, "LDX", 3, 4},
	{0xA0, "LDY", 2, 2}, {0xA4, "LDY", 2, 3}, {0xB4, "LDY", 2, 4}, {0xAC, "LDY", 3, 4},
	{0xBC, "LDY", 3, 4},
	{0x85, "STA", 2, 3}, {0x95, "STA", 2, 4}, {0x8D, "STA", 3, 4}, {0x9D, "STA", 3, 5},
	{0x99, "STA", 3, 5}, {0x81, "STA", 2, 6}, {0x91, "STA", 2, 6},
	{0x86, "STX", 2, 3}, {0x96, "STX", 2, 4}, {0x8E, "STX", 3, 4},
	{0x84, "STY", 2, 3}, {0x94, "STY", 2, 4}, {0x8C, "STY", 3, 4},

	// 演算 / 論理 / 比較
	{0x69, "ADC", 2, 2}, {0x65, "ADC", 2, 3}, {0x75, "ADC", 2, 4}, {0x6D, "ADC", 3, 4},
	{0x7D, "ADC", 3, 4}, {0x79, "ADC", 3, 4}, {0x61, "ADC", 2, 6}, {0x71, "ADC", 2, 5},
	{0xE9, "SBC", 2, 2}, {0xE5, "SBC", 2, 3}, {0xF5, "SBC", 2, 4}, {0xED, "SBC", 3, 4},
	{0xFD, "SBC", 3, 4}, {0xF9, "SBC", 3, 4}, {0xE1, "SBC", 2, 6}, {0xF1, "SBC", 2, 5},
	{0x29, "AND", 2, 2}, {0x25, "AND", 2, 3}, {0x35, "AND", 2, 4}, {0x2D, "AND", 3, 4},
	{0x3D, "AND", 3, 4}, {0x39, "AND", 3, 4}, {0x21, "AND", 2, 6}, {0x31, "AND", 2, 5},
	{0x09, "ORA", 2, 2}, {0x05, "ORA", 2, 3}, {0x15, "ORA", 2, 4}, {0x0D, "ORA", 3, 4},
	{0x1D, "ORA", 3, 4}, {0x19, "ORA", 3, 4}, {0x01, "ORA", 2, 6}, {0x11, "ORA", 2, 5},
	{0x49, "EOR", 2, 2}, {0x45, "EOR", 2, 3}, {0x55, "EOR", 2, 4}, {0x4D, "EOR", 3, 4},
	{0x5D, "EOR", 3, 4}, {0x59, "EOR", 3, 4}, {0x41, "EOR", 2, 6}, {0x51, "EOR", 2, 5},
	{0xC9, "CMP", 2, 2}, {0xC5, "CMP", 2, 3}, {0xD5, "CMP", 2, 4}, {0xCD, "CMP", 3, 4},
	{0xDD, "CMP", 3, 4}, {0xD9, "CMP", 3, 4}, {0xC1, "CMP", 2, 6}, {0xD1, "CMP", 2, 5},
	{0xE0, "CPX", 2, 2}, {0xE4, "CPX", 2, 3}, {0xEC, "CPX", 3, 4},
	{0xC0, "CPY", 2, 2}, {0xC4, "CPY", 2, 3}, {0xCC, "CPY", 3, 4},
	{0x24, "BIT", 2, 3}, {0x2C, "BIT", 3, 4},

	// RMW
	{0x0A, "ASL", 1, 2}, {0x06, "ASL", 2, 5}, {0x16, "ASL", 2, 6}, {0x0E, "ASL", 3, 6},
	{0x1E, "ASL", 3, 7},
	{0x4A, "LSR", 1, 2}, {0x46, "LSR", 2, 5}, {0x56, "LSR", 2, 6}, {0x4E, "LSR", 3, 6},
	{0x5E, "LSR", 3, 7},
	{0x2A, "ROL", 1, 2}, {0x26, "ROL", 2, 5}, {0x36, "ROL", 2, 6}, {0x2E, "ROL", 3, 6},
	{0x3E, "ROL", 3, 7},
	{0x6A, "ROR", 1, 2}, {0x66, "ROR", 2, 5}, {0x76, "ROR", 2, 6}, {0x6E, "ROR", 3, 6},
	{0x7E, "ROR", 3, 7},
	{0xE6, "INC", 2, 5}, {0xF6, "INC", 2, 6}, {0xEE, "INC", 3, 6}, {0xFE, "INC", 3, 7},
	{0xC6, "DEC", 2, 5}, {0xD6, "DEC", 2, 6}, {0xCE, "DEC", 3, 6}, {0xDE, "DEC", 3, 7},
	{0xE8, "INX", 1, 2}, {0xC8, "INY", 1, 2}, {0xCA, "DEX", 1, 2}, {0x88, "DEY", 1, 2},

	// 転送
	{0xAA, "TAX", 1, 2}, {0xA8, "TAY", 1, 2}, {0x8A, "TXA", 1, 2}, {0x98, "TYA", 1, 2},
	{0xBA, "TSX", 1, 2}, {0x9A, "TXS", 1, 2},

	// スタック
	{0x48, "PHA", 1, 3}, {0x08, "PHP", 1, 3}, {0x68, "PLA", 1, 4}, {0x28, "PLP", 1, 4},

	// 分岐
	{0x10, "BPL", 2, 2}, {0x30, "BMI", 2, 2}, {0x50, "BVC", 2, 2}, {0x70, "BVS", 2, 2},
	{0x90, "BCC", 2, 2}, {0xB0, "BCS", 2, 2}, {0xD0, "BNE", 2, 2}, {0xF0, "BEQ", 2, 2},

	// ジャンプ / 割り込み
	{0x4C, "JMP", 3, 3}, {0x6C, "JMP", 3, 5}, {0x20, "JSR", 3, 6},
	{0x60, "RTS", 1, 6}, {0x00, "BRK", 1, 7}, {0x40, "RTI", 1, 6},

	// フラグ / その他
	{0x18, "CLC", 1, 2}, {0x38, "SEC", 1, 2}, {0x58, "CLI", 1, 2}, {0x78, "SEI", 1, 2},
	{0xD8, "CLD", 1, 2}, {0xF8, "SED", 1, 2}, {0xB8, "CLV", 1, 2}, {0xEA, "NOP", 1, 2},
}

// TestOfficialOpcodeCount は公式命令が 151 個であることを確かめる。
func TestOfficialOpcodeCount(t *testing.T) {
	if len(officialTable) != 151 {
		t.Errorf("転記した表の項目数 = %d, 期待 151", len(officialTable))
	}
	n := 0
	for i := range opcodes {
		if opcodes[i].official {
			n++
		}
	}
	if n != 151 {
		t.Errorf("official な opcode = %d 個, 期待 151", n)
	}
}

// TestAllOpcodesHaveExec は 256 個すべてに実装が入っていることを確かめる。
// 未実装の opcode でエミュレータが nil 参照で落ちてはならない。
func TestAllOpcodesHaveExec(t *testing.T) {
	for i := range opcodes {
		if opcodes[i].exec == nil {
			t.Errorf("opcode $%02X に exec が無い", i)
		}
		if opcodes[i].mnemonic == "" {
			t.Errorf("opcode $%02X にニモニックが無い", i)
		}
	}
}

// TestOpcodeTableMatchesResearch は転記した表と実装の表が
// ニモニック・バイト数・サイクル数で一致することを確かめる。
func TestOpcodeTableMatchesResearch(t *testing.T) {
	seen := map[uint8]bool{}
	for _, tc := range officialTable {
		if seen[tc.code] {
			t.Errorf("転記した表に $%02X が重複している", tc.code)
		}
		seen[tc.code] = true

		op := &opcodes[tc.code]
		if !op.official {
			t.Errorf("$%02X（%s）が official になっていない", tc.code, tc.mnemonic)
			continue
		}
		if op.mnemonic != tc.mnemonic {
			t.Errorf("$%02X のニモニック = %s, 期待 %s", tc.code, op.mnemonic, tc.mnemonic)
		}
		if got := 1 + op.mode.operandLen(); got != tc.bytes {
			t.Errorf("$%02X（%s）のバイト数 = %d, 期待 %d", tc.code, tc.mnemonic, got, tc.bytes)
		}
		if got := measureCycles(t, tc.code, op); got != tc.cycles {
			t.Errorf("$%02X（%s）のサイクル数 = %d, 期待 %d", tc.code, tc.mnemonic, got, tc.cycles)
		}
	}

	// 実装側にあって転記した表に無い official なエントリが無いことを確かめる
	for i := range opcodes {
		if opcodes[i].official && !seen[uint8(i)] {
			t.Errorf("$%02X（%s）が転記した表に無い", i, opcodes[i].mnemonic)
		}
	}
}

// measureCycles は 1 命令を実行してサイクル数を数える。
//
// ページ境界をまたがず、分岐が成立しない条件を作る。インデックスを 0 に
// し、分岐のフラグを不成立側に倒す。
func measureCycles(t *testing.T, code uint8, op *opcode) int {
	t.Helper()
	c, b := newTestCPU(t)
	c.X, c.Y = 0, 0
	// 分岐が成立しない状態にする。成立すると 1 サイクル増えるため、
	// 条件が偽になる側へフラグを倒す。
	c.C, c.Z, c.V, c.N = false, false, false, false
	switch op.mnemonic {
	case "BPL":
		c.N = true
	case "BVC":
		c.V = true
	case "BCC":
		c.C = true
	case "BNE":
		c.Z = true
	}
	// オペランドは $10 と $0010 を指すようにする。ゼロページ内に収め、
	// ページ境界をまたがせない。
	b.load(0x8000, code, 0x10, 0x00)

	before := c.Cycles()
	c.StepInstruction()
	return int(c.Cycles() - before)
}
