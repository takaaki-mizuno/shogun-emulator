package debug

import (
	"errors"
	"testing"
)

// extEnv は名前と空間を扱う式の評価に使う模擬の状態。
type extEnv struct {
	*fakeEnv
	spaces map[Space]map[int]uint8
	syms   map[string][3]int // 空間、位置、CPU アドレス
	consts map[string]int64
	game   map[string]Value
	pos    map[Position]int64
}

func newExtEnv() *extEnv {
	return &extEnv{fakeEnv: newFakeEnv(), spaces: map[Space]map[int]uint8{},
		syms: map[string][3]int{}, consts: map[string]int64{}, game: map[string]Value{}, pos: map[Position]int64{}}
}

func (e *extEnv) PeekSpace(s Space, a int) uint8 { return e.spaces[s][a] }
func (e *extEnv) ResolveSymbol(n string) (Space, int, int, bool) {
	v, ok := e.syms[n]
	return Space(v[0]), v[1], v[2], ok
}
func (e *extEnv) GameValue(n string) (Value, bool) { v, ok := e.game[n]; return v, ok }
func (e *extEnv) Constant(n string) (int64, bool)  { v, ok := e.consts[n]; return v, ok }
func (e *extEnv) Position(p Position) int64        { return e.pos[p] }
func (e *extEnv) HasSymbol(n string) bool {
	_, ok := e.syms[n]
	_, c := e.consts[n]
	return ok || c
}
func (e *extEnv) HasGameValue(n string) bool { _, ok := e.game[n]; return ok }

func (e *extEnv) put(s Space, a int, b ...uint8) {
	if e.spaces[s] == nil {
		e.spaces[s] = map[int]uint8{}
	}
	for i, v := range b {
		e.spaces[s][a+i] = v
	}
}

// TestExprExtensions は式の拡張の各要素を確かめる（設計書 14 編 §14.11.4）。
func TestExprExtensions(t *testing.T) {
	env := newExtEnv()
	env.mem[0x0300] = 0x34
	env.mem[0x0301] = 0x12
	env.mem[0x0302] = 0x07
	env.mem[0x0010] = 0x00
	env.mem[0x0011] = 0x03
	env.syms["score"] = [3]int{int(SpaceCPU), 0x300, -1}
	env.syms["ptr"] = [3]int{int(SpaceCPU), 0x10, -1}
	env.syms["scope::name"] = [3]int{int(SpaceCPU), 0x302, -1}
	env.put(SpacePPU, 0x2000, 0x24)
	env.put(SpaceOAM, 0x00, 0x50)
	env.put(SpacePRGROM, 0x6123, 0xEA)
	env.game["mode"] = Value{Num: 1, Str: "play", HasStr: true}
	env.game["lives"] = num(3)
	env.pos[PosFrame] = 120
	env.consts["START"] = 0x34
	env.syms["main"] = [3]int{int(SpacePRGROM), 0x1C010, 0xC010}
	env.regs[RegPC] = 0xC010
	env.pos[PosScanline] = 241
	env.regs[RegA] = 0xFF
	for expr, want := range map[string]bool{
		"score == START":                  true, // 定数は値
		"PC == main":                      true, // ROM 側の名前は CPU アドレス
		"PC == main + 0":                  true,
		"score == $34":                    true, // 名前は値の式で 1 バイトを読む
		"[score] == $34":                  true,
		"[score].w == $1234":              true,
		"[score].b3 == $071234":           true,
		"[score+1] == $12":                true,
		"scope::name == 7":                true,
		"game.mode == 'play'":             true,
		"game.mode != 'title'":            true,
		"game.mode == 1":                  true, // 列挙の値は数でも比べられる
		"'play' == game.mode":             true,
		"game.lives * 2 + 1 == 7":         true,
		"game.lives == 'play'":            false,
		"frame == 120 && scanline >= 241": true,
		"[ppu:$2000] == $24":              true,
		"[oam:$00] == $50":                true,
		"[bank3:$8123] == $EA":            true,
		"(A & $0F) == $0F":                true,
		"(A | $100) == $1FF":              true,
		"(A ^ $F0) == $0F":                true,
		"A << 4 == $FF0":                  true,
		"A >> 4 == $0F":                   true,
		"~0 == -1":                        true,
		"10 % 3 == 1":                     true,
		"10 / 3 == 3":                     true,
		"A - $100 < 0":                    true,  // 64 bit 符号付き。切り詰めない
		"1 / 0 == 0":                      false, // 0 での除算は条件式を偽にする
		"[[ptr].w] == $34":                true,  // ptr が指す先（$0300）を読む
	} {
		c, err := ParseConditionWith(expr, env)
		if err != nil {
			t.Errorf("%q を解析できない: %v", expr, err)
			continue
		}
		if got := c.Eval(env); got != want {
			t.Errorf("%q = %v, 期待 %v", expr, got, want)
		}
	}
	// 値の式としての 0 での除算は 0。
	c, _ := ParseConditionWith("5 / 0", env)
	if v := c.Value(env); v.Num != 0 {
		t.Errorf("5 / 0 の値 = %d", v.Num)
	}
}

