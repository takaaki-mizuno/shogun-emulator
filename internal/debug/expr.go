package debug

import (
	"fmt"
	"strconv"
	"strings"
)

// 式の解析と評価（設計書 09 編 §9.6.1、設計書 14 編 §14.11.4）。
//
// ブレークポイントの条件、exec.run_until、Scenario のアサーション、位置の
// 指定で同じ解析器を使う。数値は 64 bit 符号付き整数で扱う。

// Value は式の値。整数か、Game State の列挙の名前・文字列リテラル。
type Value struct {
	Num int64
	// Str は列挙の名前か文字列リテラル。HasStr が true のときだけ意味を持つ。
	Str    string
	HasStr bool
	// Lit は文字列リテラルであることを表す。列挙の値は Num も持つ。
	Lit bool
	// Space と HasSpace は位置の式で、値がどの空間のアドレスかを表す。
	Space    Space
	HasSpace bool
	// CPU と HasCPU は PRG-ROM の位置に対応する CPU アドレス。
	CPU    int64
	HasCPU bool
}

func (v Value) truthy() bool {
	if v.Lit {
		return v.Str != ""
	}
	return v.Num != 0
}

func num(n int64) Value { return Value{Num: n} }

func boolValue(b bool) Value {
	if b {
		return num(1)
	}
	return num(0)
}

// Position は実行の位置を表す名前（frame・scanline・dot・cycles）。
type Position uint8

// 実行の位置。
const (
	PosFrame Position = iota
	PosScanline
	PosDot
	PosCycles
)

var positionNames = []string{"frame", "scanline", "dot", "cycles"}

// ExprEnv は名前と空間を扱う式の評価に使う取り出し口。Env だけを持つ値で
// 評価したときは、名前と CPU 以外の空間を 0 とする。
type ExprEnv interface {
	Env
	// PeekSpace は空間 s の addr を副作用なしに読む。
	PeekSpace(s Space, addr int) uint8
	// ResolveSymbol は Symbol の位置を返す。cpu は PRG-ROM の Symbol の CPU
	// アドレスで、不明なとき -1。
	ResolveSymbol(name string) (space Space, addr int, cpu int, ok bool)
	// Constant は定数の値を返す。定数でなければ false。
	Constant(name string) (int64, bool)
	// GameValue は Game State の値を返す。
	GameValue(name string) (Value, bool)
	// Position は実行の位置を返す。
	Position(p Position) int64
}

// Resolver は解析のときに名前があるかを確かめる。
type Resolver interface {
	HasSymbol(name string) bool
	HasGameValue(name string) bool
}

// AddrExpr は位置の式（設計書 14 編 §14.6）。
type AddrExpr struct {
	Expr string
	root node
}

// ParseAddrExpr は位置の式を解析する。
//
// 名前は位置（アドレス）として評価し、[ptr] は 2 バイトのポインタとして
// 読む。空間の接頭辞（ppu:・oam:・pal:・chr:・prg:・bankN:）を受け付ける。
func ParseAddrExpr(expr string, r Resolver) (*AddrExpr, error) {
	root, err := parseExpr(expr, r, true)
	if err != nil {
		return nil, err
	}
	return &AddrExpr{Expr: expr, root: root}, nil
}

// Eval は位置を求める。空間を持たない値は CPU アドレス空間とする。cpu は
// PRG-ROM の位置に対応する CPU アドレスで、不明なとき -1。
func (a *AddrExpr) Eval(env Env) (space Space, addr int, cpu int) {
	ctx := &evalCtx{env: env}
	ctx.ext, _ = env.(ExprEnv)
	v := a.root.eval(ctx, true)
	cpu = -1
	if v.HasCPU {
		cpu = int(v.CPU)
	}
	if !v.HasSpace {
		return SpaceCPU, int(v.Num), cpu
	}
	return v.Space, int(v.Num), cpu
}

// evalCtx は評価 1 回分の状態。
type evalCtx struct {
	env     Env
	ext     ExprEnv
	divZero bool
}

func (c *evalCtx) peek(s Space, addr int) uint8 {
	if s == SpaceCPU {
		return c.env.Peek(uint16(addr))
	}
	if c.ext == nil {
		return 0
	}
	return c.ext.PeekSpace(s, addr)
}

// node は構文木の節。addr が true のとき、名前と [x] を位置として評価する。
type node interface {
	eval(c *evalCtx, addr bool) Value
}

type numNode struct{ v int64 }

func (n numNode) eval(*evalCtx, bool) Value { return num(n.v) }

type strNode struct{ s string }

