package cpu

import (
	"fmt"
	"testing"
)

// fakeBus は 64 KiB のフラットなメモリを持つ試験用のバス。
//
// アクセスの順序と回数を記録する。サイクル数の検証は「1 サイクル =
// 1 バスアクセス」という前提の上に成り立つため、アクセスを数えれば
// サイクル数を数えたことになる。
type fakeBus struct {
	mem [0x10000]uint8

	reads  int
	writes int
	// log はアクセスの並び。"R:1234" または "W:1234=56" の形。
	log []string

	// nmiLine は NMI 線の状態。負論理のため true がアサートなし。
	nmiLine bool
	irq     bool

	// onAccess はバスアクセスごとに呼ばれる。引数は累積アクセス回数。
	// サイクル単位で割り込み線を動かすために使う。
	onAccess func(count int)
}

func newFakeBus() *fakeBus {
	return &fakeBus{nmiLine: true}
}

func (b *fakeBus) Read(addr uint16) uint8 {
	b.reads++
	v := b.mem[addr]
	b.log = append(b.log, fmt.Sprintf("R:%04X", addr))
	b.notify()
	return v
}

func (b *fakeBus) Write(addr uint16, v uint8) {
	b.writes++
	b.mem[addr] = v
	b.log = append(b.log, fmt.Sprintf("W:%04X=%02X", addr, v))
	b.notify()
}

// notify はアクセスの直後に仕掛けを呼ぶ。
//
// CPU が割り込み線を採取するのはアクセスの後であるため、ここで線を
// 動かすと「そのサイクルの終わりの状態」を作れる。
func (b *fakeBus) notify() {
	if b.onAccess != nil {
		b.onAccess(b.accesses())
	}
}

func (b *fakeBus) Peek(addr uint16) uint8 { return b.mem[addr] }

// Cycles はバスアクセスの回数を返す。1 サイクル = 1 アクセスである。
func (b *fakeBus) Cycles() uint64    { return uint64(b.accesses()) }
func (b *fakeBus) NMILine() bool     { return b.nmiLine }
func (b *fakeBus) IRQAsserted() bool { return b.irq }
func (b *fakeBus) accesses() int     { return b.reads + b.writes }
func (b *fakeBus) load(addr uint16, bs ...uint8) {
	for i, v := range bs {
		b.mem[addr+uint16(i)] = v
	}
}

// newTestCPU は電源投入済みの CPU と、そのバスを返す。
//
// リセットベクタを $8000 に向けておく。テストごとに $8000 から命令を
// 置けるようにするためである。
func newTestCPU(t *testing.T) (*CPU, *fakeBus) {
	t.Helper()
	b := newFakeBus()
	b.load(vectorReset, 0x00, 0x80)
	c := New(b)
	c.PowerOn()
	if c.PC != 0x8000 {
		t.Fatalf("PowerOn 後の PC = $%04X, 期待 $8000", c.PC)
	}
	b.reads, b.writes, b.log = 0, 0, nil
	return c, b
}

// TestPowerOnState は電源投入後のレジスタとサイクル数を確かめる。
func TestPowerOnState(t *testing.T) {
	b := newFakeBus()
	b.load(vectorReset, 0x34, 0x12)
	c := New(b)
	c.PowerOn()

	if c.A != 0 || c.X != 0 || c.Y != 0 {
		t.Errorf("A=%02X X=%02X Y=%02X, すべて 0 を期待", c.A, c.X, c.Y)
	}
	if c.S != 0xFD {
		t.Errorf("S = $%02X, 期待 $FD", c.S)
	}
	if !c.I {
		t.Error("I が立っていない")
	}
	if c.C || c.Z || c.D || c.V || c.N {
		t.Error("I 以外のフラグが立っている")
	}
	if c.PC != 0x1234 {
		t.Errorf("PC = $%04X, 期待 $1234", c.PC)
	}
	// nestest.log の 1 行目が CYC:7 であることに対応する
	if c.Cycles() != 7 {
		t.Errorf("サイクル数 = %d, 期待 7", c.Cycles())
	}
	if b.writes != 0 {
		t.Errorf("リセット中に %d 回書き込んだ。ライトは抑止されること", b.writes)
	}
}