// TestExprNameErrors は名前の誤りの位置を確かめる。
func TestExprNameErrors(t *testing.T) {
	env := newExtEnv()
	for expr, pos := range map[string]int{
		"nope == 1":       1,
		"game.nope == 1":  1,
		"[x].q == 1":      1,
		"[zz:$00] == 1":   2,
		"A == 'open":      6,
		"[bankx:$8000]":   2,
		"A == $100000000": 6,
	} {
		_, err := ParseConditionWith(expr, env)
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%q: 誤りにならない（%v）", expr, err)
			continue
		}
		if expr == "[x].q == 1" {
			continue // 名前 x が先に誤りになる
		}
		if pe.Pos != pos {
			t.Errorf("%q の位置 = %d, 期待 %d（%v）", expr, pe.Pos, pos, err)
		}
	}
	// 解析器を渡さない式は名前を受け付けない（既存の条件式の扱い）。
	if _, err := ParseCondition("frame == 1"); err == nil {
		t.Error("名前を受け付けた")
	}
}

// TestAddrExpr は位置の式を確かめる（設計書 14 編 §14.6）。
func TestAddrExpr(t *testing.T) {
	env := newExtEnv()
	env.syms["enemies"] = [3]int{int(SpaceCPU), 0x304, -1}
	env.syms["ptr"] = [3]int{int(SpaceCPU), 0x10, -1}
	env.syms["entry"] = [3]int{int(SpacePRGROM), 0x4000, 0x8000}
	env.mem[0x10] = 0xED
	env.mem[0x11] = 0xC0
	cases := []struct {
		expr  string
		space Space
		addr  int
		cpu   int
	}{
		{"$0300", SpaceCPU, 0x300, -1},
		{"768", SpaceCPU, 768, -1},
		{"0x300", SpaceCPU, 0x300, -1},
		{"enemies", SpaceCPU, 0x304, -1},
		{"enemies+2*4", SpaceCPU, 0x30C, -1},
		{"[ptr]", SpaceCPU, 0xC0ED, -1},
		{"[ptr]+1", SpaceCPU, 0xC0EE, -1},
		{"bank3:$8123", SpacePRGROM, 0x6123, 0x8123},
		{"prg:$1A123", SpacePRGROM, 0x1A123, -1},
		{"ppu:$2000", SpacePPU, 0x2000, -1},
		{"pal:$03", SpacePPU, 0x3F03, -1},
		{"oam:$10", SpaceOAM, 0x10, -1},
		{"chr:$0123", SpaceCHR, 0x123, -1},
		{"entry+3", SpacePRGROM, 0x4003, 0x8003},
	}
	for _, c := range cases {
		e, err := ParseAddrExpr(c.expr, env)
		if err != nil {
			t.Errorf("%q を解析できない: %v", c.expr, err)
			continue
		}
		s, a, cpu := e.Eval(env)
		if s != c.space || a != c.addr || cpu != c.cpu {
			t.Errorf("%q = (%v, $%X, %d), 期待 (%v, $%X, %d)", c.expr, s, a, cpu, c.space, c.addr, c.cpu)
		}
	}
}

// TestExprHasNoSideEffects は式の評価が Peek だけで読むことを確かめる。
func TestExprHasNoSideEffects(t *testing.T) {
	env := newExtEnv()
	env.syms["status"] = [3]int{int(SpaceCPU), 0x2002, -1}
	c, err := ParseConditionWith("status == 0 && [$2007].w == 0", env)
	if err != nil {
		t.Fatal(err)
	}
	c.Eval(env)
	if env.peeks != 3 {
		t.Errorf("Peek の回数 = %d, 期待 3", env.peeks)
	}
}

// BenchmarkConditionEval は命令ごとの判定で使う式の評価の速さを測る。
func BenchmarkConditionEval(b *testing.B) {
	env := newExtEnv()
	env.syms["score"] = [3]int{int(SpaceCPU), 0x300, -1}
	env.game["mode"] = Value{Num: 1, Str: "play", HasStr: true}
	c, err := ParseConditionWith("game.mode == 'play' && [score].w >= 1000 && A == $42", env)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		c.Eval(env)
	}
}