func (n strNode) eval(*evalCtx, bool) Value { return Value{Str: n.s, HasStr: true, Lit: true} }

type regNode struct{ r Register }

func (n regNode) eval(c *evalCtx, _ bool) Value { return num(int64(c.env.Reg(n.r))) }

type flagNode struct{ f Flag }

func (n flagNode) eval(c *evalCtx, _ bool) Value { return boolValue(c.env.Flag(n.f)) }

type posNode struct{ p Position }

func (n posNode) eval(c *evalCtx, _ bool) Value {
	if c.ext == nil {
		return num(0)
	}
	return num(c.ext.Position(n.p))
}

// symNode は Symbol。位置の式では位置を返す。値の式では、RAM 側（$0000–$7FFF）
// の名前はその位置の 1 バイト、ROM 側の名前（コードのラベル）はその CPU
// アドレス、定数はその値とする（設計書 14 編 §14.11.4）。PC == update_mode の
// ように、コードのラベルはアドレスとして比べることが多いためである。
type symNode struct{ name string }

func (n symNode) eval(c *evalCtx, addr bool) Value {
	if c.ext == nil {
		return num(0)
	}
	if v, ok := c.ext.Constant(n.name); ok {
		return num(v)
	}
	s, a, cpu, ok := c.ext.ResolveSymbol(n.name)
	if !ok {
		return num(0)
	}
	if addr {
		return Value{Num: int64(a), Space: s, HasSpace: true, CPU: int64(cpu), HasCPU: cpu >= 0}
	}
	switch {
	case s == SpacePRGROM && cpu >= 0:
		return num(int64(cpu))
	case s == SpacePRGROM, s == SpaceCPU && a >= 0x8000:
		return num(int64(a))
	}
	return num(int64(c.peek(s, a)))
}

type gameNode struct{ name string }

func (n gameNode) eval(c *evalCtx, _ bool) Value {
	if c.ext == nil {
		return num(0)
	}
	v, ok := c.ext.GameValue(n.name)
	if !ok {
		return num(0)
	}
	return v
}

// spaceNode は空間の接頭辞をつけた位置（ppu:$2000、bank3:$8123）。
type spaceNode struct {
	space Space
	// bank は bankN: のときの N。-1 のとき bankN: ではない。
	bank  int
	base  int
	inner node
}

func (n spaceNode) eval(c *evalCtx, _ bool) Value {
	v := n.inner.eval(c, true)
	a := n.base + int(v.Num)
	if n.bank >= 0 {
		a = n.bank*0x2000 + int(v.Num)&0x1FFF
		return Value{Num: int64(a), Space: n.space, HasSpace: true, CPU: v.Num, HasCPU: true}
	}
	return Value{Num: int64(a), Space: n.space, HasSpace: true}
}

// readNode は [x] の読み出し。値の式では size バイト（リトルエンディアン）を
// 読む。位置の式の最も外側では 2 バイトのポインタとして読む。
type readNode struct {
	inner node
	size  int
	// sized は .w・.bN を明示したことを表す。
	sized bool
}

func (n readNode) eval(c *evalCtx, addr bool) Value {
	v := n.inner.eval(c, true)
	s := SpaceCPU
	if v.HasSpace {
		s = v.Space
	}
	size := n.size
	if addr && !n.sized {
		size = 2
	}
	var out int64
	for i := range size {
		out |= int64(c.peek(s, int(v.Num)+i)) << (8 * i)
	}
	if addr {
		return Value{Num: out, Space: SpaceCPU, HasSpace: true}
	}
	return num(out)
}

type unaryNode struct {
	op    string
	inner node
}

func (n unaryNode) eval(c *evalCtx, addr bool) Value {
	v := n.inner.eval(c, addr)
	switch n.op {
	case "!":
		return boolValue(!v.truthy())
	case "~":
		return num(^v.Num)
	case "-":
		return num(-v.Num)
	}
	return v
}

type binaryNode struct {
	op   string
	l, r node
}