// TestResetKeepsRegisters はリセットが S を 3 減らし、他のレジスタを
// 変更しないことを確かめる。
func TestResetKeepsRegisters(t *testing.T) {
	c, b := newTestCPU(t)
	c.A, c.X, c.Y = 0x11, 0x22, 0x33
	c.S = 0x80
	c.C, c.N = true, true
	c.I = false
	b.load(vectorReset, 0x56, 0x34)

	c.Reset()

	if c.A != 0x11 || c.X != 0x22 || c.Y != 0x33 {
		t.Error("リセットでレジスタが変わった")
	}
	if c.S != 0x7D {
		t.Errorf("S = $%02X, 期待 $7D（3 減る）", c.S)
	}
	if !c.I {
		t.Error("リセットで I が立っていない")
	}
	if !c.C || !c.N {
		t.Error("リセットで C と N が変わった")
	}
	if c.PC != 0x3456 {
		t.Errorf("PC = $%04X, 期待 $3456", c.PC)
	}
}

// TestStatusPackUnpack は P の合成と分解を確かめる。
func TestStatusPackUnpack(t *testing.T) {
	c, _ := newTestCPU(t)

	c.C, c.Z, c.I, c.D, c.V, c.N = true, false, true, false, true, false
	// bit 5 は常に 1。bit 4 は引数で決まる。
	if got := c.packP(false); got != 0x65 {
		t.Errorf("packP(false) = $%02X, 期待 $65", got)
	}
	if got := c.packP(true); got != 0x75 {
		t.Errorf("packP(true) = $%02X, 期待 $75", got)
	}

	// bit 5 と bit 4 は無視される
	c.unpackP(0xFF)
	if !c.C || !c.Z || !c.I || !c.D || !c.V || !c.N {
		t.Error("unpackP($FF) で全フラグが立っていない")
	}
	c.unpackP(0x30)
	if c.C || c.Z || c.I || c.D || c.V || c.N {
		t.Error("unpackP($30) は bit 5 と bit 4 のみで、どのフラグも立たないこと")
	}
}

// cycleCase は 1 命令のサイクル数の検証項目。
type cycleCase struct {
	name string
	// setup は命令を配置し、レジスタを設定する。
	setup func(c *CPU, b *fakeBus)
	want  int
}

