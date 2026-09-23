package debug

import (
	"fmt"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// Peeker は副作用なしにメモリを読む関数。
type Peeker func(addr uint16) uint8

// Offsetter は CPU アドレスを PRG-ROM のオフセットへ変換する関数。
type Offsetter func(addr uint16) (int, bool)

// Line は逆アセンブルの 1 行。
type Line struct {
	Addr  uint16
	Bytes []uint8
	// Mnemonic はニーモニック。Alias は非公式命令の別名。
	Mnemonic string
	Alias    string
	// Operand はオペランドの表記。飛び先に名前があれば名前で書く。
	Operand string
	// Comment は実効アドレスとその現在値の注記。
	Comment string
	// Label はこの行のアドレスに付けた名前。
	Label string
	// Official は公式命令かを表す。
	Official bool
	// Estimated は実行の記録から確定していない行であることを表す。
	Estimated bool
	// Data は利用者がデータと指定した領域の行であることを表す。
	Data bool
}

// Text はニーモニックとオペランドを 1 つの文字列にする。
func (l Line) Text() string {
	if l.Data {
		return fmt.Sprintf(".byte $%02X", l.Bytes[0])
	}
	s := l.Mnemonic
	if l.Operand != "" {
		s += " " + l.Operand
	}
	return s
}

// Disassembler は実行の記録を使って命令境界を確定させる逆アセンブラ。
//
// 6502 はコードとデータを区別できない。線形に逆アセンブルすると、
// データの途中から命令を読み始めて以降がすべてずれる。実行したアドレスを
// PRG-ROM のオフセットで記録し、記録のある位置を命令の先頭として確定する
// （設計書 09 編 §9.4.6）。
//
// Record はエミュレーションゴルーチンから、Listing も同じゴルーチンから
// 呼ぶ（UI は命令境界のコマンドを通して結果を受け取る）。
type Disassembler struct {
	// executed は PRG-ROM のオフセットごとに、命令の先頭として実行された
	// ことを表す。
	executed []bool
	symbols  *Symbols
}

// NewDisassembler は PRG-ROM の大きさを与えて逆アセンブラを作る。
func NewDisassembler(prgSize int, symbols *Symbols) *Disassembler {
	if symbols == nil {
		symbols = NewSymbols()
	}
	return &Disassembler{executed: make([]bool, prgSize), symbols: symbols}
}

// Symbols は名前とコード・データの指定を返す。
func (d *Disassembler) Symbols() *Symbols { return d.symbols }

// Record は PRG-ROM のオフセットの命令を実行したことを記録する。
func (d *Disassembler) Record(offset int) {
	if offset >= 0 && offset < len(d.executed) {
		d.executed[offset] = true
	}
}

// Executed はオフセットの命令を実行したことがあるかを返す。
func (d *Disassembler) Executed(offset int) bool {
	return offset >= 0 && offset < len(d.executed) && d.executed[offset]
}

// ExecutedCount は実行したことのある命令の先頭の数を返す。
func (d *Disassembler) ExecutedCount() int {
	n := 0
	for _, e := range d.executed {
		if e {
			n++
		}
	}
	return n
}

// Listing は anchor から count 行を逆アセンブルする。
//
// x と y は実効アドレスの注記に使う現在のインデックスレジスタである。
func (d *Disassembler) Listing(peek Peeker, offset Offsetter, anchor uint16, count int, x, y uint8) []Line {
	out := make([]Line, 0, count)
	addr := anchor
	// estimated は直前までの行が推定であることを表す。実行の記録がある
	// 位置に当たると確定に戻る。
	estimated := false
	for range count {
		l := d.line(peek, offset, addr, x, y)
		if l.Data {
			estimated = false
		} else if off, ok := offset(addr); ok {
			if d.Executed(off) {
				estimated = false
			} else {
				estimated = true
			}
		}
		l.Estimated = estimated && !l.Data
		out = append(out, l)
		next := addr + uint16(len(l.Bytes))
		if next < addr {
			break
		}
		addr = next
	}
	return out
}

// line は addr の 1 行を作る。
func (d *Disassembler) line(peek Peeker, offset Offsetter, addr uint16, x, y uint8) Line {
	l := Line{Addr: addr, Label: d.symbols.Label(addr)}
	if off, ok := offset(addr); ok && d.symbols.IsData(off) {
		l.Data = true
		l.Bytes = []uint8{peek(addr)}
		return l
	}
	code := peek(addr)
	info := cpu.Info(code)
	l.Mnemonic = info.Mnemonic
	l.Alias = info.Alias
	l.Official = info.Official
	l.Bytes = make([]uint8, info.Length)
	for i := range info.Length {
		l.Bytes[i] = peek(addr + uint16(i))
	}
	l.Operand, l.Comment = d.operand(peek, info, addr, x, y)
	return l
}

// operand はオペランドの表記と注記を返す。
func (d *Disassembler) operand(peek Peeker, info cpu.OpcodeInfo, pc uint16, x, y uint8) (string, string) {
	b1 := peek(pc + 1)
	b2 := uint16(peek(pc+2))<<8 | uint16(b1)
	// value は実効アドレスの現在の値を注記する。
	value := func(ea uint16) string {
		if !info.Accesses {
			return ""
		}
		return fmt.Sprintf("= $%04X = #$%02X", ea, peek(ea))
	}
	name := func(addr uint16, text string) string {
		if l := d.symbols.Label(addr); l != "" {
			return l
		}
		return text
	}

	switch info.Mode {
	case cpu.ModeImplied:
		return "", ""
	case cpu.ModeAccumulator:
		return "A", ""
	case cpu.ModeImmediate:
		return fmt.Sprintf("#$%02X", b1), ""
	case cpu.ModeZeroPage:
		return name(uint16(b1), fmt.Sprintf("$%02X", b1)), value(uint16(b1))
	case cpu.ModeZeroPageX:
		return fmt.Sprintf("$%02X,X", b1), value(uint16(b1 + x))
	case cpu.ModeZeroPageY:
		return fmt.Sprintf("$%02X,Y", b1), value(uint16(b1 + y))
	case cpu.ModeAbsolute:
		return name(b2, fmt.Sprintf("$%04X", b2)), value(b2)
	case cpu.ModeAbsoluteX:
		return fmt.Sprintf("%s,X", name(b2, fmt.Sprintf("$%04X", b2))), value(b2 + uint16(x))
	case cpu.ModeAbsoluteY:
		return fmt.Sprintf("%s,Y", name(b2, fmt.Sprintf("$%04X", b2))), value(b2 + uint16(y))
	case cpu.ModeIndirectX:
		ptr := b1 + x
		ea := uint16(peek(uint16(ptr+1)))<<8 | uint16(peek(uint16(ptr)))
		return fmt.Sprintf("($%02X,X)", b1), value(ea)
	case cpu.ModeIndirectY:
		base := uint16(peek(uint16(b1+1)))<<8 | uint16(peek(uint16(b1)))
		return fmt.Sprintf("($%02X),Y", b1), value(base + uint16(y))
	case cpu.ModeRelative:
		target := uint16(int32(pc+2) + int32(int8(b1)))
		return name(target, fmt.Sprintf("$%04X", target)), ""
	case cpu.ModeIndirect:
		hi := peek(b2&0xFF00 | uint16(uint8(b2)+1))
		target := uint16(hi)<<8 | uint16(peek(b2))
		return fmt.Sprintf("(%s)", name(b2, fmt.Sprintf("$%04X", b2))),
			fmt.Sprintf("= %s", name(target, fmt.Sprintf("$%04X", target)))
	case cpu.ModeJSR:
		return name(b2, fmt.Sprintf("$%04X", b2)), ""
	}
	return "", ""
}

// StartBefore は pc の少し前から始まり、pc の行を含むように並ぶ開始
// アドレスを返す。
//
// 実行の記録を後ろ向きにたどり、前へ lines 行ほど戻った位置を探す。
// 前向きに逆アセンブルしたときに pc にちょうど着く開始点だけを選ぶ。
// 見つからないときは pc を返す。
func (d *Disassembler) StartBefore(peek Peeker, offset Offsetter, pc uint16, lines int) uint16 {
	best := pc
	for back := 1; back <= lines*3 && back <= int(pc); back++ {
		start := pc - uint16(back)
		off, ok := offset(start)
		if !ok || !d.Executed(off) {
			continue
		}
		if landsOn(peek, start, pc, lines) {
			best = start
		}
	}
	return best
}

// landsOn は start から命令長で進めたとき、lines 行以内に pc へ着くかを返す。
func landsOn(peek Peeker, start, pc uint16, lines int) bool {
	addr := start
	for range lines + 1 {
		if addr == pc {
			return true
		}
		if addr > pc {
			return false
		}
		addr += uint16(cpu.Info(peek(addr)).Length)
	}
	return false
}

// FormatLine は表示用の 1 行を組み立てる。
//
//	>  * C5F9  86 10     STX $10          ; = $0010 = #$00
func FormatLine(l Line, current, breakpoint bool) string {
	var b strings.Builder
	if current {
		b.WriteByte('>')
	} else {
		b.WriteByte(' ')
	}
	if breakpoint {
		b.WriteString(" * ")
	} else {
		b.WriteString("   ")
	}
	fmt.Fprintf(&b, "%04X  ", l.Addr)
	var bytes []string
	for _, v := range l.Bytes {
		bytes = append(bytes, fmt.Sprintf("%02X", v))
	}
	fmt.Fprintf(&b, "%-9s ", strings.Join(bytes, " "))
	text := l.Text()
	if l.Alias != "" && !l.Data {
		text += " (" + l.Alias + ")"
	}
	fmt.Fprintf(&b, "%-20s", text)
	if l.Comment != "" {
		b.WriteString(" ; ")
		b.WriteString(l.Comment)
	}
	return b.String()
}