func (n binaryNode) eval(c *evalCtx, addr bool) Value {
	switch n.op {
	case "&&":
		return boolValue(n.l.eval(c, addr).truthy() && n.r.eval(c, addr).truthy())
	case "||":
		return boolValue(n.l.eval(c, addr).truthy() || n.r.eval(c, addr).truthy())
	}
	l, r := n.l.eval(c, addr), n.r.eval(c, addr)
	switch n.op {
	case "==", "!=":
		eq := equal(l, r)
		if n.op == "!=" {
			eq = !eq
		}
		return boolValue(eq)
	case "<":
		return boolValue(l.Num < r.Num)
	case "<=":
		return boolValue(l.Num <= r.Num)
	case ">":
		return boolValue(l.Num > r.Num)
	case ">=":
		return boolValue(l.Num >= r.Num)
	}
	out := Value{Space: l.Space, HasSpace: l.HasSpace}
	base := l
	if !out.HasSpace {
		out.Space, out.HasSpace = r.Space, r.HasSpace
		base = r
	}

	switch n.op {
	case "+":
		out.Num = l.Num + r.Num
	case "-":
		out.Num = l.Num - r.Num
	case "*":
		out.Num = l.Num * r.Num
	case "/", "%":
		if r.Num == 0 {
			// 評価の途中で止めない。条件式は偽、値の式は 0 とする
			// （設計書 14 編 §14.11.4）。
			c.divZero = true
			out.Num = 0
		} else if n.op == "/" {
			out.Num = l.Num / r.Num
		} else {
			out.Num = l.Num % r.Num
		}
	case "&":
		out.Num = l.Num & r.Num
	case "|":
		out.Num = l.Num | r.Num
	case "^":
		out.Num = l.Num ^ r.Num
	case "<<":
		out.Num = l.Num << uint(r.Num&63)
	case ">>":
		out.Num = l.Num >> uint(r.Num&63)
	}
	// PRG-ROM の位置を足し引きしたときは、CPU アドレスも同じだけずらす。
	if base.HasCPU && (n.op == "+" || n.op == "-") {
		out.CPU, out.HasCPU = base.CPU+out.Num-base.Num, true
	}
	return out
}

// equal は 2 つの値を比べる。両方が名前を持てば名前で比べる。文字列
// リテラルと名前を持たない数は等しくない。列挙の値と数は数で比べる。
func equal(l, r Value) bool {
	switch {
	case l.HasStr && r.HasStr:
		return l.Str == r.Str
	case l.Lit || r.Lit:
		return false
	}
	return l.Num == r.Num
}

// ---- 字句解析 ----

type tokKind uint8

const (
	tokEOF tokKind = iota
	tokNumber
	tokIdent
	tokString
	tokOp
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokSuffix // ].w の .w
	tokSpace  // ppu: の ppu
)

type token struct {
	kind tokKind
	text string
	num  int64
	pos  int
}

// operators は長い演算子を先に並べる。長い一致を優先するためである。
var operators = []string{"==", "!=", "<=", ">=", "&&", "||", "<<", ">>", "<", ">", "!", "+", "-", "*", "/", "%", "&", "|", "^", "~"}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_' }

// isDigitOf は c が base 進の桁かを返す。
func isDigitOf(c byte, base int) bool {
	switch base {
	case 2:
		return c == '0' || c == '1' || c == '_'
	case 16:
		return isDigit(c) || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f'
	}
	return isDigit(c)
}

// lex は式を字句に分ける。
func lex(s string) ([]token, error) {
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		pos := i + 1
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '(':
			out = append(out, token{kind: tokLParen, text: "(", pos: pos})
			i++
		case c == ')':
			out = append(out, token{kind: tokRParen, text: ")", pos: pos})
			i++
		case c == '[':
			out = append(out, token{kind: tokLBracket, text: "[", pos: pos})
			i++
		case c == ']':
			out = append(out, token{kind: tokRBracket, text: "]", pos: pos})
			i++
			// ].w・].b3 のサイズの指定。
			if i < len(s) && s[i] == '.' {
				j := i + 1
				for j < len(s) && (isLetter(s[j]) || isDigit(s[j])) {
					j++
				}
				out = append(out, token{kind: tokSuffix, text: s[i+1 : j], pos: i + 1})
				i = j
			}
		case c == '\'' || c == '"':
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 {
				return nil, &ParseError{Pos: pos, Msg: "文字列が閉じていない"}
			}
			out = append(out, token{kind: tokString, text: s[i+1 : i+1+j], pos: pos})
			i += j + 2
		case c == '$' || c == '%' || isDigit(c):
			// % は 2 進の数か剰余の演算子。直前が値なら演算子とする。
			if c == '%' && len(out) > 0 && endsValue(out[len(out)-1]) {
				out = append(out, token{kind: tokOp, text: "%", pos: pos})
				i++
				continue
			}
			tok, n, err := lexNumber(s[i:], pos)
			if err != nil {
				return nil, err
			}
			out = append(out, tok)
			i += n
		case isLetter(c):
			j := i
			for j < len(s) {
				switch {
				case isLetter(s[j]) || isDigit(s[j]):
					j++
					continue
				case s[j] == '.' && j+1 < len(s) && isLetter(s[j+1]):
					// game.player_x
					j++
					continue
				case s[j] == ':' && j+2 < len(s) && s[j+1] == ':' && isLetter(s[j+2]):
					// scope::name
					j += 2
					continue
				}
				break
			}
			text := s[i:j]
			if j < len(s) && s[j] == ':' && (j+1 >= len(s) || s[j+1] != ':') {
				out = append(out, token{kind: tokSpace, text: strings.ToLower(text), pos: pos})
				i = j + 1
				continue
			}
			out = append(out, token{kind: tokIdent, text: text, pos: pos})
			i = j
		default:
			matched := false
			for _, o := range operators {
				if strings.HasPrefix(s[i:], o) {
					out = append(out, token{kind: tokOp, text: o, pos: pos})
					i += len(o)
					matched = true
					break
				}
			}
			if !matched {
				return nil, &ParseError{Pos: pos, Msg: fmt.Sprintf("知らない文字 %q", c)}
			}
		}
	}
	return append(out, token{kind: tokEOF, pos: len(s) + 1}), nil
}