// TestInstructionCycles は各アドレッシングモードのサイクル数が
// 調査結果の表と一致することを確かめる。
func TestInstructionCycles(t *testing.T) {
	at := func(code uint8, operands ...uint8) func(*CPU, *fakeBus) {
		return func(c *CPU, b *fakeBus) {
			b.load(0x8000, append([]uint8{code}, operands...)...)
		}
	}
	withX := func(x uint8, f func(*CPU, *fakeBus)) func(*CPU, *fakeBus) {
		return func(c *CPU, b *fakeBus) { c.X = x; f(c, b) }
	}
	withY := func(y uint8, f func(*CPU, *fakeBus)) func(*CPU, *fakeBus) {
		return func(c *CPU, b *fakeBus) { c.Y = y; f(c, b) }
	}

	cases := []cycleCase{
		// ロード
		{"LDA #", at(0xA9, 0x42), 2},
		{"LDA d", at(0xA5, 0x10), 3},
		{"LDA d,X", withX(1, at(0xB5, 0x10)), 4},
		{"LDA a", at(0xAD, 0x00, 0x20), 4},
		{"LDA a,X ページ内", withX(1, at(0xBD, 0x00, 0x20)), 4},
		{"LDA a,X ページ越え", withX(0xFF, at(0xBD, 0x01, 0x20)), 5},
		{"LDA a,Y ページ内", withY(1, at(0xB9, 0x00, 0x20)), 4},
		{"LDA a,Y ページ越え", withY(0xFF, at(0xB9, 0x01, 0x20)), 5},
		{"LDA (d,X)", withX(1, at(0xA1, 0x10)), 6},
		{"LDA (d),Y ページ内", withY(1, at(0xB1, 0x10)), 5},
		{"LDA (d),Y ページ越え", func(c *CPU, b *fakeBus) {
			c.Y = 0xFF
			b.load(0x8000, 0xB1, 0x10)
			b.load(0x0010, 0x01, 0x20) // ポインタは $2001。+$FF でページを越える
		}, 6},

		// ストア。ページ境界に関係なくサイクル数が変わらない
		{"STA d", at(0x85, 0x10), 3},
		{"STA d,X", withX(1, at(0x95, 0x10)), 4},
		{"STA a", at(0x8D, 0x00, 0x20), 4},
		{"STA a,X ページ内", withX(1, at(0x9D, 0x00, 0x20)), 5},
		{"STA a,X ページ越え", withX(0xFF, at(0x9D, 0x01, 0x20)), 5},
		{"STA a,Y ページ内", withY(1, at(0x99, 0x00, 0x20)), 5},
		{"STA (d,X)", withX(1, at(0x81, 0x10)), 6},
		{"STA (d),Y ページ内", withY(1, at(0x91, 0x10)), 6},

		// RMW。ページ境界に関係なく常に最長
		{"ASL A", at(0x0A), 2},
		{"ASL d", at(0x06, 0x10), 5},
		{"ASL d,X", withX(1, at(0x16, 0x10)), 6},
		{"ASL a", at(0x0E, 0x00, 0x20), 6},
		{"ASL a,X ページ内", withX(1, at(0x1E, 0x00, 0x20)), 7},
		{"ASL a,X ページ越え", withX(0xFF, at(0x1E, 0x01, 0x20)), 7},
		{"INC d", at(0xE6, 0x10), 5},
		{"INC a,X", withX(1, at(0xFE, 0x00, 0x20)), 7},

		// 比較とビット検査
		{"CPX #", at(0xE0, 0x00), 2},
		{"BIT d", at(0x24, 0x10), 3},
		{"BIT a", at(0x2C, 0x00, 0x20), 4},

		// 転送とフラグ
		{"TAX", at(0xAA), 2},
		{"INX", at(0xE8), 2},
		{"CLC", at(0x18), 2},
		{"NOP", at(0xEA), 2},

		// スタック
		{"PHA", at(0x48), 3},
		{"PHP", at(0x08), 3},
		{"PLA", at(0x68), 4},
		{"PLP", at(0x28), 4},

		// ジャンプ
		{"JMP a", at(0x4C, 0x00, 0x90), 3},
		{"JMP (a)", at(0x6C, 0x00, 0x20), 5},
		{"JSR a", at(0x20, 0x00, 0x90), 6},
		{"RTS", at(0x60), 6},
		{"RTI", at(0x40), 6},
		{"BRK", at(0x00), 7},

		// 分岐
		{"BNE 成立せず", func(c *CPU, b *fakeBus) { c.Z = true; b.load(0x8000, 0xD0, 0x10) }, 2},
		{"BNE 成立", func(c *CPU, b *fakeBus) { c.Z = false; b.load(0x8000, 0xD0, 0x10) }, 3},
		{"BNE 成立してページ越え", func(c *CPU, b *fakeBus) {
			c.Z = false
			// $80F0 から +$20 で $8112。ページが変わる
			b.load(0x80F0, 0xD0, 0x20)
			c.PC = 0x80F0
		}, 4},
		{"BNE 後方へページ越え", func(c *CPU, b *fakeBus) {
			c.Z = false
			b.load(0x8010, 0xD0, 0x80) // -128 で $7F92
			c.PC = 0x8010
		}, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, b := newTestCPU(t)
			tc.setup(c, b)
			before := c.Cycles()
			c.StepInstruction()
			got := int(c.Cycles() - before)
			if got != tc.want {
				t.Errorf("サイクル数 = %d, 期待 %d\nアクセス: %v", got, tc.want, b.log)
			}
			if got != b.accesses() {
				t.Errorf("サイクル数 %d とバスアクセス回数 %d が一致しない", got, b.accesses())
			}
		})
	}
}

// TestJMPIndirectPageBug は JMP ($xxFF) が上位バイトを下位バイトのみで
// 計算することを確かめる。
func TestJMPIndirectPageBug(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x6C, 0xFF, 0x03)
	b.load(0x03FF, 0x34)
	b.load(0x0400, 0xAB) // ページをまたげばここを読む
	b.load(0x0300, 0x12) // 実機はこちらを読む

	c.StepInstruction()

	if c.PC != 0x1234 {
		t.Errorf("PC = $%04X, 期待 $1234（$0400 ではなく $0300 を読むこと）", c.PC)
	}
	wantLog := []string{"R:8000", "R:8001", "R:8002", "R:03FF", "R:0300"}
	assertLog(t, b, wantLog)
}

