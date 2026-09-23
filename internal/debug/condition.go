package debug

import (
	"fmt"
	"strconv"
	"strings"
)

// Register は条件式で参照できるレジスタ。
type Register uint8

// レジスタの一覧。
const (
	RegA Register = iota
	RegX
	RegY
	RegS
	RegP
	RegPC
)

// Flag は条件式で参照できるフラグ。
type Flag uint8

// フラグの一覧。P の各ビットに対応する。
const (
	FlagC Flag = iota
	FlagZ
	FlagI
	FlagD
	FlagV
	FlagN
)

// Env は条件式の評価に使う値の取り出し口。
//
// メモリの読み出しは Peek を通す。評価がエミュレーションの状態を
// 変えてはならない（設計書 09 編 §9.6.1）。
type Env interface {
	Reg(r Register) uint16
	Flag(f Flag) bool
	Peek(addr uint16) uint8
}

// Condition はブレークポイントの条件式。
type Condition struct {
	// Expr は利用者が書いた式。
	Expr string
	prog []instruction
}

// op は評価の手順の種類。
type op uint8

const (
	opPush op = iota
	opReg
	opFlag
	opPeek
	opAdd
	opSub
	opEq
	opNe
	opLt
	opLe
	opGt
	opGe
	opAnd
	opOr
	opNot
)

// instruction は評価の手順 1 つ。後置記法で並べる。
type instruction struct {
	op  op
	arg int
}

// ParseError は式を解析できなかった理由と位置を表す。
type ParseError struct {
	// Pos は式の先頭を 1 とした文字の位置。
	Pos int
	Msg string
}

// Error は人が読める説明を返す。
func (e *ParseError) Error() string {
	return fmt.Sprintf("条件式の %d 文字目: %s", e.Pos, e.Msg)
}

// ParseCondition は式を解析する。
func ParseCondition(expr string) (*Condition, error) {
	toks, err := lex(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if err := p.parseOr(); err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		return nil, &ParseError{Pos: t.pos, Msg: fmt.Sprintf("余分な %q がある", t.text)}
	}
	return &Condition{Expr: expr, prog: p.out}, nil
}

// Eval は式を評価する。0 以外のとき true を返す。
func (c *Condition) Eval(env Env) bool {
	if c == nil {
		return true
	}
	return c.value(env) != 0
}

