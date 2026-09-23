package cpu

import "testing"

// TestUnofficialOpcodeCount は非公式命令が 105 個であることを確かめる。
// 公式 151 個と合わせて 256 個になる。
func TestUnofficialOpcodeCount(t *testing.T) {
	n := 0
	for i := range opcodes {
		if !opcodes[i].official {
			n++
		}
	}
	if n != 105 {
		t.Errorf("非公式の opcode = %d 個, 期待 105", n)
	}
}

// TestNoUndefinedOpcodeRemains は未実装の opcode が残っていないことを
// 確かめる。実機では 256 個すべてが定まった動作をする。
func TestNoUndefinedOpcodeRemains(t *testing.T) {
	for i := range opcodes {
		if opcodes[i].mnemonic == "???" {
			t.Errorf("opcode $%02X が未実装のまま", i)
		}
	}
}

// TestRMWComboCycles は RMW 系の非公式命令のサイクル数を確かめる。
//
// a,X・a,Y・(d),Y ではページ境界をまたがなくても常にペナルティサイクルが
// 付く。ストア命令と同じ理由である。
func TestRMWComboCycles(t *testing.T) {
	// codes の並びは (d,X) / d / a / (d),Y / d,X / a,Y / a,X
	wantCycles := []int{8, 5, 6, 8, 6, 7, 7}
	names := []string{"(d,X)", "d", "a", "(d),Y", "d,X", "a,Y", "a,X"}
	combos := map[string][]uint8{
		"SLO": {0x03, 0x07, 0x0F, 0x13, 0x17, 0x1B, 0x1F},
		"RLA": {0x23, 0x27, 0x2F, 0x33, 0x37, 0x3B, 0x3F},
		"SRE": {0x43, 0x47, 0x4F, 0x53, 0x57, 0x5B, 0x5F},
		"RRA": {0x63, 0x67, 0x6F, 0x73, 0x77, 0x7B, 0x7F},
		"DCP": {0xC3, 0xC7, 0xCF, 0xD3, 0xD7, 0xDB, 0xDF},
		"ISB": {0xE3, 0xE7, 0xEF, 0xF3, 0xF7, 0xFB, 0xFF},
	}
	// map の走査順に依存しないように名前を並べる
	order := []string{"SLO", "RLA", "SRE", "RRA", "DCP", "ISB"}

	for _, name := range order {
		codes := combos[name]
		for i, code := range codes {
			t.Run(name+" "+names[i], func(t *testing.T) {
				if opcodes[code].mnemonic != name {
					t.Fatalf("$%02X のニモニック = %s, 期待 %s", code, opcodes[code].mnemonic, name)
				}
				c, b := newTestCPU(t)
				// ページ境界をまたがない条件にする
				c.X, c.Y = 0, 0
				b.load(0x8000, code, 0x10, 0x00)
				before := c.Cycles()
				c.StepInstruction()
				if got := int(c.Cycles() - before); got != wantCycles[i] {
					t.Errorf("サイクル数 = %d, 期待 %d\nアクセス: %v", got, wantCycles[i], b.log)
				}
			})
		}
	}
}

// TestRMWComboAlwaysPenalizes は a,X でページ境界をまたいでも
// サイクル数が変わらないことを確かめる。
func TestRMWComboAlwaysPenalizes(t *testing.T) {
	for _, code := range []uint8{0x1F, 0x3F, 0x5F, 0x7F, 0xDF, 0xFF} {
		c, b := newTestCPU(t)
		c.X = 0xFF
		b.load(0x8000, code, 0x01, 0x20) // $2001,X → $2100 でページ越え
		before := c.Cycles()
		c.StepInstruction()
		if got := int(c.Cycles() - before); got != 7 {
			t.Errorf("$%02X（%s a,X ページ越え）のサイクル数 = %d, 期待 7",
				code, opcodes[code].mnemonic, got)
		}
	}
}

// TestSLOSemantics は SLO が ASL の後に ORA を行うことを確かめる。
func TestSLOSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x01
	b.load(0x8000, 0x07, 0x10) // SLO $10
	b.load(0x0010, 0x81)       // ASL で $02、C = 1

	c.StepInstruction()

	if b.mem[0x0010] != 0x02 {
		t.Errorf("メモリ = $%02X, 期待 $02（ASL の結果）", b.mem[0x0010])
	}
	if c.A != 0x03 {
		t.Errorf("A = $%02X, 期待 $03（$01 | $02）", c.A)
	}
	if !c.C {
		t.Error("ASL の桁上がりが C に入っていない")
	}
	// 元の値を書き戻してから変更後の値を書く
	assertLog(t, b, []string{"R:8000", "R:8001", "R:0010", "W:0010=81", "W:0010=02"})
}

