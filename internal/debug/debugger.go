// Package debug はデバッガの機能を提供する。
package debug

import (
	"fmt"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// BreakInfo は止まった理由。
type BreakInfo struct {
	// Breakpoint は止めたブレークポイントの写し。
	Breakpoint Breakpoint
	// Reason は表示用の説明。
	Reason string
	// PC と Cycles は止まったときの位置。
	PC     uint16
	Cycles uint64
	// Scanline と Dot は止まったときの PPU の位置。
	Scanline int
	Dot      int
}

// Features はデバッガの機能のうち、利用者が有効にしているもの。
//
// 有効な機能とブレークポイントの有無から、設定するフックを決める。
// 使わない機能のフックを設定しないことで、デバッガを閉じたときの費用を
// なくす（設計書 09 編 §9.6）。
type Features struct {
	// Tracing はトレースを記録する。
	Tracing bool
	// CPUView は CPU デバッガの表示（逆アセンブルの記録とコールスタック）。
	CPUView bool
	// ChangeTracking はメモリの変更追跡。
	ChangeTracking bool
}

// Debugger はエミュレーションコアへフックを設定し、状態を取り出す。
//
// Bus.Peek と Bus.Poke だけを使う。Bus.Read と Bus.Write を呼ばない。
// 表示のためにレジスタの副作用を起こさないためである（設計書 09 編 §9.1）。
//
// 公開していないフィールドとフックはエミュレーションゴルーチンだけが触る。
// UI スレッドとの受け渡しは SnapshotSet・ChangeTracker・Logger・published
// が担う。
type Debugger struct {
	n *nes.NES

	bps     *BreakpointSet
	tracer  *Tracer
	disasm  *Disassembler
	changes *ChangeTracker
	calls   CallStack
	log     *Logger
	symbols *Symbols

	features Features
	// subs はスナップショットの購読。取得位置ごとに 1 つ（設計書 09 編 §9.3.1）。
	subs []snapshotSub

	// hit は止まる理由。nil のとき止まらない。
	hit *BreakInfo
	// skipExec は次の 1 回だけ実行ブレークポイントを見送るアドレス。
	// ブレークポイントで止まった位置からステップ実行するときに使う。
	skipExec    uint16
	hasSkipExec bool

	// ramWritten は内蔵 RAM のうち書き込みのあったアドレス。
	ramWritten [2048]bool
	// lastScanline は直前の CPU サイクルでの PPU のスキャンライン。
	lastScanline int
	// lastDot は直前の CPU サイクルでの PPU のドット。
	lastDot int
	// mapperIRQ は直前の CPU サイクルでのマッパー IRQ 線の状態。
	mapperIRQ bool
	// mmc3Rises は前回の $C001 への書き込みの時点の A12 の立ち上がり数。
	mmc3Rises   uint64
	mmc3Written bool
	// dmcCount は前のフレームまでの DMC DMA の回数。
	dmcCount uint64

	// CycleHook はサイクル単位ステップの待ち。internal/emu が設定する。
	CycleHook func()
	// OnCycleBreak は命令の途中で止まる理由が生じたときに呼ばれる。
	// PPU 位置ブレークポイントがこれを使う。internal/emu が設定する。
	OnCycleBreak func()

	// published は UI スレッドへ見せる一覧。
	publishedMu sync.Mutex
	published   []Breakpoint
	lastBreak   *BreakInfo
}

// New はデバッガを作る。本体は Attach で渡す。
func New(logger *Logger, traceRingSize int, decayFrames uint32) *Debugger {
	if logger == nil {
		logger = NewLogger(DefaultCategories, nil)
	}
	return &Debugger{
		bps:     NewBreakpointSet(),
		tracer:  NewTracer(traceRingSize),
		changes: NewChangeTracker(decayFrames),
		log:     logger,
		symbols: NewSymbols(),
		disasm:  NewDisassembler(0, nil),
	}
}

// Attach は本体を差し替える。ROM を読み込んだときに呼ぶ。
//
// symbols は ROM ごとに保存した名前とブレークポイント。nil のとき空。
func (d *Debugger) Attach(n *nes.NES, symbols *Symbols) {
	if d.n != nil {
		d.n.SetHooks(nes.Hooks{})
		d.detachWarn()
	}
	d.n = n
	if symbols == nil {
		symbols = NewSymbols()
	}
	d.symbols = symbols
	prgSize := 0
	if n != nil {
		prgSize = len(n.ROM.PRG)
	}
	d.disasm = NewDisassembler(prgSize, symbols)
	d.calls.Reset()
	d.changes.Reset()
	d.log.ResetRepeats()
	d.tracer.Clear()
	d.ramWritten = [2048]bool{}
	d.hit = nil
	d.mmc3Written = false
	d.bps = NewBreakpointSet()
	for _, b := range symbols.Breakpoints() {
		d.bps.Add(b)
	}
	d.publish()
	d.updateHooks()
}

// Machine は接続している本体を返す。
func (d *Debugger) Machine() *nes.NES { return d.n }

// Logger はログを返す。
func (d *Debugger) Logger() *Logger { return d.log }

// Symbols は名前とブレークポイントの保存先を返す。
func (d *Debugger) Symbols() *Symbols { return d.symbols }

// Changes は変更追跡を返す。UI スレッドから Heat を呼べる。
func (d *Debugger) Changes() *ChangeTracker { return d.changes }

// Tracer はトレースを返す。
func (d *Debugger) Tracer() *Tracer { return d.tracer }

// Disassembler は逆アセンブラを返す。
func (d *Debugger) Disassembler() *Disassembler { return d.disasm }

// CallStack はコールスタックの写しを返す。
func (d *Debugger) CallStack() []CallFrame { return d.calls.Frames() }

// SetFeatures は有効な機能を切り替え、フックを設定し直す。
func (d *Debugger) SetFeatures(f Features) {
	if f.ChangeTracking && !d.features.ChangeTracking {
		// 有効にした時点の内容を基準にする。前の記録が残っていると、
		// 閉じていた間の変化がまとめて光る。
		d.changes.Reset()
	}
	d.features = f
	d.updateHooks()
}

// Features は有効な機能を返す。
func (d *Debugger) Features() Features { return d.features }

// snapshotSub はスナップショットの 1 つの取得位置の購読。
type snapshotSub struct {
	line int
	set  *SnapshotSet
	refs int
}

// AcquireSnapshots は line の開始時（-1 でフレーム末）に取るスナップショット
// を購読する。同じ位置の購読は 1 つの SnapshotSet を共有する。
//
// 返した SnapshotSet の Latest は UI スレッドから呼べる。
func (d *Debugger) AcquireSnapshots(line int) *SnapshotSet {
	if line < -1 || line >= visibleScanlines {
		line = -1
	}
	for i := range d.subs {
		if d.subs[i].line == line {
			d.subs[i].refs++
			return d.subs[i].set
		}
	}
	set := NewSnapshotSet()
	set.line = line
	d.subs = append(d.subs, snapshotSub{line: line, set: set, refs: 1})
	d.updateHooks()
	return set
}

// ReleaseSnapshots は購読をやめる。最後の購読をやめた位置では取らない。
func (d *Debugger) ReleaseSnapshots(set *SnapshotSet) {
	for i := range d.subs {
		if d.subs[i].set != set {
			continue
		}
		d.subs[i].refs--
		if d.subs[i].refs <= 0 {
			d.subs = append(d.subs[:i], d.subs[i+1:]...)
			d.updateHooks()
		}
		return
	}
}

// snapshotWanted は取得位置 -1（フレーム末）または任意のスキャンラインの
// 購読があるかを返す。
func (d *Debugger) snapshotWanted(frameEnd bool) bool {
	for _, sub := range d.subs {
		if (sub.line < 0) == frameEnd {
			return true
		}
	}
	return false
}

// captureAt は取得位置 line の購読へスナップショットを取る。
func (d *Debugger) captureAt(line int) {
	for _, sub := range d.subs {
		if sub.line == line {
			sub.set.Capture(d.n, line)
		}
	}
}

// SetLogCategories はログのカテゴリを変え、フックを設定し直す。
func (d *Debugger) SetLogCategories(c Category) {
	d.log.Categories = c
	d.updateHooks()
}

// AddBreakpoint はブレークポイントを加える。
func (d *Debugger) AddBreakpoint(b Breakpoint) int {
	if b.Kind == BreakEvent && b.Event == EventUninitializedRAMRead && b.Enabled {
		// 記録はこの時点から始まる。
		d.ramWritten = [2048]bool{}
	}
	id := d.bps.Add(b)
	d.afterBreakpointChange()
	return id
}

// RemoveBreakpoint はブレークポイントを取り除く。
func (d *Debugger) RemoveBreakpoint(id int) {
	d.bps.Remove(id)
	d.afterBreakpointChange()
}

// SetBreakpointEnabled は有効・無効を切り替える。
func (d *Debugger) SetBreakpointEnabled(id int, on bool) {
	d.bps.SetEnabled(id, on)
	d.afterBreakpointChange()
}

// SetBreakpointCondition は条件式を差し替える。空文字列で外す。
func (d *Debugger) SetBreakpointCondition(id int, expr string) error {
	if expr == "" {
		d.bps.SetCondition(id, nil)
		d.afterBreakpointChange()
		return nil
	}
	c, err := ParseCondition(expr)
	if err != nil {
		return err
	}
	d.bps.SetCondition(id, c)
	d.afterBreakpointChange()
	return nil
}

// afterBreakpointChange は一覧を公開し、保存し、フックを設定し直す。
func (d *Debugger) afterBreakpointChange() {
	d.symbols.SetBreakpoints(d.bps.List())
	d.publish()
	d.updateHooks()
}

// publish は UI スレッドへ一覧の写しを渡す。
func (d *Debugger) publish() {
	list := d.bps.List()
	d.publishedMu.Lock()
	d.published = list
	d.publishedMu.Unlock()
}

// Breakpoints は一覧の写しを返す。UI スレッドから呼べる。
func (d *Debugger) Breakpoints() []Breakpoint {
	d.publishedMu.Lock()
	defer d.publishedMu.Unlock()
	return append([]Breakpoint(nil), d.published...)
}

// LastBreak は直近に止まった理由を返す。UI スレッドから呼べる。
func (d *Debugger) LastBreak() (BreakInfo, bool) {
	d.publishedMu.Lock()
	defer d.publishedMu.Unlock()
	if d.lastBreak == nil {
		return BreakInfo{}, false
	}
	return *d.lastBreak, true
}

// TakeHit は止まる理由を取り出して消す。無ければ false。
//
// エミュレーションゴルーチンが命令ごとに確かめる。
func (d *Debugger) TakeHit() (BreakInfo, bool) {
	if d.hit == nil {
		return BreakInfo{}, false
	}
	h := *d.hit
	d.hit = nil
	d.publishedMu.Lock()
	d.lastBreak = &h
	d.publishedMu.Unlock()
	d.publish()
	if d.log.Categories&CatError != 0 {
		// 止まった理由は利用者の操作の結果であり、エラーではない。
		// ここでは記録しない。
	}
	return h, true
}

// ClearHit は止まる理由を捨てる。
func (d *Debugger) ClearHit() { d.hit = nil }

// HasHit は止まる理由が残っているかを返す。
func (d *Debugger) HasHit() bool { return d.hit != nil }

// raise は止まる理由を記録する。すでにあるときは先のものを残す。
func (d *Debugger) raise(b *Breakpoint, reason string) {
	if d.hit != nil {
		return
	}
	info := BreakInfo{
		Reason:   reason,
		PC:       d.n.CPU.PC,
		Cycles:   d.n.Cycles(),
		Scanline: d.n.PPU.Scanline(),
		Dot:      d.n.PPU.Dot(),
	}
	if b != nil {
		info.Breakpoint = *b
	}
	d.hit = &info
}

// updateHooks は必要なフックだけを設定する。
//
// 使わないフックを nil にする。OnCPURead と OnCPUWrite は毎秒 180 万回
// 呼ばれ、nil であれば比較 1 回で済む。
func (d *Debugger) updateHooks() {
	if d.n == nil {
		return
	}
	var h nes.Hooks
	cats := d.log.Categories
	f := d.features

	if d.bps.Count(BreakExec) > 0 || f.Tracing || f.CPUView {
		h.OnBeforeExec = d.onBeforeExec
	}
	busLog := cats&(CatTraceCPUBus|CatPPURegister|CatAPURegister|CatInput|CatMapper|CatDMA) != 0
	if d.bps.Count(BreakRead) > 0 || d.bps.EventCount(EventUninitializedRAMRead) > 0 || busLog {
		h.OnCPURead = d.onCPURead
	}
	if d.bps.Count(BreakWrite) > 0 || d.bps.EventCount(EventUninitializedRAMRead) > 0 ||
		d.bps.EventCount(EventMMC3IRQReloadWithoutClocks) > 0 || busLog {
		h.OnCPUWrite = d.onCPUWrite
	}
	if d.bps.Count(BreakPPUPosition) > 0 || d.bps.EventCount(EventMapperIRQ) > 0 ||
		d.snapshotWanted(false) || cats&CatPPUTiming != 0 || d.CycleHook != nil {
		h.OnCycle = d.onCycle
		d.lastScanline = d.n.PPU.Scanline()
		d.lastDot = d.n.PPU.Dot()
		d.mapperIRQ = d.n.Cart.IRQAsserted()
	}
	if d.bps.EventCount(EventNMI) > 0 || d.bps.EventCount(EventIRQ) > 0 ||
		d.bps.EventCount(EventReset) > 0 || f.CPUView || cats&CatPPUTiming != 0 {
		h.OnInterrupt = d.onInterrupt
	}
	if d.bps.EventCount(EventSprite0Hit) > 0 {
		h.OnSprite0Hit = d.onSprite0Hit
	}
	if d.snapshotWanted(true) || f.ChangeTracking || cats&CatDMA != 0 {
		h.OnFrameComplete = d.onFrameComplete
	}
	d.n.SetHooks(h)
	d.attachWarn()
}

// RefreshHooks は CycleHook を差し替えた後にフックを設定し直す。
func (d *Debugger) RefreshHooks() { d.updateHooks() }

// attachWarn はエミュレーションコアの互換性の記録をログへ繋ぐ。
func (d *Debugger) attachWarn() {
	warn := d.log.Warnf
	if d.log.Categories&CatWarnCompat == 0 {
		warn = nil
	}
	d.n.CPU.Warn = warn
	d.n.PPU.Warn = warn
	d.n.Bus.Warn = warn
	d.n.Warn = warn
	if d.log.Categories&CatAPUFrame != 0 {
		d.n.APU.OnFrameStep = d.onFrameStep
	} else {
		d.n.APU.OnFrameStep = nil
	}
}

// detachWarn は記録の接続を外す。
func (d *Debugger) detachWarn() {
	d.n.CPU.Warn = nil
	d.n.PPU.Warn = nil
	d.n.Bus.Warn = nil
	d.n.Warn = nil
	d.n.APU.OnFrameStep = nil
}

// onBeforeExec は命令を実行する前に呼ばれる。true を返すと止める。
func (d *Debugger) onBeforeExec(pc uint16) bool {
	n := d.n
	if d.features.Tracing {
		d.tracer.Record(n.CPU.TraceRecord(n.PPU.Scanline(), n.PPU.Dot()))
	}
	if d.features.CPUView {
		if off, ok := n.Cart.PRGOffset(pc); ok {
			d.disasm.Record(off)
		}
		d.calls.OnExec(pc, n.Bus.Peek, n.CPU.S)
	}
	if d.hasSkipExec {
		d.hasSkipExec = false
		if d.skipExec == pc {
			return false
		}
	}
	if b := d.bps.MatchAddr(BreakExec, pc, d); b != nil {
		d.raise(b, fmt.Sprintf("実行ブレークポイント $%04X", pc))
		return true
	}
	return false
}

// SkipExecAt は次に実行する命令が pc のとき、実行ブレークポイントを
// 1 回だけ見送る。
//
// ブレークポイントで止まった位置から進めるとき、同じブレークポイントで
// 進めずに止まり直すことを避ける。
func (d *Debugger) SkipExecAt(pc uint16) {
	d.skipExec = pc
	d.hasSkipExec = true
}

// onCPURead はバスの読み出しで呼ばれる。
func (d *Debugger) onCPURead(addr uint16, v uint8) {
	d.logBus(addr, v, false)
	if d.bps.EventCount(EventUninitializedRAMRead) > 0 && addr < 0x2000 && !d.ramWritten[addr&0x07FF] {
		if b := d.bps.MatchEvent(EventUninitializedRAMRead, d); b != nil {
			d.raise(b, fmt.Sprintf("書き込まれていない RAM $%04X を読んだ", addr))
		}
	}
	if b := d.bps.MatchAddr(BreakRead, addr, d); b != nil {
		d.raise(b, fmt.Sprintf("読み出しブレークポイント $%04X（値 $%02X）", addr, v))
	}
}

// onCPUWrite はバスの書き込みで呼ばれる。
func (d *Debugger) onCPUWrite(addr uint16, v, old uint8) {
	d.logBus(addr, v, true)
	if addr < 0x2000 {
		d.ramWritten[addr&0x07FF] = true
	}
	if d.bps.EventCount(EventMMC3IRQReloadWithoutClocks) > 0 && addr >= 0xC000 && addr < 0xE000 && addr&1 == 1 {
		d.checkMMC3Reload()
	}
	if b := d.bps.MatchAddr(BreakWrite, addr, d); b != nil {
		d.raise(b, fmt.Sprintf("書き込みブレークポイント $%04X（値 $%02X）", addr, v))
	}
}

// risesCounter は A12 の立ち上がりを数えるマッパー。
type risesCounter interface{ A12Rises() uint64 }

// checkMMC3Reload は A12 の立ち上がりを 2 回挟まない $C001 を検出する。
func (d *Debugger) checkMMC3Reload() {
	rc, ok := d.n.Cart.(risesCounter)
	if !ok {
		return
	}
	rises := rc.A12Rises()
	if d.mmc3Written && rises-d.mmc3Rises < 2 {
		if b := d.bps.MatchEvent(EventMMC3IRQReloadWithoutClocks, d); b != nil {
			d.raise(b, fmt.Sprintf("A12 の立ち上がり %d 回で $C001 へ書いた", rises-d.mmc3Rises))
		}
	}
	d.mmc3Rises = rises
	d.mmc3Written = true
}

// onCycle は CPU サイクルの終わりに呼ばれる。
func (d *Debugger) onCycle() {
	n := d.n
	line, dot := n.PPU.Scanline(), n.PPU.Dot()

	if line != d.lastScanline {
		if line < visibleScanlines && len(d.subs) > 0 {
			d.captureAt(line)
		}
		if d.log.Categories&CatPPUTiming != 0 {
			d.logTiming(line)
		}
	}

	if d.bps.Count(BreakPPUPosition) > 0 {
		d.checkPPUPosition(line, dot)
	}

	if d.bps.EventCount(EventMapperIRQ) > 0 {
		irq := n.Cart.IRQAsserted()
		if irq && !d.mapperIRQ {
			if b := d.bps.MatchEvent(EventMapperIRQ, d); b != nil {
				d.raise(b, "マッパーが IRQ をアサートした")
			}
		}
		d.mapperIRQ = irq
	}

	d.lastScanline, d.lastDot = line, dot

	if d.CycleHook != nil {
		d.CycleHook()
	}
}

// checkPPUPosition は直前のサイクルから今までに通ったドットに、PPU 位置
// ブレークポイントが掛かるかを調べる。
//
// 1 CPU サイクルの間に PPU は 3 ドット（PAL は 3 か 4）進む。通過した
// ドットのどれかに一致すれば、このサイクルの終わりで止まる。
func (d *Debugger) checkPPUPosition(line, dot int) {
	prevLine, prevDot := d.lastScanline, d.lastDot
	for i := 0; i < 8; i++ {
		// 直前の位置の次のドットから今の位置までを順にたどる。
		prevDot++
		if prevDot > 340 {
			prevDot = 0
			prevLine++
			if prevLine > d.n.Region.TotalScanlines()-1 {
				prevLine = 0
			}
		}
		if b := d.bps.MatchPPU(prevLine, prevDot, d); b != nil {
			d.raise(b, fmt.Sprintf("PPU 位置ブレークポイント %d, %d", b.Scanline, b.Dot))
			if d.OnCycleBreak != nil {
				d.OnCycleBreak()
			}
			return
		}
		if prevLine == line && prevDot == dot {
			return
		}
	}
}

// onInterrupt は割り込みシーケンスを終えたときに呼ばれる。
func (d *Debugger) onInterrupt(k cpu.Interrupt) {
	n := d.n
	if d.features.CPUView {
		d.calls.OnInterrupt(k, n.CPU.PC, n.Bus.Peek, n.CPU.S)
	}
	if k == cpu.InterruptNMI && d.log.Categories&CatPPUTiming != 0 {
		d.log.Log(CatPPUTiming, "NMI（スキャンライン %d, ドット %d）", n.PPU.Scanline(), n.PPU.Dot())
	}
	var ev EventKind
	switch k {
	case cpu.InterruptNMI:
		ev = EventNMI
	case cpu.InterruptIRQ:
		ev = EventIRQ
	case cpu.InterruptReset:
		ev = EventReset
		d.ramWritten = [2048]bool{}
	default:
		return
	}
	if b := d.bps.MatchEvent(ev, d); b != nil {
		d.raise(b, fmt.Sprintf("%s を受け付けた", ev))
	}
}

// onSprite0Hit はスプライト 0 ヒットが立ったときに呼ばれる。
func (d *Debugger) onSprite0Hit() {
	if b := d.bps.MatchEvent(EventSprite0Hit, d); b != nil {
		d.raise(b, fmt.Sprintf("スプライト 0 ヒット（スキャンライン %d, ドット %d）",
			d.n.PPU.Scanline(), d.n.PPU.Dot()))
	}
}

// onFrameStep は APU のフレームカウンタの信号で呼ばれる。
func (d *Debugger) onFrameStep(quarter, half bool) {
	if d.log.Categories&CatAPUFrame == 0 {
		return
	}
	switch {
	case half:
		d.log.Log(CatAPUFrame, "フレームカウンタ: 1/2 フレーム")
	case quarter:
		d.log.Log(CatAPUFrame, "フレームカウンタ: 1/4 フレーム")
	}
}

// onFrameComplete はフレームが完成したときに呼ばれる。
func (d *Debugger) onFrameComplete(*video.Frame) {
	n := d.n
	if len(d.subs) > 0 {
		d.captureAt(-1)
	}
	if d.features.ChangeTracking {
		d.changes.Update(d.trackedMemory())
	}
	if d.log.Categories&CatDMA != 0 {
		dmc, _ := n.Bus.DMAStats()
		if dmc != d.dmcCount {
			d.log.Log(CatDMA, "DMC DMA %d 回（フレーム %d）", dmc-d.dmcCount, n.Frames())
		}
		d.dmcCount = dmc
	}
}

// trackedMemory は変更追跡の対象を並べる。
func (d *Debugger) trackedMemory() [regionCount][]uint8 {
	var m [regionCount][]uint8
	m[RegionRAM] = d.n.Bus.RAM()
	m[RegionCIRAM] = d.n.PPU.CIRAM()
	m[RegionOAM] = d.n.PPU.OAM()
	m[RegionPalette] = d.n.PPU.Palette()
	m[RegionPRGRAM] = d.n.Cart.PRGRAM()
	return m
}

// Reg は条件式の評価に使うレジスタの値を返す。
func (d *Debugger) Reg(r Register) uint16 {
	c := d.n.CPU
	switch r {
	case RegA:
		return uint16(c.A)
	case RegX:
		return uint16(c.X)
	case RegY:
		return uint16(c.Y)
	case RegS:
		return uint16(c.S)
	case RegP:
		return uint16(c.P())
	case RegPC:
		return c.PC
	}
	return 0
}

// Flag は条件式の評価に使うフラグの値を返す。
func (d *Debugger) Flag(f Flag) bool {
	c := d.n.CPU
	switch f {
	case FlagC:
		return c.C
	case FlagZ:
		return c.Z
	case FlagI:
		return c.I
	case FlagD:
		return c.D
	case FlagV:
		return c.V
	case FlagN:
		return c.N
	}
	return false
}

// Peek は条件式の評価に使うメモリの値を返す。副作用を起こさない。
func (d *Debugger) Peek(addr uint16) uint8 { return d.n.Bus.Peek(addr) }