// TestZeroPageIndexedWrapsInPage はゼロページのインデックス付きアドレスが
// ゼロページ内でラップすることを確かめる。
func TestZeroPageIndexedWrapsInPage(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0x01
	b.load(0x8000, 0xB5, 0xFF) // LDA $FF,X
	b.load(0x0000, 0x77)
	b.load(0x0100, 0x99) // ラップしなければここを読む

	c.StepInstruction()

	if c.A != 0x77 {
		t.Errorf("A = $%02X, 期待 $77（$0000 を読むこと）", c.A)
	}
	assertLog(t, b, []string{"R:8000", "R:8001", "R:00FF", "R:0000"})
}

// TestIndirectXWrapsInZeroPage は (d,X) のポインタ読み出しが
// ゼロページ内でラップすることを確かめる。
func TestIndirectXWrapsInZeroPage(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0x01
	b.load(0x8000, 0xA1, 0xFE) // LDA ($FE,X) → ポインタは $FF と $00
	b.load(0x00FF, 0x34)
	b.load(0x0000, 0x12)
	b.load(0x1234, 0x5A)

	c.StepInstruction()

	if c.A != 0x5A {
		t.Errorf("A = $%02X, 期待 $5A", c.A)
	}
	assertLog(t, b, []string{"R:8000", "R:8001", "R:00FE", "R:00FF", "R:0000", "R:1234"})
}

// TestAbsoluteIndexedDummyReadAddress はページ越えのときに読む
// 不正なアドレスを確かめる。PPU レジスタに当たると副作用が起きるため、
// アドレスが正確である必要がある。
func TestAbsoluteIndexedDummyReadAddress(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0xFF
	b.load(0x8000, 0xBD, 0x01, 0x20) // LDA $2001,X → 実効アドレス $2100

	c.StepInstruction()

	// サイクル 4 は上位バイトが修正される前の $2000 を読む
	assertLog(t, b, []string{"R:8000", "R:8001", "R:8002", "R:2000", "R:2100"})
}

// TestStoreAbsoluteIndexedAlwaysDummyReads はストアがページ境界に
// 関係なくダミーリードを行うことを確かめる。
func TestStoreAbsoluteIndexedAlwaysDummyReads(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0x01
	c.A = 0x42
	b.load(0x8000, 0x9D, 0x00, 0x20) // STA $2000,X

	c.StepInstruction()

	assertLog(t, b, []string{"R:8000", "R:8001", "R:8002", "R:2001", "W:2001=42"})
}

// TestRMWWritesTwice は RMW 命令が元の値を書き戻してから
// 変更後の値を書くことを確かめる。
//
// この二重ライトは PPUDATA やマッパーのレジスタに対して観測できる。
func TestRMWWritesTwice(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0xE6, 0x10) // INC $10
	b.load(0x0010, 0x41)

	c.StepInstruction()

	assertLog(t, b, []string{"R:8000", "R:8001", "R:0010", "W:0010=41", "W:0010=42"})
	if b.mem[0x0010] != 0x42 {
		t.Errorf("$0010 = $%02X, 期待 $42", b.mem[0x0010])
	}
}

// TestJSRPushesPCMinusOne は JSR が次の命令の 1 バイト前を push することを
// 確かめる。RTS が pull 後にインクリメントすることと対応する。
func TestJSRPushesPCMinusOne(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x20, 0x00, 0x90) // JSR $9000

	c.StepInstruction()

	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000", c.PC)
	}
	// $8002 を push する（次の命令 $8003 の 1 バイト前）
	if hi, lo := b.mem[0x01FD], b.mem[0x01FC]; hi != 0x80 || lo != 0x02 {
		t.Errorf("push された値 = $%02X%02X, 期待 $8002", hi, lo)
	}
	if c.S != 0xFB {
		t.Errorf("S = $%02X, 期待 $FB", c.S)
	}
}