// TestDCPSemantics は DCP が DEC の後に CMP を行うことを確かめる。
func TestDCPSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x40
	b.load(0x8000, 0xC7, 0x10) // DCP $10
	b.load(0x0010, 0x41)       // DEC で $40。A と等しくなる

	c.StepInstruction()

	if b.mem[0x0010] != 0x40 {
		t.Errorf("メモリ = $%02X, 期待 $40", b.mem[0x0010])
	}
	if !c.Z {
		t.Error("A と等しいので Z が立つこと")
	}
	if !c.C {
		t.Error("A >= M なので C が立つこと")
	}
}

// TestISBSemantics は ISB が INC の後に SBC を行うことを確かめる。
func TestISBSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x50
	c.C = true
	b.load(0x8000, 0xE7, 0x10) // ISB $10
	b.load(0x0010, 0x0F)       // INC で $10

	c.StepInstruction()

	if b.mem[0x0010] != 0x10 {
		t.Errorf("メモリ = $%02X, 期待 $10", b.mem[0x0010])
	}
	if c.A != 0x40 {
		t.Errorf("A = $%02X, 期待 $40（$50 - $10）", c.A)
	}
}

// TestLAXLoadsBothRegisters は LAX が A と X の両方にロードすることを
// 確かめる。
func TestLAXLoadsBothRegisters(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0xA7, 0x10) // LAX $10
	b.load(0x0010, 0x80)

	c.StepInstruction()

	if c.A != 0x80 || c.X != 0x80 {
		t.Errorf("A = $%02X, X = $%02X, どちらも $80 を期待", c.A, c.X)
	}
	if !c.N {
		t.Error("N が立つこと")
	}
}

// TestSAXWritesAndX は SAX が A & X を書き、フラグを変えないことを確かめる。
func TestSAXWritesAndX(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0xF0
	c.X = 0x3C
	c.Z, c.N, c.C = true, true, true
	b.load(0x8000, 0x87, 0x10) // SAX $10

	c.StepInstruction()

	if b.mem[0x0010] != 0x30 {
		t.Errorf("メモリ = $%02X, 期待 $30（$F0 & $3C）", b.mem[0x0010])
	}
	if !c.Z || !c.N || !c.C {
		t.Error("SAX はフラグを変えないこと")
	}
}

// TestALRSemantics は ALR が AND の後に LSR A を行うことを確かめる。
func TestALRSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0xFF
	b.load(0x8000, 0x4B, 0x03) // ALR #$03

	c.StepInstruction()

	// $FF & $03 = $03 → LSR で $01、C = 1
	if c.A != 0x01 {
		t.Errorf("A = $%02X, 期待 $01", c.A)
	}
	if !c.C {
		t.Error("シフトで押し出された bit 0 が C に入ること")
	}
}

// TestANCCopiesNToC は ANC が AND の後に N を C へコピーすることを確かめる。
func TestANCCopiesNToC(t *testing.T) {
	tests := []struct {
		a, imm uint8
		wantA  uint8
		wantC  bool
	}{
		{0xFF, 0x80, 0x80, true},  // 結果の bit 7 が立つ
		{0xFF, 0x7F, 0x7F, false}, // 立たない
	}
	for _, tt := range tests {
		c, b := newTestCPU(t)
		c.A = tt.a
		b.load(0x8000, 0x0B, tt.imm) // ANC #

		c.StepInstruction()

		if c.A != tt.wantA {
			t.Errorf("A = $%02X, 期待 $%02X", c.A, tt.wantA)
		}
		if c.C != tt.wantC {
			t.Errorf("ANC #$%02X の C = %v, 期待 %v", tt.imm, c.C, tt.wantC)
		}
	}
}

