package debug

import (
	"errors"
	"testing"
)

// fakeEnv は条件式の評価に使う模擬の状態。
type fakeEnv struct {
	regs  map[Register]uint16
	flags map[Flag]bool
	mem   map[uint16]uint8
	// peeks は Peek を呼んだ回数。
	peeks int
}

func newFakeEnv() *fakeEnv {
	return &fakeEnv{regs: map[Register]uint16{}, flags: map[Flag]bool{}, mem: map[uint16]uint8{}}
}

func (e *fakeEnv) Reg(r Register) uint16 { return e.regs[r] }
func (e *fakeEnv) Flag(f Flag) bool      { return e.flags[f] }
func (e *fakeEnv) Peek(addr uint16) uint8 {
	e.peeks++
	return e.mem[addr]
}

// evalExpr は式を解析して評価する。
func evalExpr(t *testing.T, expr string, env Env) bool {
	t.Helper()
	c, err := ParseCondition(expr)
	if err != nil {
		t.Fatalf("%q を解析できない: %v", expr, err)
	}
	return c.Eval(env)
}

// TestConditionNumbers は数値リテラルの 3 つの書き方を確かめる。
func TestConditionNumbers(t *testing.T) {
	env := newFakeEnv()
	env.regs[RegA] = 0x42
	for _, expr := range []string{"A == $42", "A == 66", "A == %01000010", "A == $0042"} {
		if !evalExpr(t, expr, env) {
			t.Errorf("%q が偽になった", expr)
		}
	}
}

// TestConditionRegisters はレジスタの参照を確かめる。
func TestConditionRegisters(t *testing.T) {
	env := newFakeEnv()
	env.regs[RegA] = 1
	env.regs[RegX] = 2
	env.regs[RegY] = 3
	env.regs[RegS] = 0xFD
	env.regs[RegP] = 0x24
	env.regs[RegPC] = 0xC123
	for _, expr := range []string{
		"A == 1", "X == 2", "Y == 3", "S == $FD", "P == $24", "PC == $C123",
		"pc == $c123", // 小文字も受け付ける
	} {
		if !evalExpr(t, expr, env) {
			t.Errorf("%q が偽になった", expr)
		}
	}
}

// TestConditionFlags はフラグの参照を確かめる。
func TestConditionFlags(t *testing.T) {
	env := newFakeEnv()
	env.flags[FlagC] = true
	env.flags[FlagN] = true
	for expr, want := range map[string]bool{
		"C": true, "Z": false, "I": false, "D": false, "V": false, "N": true,
		"C && N": true, "!Z": true, "C == 1": true, "Z == 0": true,
	} {
		if got := evalExpr(t, expr, env); got != want {
			t.Errorf("%q = %v, 期待 %v", expr, got, want)
		}
	}
}

// TestConditionMemory はメモリの参照とインデックスの加算を確かめる。
func TestConditionMemory(t *testing.T) {
	env := newFakeEnv()
	env.regs[RegX] = 5
	env.mem[0x0300] = 0x10
	env.mem[0x0305] = 0x20
	for expr, want := range map[string]bool{
		"[$0300] == $10":      true,
		"[$0300+X] == $20":    true,
		"[$0300 + X] == $20":  true,
		"[$0306-1] == $20":    true,
		"[[$0300]] == 0":      true, // 入れ子の参照
		"[$0300] != $10":      false,
		"[$0300] < [$0305]":   true,
		"[$0305] >= [$0300]":  true,
		"[$0300] + 1 == $11":  true,
		"[$0300] > $10":       false,
		"[$0300] <= $10":      true,
		"[$0300+X] > [$0300]": true,
	} {
		if got := evalExpr(t, expr, env); got != want {
			t.Errorf("%q = %v, 期待 %v", expr, got, want)
		}
	}
}

// TestConditionLogic は論理演算子と括弧と優先順位を確かめる。
func TestConditionLogic(t *testing.T) {
	env := newFakeEnv()
	env.regs[RegA] = 0x42
	env.regs[RegX] = 0x05
	for expr, want := range map[string]bool{
		"A == $42 && X < $10":            true,
		"A == $42 && X > $10":            false,
		"A == $00 || X == 5":             true,
		"!(A == $42)":                    false,
		"A == 1 || A == $42 && X == 5":   true, // && は || より先に結びつく
		"(A == 1 || A == $42) && X == 6": false,
		"!A":                             false,
		"!!A":                            true,
	} {
		if got := evalExpr(t, expr, env); got != want {
			t.Errorf("%q = %v, 期待 %v", expr, got, want)
		}
	}
}

// TestConditionErrors は不正な式でエラーになり、位置が入ることを
// 確かめる。
func TestConditionErrors(t *testing.T) {
	for expr, pos := range map[string]int{
		"A ==":        5,
		"A == $":      6,
		"Q == 1":      1,
		"A == 1 )":    8,
		"(A == 1":     8,
		"[$0300 == 1": 8,
		"A # 1":       3,
		"A == $10000": 6,
	} {
		_, err := ParseCondition(expr)
		if err == nil {
			t.Errorf("%q を受け入れた", expr)
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%q のエラーの種類が違う: %v", expr, err)
			continue
		}
		if pe.Pos != pos {
			t.Errorf("%q のエラー位置 = %d, 期待 %d（%v）", expr, pe.Pos, pos, err)
		}
	}
}

// TestConditionHasNoSideEffects は評価がメモリを Peek だけで読むことを
// 確かめる。
//
// 模擬の状態は Peek しか持たない。書き込みや Read の経路が無いことを
// 型で保証している。ここでは Peek の回数が式の参照数と一致することを
// 確かめる。
func TestConditionHasNoSideEffects(t *testing.T) {
	env := newFakeEnv()
	c, err := ParseCondition("[$2002] == 0 && [$2007] == 0")
	if err != nil {
		t.Fatal(err)
	}
	c.Eval(env)
	if env.peeks != 2 {
		t.Errorf("Peek の回数 = %d, 期待 2", env.peeks)
	}
}

// TestNilConditionIsTrue は条件の無いブレークポイントが常に成り立つ
// ことを確かめる。
func TestNilConditionIsTrue(t *testing.T) {
	var c *Condition
	if !c.Eval(newFakeEnv()) {
		t.Error("条件なしが偽になった")
	}
}