// TestRTSIncrementsAfterPull は RTS が pull した後に PC を進めることを
// 確かめる。
func TestRTSIncrementsAfterPull(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x60) // RTS
	c.S = 0xFB
	b.load(0x01FC, 0x02, 0x80) // $8002 を積んである

	c.StepInstruction()

	if c.PC != 0x8003 {
		t.Errorf("PC = $%04X, 期待 $8003", c.PC)
	}
	if c.S != 0xFD {
		t.Errorf("S = $%02X, 期待 $FD", c.S)
	}
}

// TestRTIDoesNotIncrement は RTI が pull した PC をインクリメント
// しないことを確かめる。
func TestRTIDoesNotIncrement(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x40) // RTI
	c.S = 0xFA
	b.load(0x01FB, 0x00, 0x34, 0x12) // P=$00, PC=$1234

	c.StepInstruction()

	if c.PC != 0x1234 {
		t.Errorf("PC = $%04X, 期待 $1234", c.PC)
	}
}

// TestRTIAppliesIImmediately は RTI が I を即座に反映することを確かめる。
func TestRTIAppliesIImmediately(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x40)
	c.S = 0xFA
	c.I = true
	b.load(0x01FB, 0x00, 0x34, 0x12) // P の I ビットが 0

	c.StepInstruction()

	if c.I {
		t.Error("RTI 後も I が立っている。RTI は即座に反映すること")
	}
}

// TestSEIUpdatesFlagImmediately は SEI が I を即座に立てることを確かめる。
//
// 遅れて見えるのはポーリングの位置によるものであり、フラグの値ではない。
// フラグの更新を遅らせると、直後に割り込みが起きたときスタックへ積まれる
// P が実機と違う値になる。
func TestSEIUpdatesFlagImmediately(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.load(0x8000, 0x78) // SEI

	c.StepInstruction()
	if !c.I {
		t.Error("SEI の直後に I が立っていない")
	}
}

// TestSEIDoesNotBlockPendingIRQ は SEI の直前からアサートされていた IRQ が
// SEI の直後に入ることを確かめる。
//
// SEI のポーリングは最後から 2 番目のサイクルの終わりに行われ、その時点の
// I はまだ 0 である。
func TestSEIDoesNotBlockPendingIRQ(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0x78, 0xEA) // SEI, NOP

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（SEI の直後に IRQ が入ること）", c.PC)
	}
}

// TestIRQStatusHasIFlagSetAfterSEI は SEI の直後に入った IRQ で、
// スタックへ積まれる P の I ビットが 1 であることを確かめる。
//
// `cpu_interrupts_v2/1-cli_latency` がこれを検証する。
func TestIRQStatusHasIFlagSetAfterSEI(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	c.S = 0xFD
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0x78, 0xEA) // SEI, NOP

	c.StepInstruction()

	// 積まれた P はスタックの 3 番目（PC 上位・下位の後）。
	// P の I ビットは bit 2。
	const iBit = 0x04
	p := b.mem[0x0100|uint16(c.S+1)]
	if p&iBit == 0 {
		t.Errorf("積まれた P = %#02x。I ビットが立っていない", p)
	}
}

// TestCLIAllowsIRQAfterNextInstruction は CLI の効果が 1 命令遅れて
// 現れることを確かめる。
func TestCLIAllowsIRQAfterNextInstruction(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = true
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0x58, 0xEA, 0xEA) // CLI, NOP, NOP

	c.StepInstruction()
	if c.I {
		t.Error("CLI の直後に I が下りていない")
	}
	if c.PC != 0x8001 {
		t.Fatalf("PC = $%04X, 期待 $8001（CLI の直後は IRQ が入らないこと）", c.PC)
	}

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（次の命令の後に入ること）", c.PC)
	}
}

// TestPLPUpdatesFlagImmediately は PLP が I を即座に変えることを確かめる。
func TestPLPUpdatesFlagImmediately(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = true
	c.S = 0xFC
	b.load(0x8000, 0x28) // PLP
	b.load(0x01FD, 0x00) // I = 0 の P

	c.StepInstruction()
	if c.I {
		t.Error("PLP の直後に I が下りていない")
	}
}