// TestARRFlagsDifferFromROR は ARR のフラグが公式の ROR と異なることを
// 確かめる。C は結果の bit 6、V は bit 6 と bit 5 の排他的論理和になる。
func TestARRFlagsDifferFromROR(t *testing.T) {
	tests := []struct {
		a, imm  uint8
		carryIn bool
		wantA   uint8
		wantC   bool
		wantV   bool
	}{
		// $C0 を右回転 → $60。bit 6 = 1、bit 5 = 1 → C = 1、V = 0
		{0xFF, 0xC0, false, 0x60, true, false},
		// $80 を右回転 → $40。bit 6 = 1、bit 5 = 0 → C = 1、V = 1
		{0xFF, 0x80, false, 0x40, true, true},
		// $40 を右回転 → $20。bit 6 = 0、bit 5 = 1 → C = 0、V = 1
		{0xFF, 0x40, false, 0x20, false, true},
		// $00 を右回転 → $00。どちらも 0 → C = 0、V = 0
		{0xFF, 0x00, false, 0x00, false, false},
	}
	for _, tt := range tests {
		c, b := newTestCPU(t)
		c.A = tt.a
		c.C = tt.carryIn
		b.load(0x8000, 0x6B, tt.imm) // ARR #

		c.StepInstruction()

		if c.A != tt.wantA || c.C != tt.wantC || c.V != tt.wantV {
			t.Errorf("ARR #$%02X → A=$%02X C=%v V=%v、期待 A=$%02X C=%v V=%v",
				tt.imm, c.A, c.C, c.V, tt.wantA, tt.wantC, tt.wantV)
		}
	}

	// 同じ入力で公式の ROR A を実行し、フラグが違うことを確かめる
	c, b := newTestCPU(t)
	c.A = 0x80
	c.C = false
	b.load(0x8000, 0x6A) // ROR A
	c.StepInstruction()
	if c.A != 0x40 {
		t.Fatalf("ROR A の結果 = $%02X, 期待 $40", c.A)
	}
	if c.C {
		t.Error("ROR A では bit 0 が C に入る。ARR とは違う規則であること")
	}
}

// TestAXSSemantics は AXS が X = (A & X) - i を行い、V を変えないことを
// 確かめる。
func TestAXSSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0xF0
	c.X = 0x3C
	c.V = true
	b.load(0x8000, 0xCB, 0x10) // AXS #$10

	c.StepInstruction()

	// ($F0 & $3C) - $10 = $30 - $10 = $20
	if c.X != 0x20 {
		t.Errorf("X = $%02X, 期待 $20", c.X)
	}
	if !c.C {
		t.Error("借りが出ないので C が立つこと")
	}
	if !c.V {
		t.Error("AXS は V を変えないこと")
	}
}

// TestAXSBorrow は借りが出たときの C を確かめる。
func TestAXSBorrow(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x0F
	c.X = 0x0F
	b.load(0x8000, 0xCB, 0x10) // AXS #$10

	c.StepInstruction()

	if c.X != 0xFF {
		t.Errorf("X = $%02X, 期待 $FF（$0F - $10）", c.X)
	}
	if c.C {
		t.Error("借りが出たので C は下りること")
	}
}

// TestUnofficialSBCMatchesOfficial は $EB が公式の $E9 と同じ動作をする
// ことを確かめる。
func TestUnofficialSBCMatchesOfficial(t *testing.T) {
	run := func(code uint8) (a uint8, cf, vf, zf, nf bool) {
		c, b := newTestCPU(t)
		c.A = 0x50
		c.C = true
		b.load(0x8000, code, 0x60)
		c.StepInstruction()
		return c.A, c.C, c.V, c.Z, c.N
	}
	a1, c1, v1, z1, n1 := run(0xE9)
	a2, c2, v2, z2, n2 := run(0xEB)
	if a1 != a2 || c1 != c2 || v1 != v2 || z1 != z2 || n1 != n2 {
		t.Errorf("$EB と $E9 の結果が違う: $EB=(A=$%02X C=%v V=%v Z=%v N=%v) $E9=(A=$%02X C=%v V=%v Z=%v N=%v)",
			a2, c2, v2, z2, n2, a1, c1, v1, z1, n1)
	}
}

// TestNOPVariantsCycles は NOP 系のサイクル数を確かめる。
func TestNOPVariantsCycles(t *testing.T) {
	tests := []struct {
		name  string
		code  uint8
		x     uint8
		want  int
		bytes []uint8
	}{
		{"1 バイト NOP", 0x1A, 0, 2, nil},
		{"2 バイト NOP", 0x80, 0, 2, []uint8{0x10}},
		{"IGN d", 0x04, 0, 3, []uint8{0x10}},
		{"IGN d,X", 0x14, 1, 4, []uint8{0x10}},
		{"IGN a", 0x0C, 0, 4, []uint8{0x00, 0x20}},
		{"IGN a,X ページ内", 0x1C, 1, 4, []uint8{0x00, 0x20}},
		{"IGN a,X ページ越え", 0x1C, 0xFF, 5, []uint8{0x01, 0x20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, b := newTestCPU(t)
			c.X = tt.x
			b.load(0x8000, append([]uint8{tt.code}, tt.bytes...)...)
			before := c.Cycles()
			c.StepInstruction()
			if got := int(c.Cycles() - before); got != tt.want {
				t.Errorf("サイクル数 = %d, 期待 %d\nアクセス: %v", got, tt.want, b.log)
			}
		})
	}
}

