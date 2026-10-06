package debug

import (
	"sort"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// 関数ごとのサイクル数の集計（設計書 14 編 §14.18.2、§14.19）。
//
// trace.summary（記録したトレースから）とプロファイル（実行しながら）が
// 同じ判定を使う。関数の区切りは JSR と RTS、割り込みと RTI で決める。
// スタックを直接書き換えるプログラムでは区切りが崩れるため、崩れた回数を
// 数えて推定である旨を知らせる。

// funcKey は関数の入口の識別。PRG-ROM の入口は Loc の空間とオフセットで、
// バンクを区別する。
type funcKey = SymbolLoc

// FuncStat は関数 1 つの集計。
type FuncStat struct {
	Key   SymbolLoc
	Addr  uint16
	Calls uint64
	// Incl は呼び出し先を含むサイクル数、Excl は含まないサイクル数。
	Incl, Excl uint64
	// Callers は呼び出し元の入口（重複なし、初出の順）。割り込みから入った
	// ものは Interrupt に種類を入れる。
	Callers   []SymbolLoc
	Interrupt bool
}

// callFrame は呼び出しの 1 段。
type callFrame struct {
	key       SymbolLoc
	addr      uint16
	start     uint64
	child     uint64
	interrupt bool
	kind      cpu.Interrupt
}

// callAccum は関数の集計の状態。
type callAccum struct {
	stack []callFrame
	funcs map[SymbolLoc]*FuncStat
	// mismatches は区切りが崩れた回数（JSR の無い RTS、割り込みの無い RTI）。
	mismatches uint64
	// nmi と irq は割り込みの回数。
	nmi, irq uint64
	// onReturn は関数から戻ったとき呼ばれる（プロファイルの NMI 処理の計測）。
	onReturn func(f callFrame, end uint64)
}

// maxAccumDepth は数える呼び出しの深さの上限。超えたら古い段を捨てる。
const maxAccumDepth = 256

func newCallAccum() *callAccum { return &callAccum{funcs: map[SymbolLoc]*FuncStat{}} }

func (a *callAccum) stat(key SymbolLoc, addr uint16) *FuncStat {
	st, ok := a.funcs[key]
	if !ok {
		st = &FuncStat{Key: key, Addr: addr}
		a.funcs[key] = st
	}
	return st
}

// push は呼び出しを始める。
func (a *callAccum) push(f callFrame) {
	st := a.stat(f.key, f.addr)
	st.Calls++
	if f.interrupt {
		st.Interrupt = true
	}
	if len(a.stack) > 0 {
		caller := a.stack[len(a.stack)-1].key
		found := false
		for _, c := range st.Callers {
			if c == caller {
				found = true
				break
			}
		}
		if !found && len(st.Callers) < 16 {
			st.Callers = append(st.Callers, caller)
		}
	}
	if len(a.stack) == maxAccumDepth {
		a.stack = append(a.stack[:0], a.stack[1:]...)
		a.mismatches++
	}
	a.stack = append(a.stack, f)
}

// pop は呼び出しを終える。interrupt は RTI による戻りを表す。
func (a *callAccum) pop(end uint64, interrupt bool) {
	idx := -1
	for i := len(a.stack) - 1; i >= 0; i-- {
		if a.stack[i].interrupt == interrupt {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mismatches++
		return
	}
	if idx != len(a.stack)-1 {
		// 戻らずに抜けた段がある（RTS を使った間接ジャンプなど）。
		a.mismatches++
	}
	for len(a.stack) > idx {
		f := a.stack[len(a.stack)-1]
		a.stack = a.stack[:len(a.stack)-1]
		a.close(f, end)
	}
}

// close は 1 段を終え、サイクル数を加える。
func (a *callAccum) close(f callFrame, end uint64) {
	incl := uint64(0)
	if end > f.start {
		incl = end - f.start
	}
	st := a.stat(f.key, f.addr)
	st.Incl += incl
	if incl > f.child {
		st.Excl += incl - f.child
	}
	if len(a.stack) > 0 {
		a.stack[len(a.stack)-1].child += incl
	}
	if a.onReturn != nil {
		a.onReturn(f, end)
	}
}

// instruction は命令 1 つを渡す。cycles は命令の開始のサイクル数。target は
// JSR の飛び先の識別。
func (a *callAccum) instruction(op uint8, cycles uint64, target SymbolLoc, targetAddr uint16) {
	switch op {
	case 0x20: // JSR
		a.push(callFrame{key: target, addr: targetAddr, start: cycles})
	case 0x60: // RTS（6 サイクル）
		a.pop(cycles+6, false)
	case 0x40: // RTI（6 サイクル）
		a.pop(cycles+6, true)
	}
}

// interrupt は割り込みシーケンスの終わりを渡す。cycles はシーケンスの終わりの
// サイクル数（シーケンスは 7 サイクル）。
func (a *callAccum) interrupt(k cpu.Interrupt, cycles uint64, handler SymbolLoc, handlerAddr uint16) {
	switch k {
	case cpu.InterruptNMI:
		a.nmi++
	case cpu.InterruptIRQ, cpu.InterruptBRK:
		a.irq++
	case cpu.InterruptReset:
		a.stack = a.stack[:0]
		return
	}
	start := cycles
	if start >= 7 {
		start -= 7
	}
	a.push(callFrame{key: handler, addr: handlerAddr, start: start, interrupt: true, kind: k})
}

// finish は戻っていない段を end で閉じた集計を返す（包含サイクル数の
// 降順）。集計の状態は変えない（プロファイルの途中でも呼べる）。
func (a *callAccum) finish(end uint64) []FuncStat {
	c := &callAccum{funcs: make(map[SymbolLoc]*FuncStat, len(a.funcs)), stack: append([]callFrame(nil), a.stack...)}
	for k, v := range a.funcs {
		cp := *v
		cp.Callers = append([]SymbolLoc(nil), v.Callers...)
		c.funcs[k] = &cp
	}
	for len(c.stack) > 0 {
		f := c.stack[len(c.stack)-1]
		c.stack = c.stack[:len(c.stack)-1]
		c.close(f, end)
	}
	a = c
	out := make([]FuncStat, 0, len(a.funcs))
	for _, st := range a.funcs {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Incl != out[j].Incl {
			return out[i].Incl > out[j].Incl
		}
		return out[i].Addr < out[j].Addr
	})
	return out
}