// TestADCOverflow は加算のオーバーフロー判定を確かめる。
func TestADCOverflow(t *testing.T) {
	tests := []struct {
		a, m       uint8
		carryIn    bool
		wantA      uint8
		wantC      bool
		wantV      bool
		wantN      bool
		wantZ      bool
		annotation string
	}{
		{0x50, 0x10, false, 0x60, false, false, false, false, "正 + 正 = 正"},
		{0x50, 0x50, false, 0xA0, false, true, true, false, "正 + 正 = 負（V）"},
		{0x50, 0x90, false, 0xE0, false, false, true, false, "正 + 負"},
		{0x50, 0xD0, false, 0x20, true, false, false, false, "正 + 負（桁上がり）"},
		{0xD0, 0x90, false, 0x60, true, true, false, false, "負 + 負 = 正（V）"},
		{0xFF, 0x01, false, 0x00, true, false, false, true, "$FF + 1 = 0"},
		{0x00, 0x00, true, 0x01, false, false, false, false, "C が加算される"},
	}
	for _, tt := range tests {
		c, b := newTestCPU(t)
		c.A = tt.a
		c.C = tt.carryIn
		b.load(0x8000, 0x69, tt.m) // ADC #
		c.StepInstruction()

		if c.A != tt.wantA || c.C != tt.wantC || c.V != tt.wantV || c.N != tt.wantN || c.Z != tt.wantZ {
			t.Errorf("%s: ADC $%02X + $%02X (C=%v) → A=$%02X C=%v V=%v N=%v Z=%v\n"+
				"  期待 A=$%02X C=%v V=%v N=%v Z=%v",
				tt.annotation, tt.a, tt.m, tt.carryIn, c.A, c.C, c.V, c.N, c.Z,
				tt.wantA, tt.wantC, tt.wantV, tt.wantN, tt.wantZ)
		}
	}
}

// TestSBCBorrow は減算のフラグを確かめる。
func TestSBCBorrow(t *testing.T) {
	tests := []struct {
		a, m    uint8
		carryIn bool
		wantA   uint8
		wantC   bool
		wantV   bool
	}{
		{0x50, 0x10, true, 0x40, true, false},
		{0x50, 0x50, true, 0x00, true, false},
		{0x50, 0x60, true, 0xF0, false, false}, // 借りが出る
		{0x50, 0xB0, true, 0xA0, false, true},  // 正 - 負 = 負（V）
		{0x50, 0x10, false, 0x3F, true, false}, // C=0 のとき 1 余分に引く
	}
	for _, tt := range tests {
		c, b := newTestCPU(t)
		c.A = tt.a
		c.C = tt.carryIn
		b.load(0x8000, 0xE9, tt.m) // SBC #
		c.StepInstruction()

		if c.A != tt.wantA || c.C != tt.wantC || c.V != tt.wantV {
			t.Errorf("SBC $%02X - $%02X (C=%v) → A=$%02X C=%v V=%v、期待 A=$%02X C=%v V=%v",
				tt.a, tt.m, tt.carryIn, c.A, c.C, c.V, tt.wantA, tt.wantC, tt.wantV)
		}
	}
}

// TestBITFlags は BIT が N と V に M のビットをそのまま入れることを確かめる。
func TestBITFlags(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x00
	b.load(0x8000, 0x24, 0x10) // BIT $10
	b.load(0x0010, 0xC0)

	c.StepInstruction()

	if !c.Z {
		t.Error("A & M = 0 のとき Z が立つこと")
	}
	if !c.N {
		t.Error("N には M の bit 7 が入ること")
	}
	if !c.V {
		t.Error("V には M の bit 6 が入ること")
	}
}

// TestCompareDoesNotChangeRegisterOrV は比較命令がレジスタと V を
// 変更しないことを確かめる。
func TestCompareDoesNotChangeRegisterOrV(t *testing.T) {
	c, b := newTestCPU(t)
	c.A = 0x40
	c.V = true
	b.load(0x8000, 0xC9, 0x50) // CMP #$50

	c.StepInstruction()

	if c.A != 0x40 {
		t.Errorf("A = $%02X, 変更しないこと", c.A)
	}
	if !c.V {
		t.Error("V を変更しないこと")
	}
	if c.C {
		t.Error("A < M のとき C は下りること")
	}
	if !c.N {
		t.Error("$40 - $50 = $F0 なので N が立つこと")
	}
}

