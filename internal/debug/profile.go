package debug

import (
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// プロファイル（設計書 14 編 §14.19）。
//
// profile.start から profile.stop までのフレームについて、CPU の時間の使い方を
// 測る。命令の開始（OnBeforeExec）と割り込み（OnInterrupt）と、VBlank 中の
// PPU 操作を見るためのバスアクセスのフックで測る。セーブステートには含めない。

// アイドルループの判定の方法。
const (
	IdleByArgument = "argument"
	IdleBySymbol   = "symbol"
	IdleByAuto     = "auto"
)

// idleSymbolWords はアイドルループの名前に含まれる語（判定 2）。
var idleSymbolWords = []string{"idle", "wait_vblank", "wait_nmi"}

const (
	// busyThreshold は忙しいフレームの閾値。
	busyThreshold = 0.9
	// maxProfileList は一覧に入れるフレームの上限。
	maxProfileList = 200
	// autoIdleRepeats は後方分岐をアイドルループとみなす繰り返しの回数。
	autoIdleRepeats = 16
	// autoIdleSpan は後方分岐のループとみなす範囲のバイト数の上限。
	autoIdleSpan = 16
)

// ProfileOptions は profile.start の指定。
type ProfileOptions struct {
	// Idle はアイドルループの範囲（判定 1）。空なら判定 2・3 を使う。
	Idle []LocSpan
}

// minAvgMax は最小・平均・最大。
type minAvgMax struct {
	n             uint64
	min, max, sum float64
}

func (m *minAvgMax) add(v float64) {
	if m.n == 0 || v < m.min {
		m.min = v
	}
	if m.n == 0 || v > m.max {
		m.max = v
	}
	m.sum += v
	m.n++
}

// Stat は最小・平均・最大。
type Stat struct {
	Count         uint64
	Min, Avg, Max float64
}

func (m minAvgMax) stat() Stat {
	if m.n == 0 {
		return Stat{}
	}
	return Stat{Count: m.n, Min: m.min, Avg: m.sum / float64(m.n), Max: m.max}
}

// FrameBusy は忙しいフレーム 1 つ。
type FrameBusy struct {
	Frame  uint64
	Cycles uint64
	Ratio  float64
}

// VBlankAccess は VBlank の中の PPU 操作。
type VBlankAccess struct {
	Frame    uint64
	PC       uint16
	Reg      uint16
	Scanline int
	Dot      int
	// Margin は VBlank の終わりまでの余裕の CPU サイクル数。
	Margin int
}

// ProfileReport は profile.report の結果。
type ProfileReport struct {
	Active bool
	// FromFrame と ToFrame は測った（完了した）フレームの範囲。Frames はその数。
	FromFrame, ToFrame uint64
	Frames             uint64
	// BusyCycles と BusyRatio はフレームごとの忙しさ。
	BusyCycles, BusyRatio Stat
	Over90                []FrameBusy
	// NMICycles は NMI の開始から RTI までのサイクル数。NMIOverruns は VBlank の
	// 終わりを越えた回数、OverrunFrames はそのフレーム。
	NMICycles     Stat
	NMIOverruns   uint64
	OverrunFrames []uint64
	// VBlankMargin は VBlank の中で最後の PPU 操作から VBlank の終わりまでの
	// 余裕。Worst は余裕が最も少なかったもの。
	VBlankMargin Stat
	Worst        *VBlankAccess
	Functions    []FuncStat
	// LagFrames はアイドルループに一度も入らなかったフレーム。
	LagCount  uint64
	LagFrames []uint64
	// IdleMethod は判定の方法、IdleSpans は使った範囲。
	IdleMethod string
	IdleSpans  []LocSpan
	Notes      []string
}

// profiler はプロファイルの状態。エミュレーションゴルーチンだけが触る。
type profiler struct {
	active bool
	opts   ProfileOptions
	method string
	spans  []LocSpan
	acc    *callAccum

	// 今のフレーム。started は最初の（途中から始まった）フレームを越えたか。
	frame      uint64
	frameStart uint64
	started    bool
	busy       uint64
	idleSeen   bool
	haveLast   bool
	lastCycles uint64
	lastIdle   bool
	firstFrame uint64
	lastFrame  uint64
	frames     uint64

	busyCycles, busyRatio minAvgMax
	over90                []FrameBusy
	lagCount              uint64
	lagFrames             []uint64

	nmiCycles     minAvgMax
	nmiOverruns   uint64
	overrunFrames []uint64

	// vblank は今のフレームの VBlank の中の最後の PPU 操作。
	vblank       *VBlankAccess
	vblankMargin minAvgMax
	worst        *VBlankAccess

	// 判定 3 の後方分岐の追跡。
	prevOp     uint8
	prevPC     uint16
	loopPC     uint16
	loopTarget uint16
	loopCount  int
}

// StartProfile はプロファイルを始める。前の結果は捨てる。
func (d *Debugger) StartProfile(o ProfileOptions) {
	p := &profiler{active: true, opts: o, acc: newCallAccum()}
	switch {
	case len(o.Idle) > 0:
		p.method, p.spans = IdleByArgument, o.Idle
	default:
		p.spans = d.symbols.LabelSpans(func(name string) bool {
			low := strings.ToLower(name)
			for _, w := range idleSymbolWords {
				if strings.Contains(low, w) {
					return true
				}
			}
			return false
		})
		p.method = IdleBySymbol
		if len(p.spans) == 0 {
			p.method = IdleByAuto
		}
	}
	p.acc.onReturn = d.profileReturn
	d.prof = p
	d.updateHooks()
}

// StopProfile はプロファイルを止める。結果は ProfileReport で読める。
func (d *Debugger) StopProfile() {
	if d.prof != nil {
		d.prof.active = false
	}
	d.updateHooks()
}

// ActiveProfile は測っているプロファイルの指定を返す（Fork で複製する）。
func (d *Debugger) ActiveProfile() (ProfileOptions, bool) {
	if !d.profiling() {
		return ProfileOptions{}, false
	}
	return d.prof.opts, true
}

// profiling はプロファイルを測っているかを返す。
func (d *Debugger) profiling() bool { return d.prof != nil && d.prof.active }

// idleAt は位置がアイドルループの範囲かを返す。
func (p *profiler) idleAt(loc SymbolLoc) bool {
	for _, s := range p.spans {
		if s.Contains(loc) {
			return true
		}
	}
	return false
}

// profileBeforeExec は命令の開始で呼ばれる。
func (d *Debugger) profileBeforeExec(pc uint16) {
	p := d.prof
	n := d.n
	cyc := n.Cycles()
	if p.haveLast && cyc > p.lastCycles && !p.lastIdle {
		p.busy += cyc - p.lastCycles
	}
	if f := n.Frames(); !p.haveLast || f != p.frame {
		if p.haveLast {
			d.finishProfileFrame(cyc)
		}
		p.frame, p.frameStart, p.busy, p.idleSeen, p.vblank = f, cyc, 0, false, nil
	}
	op := n.Bus.Peek(pc)
	offset := n.Cart.PRGOffset
	loc := LocOf(pc, offset)
	idle := p.idleAt(loc)
	if !idle && p.method == IdleByAuto {
		idle = d.autoIdle(pc, op, offset)
	}
	if idle {
		p.idleSeen = true
	}
	var target uint16
	if op == 0x20 {
		target = uint16(n.Bus.Peek(pc+1)) | uint16(n.Bus.Peek(pc+2))<<8
	}
	p.acc.instruction(op, cyc, LocOf(target, offset), target)
	p.haveLast, p.lastCycles, p.lastIdle = true, cyc, idle
	p.prevOp, p.prevPC = op, pc
}

// autoIdle はアイドルループの判定 3。自身へのジャンプ（JMP *）と、同じ数バイトの
// 範囲を 16 回以上繰り返す後方分岐を見つけたら、範囲に加える。
func (d *Debugger) autoIdle(pc uint16, op uint8, offset Offsetter) bool {
	p := d.prof
	n := d.n
	if op == 0x4C && uint16(n.Bus.Peek(pc+1))|uint16(n.Bus.Peek(pc+2))<<8 == pc {
		d.addIdleSpan(pc, pc+2, offset)
		return true
	}
	if isBranch(p.prevOp) {
		target := p.prevPC + 2 + uint16(int8(n.Bus.Peek(p.prevPC+1)))
		if pc == target && target < p.prevPC && p.prevPC-target <= autoIdleSpan {
			if p.loopPC == p.prevPC && p.loopTarget == target {
				p.loopCount++
			} else {
				p.loopPC, p.loopTarget, p.loopCount = p.prevPC, target, 1
			}
			if p.loopCount >= autoIdleRepeats {
				d.addIdleSpan(target, p.prevPC+1, offset)
				return true
			}
		}
	}
	return false
}

// addIdleSpan は CPU アドレスの範囲をアイドルループの範囲に加える。
func (d *Debugger) addIdleSpan(lo, hi uint16, offset Offsetter) {
	p := d.prof
	a, b := LocOf(lo, offset), LocOf(hi, offset)
	if a.Space != b.Space {
		return
	}
	span := LocSpan{Space: a.Space, Lo: a.Offset, Hi: b.Offset, CPULo: lo, CPUHi: hi}
	for _, s := range p.spans {
		if s.Space == span.Space && s.Lo == span.Lo && s.Hi == span.Hi {
			return
		}
	}
	p.spans = append(p.spans, span)
}

// finishProfileFrame は今のフレームを閉じる。最初のフレームは途中から測った
// ため数えない。
func (d *Debugger) finishProfileFrame(cyc uint64) {
	p := d.prof
	if !p.started {
		p.started = true
		return
	}
	total := cyc - p.frameStart
	if total == 0 {
		return
	}
	ratio := float64(p.busy) / float64(total)
	p.busyCycles.add(float64(p.busy))
	p.busyRatio.add(ratio)
	if p.frames == 0 {
		p.firstFrame = p.frame
	}
	p.frames++
	p.lastFrame = p.frame
	if ratio > busyThreshold && len(p.over90) < maxProfileList {
		p.over90 = append(p.over90, FrameBusy{Frame: p.frame, Cycles: p.busy, Ratio: ratio})
	}
	if !p.idleSeen {
		p.lagCount++
		if len(p.lagFrames) < maxProfileList {
			p.lagFrames = append(p.lagFrames, p.frame)
		}
	}
	if v := p.vblank; v != nil {
		p.vblankMargin.add(float64(v.Margin))
		if p.worst == nil || v.Margin < p.worst.Margin {
			w := *v
			p.worst = &w
		}
	}
}

// profileInterrupt は割り込みで呼ばれる。
func (d *Debugger) profileInterrupt(k cpu.Interrupt) {
	n := d.n
	pc := n.CPU.PC
	d.prof.acc.interrupt(k, n.Cycles(), LocOf(pc, n.Cart.PRGOffset), pc)
}

// profileReturn は呼び出しから戻ったとき呼ばれる。NMI の処理を測る。
func (d *Debugger) profileReturn(f callFrame, end uint64) {
	if !f.interrupt || f.kind != cpu.InterruptNMI {
		return
	}
	p := d.prof
	p.nmiCycles.add(float64(end - f.start))
	r := d.n.Region
	line := d.n.PPU.Scanline()
	if line == r.PreRenderScanline() || line < r.VBlankStartScanline() {
		p.nmiOverruns++
		if len(p.overrunFrames) < maxProfileList {
			p.overrunFrames = append(p.overrunFrames, d.n.Frames())
		}
	}
}

// profileAccess は VBlank の中の $2006・$2007・$4014 へのアクセスを記録する。
func (d *Debugger) profileAccess(addr uint16, write bool) {
	reg := addr
	switch {
	case addr >= 0x2000 && addr < 0x4000 && (addr&7 == 6 || addr&7 == 7):
		reg = 0x2000 | addr&7
	case addr == 0x4014 && write:
	default:
		return
	}
	n := d.n
	r := n.Region
	line, dot := n.PPU.Scanline(), n.PPU.Dot()
	if line < r.VBlankStartScanline() || line >= r.PreRenderScanline() {
		return
	}
	dots := (r.PreRenderScanline()-line)*341 - dot
	margin := dots * r.PPUDotsDen / r.PPUDotsNum
	d.prof.vblank = &VBlankAccess{Frame: n.Frames(), PC: n.CPU.OpPC(), Reg: reg, Scanline: line, Dot: dot, Margin: margin}
}

// ProfileReport はプロファイルの結果を返す。測っている途中でも読める。
func (d *Debugger) ProfileReport() (ProfileReport, bool) {
	p := d.prof
	if p == nil {
		return ProfileReport{}, false
	}
	rep := ProfileReport{
		Active: p.active, FromFrame: p.firstFrame, ToFrame: p.lastFrame, Frames: p.frames,
		BusyCycles: p.busyCycles.stat(), BusyRatio: p.busyRatio.stat(),
		Over90:    append([]FrameBusy(nil), p.over90...),
		NMICycles: p.nmiCycles.stat(), NMIOverruns: p.nmiOverruns,
		OverrunFrames: append([]uint64(nil), p.overrunFrames...),
		VBlankMargin:  p.vblankMargin.stat(),
		LagCount:      p.lagCount, LagFrames: append([]uint64(nil), p.lagFrames...),
		IdleMethod: p.method, IdleSpans: append([]LocSpan(nil), p.spans...),
	}
	if p.worst != nil {
		w := *p.worst
		rep.Worst = &w
	}
	end := p.lastCycles
	if d.n != nil {
		end = d.n.Cycles()
	}
	rep.Functions = p.acc.finish(end)
	if p.acc.mismatches > 0 {
		rep.Notes = append(rep.Notes, "関数の区切りが崩れた（RTS による間接ジャンプやスタックの直接の書き換え）。関数ごとのサイクル数は推定である")
	}
	if p.method == IdleByAuto && len(p.spans) == 0 {
		rep.Notes = append(rep.Notes, "アイドルループを見つけられなかった。すべてのフレームを処理落ちとして数えている。profile.start の idle で範囲を指定する")
	}
	if p.frames == 0 {
		rep.Notes = append(rep.Notes, "完了したフレームが無い（profile.start の後に 2 フレーム以上進める）")
	}
	return rep, true
}
