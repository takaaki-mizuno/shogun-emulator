package debug

import (
	"fmt"
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
	root node
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

// ParseCondition は式を解析する。名前（Symbol と Game State）を使えない。
// 名前を使う式は ParseConditionWith で解析する。
func ParseCondition(expr string) (*Condition, error) { return ParseConditionWith(expr, nil) }

// ParseConditionWith は名前の有無を r で確かめながら式を解析する
// （設計書 14 編 §14.11.4）。r が nil のとき、レジスタとフラグ以外の名前は
// 誤りとする。
func ParseConditionWith(expr string, r Resolver) (*Condition, error) {
	root, err := parseExpr(expr, r, false)
	if err != nil {
		return nil, err
	}
	return &Condition{Expr: expr, root: root}, nil
}

// Eval は式を評価する。0 以外のとき true を返す。0 による除算を含むときは
// 偽とする。
func (c *Condition) Eval(env Env) bool {
	if c == nil {
		return true
	}
	ctx := &evalCtx{env: env}
	ctx.ext, _ = env.(ExprEnv)
	v := c.root.eval(ctx, false)
	if ctx.divZero {
		return false
	}
	return v.truthy()
}

// Value は条件式を値の式として評価する。0 による除算は 0 とする。
func (c *Condition) Value(env Env) Value {
	ctx := &evalCtx{env: env}
	ctx.ext, _ = env.(ExprEnv)
	return c.root.eval(ctx, false)
}

// registers と flags は識別子の対応。
var registers = map[string]Register{
	"A": RegA, "X": RegX, "Y": RegY, "S": RegS, "P": RegP, "PC": RegPC,
}

var flags = map[string]Flag{
	"C": FlagC, "Z": FlagZ, "I": FlagI, "D": FlagD, "V": FlagV, "N": FlagN,
}