// TestIGNReadsAppearOnBus は IGN のリードがバスに現れることを確かめる。
//
// 読み捨てであってもリードサイクルが発生する。PPU レジスタに当たれば
// 副作用が起きるため、省略してはならない。
func TestIGNReadsAppearOnBus(t *testing.T) {
	// IGN a は絶対アドレスを読む
	c, b := newTestCPU(t)
	b.load(0x8000, 0x0C, 0x02, 0x20) // NOP $2002
	c.StepInstruction()
	assertLog(t, b, []string{"R:8000", "R:8001", "R:8002", "R:2002"})

	// IGN d,X は d と (d+X)&$FF の両方を読む
	c2, b2 := newTestCPU(t)
	c2.X = 0x30
	b2.load(0x8000, 0x14, 0x10) // NOP $10,X
	c2.StepInstruction()
	assertLog(t, b2, []string{"R:8000", "R:8001", "R:0010", "R:0040"})
}

// TestSKBReadsImmediate は 2 バイト NOP が即値を読むことを確かめる。
func TestSKBReadsImmediate(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x89, 0x42) // NOP #$42
	c.StepInstruction()
	assertLog(t, b, []string{"R:8000", "R:8001"})
	if c.PC != 0x8002 {
		t.Errorf("PC = $%04X, 期待 $8002", c.PC)
	}
}

// TestXAASemantics は XAA が A = (A | $EE) & X & i を行うことを確かめる。
func TestXAASemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x00
	c.X = 0xFF
	b.load(0x8000, 0x8B, 0xFF) // XAA #$FF

	c.StepInstruction()

	if c.A != 0xEE {
		t.Errorf("A = $%02X, 期待 $EE（($00 | $EE) & $FF & $FF）", c.A)
	}
}

// TestLAXImmediateSemantics は $AB が即値をそのまま A と X に入れることを
// 確かめる。
func TestLAXImmediateSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x0F
	b.load(0x8000, 0xAB, 0x5A) // LAX #$5A

	c.StepInstruction()

	if c.A != 0x5A || c.X != 0x5A {
		t.Errorf("A = $%02X, X = $%02X, どちらも $5A を期待", c.A, c.X)
	}
}

// TestLASSemantics は LAS が A・X・S を memory & S にすることを確かめる。
func TestLASSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.S = 0xF0
	c.Y = 0
	b.load(0x8000, 0xBB, 0x10, 0x00) // LAS $0010,Y
	b.load(0x0010, 0x3C)

	c.StepInstruction()

	if c.A != 0x30 || c.X != 0x30 || c.S != 0x30 {
		t.Errorf("A = $%02X, X = $%02X, S = $%02X, すべて $30 を期待（$3C & $F0）",
			c.A, c.X, c.S)
	}
}

// TestSHXWithoutPageCross はページ境界をまたがないときの SHX を確かめる。
func TestSHXWithoutPageCross(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0xFF
	c.Y = 0x01
	b.load(0x8000, 0x9E, 0x00, 0x20) // SHX $2000,Y → $2001

	c.StepInstruction()

	// 値は X & (上位バイト $20 + 1) = $FF & $21 = $21
	if b.mem[0x2001] != 0x21 {
		t.Errorf("$2001 = $%02X, 期待 $21", b.mem[0x2001])
	}
}

// TestSHXWithPageCross はページ境界をまたぐとき、書き込み先アドレスの
// 上位バイトも書き込む値に置き換わることを確かめる。
//
// アドレスの上位バイトを保持するラッチが正しく駆動されないため、修正後の
// 上位バイトではなく書き込む値がアドレスに現れる。
func TestSHXWithPageCross(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0x01
	c.Y = 0x01
	// $20FF + $01 = $2100。ページ境界をまたぐ。
	b.load(0x8000, 0x9E, 0xFF, 0x20)

	c.StepInstruction()

	// 値は X & (上位バイト $20 + 1) = $01 & $21 = $01。
	// 書き込み先は ($01 << 8) | $00 = $0100 になる。
	if got := b.mem[0x0100]; got != 0x01 {
		t.Errorf("$0100 = $%02X, 期待 $01", got)
	}
	if got := b.mem[0x2100]; got != 0 {
		t.Errorf("$2100 = $%02X。修正後のアドレスへは書き込まないこと", got)
	}
}

