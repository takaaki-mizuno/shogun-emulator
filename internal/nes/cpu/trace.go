package cpu

import (
	"fmt"
	"unsafe"
)

// traceReads はトレースの注記に必要な読み出しの最大数。
//
// 間接アドレッシングはポインタの 2 バイトと実効アドレスの値を読む。
const traceReads = 3

// TraceRecord はトレースの 1 行分を、整形せずに保持する。
//
// 1 行あたり 32 バイトに収める。100 万命令のリングで 32 MiB になる
// （設計書 09 編 §9.7）。整形は書き出すときに行う。命令ごとに
// 逆アセンブルの文字列を作ると、トレースを有効にするだけで実行が遅れる。
type TraceRecord struct {
	Cycles uint64
	PC     uint16
	// Scanline と Dot は PPU の位置。
	Scanline int16
	Dot      int16
	A, X, Y  uint8
	P, S     uint8
	Bytes    [3]uint8
	// Reads は注記のために読んだ値。readAddrs と同じ順に並ぶ。
	Reads [traceReads]uint8
}

// TraceRecord は現在の命令境界のトレースを返す。
//
// 読み出しは Peek を通す。トレースの取得がエミュレーションの状態を
// 変えてはならない。
func (c *CPU) TraceRecord(scanline, dot int) TraceRecord {
	r := TraceRecord{
		Cycles:   c.Cycles(),
		PC:       c.PC,
		Scanline: int16(scanline),
		Dot:      int16(dot),
		A:        c.A,
		X:        c.X,
		Y:        c.Y,
		P:        c.packP(false),
		S:        c.S,
	}
	op := &opcodes[c.bus.Peek(c.PC)]
	n := 1 + op.mode.operandLen()
	for i := range n {
		r.Bytes[i] = c.bus.Peek(c.PC + uint16(i))
	}
	addrs, count := readAddrs(op, &r)
	for i := range count {
		if i == 2 && isIndirectData(op) {
			break
		}
		r.Reads[i] = c.bus.Peek(addrs[i])
	}
	if count == traceReads && isIndirectData(op) {
		// ポインタを読んでから実効アドレスの値を読む。
		r.Reads[2] = c.bus.Peek(indirectEffective(op, &r))
	}
	return r
}

// isIndirectData はポインタを経由してデータを読むアドレッシングかを返す。
func isIndirectData(op *opcode) bool {
	return op.mode == modeIndirectX || op.mode == modeIndirectY
}

// indirectEffective は記録したポインタの値から実効アドレスを求める。
func indirectEffective(op *opcode, r *TraceRecord) uint16 {
	base := uint16(r.Reads[1])<<8 | uint16(r.Reads[0])
	if op.mode == modeIndirectY {
		return base + uint16(r.Y)
	}
	return base
}

// readAddrs は逆アセンブルの注記が読むアドレスを、読む順に返す。
//
// Disassemble の注記と同じ規則で求める。ポインタを読んでから実効
// アドレスを決める間接アドレッシングでは、記録したポインタの値を使う。
func readAddrs(op *opcode, r *TraceRecord) (addrs [traceReads]uint16, n int) {
	b1 := r.Bytes[1]
	b2 := uint16(r.Bytes[2])<<8 | uint16(r.Bytes[1])
	annotated := op.access != accessNone

	add := func(a uint16) {
		addrs[n] = a
		n++
	}
	switch op.mode {
	case modeZeroPage:
		if annotated {
			add(uint16(b1))
		}
	case modeZeroPageX:
		if annotated {
			add(uint16(b1 + r.X))
		}
	case modeZeroPageY:
		if annotated {
			add(uint16(b1 + r.Y))
		}
	case modeAbsolute:
		if annotated {
			add(b2)
		}
	case modeAbsoluteX:
		if annotated {
			add(b2 + uint16(r.X))
		}
	case modeAbsoluteY:
		if annotated {
			add(b2 + uint16(r.Y))
		}
	case modeIndirectX:
		ptr := b1 + r.X
		add(uint16(ptr))
		add(uint16(ptr + 1))
		// 実効アドレスはポインタの値から決まる。値は読み出しの後に
		// わかるため、ここでは印だけを置き、TraceRecord で読み直す。
		if annotated {
			n++
		}
	case modeIndirectY:
		add(uint16(b1))
		add(uint16(b1 + 1))
		if annotated {
			n++
		}
	case modeIndirect:
		add(b2)
		add(b2&0xFF00 | uint16(uint8(b2)+1))
	}
	return addrs, n
}

// Line は State.TraceLine と同じ形式の 1 行を返す。
func (r TraceRecord) Line() string {
	op := &opcodes[r.Bytes[0]]
	n := 1 + op.mode.operandLen()
	peek := r.replayPeeker(op)
	text, _ := Disassemble(peek, r.PC, r.X, r.Y)
	s := State{
		PC:          r.PC,
		A:           r.A,
		X:           r.X,
		Y:           r.Y,
		P:           r.P,
		S:           r.S,
		Bytes:       r.Bytes,
		ByteLen:     n,
		Disasm:      text,
		Official:    op.official,
		PPUScanline: int(r.Scanline),
		PPUDot:      int(r.Dot),
		Cycles:      r.Cycles,
	}
	return s.TraceLine()
}

// replayPeeker は記録した値を返す Peeker を作る。
//
// 命令のバイト列と、注記のために読んだ値だけを持つ。それ以外の
// アドレスは読まれない。
func (r *TraceRecord) replayPeeker(op *opcode) Peeker {
	addrs, count := readAddrs(op, r)
	// 間接アドレッシングの実効アドレスはポインタの値から決まる。
	var effective uint16
	hasEffective := count == traceReads && isIndirectData(op)
	if hasEffective {
		effective = indirectEffective(op, r)
	}
	return func(addr uint16) uint8 {
		if d := addr - r.PC; d < 3 {
			return r.Bytes[d]
		}
		if hasEffective && addr == effective {
			return r.Reads[2]
		}
		for i := range count {
			if addrs[i] == addr && !(hasEffective && i == 2) {
				return r.Reads[i]
			}
		}
		panic(fmt.Sprintf("cpu: トレースに記録していないアドレス $%04X を読んだ", addr))
	}
}

// traceRecordSize は TraceRecord の大きさを返す。テストで上限を確かめる。
func traceRecordSize() int { return int(unsafe.Sizeof(TraceRecord{})) }
