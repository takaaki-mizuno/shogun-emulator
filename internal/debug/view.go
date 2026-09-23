package debug

import "strings"

// CPUView は CPU デバッガに表示する内容の写し。
//
// エミュレーションゴルーチンで組み立て、UI スレッドへ渡す。
type CPUView struct {
	PC          uint16
	A, X, Y, S  uint8
	P           uint8
	Cycles      uint64
	Frame       uint64
	Scanline    int
	Dot         int
	NMIPending  bool
	IRQSources  []string
	Stack       []uint8
	Calls       []CallFrame
	Lines       []Line
	CurrentLine int
	// Breakpoints は逆アセンブルの行に印を付けるための実行ブレーク
	// ポイントのアドレス。
	ExecBreaks map[uint16]bool
}

// PFlags は P を NV-BDIZC の形で返す。立っていないビットは小文字。
func (v CPUView) PFlags() string {
	const names = "NV-BDIZC"
	var b strings.Builder
	for i := range 8 {
		bit := uint8(0x80) >> i
		c := names[i]
		if v.P&bit == 0 && c != '-' {
			c = c - 'A' + 'a'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// CPUView は CPU デバッガの表示内容を組み立てる。
//
// lines は逆アセンブルの行数。現在の PC が上から 1/3 あたりに来るよう
// 開始位置を選ぶ。
func (d *Debugger) CPUView(lines int) CPUView {
	n := d.n
	if n == nil {
		return CPUView{CurrentLine: -1}
	}
	c := n.CPU
	v := CPUView{
		PC:          c.PC,
		A:           c.A,
		X:           c.X,
		Y:           c.Y,
		S:           c.S,
		P:           c.P(),
		Cycles:      n.Cycles(),
		Frame:       n.Frames(),
		Scanline:    n.PPU.Scanline(),
		Dot:         n.PPU.Dot(),
		NMIPending:  c.NMIPending(),
		Calls:       d.calls.Frames(),
		CurrentLine: -1,
		ExecBreaks:  map[uint16]bool{},
	}
	if n.APU.FrameIRQ() {
		v.IRQSources = append(v.IRQSources, "APU フレーム")
	}
	if n.APU.DMCIRQ() {
		v.IRQSources = append(v.IRQSources, "DMC")
	}
	if n.Cart.IRQAsserted() {
		v.IRQSources = append(v.IRQSources, "マッパー")
	}
	for a := int(c.S) + 1; a <= 0xFF; a++ {
		v.Stack = append(v.Stack, n.Bus.Peek(0x0100|uint16(a)))
	}
	for _, b := range d.bps.List() {
		if b.Kind == BreakExec && b.Enabled {
			for a := int(b.AddrStart); a <= int(b.AddrEnd); a++ {
				v.ExecBreaks[uint16(a)] = true
			}
		}
	}

	peek := Peeker(n.Bus.Peek)
	offset := Offsetter(n.Cart.PRGOffset)
	start := d.disasm.StartBefore(peek, offset, c.PC, lines/3)
	v.Lines = d.disasm.Listing(peek, offset, start, lines, c.X, c.Y)
	for i, l := range v.Lines {
		if l.Addr == c.PC {
			v.CurrentLine = i
			break
		}
	}
	return v
}

// ListingAt は anchor から lines 行を逆アセンブルする。表示のスクロールに使う。
func (d *Debugger) ListingAt(anchor uint16, lines int) []Line {
	n := d.n
	if n == nil {
		return nil
	}
	return d.disasm.Listing(n.Bus.Peek, n.Cart.PRGOffset, anchor, lines, n.CPU.X, n.CPU.Y)
}

// SetRegister はレジスタの値を書き換える。一時停止中に使う。
func (d *Debugger) SetRegister(r Register, v uint16) {
	c := d.n.CPU
	switch r {
	case RegA:
		c.A = uint8(v)
	case RegX:
		c.X = uint8(v)
	case RegY:
		c.Y = uint8(v)
	case RegS:
		c.S = uint8(v)
	case RegPC:
		c.PC = v
	case RegP:
		c.SetP(uint8(v))
	}
}
