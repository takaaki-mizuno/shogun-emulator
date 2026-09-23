package cpu

import (
	"fmt"
	"strings"
)

// State は命令境界での CPU の状態。トレース出力に使う。
type State struct {
	PC      uint16
	A, X, Y uint8
	// P は packP(false) の結果。
	P uint8
	S uint8
	// Bytes は命令の機械語。
	Bytes [3]uint8
	// ByteLen は命令の長さ。
	ByteLen int
	// Disasm は逆アセンブル結果。
	Disasm string
	// Official は公式命令かを表す。非公式命令には * を付ける。
	Official bool

	PPUScanline int
	PPUDot      int
	Cycles      uint64
}

// State は現在の状態を返す。
//
// 命令バイト列の読み出しに Peek を使う。トレースの取得が
// エミュレーションの状態を変えてはならない。
func (c *CPU) State(scanline, dot int) State {
	code := c.bus.Peek(c.PC)
	op := &opcodes[code]

	text, n := Disassemble(c.bus.Peek, c.PC, c.X, c.Y)

	s := State{
		PC:          c.PC,
		A:           c.A,
		X:           c.X,
		Y:           c.Y,
		P:           c.packP(false),
		S:           c.S,
		ByteLen:     n,
		Disasm:      text,
		Official:    op.official,
		PPUScanline: scanline,
		PPUDot:      dot,
		Cycles:      c.Cycles(),
	}
	for i := range n {
		s.Bytes[i] = c.bus.Peek(c.PC + uint16(i))
	}
	return s
}

// TraceLine は nestest.log と同じ形式の 1 行を返す。
//
// 桁位置を合わせるのは、ログと行単位で比較するためである。
//
//	C000  4C F5 C5  JMP $C5F5                       A:00 X:00 Y:00 P:24 SP:FD PPU:  0, 21 CYC:7
func (s State) TraceLine() string {
	var b strings.Builder
	b.Grow(96)

	fmt.Fprintf(&b, "%04X  ", s.PC)

	// 機械語の欄は 3 バイト分の幅で固定する
	var bytes strings.Builder
	for i := range s.ByteLen {
		if i > 0 {
			bytes.WriteByte(' ')
		}
		fmt.Fprintf(&bytes, "%02X", s.Bytes[i])
	}
	fmt.Fprintf(&b, "%-8s ", bytes.String())

	// 非公式命令の印は機械語の欄とニモニックの間に置く
	if s.Official {
		b.WriteByte(' ')
	} else {
		b.WriteByte('*')
	}

	// 命令欄は 16 桁目から 32 桁。レジスタの欄は 48 桁目から始まる。
	fmt.Fprintf(&b, "%-32s", s.Disasm)
	fmt.Fprintf(&b, "A:%02X X:%02X Y:%02X P:%02X SP:%02X PPU:%3d,%3d CYC:%d",
		s.A, s.X, s.Y, s.P, s.S, s.PPUScanline, s.PPUDot, s.Cycles)

	return b.String()
}
