package cpu

import "fmt"

// Peeker は副作用なしにメモリを読む関数。
type Peeker func(addr uint16) uint8

// Disassemble は pc の命令を逆アセンブルする。
//
// 副作用を起こさないため、メモリの読み出しは Peeker を通す。x と y は
// インデックス付きアドレッシングの実効アドレスを求めるために使う。
//
// 出力は nestest.log の命令欄と同じ形にする。実効アドレスと、その位置の
// 現在の値を注記するのはログと突き合わせるためである。
func Disassemble(peek Peeker, pc uint16, x, y uint8) (text string, length int) {
	code := peek(pc)
	op := &opcodes[code]
	length = 1 + op.mode.operandLen()

	name := op.mnemonic
	operand := disasmOperand(peek, op, pc, x, y)
	if operand == "" {
		return name, length
	}
	return name + " " + operand, length
}

// disasmOperand はオペランドの表記を返す。
func disasmOperand(peek Peeker, op *opcode, pc uint16, x, y uint8) string {
	b1 := func() uint8 { return peek(pc + 1) }
	b2 := func() uint16 { return uint16(peek(pc+2))<<8 | uint16(peek(pc+1)) }
	// annotate は実効アドレスの現在の値を注記する。
	// アクセスしないアドレスには注記しない。
	annotate := func(addr uint16) string {
		if op.access == accessNone {
			return ""
		}
		return fmt.Sprintf(" = %02X", peek(addr))
	}

	switch op.mode {
	case modeImplied:
		return ""
	case modeAccumulator:
		return "A"
	case modeImmediate:
		return fmt.Sprintf("#$%02X", b1())
	case modeZeroPage:
		addr := uint16(b1())
		return fmt.Sprintf("$%02X%s", b1(), annotate(addr))
	case modeZeroPageX:
		addr := uint16(b1() + x)
		return fmt.Sprintf("$%02X,X @ %02X%s", b1(), addr, annotate(addr))
	case modeZeroPageY:
		addr := uint16(b1() + y)
		return fmt.Sprintf("$%02X,Y @ %02X%s", b1(), addr, annotate(addr))
	case modeAbsolute:
		addr := b2()
		return fmt.Sprintf("$%04X%s", addr, annotate(addr))
	case modeAbsoluteX:
		base := b2()
		addr := base + uint16(x)
		return fmt.Sprintf("$%04X,X @ %04X%s", base, addr, annotate(addr))
	case modeAbsoluteY:
		base := b2()
		addr := base + uint16(y)
		return fmt.Sprintf("$%04X,Y @ %04X%s", base, addr, annotate(addr))
	case modeIndirectX:
		ptr := b1() + x
		addr := uint16(peek(uint16(ptr+1)))<<8 | uint16(peek(uint16(ptr)))
		return fmt.Sprintf("($%02X,X) @ %02X = %04X%s", b1(), ptr, addr, annotate(addr))
	case modeIndirectY:
		ptr := b1()
		base := uint16(peek(uint16(ptr+1)))<<8 | uint16(peek(uint16(ptr)))
		addr := base + uint16(y)
		return fmt.Sprintf("($%02X),Y = %04X @ %04X%s", ptr, base, addr, annotate(addr))
	case modeRelative:
		target := uint16(int32(pc+2) + int32(int8(b1())))
		return fmt.Sprintf("$%04X", target)
	case modeIndirect:
		ptr := b2()
		// ポインタの上位バイトは下位バイトのみをインクリメントして計算する
		hi := peek(ptr&0xFF00 | uint16(uint8(ptr)+1))
		target := uint16(hi)<<8 | uint16(peek(ptr))
		return fmt.Sprintf("($%04X) = %04X", ptr, target)
	case modeJSR:
		return fmt.Sprintf("$%04X", b2())
	}
	return ""
}