// TestSHYSemantics は SHY が Y を使うことを確かめる。
func TestSHYSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.Y = 0xFF
	c.X = 0x01
	b.load(0x8000, 0x9C, 0x00, 0x30) // SHY $3000,X → $3001

	c.StepInstruction()

	// 値は Y & ($30 + 1) = $FF & $31 = $31
	if b.mem[0x3001] != 0x31 {
		t.Errorf("$3001 = $%02X, 期待 $31", b.mem[0x3001])
	}
}

// TestAHXSemantics は AHX が A & X & (上位バイト + 1) を書くことを確かめる。
func TestAHXSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0xFF
	c.X = 0xF0
	c.Y = 0x01
	b.load(0x8000, 0x9F, 0x00, 0x20) // AHX $2000,Y → $2001

	c.StepInstruction()

	// $FF & $F0 & $21 = $20
	if b.mem[0x2001] != 0x20 {
		t.Errorf("$2001 = $%02X, 期待 $20", b.mem[0x2001])
	}
}

// TestTASSetsStackPointer は TAS が S も変えることを確かめる。
func TestTASSemantics(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0xFF
	c.X = 0xF0
	c.Y = 0x01
	b.load(0x8000, 0x9B, 0x00, 0x20) // TAS $2000,Y

	c.StepInstruction()

	if c.S != 0xF0 {
		t.Errorf("S = $%02X, 期待 $F0（A & X）", c.S)
	}
	if b.mem[0x2001] != 0x20 {
		t.Errorf("$2001 = $%02X, 期待 $20", b.mem[0x2001])
	}
}

// TestSTPHalts は STP が停止し、記録を残すことを確かめる。
func TestSTPHalts(t *testing.T) {
	c, b := newTestCPU(t)
	var warnings int
	c.Warn = func(string, ...any) { warnings++ }
	b.load(0x8000, 0x02, 0xEA) // STP, NOP

	before := c.Cycles()
	c.StepInstruction()

	if !c.Halted() {
		t.Error("STP で停止していない")
	}
	if warnings == 0 {
		t.Error("STP の記録が残っていない")
	}
	if got := int(c.Cycles() - before); got != 2 {
		t.Errorf("STP のサイクル数 = %d, 期待 2", got)
	}

	// 停止中は PC のリードのみを行い、PC は進まない
	pc := c.PC
	b.log = nil
	c.StepInstruction()
	if c.PC != pc {
		t.Errorf("停止中に PC が $%04X から $%04X へ動いた", pc, c.PC)
	}
	if len(b.log) != 1 {
		t.Errorf("停止中のバスアクセス = %v, 1 回のリードのみを期待", b.log)
	}
}

// TestUnstableOpcodesAreRecorded は不安定な命令の実行が記録されることを
// 確かめる。
func TestUnstableOpcodesAreRecorded(t *testing.T) {
	unstable := []struct {
		code  uint8
		bytes []uint8
	}{
		{0x8B, []uint8{0x00}},       // XAA
		{0xAB, []uint8{0x00}},       // LAX #
		{0x9B, []uint8{0x00, 0x20}}, // TAS
		{0x93, []uint8{0x10}},       // AHX (d),Y
		{0x9F, []uint8{0x00, 0x20}}, // AHX a,Y
		{0x9C, []uint8{0x00, 0x20}}, // SHY
		{0x9E, []uint8{0x00, 0x20}}, // SHX
		{0xBB, []uint8{0x00, 0x20}}, // LAS
	}
	for _, tt := range unstable {
		c, b := newTestCPU(t)
		var warnings int
		c.Warn = func(string, ...any) { warnings++ }
		b.load(0x8000, append([]uint8{tt.code}, tt.bytes...)...)

		c.StepInstruction()

		if warnings == 0 {
			t.Errorf("$%02X（%s）の実行が記録されていない", tt.code, opcodes[tt.code].mnemonic)
		}
	}
}

// TestAliasesAreSet は別名が入っていることを確かめる。
//
// 文書によって呼び名が異なる命令があり、逆アセンブラが併記する。
func TestAliasesAreSet(t *testing.T) {
	want := map[uint8]string{
		0xCB: "SBX", // AXS
		0xE3: "ISC", // ISB
		0x93: "SHA", // AHX
		0x9B: "SHS", // TAS
		0x8B: "ANE", // XAA
		0xBB: "LAE", // LAS
		0x02: "JAM", // STP
		0x4B: "ASR", // ALR
	}
	for code, alias := range want {
		if got := opcodes[code].alias; got != alias {
			t.Errorf("$%02X の別名 = %q, 期待 %q", code, got, alias)
		}
	}
}