// value は式の値を求める。
func (c *Condition) value(env Env) int {
	var stack [32]int
	sp := 0
	push := func(v int) {
		if sp < len(stack) {
			stack[sp] = v
			sp++
		}
	}
	pop := func() int {
		if sp == 0 {
			return 0
		}
		sp--
		return stack[sp]
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	for _, in := range c.prog {
		switch in.op {
		case opPush:
			push(in.arg)
		case opReg:
			push(int(env.Reg(Register(in.arg))))
		case opFlag:
			push(b2i(env.Flag(Flag(in.arg))))
		case opPeek:
			push(int(env.Peek(uint16(pop()))))
		case opNot:
			push(b2i(pop() == 0))
		default:
			r, l := pop(), pop()
			switch in.op {
			case opAdd:
				push((l + r) & 0xFFFF)
			case opSub:
				push((l - r) & 0xFFFF)
			case opEq:
				push(b2i(l == r))
			case opNe:
				push(b2i(l != r))
			case opLt:
				push(b2i(l < r))
			case opLe:
				push(b2i(l <= r))
			case opGt:
				push(b2i(l > r))
			case opGe:
				push(b2i(l >= r))
			case opAnd:
				push(b2i(l != 0 && r != 0))
			case opOr:
				push(b2i(l != 0 || r != 0))
			}
		}
	}
	return pop()
}

// tokKind は字句の種類。
type tokKind uint8

const (
	tokEOF tokKind = iota
	tokNumber
	tokIdent
	tokOp
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
)

// token は字句 1 つ。
type token struct {
	kind tokKind
	text string
	num  int
	pos  int
}

// operators は 2 文字の演算子を先に並べる。長い一致を優先するためである。
var operators = []string{"==", "!=", "<=", ">=", "&&", "||", "<", ">", "!", "+", "-"}

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
		case c == '$' || c == '%' || isDigit(c):
			tok, n, err := lexNumber(s[i:], pos)
			if err != nil {
				return nil, err
			}
			out = append(out, tok)
			i += n
		case isLetter(c):
			j := i
			for j < len(s) && isLetter(s[j]) {
				j++
			}
			out = append(out, token{kind: tokIdent, text: strings.ToUpper(s[i:j]), pos: pos})
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

// lexNumber は数値リテラルを読む。$ は 16 進、% は 2 進、それ以外は 10 進。
func lexNumber(s string, pos int) (token, int, error) {
	base := 10
	start := 0
	switch s[0] {
	case '$':
		base, start = 16, 1
	case '%':
		base, start = 2, 1
	}
	end := start
	for end < len(s) && isDigitOf(s[end], base) {
		end++
	}
	if end == start {
		return token{}, 0, &ParseError{Pos: pos, Msg: "数値の桁が無い"}
	}
	v, err := strconv.ParseInt(s[start:end], base, 32)
	if err != nil || v > 0xFFFF {
		return token{}, 0, &ParseError{Pos: pos, Msg: fmt.Sprintf("数値 %q が大きすぎる", s[:end])}
	}
	return token{kind: tokNumber, text: s[:end], num: int(v), pos: pos}, end, nil
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// isDigitOf は c が base 進の桁かを返す。
func isDigitOf(c byte, base int) bool {
	switch base {
	case 2:
		return c == '0' || c == '1'
	case 16:
		return isDigit(c) || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f'
	}
	return isDigit(c)
}

// registers と flags は識別子の対応。
var registers = map[string]Register{
	"A": RegA, "X": RegX, "Y": RegY, "S": RegS, "P": RegP, "PC": RegPC,
}

var flags = map[string]Flag{
	"C": FlagC, "Z": FlagZ, "I": FlagI, "D": FlagD, "V": FlagV, "N": FlagN,
}

// parser は字句を後置記法の手順へ変える。
type parser struct {
	toks []token
	pos  int
	out  []instruction
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) emit(o op, arg int) { p.out = append(p.out, instruction{op: o, arg: arg}) }

// isOp は次の字句が演算子 text かを返す。
func (p *parser) isOp(text string) bool {
	t := p.peek()
	return t.kind == tokOp && t.text == text
}

func (p *parser) parseOr() error {
	if err := p.parseAnd(); err != nil {
		return err
	}
	for p.isOp("||") {
		p.next()
		if err := p.parseAnd(); err != nil {
			return err
		}
		p.emit(opOr, 0)
	}
	return nil
}

func (p *parser) parseAnd() error {
	if err := p.parseUnary(); err != nil {
		return err
	}
	for p.isOp("&&") {
		p.next()
		if err := p.parseUnary(); err != nil {
			return err
		}
		p.emit(opAnd, 0)
	}
	return nil
}

func (p *parser) parseUnary() error {
	if p.isOp("!") {
		p.next()
		if err := p.parseUnary(); err != nil {
			return err
		}
		p.emit(opNot, 0)
		return nil
	}
	return p.parseCompare()
}

// compareOps は比較演算子と手順の対応。
var compareOps = map[string]op{
	"==": opEq, "!=": opNe, "<": opLt, "<=": opLe, ">": opGt, ">=": opGe,
}

func (p *parser) parseCompare() error {
	if err := p.parseSum(); err != nil {
		return err
	}
	t := p.peek()
	if t.kind != tokOp {
		return nil
	}
	o, ok := compareOps[t.text]
	if !ok {
		return nil
	}
	p.next()
	if err := p.parseSum(); err != nil {
		return err
	}
	p.emit(o, 0)
	return nil
}

func (p *parser) parseSum() error {
	if err := p.parsePrimary(); err != nil {
		return err
	}
	for p.isOp("+") || p.isOp("-") {
		o := opAdd
		if p.next().text == "-" {
			o = opSub
		}
		if err := p.parsePrimary(); err != nil {
			return err
		}
		p.emit(o, 0)
	}
	return nil
}

func (p *parser) parsePrimary() error {
	t := p.next()
	switch t.kind {
	case tokNumber:
		p.emit(opPush, t.num)
		return nil
	case tokIdent:
		if r, ok := registers[t.text]; ok {
			p.emit(opReg, int(r))
			return nil
		}
		if f, ok := flags[t.text]; ok {
			p.emit(opFlag, int(f))
			return nil
		}
		return &ParseError{Pos: t.pos, Msg: fmt.Sprintf("知らない名前 %q", t.text)}
	case tokLParen:
		if err := p.parseOr(); err != nil {
			return err
		}
		return p.expect(tokRParen, ")")
	case tokLBracket:
		if err := p.parseSum(); err != nil {
			return err
		}
		if err := p.expect(tokRBracket, "]"); err != nil {
			return err
		}
		p.emit(opPeek, 0)
		return nil
	case tokEOF:
		return &ParseError{Pos: t.pos, Msg: "式が途中で終わっている"}
	}
	return &ParseError{Pos: t.pos, Msg: fmt.Sprintf("ここに %q は置けない", t.text)}
}

// expect は次の字句が kind であることを確かめて読み進める。
func (p *parser) expect(kind tokKind, text string) error {
	t := p.next()
	if t.kind != kind {
		return &ParseError{Pos: t.pos, Msg: fmt.Sprintf("%q が必要である", text)}
	}
	return nil
}
