package debug

import (
	"errors"
	"fmt"
	"sort"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// トレースの絞り込みと要約（設計書 14 編 §14.18）。
//
// TraceRecord は 32 バイトに保つため、フレーム番号とバスアクセスと割り込みを
// 持たない。フレームの開始のサイクル数の表、バスアクセスのリング（bus: true
// のときだけ）、割り込みのリングを Tracer が別に持ち、サイクル数で対応付ける。

// FrameStart はフレームの最初の命令のサイクル数。
type FrameStart struct {
	Frame, Cycles uint64
}

// BusAccess はバスアクセス 1 回。
type BusAccess struct {
	Cycles uint64
	Addr   uint16
	Value  uint8
	Write  bool
}

// IntEvent は割り込みシーケンスの終わり。Handler は飛び先。
type IntEvent struct {
	Cycles  uint64
	Kind    cpu.Interrupt
	Handler uint16
}

// intRingSize は割り込みのリングの大きさ。
const intRingSize = 16384

// NoteFrame はフレームの最初の命令を記録する。リングの範囲より古い表の
// 要素は捨てる。
func (t *Tracer) NoteFrame(frame, cycles uint64) {
	t.frames = append(t.frames, FrameStart{frame, cycles})
	oldest := t.oldestCycles()
	drop := 0
	for drop+1 < len(t.frames) && t.frames[drop+1].Cycles <= oldest {
		drop++
	}
	if drop > 0 {
		t.frames = append(t.frames[:0], t.frames[drop:]...)
	}
}

// NoteInterrupt は割り込みを記録する。
func (t *Tracer) NoteInterrupt(k cpu.Interrupt, cycles uint64, handler uint16) {
	if t.ints == nil {
		t.ints = make([]IntEvent, intRingSize)
	}
	t.ints[t.intCount%intRingSize] = IntEvent{cycles, k, handler}
	t.intCount++
}

// EnableBus はバスアクセスの記録を size 回分のリングで始める。0 で止める。
func (t *Tracer) EnableBus(size int) {
	if size <= 0 {
		t.bus, t.busCount = nil, 0
		return
	}
	if len(t.bus) != size {
		t.bus = make([]BusAccess, size)
	}
	t.busCount = 0
}

// BusEnabled はバスアクセスを記録しているかを返す。
func (t *Tracer) BusEnabled() bool { return t.bus != nil }

// RecordBus はバスアクセスを記録する。
func (t *Tracer) RecordBus(cycles uint64, addr uint16, v uint8, write bool) {
	t.bus[t.busCount%uint64(len(t.bus))] = BusAccess{cycles, addr, v, write}
	t.busCount++
}

// Size はリングの大きさを返す。
func (t *Tracer) Size() int { return len(t.ring) }

// At は古い順で i 番目の記録を返す。
func (t *Tracer) At(i int) cpu.TraceRecord {
	start := t.next - t.Len()
	if start < 0 {
		start += len(t.ring)
	}
	return t.ring[(start+i)%len(t.ring)]
}

// oldestCycles はリングの最も古い記録のサイクル数を返す。
func (t *Tracer) oldestCycles() uint64 {
	if t.Len() == 0 {
		return 0
	}
	return t.At(0).Cycles
}

// frameOf はサイクル数の属するフレームを返す。表より前なら false。
func (t *Tracer) frameOf(cycles uint64) (uint64, bool) {
	i := sort.Search(len(t.frames), func(k int) bool { return t.frames[k].Cycles > cycles }) - 1
	if i < 0 {
		return 0, false
	}
	return t.frames[i].Frame, true
}

// frameStartCycles はフレームの開始のサイクル数を返す。表に無いときは、
// そのフレーム以降で最初の表の要素（無ければ false）。
func (t *Tracer) frameStartCycles(frame uint64) (uint64, bool) {
	i := sort.Search(len(t.frames), func(k int) bool { return t.frames[k].Frame >= frame })
	if i == len(t.frames) {
		return 0, false
	}
	return t.frames[i].Cycles, true
}

// indexAt は Cycles が c 以上の最初の記録の番号を返す。
func (t *Tracer) indexAt(c uint64) int {
	return sort.Search(t.Len(), func(i int) bool { return t.At(i).Cycles >= c })
}

// interrupts は [lo, hi) のサイクル数の割り込みを古い順に返す。
func (t *Tracer) interrupts(lo, hi uint64) []IntEvent {
	n := min(t.intCount, intRingSize)
	var out []IntEvent
	for i := t.intCount - n; i < t.intCount; i++ {
		ev := t.ints[i%intRingSize]
		if ev.Cycles >= lo && ev.Cycles < hi {
			out = append(out, ev)
		}
	}
	return out
}

// AddrSpan は CPU アドレスの範囲（Hi を含む）。
type AddrSpan struct {
	Lo, Hi uint16
}

func (s AddrSpan) has(a uint16) bool { return a >= s.Lo && a <= s.Hi }

// TraceFilter は trace.query の条件。
type TraceFilter struct {
	// Frames は [開始, 終了] のフレーム番号。nil ならリング全体。
	Frames *[2]uint64
	// PC は命令の位置の範囲。空なら絞り込まない。
	PC []AddrSpan
	// Kinds は jsr・rts・rti・interrupt・branch_taken・read・write。空なら
	// すべての命令。
	Kinds []string
	// Addr は read・write のアクセス先。nil なら絞り込まない。
	Addr *AddrSpan
	// Limit は返す件数の上限。
	Limit int
}

// TraceKinds は trace.query の kinds に書ける名前。
var TraceKinds = []string{"jsr", "rts", "rti", "interrupt", "branch_taken", "read", "write"}

// ErrBusNotTraced は read・write の絞り込みにバスアクセスの記録が要ることを表す。
var ErrBusNotTraced = errors.New("debug: read・write で絞り込むには trace.enable に bus: true を渡す")

// TraceHit は条件に合った 1 件。
type TraceHit struct {
	Rec   cpu.TraceRecord
	Frame uint64
	// Kind は合った種類（kinds を指定しないとき空）。
	Kind string
	// Interrupt は interrupt のとき割り込みの種類（nmi・irq・brk・reset）。
	// Rec は割り込みの処理の最初の命令。
	Interrupt string
	// Access は read・write のときのアクセス。
	Access *BusAccess
}

// TraceResult は trace.query の結果。
type TraceResult struct {
	Hits      []TraceHit
	Truncated bool
	// Covered は実際に調べたフレームの範囲。
	Covered [2]uint64
	// Scanned は調べた命令の数。
	Scanned int
	Notes   []string
}

// InterruptKindName は割り込みの種類の名前を返す。
func InterruptKindName(k cpu.Interrupt) string {
	switch k {
	case cpu.InterruptNMI:
		return "nmi"
	case cpu.InterruptIRQ:
		return "irq"
	case cpu.InterruptBRK:
		return "brk"
	case cpu.InterruptReset:
		return "reset"
	}
	return "unknown"
}

// traceRange はフレームの範囲に当たる記録の番号 [lo, hi) とサイクル数の範囲を
// 求める。
func (d *Debugger) traceRange(frames *[2]uint64, notes *[]string) (lo, hi int, cLo, cHi uint64) {
	t := d.tracer
	lo, hi = 0, t.Len()
	cLo, cHi = 0, ^uint64(0)
	if frames == nil || hi == 0 {
		return
	}
	first, _ := t.frameOf(t.oldestCycles())
	if frames[0] < first || len(t.frames) == 0 {
		*notes = append(*notes, fmt.Sprintf("フレーム %d より前はリングに残っていない（残っている最も古いフレームは %d）", frames[0], first))
	}
	if c, ok := t.frameStartCycles(frames[0]); ok {
		cLo = c
	} else {
		cLo = ^uint64(0)
	}
	if c, ok := t.frameStartCycles(frames[1] + 1); ok {
		cHi = c
	}
	if frames[0] <= first {
		cLo = 0
	}
	return t.indexAt(cLo), t.indexAt(cHi), cLo, cHi
}

// QueryTrace はトレースから条件に合うものを取り出す（§14.18.1）。
func (d *Debugger) QueryTrace(f TraceFilter) (TraceResult, error) {
	t := d.tracer
	var res TraceResult
	kinds := map[string]bool{}
	for _, k := range f.Kinds {
		kinds[k] = true
	}
	if (kinds["read"] || kinds["write"]) && !t.BusEnabled() {
		return res, ErrBusNotTraced
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	lo, hi, cLo, cHi := d.traceRange(f.Frames, &res.Notes)
	res.Scanned = max(hi-lo, 0)
	if hi > lo {
		res.Covered[0], _ = t.frameOf(t.At(lo).Cycles)
		res.Covered[1], _ = t.frameOf(t.At(hi - 1).Cycles)
	}
	pcOK := func(pc uint16) bool {
		if len(f.PC) == 0 {
			return true
		}
		for _, s := range f.PC {
			if s.has(pc) {
				return true
			}
		}
		return false
	}
	var hits []TraceHit
	// 上限を少し超えて集め、超えたかを知る。種類ごとに集めて時刻順に並べる。
	add := func(h TraceHit) bool {
		h.Frame, _ = t.frameOf(h.Rec.Cycles)
		hits = append(hits, h)
		return len(hits) > f.Limit*4+1000
	}
	instrKinds := len(kinds) == 0 || kinds["jsr"] || kinds["rts"] || kinds["rti"] || kinds["branch_taken"]
	if instrKinds {
		for i := lo; i < hi; i++ {
			r := t.At(i)
			if !pcOK(r.PC) {
				continue
			}
			kind := ""
			if len(kinds) > 0 {
				switch op := r.Bytes[0]; {
				case op == 0x20 && kinds["jsr"]:
					kind = "jsr"
				case op == 0x60 && kinds["rts"]:
					kind = "rts"
				case op == 0x40 && kinds["rti"]:
					kind = "rti"
				case isBranch(op) && kinds["branch_taken"] && i+1 < t.Len():
					target := r.PC + 2 + uint16(int8(r.Bytes[1]))
					if t.At(i+1).PC == target && target != r.PC+2 {
						kind = "branch_taken"
					}
				}
				if kind == "" {
					continue
				}
			}
			if add(TraceHit{Rec: r, Kind: kind}) {
				break
			}
		}
	}
	if kinds["interrupt"] {
		for _, ev := range t.interrupts(cLo, cHi) {
			i := t.indexAt(ev.Cycles)
			if i >= t.Len() || i < lo || i >= hi {
				continue
			}
			r := t.At(i)
			if !pcOK(r.PC) {
				continue
			}
			add(TraceHit{Rec: r, Kind: "interrupt", Interrupt: InterruptKindName(ev.Kind)})
		}
	}
	if kinds["read"] || kinds["write"] {
		n := min(t.busCount, uint64(len(t.bus)))
		for k := t.busCount - n; k < t.busCount; k++ {
			a := t.bus[k%uint64(len(t.bus))]
			if a.Cycles < cLo || a.Cycles > cHi || (a.Write && !kinds["write"]) || (!a.Write && !kinds["read"]) {
				continue
			}
			if f.Addr != nil && !f.Addr.has(a.Addr) {
				continue
			}
			// アクセスを起こした命令（アクセスより前に始まった最後の命令）。
			// バスアクセスのサイクル数はそのサイクルを終えた後に数えたもの
			// であり、命令の最後のサイクルのアクセスは次の命令の開始と同じ
			// 値になるため、開始がアクセスより小さい最後の命令とする。
			i := sort.Search(t.Len(), func(x int) bool { return t.At(x).Cycles >= a.Cycles }) - 1
			if i < lo || i >= hi {
				continue
			}
			r := t.At(i)
			if !pcOK(r.PC) {
				continue
			}
			kind := "read"
			if a.Write {
				kind = "write"
			}
			acc := a
			if add(TraceHit{Rec: r, Kind: kind, Access: &acc}) {
				break
			}
		}
		if n > 0 {
			if oldest := t.bus[(t.busCount-n)%uint64(len(t.bus))].Cycles; oldest > cLo && lo < hi && oldest > t.At(lo).Cycles {
				res.Notes = append(res.Notes, "範囲の前半のバスアクセスはリングに残っていない（trace.enable の bus_ring_size を大きくする）")
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		ci, cj := hits[i].Rec.Cycles, hits[j].Rec.Cycles
		if hits[i].Access != nil {
			ci = hits[i].Access.Cycles
		}
		if hits[j].Access != nil {
			cj = hits[j].Access.Cycles
		}
		return ci < cj
	})
	if len(hits) > f.Limit {
		hits, res.Truncated = hits[:f.Limit], true
	}
	res.Hits = hits
	if t.Len() == 0 {
		res.Notes = append(res.Notes, "トレースが空である（trace.enable で記録を始めてから進める）")
	}
	return res, nil
}

// isBranch は条件分岐の命令かを返す。
func isBranch(op uint8) bool { return op&0x1F == 0x10 }

// HotSpot は実行回数の多い位置。
type HotSpot struct {
	PC    uint16
	Count int
	Share float64
}

// TraceSummary は trace.summary の結果。
type TraceSummary struct {
	Frames       [2]uint64
	Instructions int
	Functions    []FuncStat
	NMI, IRQ     uint64
	HotSpots     []HotSpot
	Notes        []string
}

// SummarizeTrace はフレームの範囲の処理の流れを要約する（§14.18.2）。
func (d *Debugger) SummarizeTrace(frames *[2]uint64) TraceSummary {
	t := d.tracer
	var s TraceSummary
	lo, hi, _, _ := d.traceRange(frames, &s.Notes)
	if hi <= lo {
		s.Notes = append(s.Notes, "範囲に命令が無い（trace.enable で記録を始めてから進める）")
		return s
	}
	s.Instructions = hi - lo
	s.Frames[0], _ = t.frameOf(t.At(lo).Cycles)
	s.Frames[1], _ = t.frameOf(t.At(hi - 1).Cycles)
	acc := newCallAccum()
	offset := d.n.Cart.PRGOffset
	ints := t.interrupts(t.At(lo).Cycles, t.At(hi-1).Cycles+1)
	counts := map[uint16]int{}
	for i := lo; i < hi; i++ {
		r := t.At(i)
		for len(ints) > 0 && ints[0].Cycles <= r.Cycles {
			ev := ints[0]
			ints = ints[1:]
			acc.interrupt(ev.Kind, ev.Cycles, LocOf(ev.Handler, offset), ev.Handler)
		}
		counts[r.PC]++
		var target uint16
		if r.Bytes[0] == 0x20 {
			target = uint16(r.Bytes[1]) | uint16(r.Bytes[2])<<8
		}
		acc.instruction(r.Bytes[0], r.Cycles, LocOf(target, offset), target)
	}
	end := t.At(hi - 1).Cycles
	if hi < t.Len() {
		end = t.At(hi).Cycles
	}
	s.Functions = acc.finish(end)
	s.NMI, s.IRQ = acc.nmi, acc.irq
	if acc.mismatches > 0 {
		s.Notes = append(s.Notes, fmt.Sprintf("関数の区切りが %d 回崩れた（RTS による間接ジャンプやスタックの直接の書き換え）。サイクル数は推定である", acc.mismatches))
	}
	if len(d.n.ROM.PRG) > 0x8000 {
		s.Notes = append(s.Notes, "バンク切り替えのある ROM では、関数の名前を今のバンクの構成で引いている")
	}
	pcs := make([]uint16, 0, len(counts))
	for pc := range counts {
		pcs = append(pcs, pc)
	}
	sort.Slice(pcs, func(i, j int) bool {
		if counts[pcs[i]] != counts[pcs[j]] {
			return counts[pcs[i]] > counts[pcs[j]]
		}
		return pcs[i] < pcs[j]
	})
	for _, pc := range pcs[:min(10, len(pcs))] {
		s.HotSpots = append(s.HotSpots, HotSpot{PC: pc, Count: counts[pc], Share: float64(counts[pc]) / float64(s.Instructions)})
	}
	return s
}

// AgentTrace は trace.enable の指定。
type AgentTrace struct {
	Enabled bool
	// RingSize は命令のリングの大きさ、BusRingSize はバスアクセスのリングの
	// 大きさ（0 ならバスアクセスを記録しない）。
	RingSize, BusRingSize int
}

// SetAgentTrace は Agent Interface のトレースを切り替える。GUI のトレースの
// 表示（Features.Tracing）とは別に持つ。リングの大きさを変えると記録を消す。
func (d *Debugger) SetAgentTrace(a AgentTrace) {
	if a.Enabled && a.RingSize > 0 && a.RingSize != d.tracer.Size() {
		d.tracer = NewTracer(a.RingSize)
	}
	if a.Enabled {
		d.tracer.EnableBus(a.BusRingSize)
	} else {
		d.tracer.EnableBus(0)
	}
	d.agentTrace = a
	d.traceFrame = ^uint64(0)
	d.updateHooks()
}

// AgentTraceState は Agent Interface のトレースの指定を返す。
func (d *Debugger) AgentTraceState() AgentTrace { return d.agentTrace }

// tracing は命令を記録するかを返す。
func (d *Debugger) tracing() bool { return d.features.Tracing || d.agentTrace.Enabled }