// TestTXSDoesNotSetFlags は TXS がフラグを変えないことを確かめる。
func TestTXSDoesNotSetFlags(t *testing.T) {
	c, b := newTestCPU(t)
	c.X = 0x00
	c.Z, c.N = false, true
	b.load(0x8000, 0x9A) // TXS

	c.StepInstruction()

	if c.S != 0x00 {
		t.Errorf("S = $%02X, 期待 $00", c.S)
	}
	if c.Z {
		t.Error("TXS が Z を変えた")
	}
	if !c.N {
		t.Error("TXS が N を変えた")
	}
}

// TestBRKPushesBFlagAndPCPlusTwo は BRK の push 内容を確かめる。
func TestBRKPushesBFlagAndPCPlusTwo(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x00) // BRK
	b.load(vectorIRQ, 0xCD, 0xAB)
	c.C = true

	c.StepInstruction()

	if c.PC != 0xABCD {
		t.Errorf("PC = $%04X, 期待 $ABCD", c.PC)
	}
	// $8002 を push する
	if hi, lo := b.mem[0x01FD], b.mem[0x01FC]; hi != 0x80 || lo != 0x02 {
		t.Errorf("push された PC = $%02X%02X, 期待 $8002", hi, lo)
	}
	p := b.mem[0x01FB]
	if p&0x10 == 0 {
		t.Errorf("push された P = $%02X。B フラグが立つこと", p)
	}
	if p&0x20 == 0 {
		t.Errorf("push された P = $%02X。bit 5 は常に 1", p)
	}
	if !c.I {
		t.Error("BRK 後に I が立っていない")
	}
}

// TestNMIEdgeTriggered は NMI が立ち下がりで検出されることを確かめる。
func TestNMIEdgeTriggered(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0xEA, 0xEA, 0xEA) // NOP を 3 つ
	b.load(vectorNMI, 0x00, 0x90)

	// 線を下げる（アサート）
	b.nmiLine = false
	c.StepInstruction() // NOP。この間に立ち下がりを検出する

	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（NMI が入ること）", c.PC)
	}

	// 線を下げたままにしても 2 回目は発生しない
	b.load(0x9000, 0xEA)
	c.StepInstruction()
	if c.PC != 0x9001 {
		t.Errorf("PC = $%04X, 期待 $9001（レベルのままでは再発しないこと）", c.PC)
	}
}

// TestIRQLevelTriggeredAndMasked は IRQ がレベル検出であり、
// I でマスクされることを確かめる。
func TestIRQLevelTriggeredAndMasked(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0xEA, 0xEA)
	b.load(vectorIRQ, 0x00, 0x90)
	b.irq = true

	// I が立っているので受け付けない
	c.I = true
	c.StepInstruction()
	if c.PC != 0x8001 {
		t.Errorf("PC = $%04X, 期待 $8001（I でマスクされること）", c.PC)
	}

	c.I = false
	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（IRQ が入ること）", c.PC)
	}
}

// TestInterruptSequenceCycles は割り込みシーケンスが 7 サイクルであることを
// 確かめる。
func TestInterruptSequenceCycles(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0xEA)
	b.load(vectorNMI, 0x00, 0x90)
	b.nmiLine = false

	before := c.Cycles()
	c.StepInstruction() // NOP（2 サイクル）+ NMI（7 サイクル）
	if got := c.Cycles() - before; got != 9 {
		t.Errorf("サイクル数 = %d, 期待 9（NOP 2 + 割り込み 7）", got)
	}
}

// assertLog はバスアクセスの並びを検証する。
func assertLog(t *testing.T, b *fakeBus, want []string) {
	t.Helper()
	if len(b.log) != len(want) {
		t.Fatalf("アクセス回数 = %d, 期待 %d\n実際: %v\n期待: %v",
			len(b.log), len(want), b.log, want)
	}
	for i := range want {
		if b.log[i] != want[i] {
			t.Errorf("アクセス %d 番目 = %s, 期待 %s\n実際: %v\n期待: %v",
				i, b.log[i], want[i], b.log, want)
		}
	}
}