// endsValue は字句が値の終わりになるかを返す。
func endsValue(t token) bool {
	switch t.kind {
	case tokNumber, tokIdent, tokString, tokRParen, tokRBracket, tokSuffix:
		return true
	}
	return false
}

// lexNumber は数値リテラルを読む。$ は 16 進、% は 2 進、0x も 16 進、
// それ以外は 10 進。32 bit に収まる値を受け付ける。
func lexNumber(s string, pos int) (token, int, error) {
	base := 10
	start := 0
	switch {
	case s[0] == '$':
		base, start = 16, 1
	case s[0] == '%':
		base, start = 2, 1
	case len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X'):
		base, start = 16, 2
	}
	end := start
	for end < len(s) && isDigitOf(s[end], base) {
		end++
	}
	if end == start {
		return token{}, 0, &ParseError{Pos: pos, Msg: "数値の桁が無い"}
	}
	v, err := strconv.ParseInt(strings.ReplaceAll(s[start:end], "_", ""), base, 64)
	if err != nil || v > 0xFFFFFFFF {
		return token{}, 0, &ParseError{Pos: pos, Msg: fmt.Sprintf("数値 %q が大きすぎる", s[:end])}
	}
	return token{kind: tokNumber, text: s[:end], num: v, pos: pos}, end, nil
}

// ---- 構文解析 ----
//
// 優先順位（低い順）: || → && → !（論理の否定） → 比較 → | → ^ → & →
// << >> → + - → * / % → 単項の ~ - → 一次式。
// ! を比較より低くするのは、既存の条件式の結果（!A == 1 は !(A == 1)）を
// 変えないためである。

type parser struct {
	toks []token
	pos  int
	r    Resolver
	// addrTop は位置の式の解析であることを表す。
	addrTop bool
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) isOp(texts ...string) (string, bool) {
	t := p.peek()
	if t.kind != tokOp {
		return "", false
	}
	for _, x := range texts {
		if t.text == x {
			return x, true
		}
	}
	return "", false
}

func parseExpr(expr string, r Resolver, addr bool) (node, error) {
	toks, err := lex(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, r: r, addrTop: addr}
	var root node
	if addr {
		root, err = p.parseBitOr()
	} else {
		root, err = p.parseOr()
	}
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("余分な %q がある", t.text)}
	}
	return root, nil
}

func (p *parser) parseOr() (node, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := p.isOp("||"); !ok {
			return l, nil
		}
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = binaryNode{op: "||", l: l, r: r}
	}
}

func (p *parser) parseAnd() (node, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := p.isOp("&&"); !ok {
			return l, nil
		}
		p.next()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = binaryNode{op: "&&", l: l, r: r}
	}
}

func (p *parser) parseNot() (node, error) {
	if _, ok := p.isOp("!"); ok {
		p.next()
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: "!", inner: inner}, nil
	}
	return p.parseCompare()
}

func (p *parser) parseCompare() (node, error) {
	l, err := p.parseBitOr()
	if err != nil {
		return nil, err
	}
	op, ok := p.isOp("==", "!=", "<", "<=", ">", ">=")
	if !ok {
		return l, nil
	}
	p.next()
	r, err := p.parseBitOr()
	if err != nil {
		return nil, err
	}
	return binaryNode{op: op, l: l, r: r}, nil
}

// binaryLevel は左結合の 2 項演算子の 1 段を解析する。
func (p *parser) binaryLevel(next func() (node, error), ops ...string) (node, error) {
	l, err := next()
	if err != nil {
		return nil, err
	}
	for {
		op, ok := p.isOp(ops...)
		if !ok {
			return l, nil
		}
		p.next()
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = binaryNode{op: op, l: l, r: r}
	}
}

func (p *parser) parseBitOr() (node, error)  { return p.binaryLevel(p.parseBitXor, "|") }
func (p *parser) parseBitXor() (node, error) { return p.binaryLevel(p.parseBitAnd, "^") }
func (p *parser) parseBitAnd() (node, error) { return p.binaryLevel(p.parseShift, "&") }
func (p *parser) parseShift() (node, error)  { return p.binaryLevel(p.parseSum, "<<", ">>") }
func (p *parser) parseSum() (node, error)    { return p.binaryLevel(p.parseProduct, "+", "-") }
func (p *parser) parseProduct() (node, error) {
	return p.binaryLevel(p.parseUnary, "*", "/", "%")
}

func (p *parser) parseUnary() (node, error) {
	if op, ok := p.isOp("~", "-"); ok {
		p.next()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: op, inner: inner}, nil
	}
	return p.parsePrimary()
}

// spacePrefixes は空間の接頭辞（設計書 14 編 §14.6）。
var spacePrefixes = []struct {
	name  string
	space Space
	base  int
}{
	{"cpu", SpaceCPU, 0}, {"ppu", SpacePPU, 0}, {"oam", SpaceOAM, 0}, {"pal", SpacePPU, 0x3F00},
	{"chr", SpaceCHR, 0}, {"prg", SpacePRGROM, 0}, {"ram", SpaceRAM, 0},
}

func (p *parser) parsePrimary() (node, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		return numNode{t.num}, nil
	case tokString:
		return strNode{t.text}, nil
	case tokSpace:
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if n, ok := strings.CutPrefix(t.text, "bank"); ok {
			b, err := strconv.Atoi(n)
			if err != nil || b < 0 {
				return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("バンク %q を読めない（bank0: の形）", t.text)}
			}
			return spaceNode{space: SpacePRGROM, bank: b, inner: inner}, nil
		}
		for _, sp := range spacePrefixes {
			if sp.name == t.text {
				return spaceNode{space: sp.space, bank: -1, base: sp.base, inner: inner}, nil
			}
		}
		return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("知らない空間 %q（cpu・ppu・oam・pal・chr・prg・ram・bankN）", t.text)}
	case tokIdent:
		return p.ident(t)
	case tokLParen:
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, ")"); err != nil {
			return nil, err
		}
		return inner, nil
	case tokLBracket:
		inner, err := p.parseBitOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRBracket, "]"); err != nil {
			return nil, err
		}
		n := readNode{inner: inner, size: 1}
		if s := p.peek(); s.kind == tokSuffix {
			p.next()
			size, ok := sizeSuffix(s.text)
			if !ok {
				return nil, &ParseError{Pos: s.pos, Msg: fmt.Sprintf("サイズの指定 .%s を知らない（.w・.b1–.b4）", s.text)}
			}
			n.size, n.sized = size, true
		}
		return n, nil
	case tokEOF:
		return nil, &ParseError{Pos: t.pos, Msg: "式が途中で終わっている"}
	}
	return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("ここに %q は置けない", t.text)}
}

func sizeSuffix(s string) (int, bool) {
	switch strings.ToLower(s) {
	case "w", "b2":
		return 2, true
	case "b", "b1":
		return 1, true
	case "b3":
		return 3, true
	case "b4":
		return 4, true
	}
	return 0, false
}

// ident は名前を解釈する。レジスタ・フラグ・実行の位置を、同じ綴りの
// Symbol より優先する。
func (p *parser) ident(t token) (node, error) {
	up := strings.ToUpper(t.text)
	if r, ok := registers[up]; ok {
		return regNode{r}, nil
	}
	if f, ok := flags[up]; ok {
		return flagNode{f}, nil
	}
	for i, n := range positionNames {
		if strings.EqualFold(t.text, n) && p.r != nil {
			return posNode{Position(i)}, nil
		}
	}
	if g, ok := strings.CutPrefix(t.text, "game."); ok && p.r != nil {
		if !p.r.HasGameValue(g) {
			return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("Game State %q は無い", g)}
		}
		return gameNode{g}, nil
	}
	if p.r != nil && p.r.HasSymbol(t.text) {
		return symNode{t.text}, nil
	}
	return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("知らない名前 %q", t.text)}
}

// expect は次の字句が kind であることを確かめて読み進める。
func (p *parser) expect(kind tokKind, text string) error {
	t := p.next()
	if t.kind != kind {
		return &ParseError{Pos: t.pos, Msg: fmt.Sprintf("%q が必要である", text)}
	}
	return nil
}
