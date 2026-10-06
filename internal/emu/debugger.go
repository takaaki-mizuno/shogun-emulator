package emu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
)

// errMovieEdit はムービーを扱っている間のメモリ編集を断る理由。
var errMovieEdit = errors.New("emu: ムービーの記録中と再生中はメモリを編集できない")

// cycleGate は命令の途中で止まっている間の受け渡し。
//
// サイクル単位ステップと PPU 位置ブレークポイントは命令の途中で止まる。
// このときエミュレーションゴルーチンは Hooks.OnCycle の中で待っており、
// コマンドキューを処理できない。UI スレッドからの要求はこのチャネルで
// 受け取る（設計書 09 編 §9.5）。
type cycleGate struct {
	// active はエミュレーションゴルーチンが待っていることを表す。
	active atomic.Bool
	req    chan gateRequest
}

// gateRequest は待っている間に受け付ける要求。
type gateRequest struct {
	// steps が正のとき、その数の CPU サイクルを進める。
	steps int
	// release は待ちを解き、命令を最後まで実行させる。
	release bool
	// fn は待ったまま実行する処理。表示のために状態を読む。
	fn   func()
	done chan struct{}
	// result は steps の進行が止まったときに結果を受け取る。nil のとき
	// 知らせない。
	result chan StepResult
}

// newCycleGate は受け渡しを作る。
func newCycleGate() cycleGate { return cycleGate{req: make(chan gateRequest, 1)} }

// step は次の CPU サイクルへ進める。待っていないときは何もしない。
func (g *cycleGate) step() {
	select {
	case g.req <- gateRequest{steps: 1}:
	default:
	}
}

// release は待ちを解く。待っていないときは何もしない。
func (g *cycleGate) release() {
	if !g.active.Load() {
		return
	}
	select {
	case g.req <- gateRequest{release: true}:
	default:
	}
}

// debugState はデバッガに関わるエミュレーションゴルーチンの状態。
type debugState struct {
	// budget はサイクル単位ステップで残っている CPU サイクル数。
	budget int
	// midBreak は命令の途中のブレークポイントを処理していることを表す。
	midBreak bool
	// gateStatus は待っている間に UI へ見せる位置。
	gateMu     sync.Mutex
	gateStatus GatePosition
}

// GatePosition は命令の途中で止まっている位置。
type GatePosition struct {
	Cycles   uint64
	Scanline int
	Dot      int
	PC       uint16
}

// Debugger はデバッガを返す。
//
// 返したデバッガのメソッドのうち、スレッド安全と書かれたもの以外は
// WithDebugger の中で呼ぶ。
func (e *Emulator) Debugger() *debug.Debugger { return e.dbg }

// WithDebugger はエミュレーションゴルーチンでデバッガを操作する。
//
// 命令の途中で止まっているときは、待ちの中で実行する。
func (e *Emulator) WithDebugger(fn func(d *debug.Debugger)) bool {
	return e.WithMachine(func(*nes.NES) { fn(e.dbg) })
}

// GatePosition は命令の途中で止まっている位置を返す。止まっていない
// とき false。
func (e *Emulator) GatePosition() (GatePosition, bool) {
	if !e.gate.active.Load() {
		return GatePosition{}, false
	}
	e.debug.gateMu.Lock()
	defer e.debug.gateMu.Unlock()
	return e.debug.gateStatus, true
}

// stepCycles は count 個の CPU サイクルを進めて止まる。
//
// 命令を実行し始め、サイクルの終わりのフックで残りを数える。残りが
// 0 になったところで待つ。
func (e *Emulator) stepCycles(count int) {
	e.debug.budget = count
	e.dbg.CycleHook = e.onCycleGate
	e.dbg.RefreshHooks()
	if e.stepOnce() {
		e.frameBoundary()
	}
	e.leaveCycleMode()
	e.afterStep()
}

// leaveCycleMode はサイクル単位の待ちをやめる。
func (e *Emulator) leaveCycleMode() {
	if e.dbg.CycleHook == nil {
		return
	}
	e.dbg.CycleHook = nil
	e.dbg.RefreshHooks()
}

// onCycleGate は CPU サイクルの終わりに呼ばれ、残りが無くなったら待つ。
func (e *Emulator) onCycleGate() {
	if e.debug.budget > 0 {
		e.debug.budget--
	}
	if e.debug.budget > 0 {
		return
	}
	e.waitInGate()
}

// onCycleBreak は命令の途中でブレークポイントに当たったときに呼ばれる。
func (e *Emulator) onCycleBreak() {
	if e.ignoreBreaks {
		e.dbg.ClearHit()
		return
	}
	e.paused = true
	e.setPaused(true)
	// 命令の途中で止まったことを結果に含める。
	e.debug.midBreak = true
	e.takeBreak()
	e.debug.midBreak = false
	e.until = nil
	e.waitInGate()
}

// waitInGate は命令の途中で止まり、UI スレッドからの要求を待つ。
func (e *Emulator) waitInGate() {
	m := e.machine
	e.debug.gateMu.Lock()
	e.debug.gateStatus = GatePosition{
		Cycles:   m.Cycles(),
		Scanline: m.PPU.Scanline(),
		Dot:      m.PPU.Dot(),
		PC:       m.CPU.PC,
	}
	e.debug.gateMu.Unlock()
	e.gate.active.Store(true)
	e.setCycleStepping(true)
	// サイクル単位の進行はここで止まる。待ちに入る前に結果を知らせる。
	e.finishStep(StopStepDone)
	defer func() {
		e.gate.active.Store(false)
		e.setCycleStepping(false)
	}()

	for {
		select {
		case <-e.stop:
			e.dbg.CycleHook = nil
			return
		case r := <-e.gate.req:
			switch {
			case r.fn != nil:
				r.fn()
				close(r.done)
			case r.steps > 0:
				// サイクルの待ちを続ける。次のサイクルの終わりで再び待つ。
				if r.result != nil {
					e.beginPending(r.result)
				}
				e.debug.budget = r.steps
				e.dbg.CycleHook = e.onCycleGate
				return
			case r.release:
				// 命令を最後まで実行させる。命令境界でコマンドを処理する。
				e.dbg.CycleHook = nil
				return
			}
		}
	}
}

// afterStep はステップ実行の後に止まった理由を確かめる。
func (e *Emulator) afterStep() {
	e.takeBreak()
	e.updateStatus()
}

// takeBreak はデバッガが止まる理由を持っていれば取り出して知らせる。
func (e *Emulator) takeBreak() bool {
	if e.ignoreBreaks {
		e.dbg.ClearHit()
		return false
	}
	info, ok := e.dbg.TakeHit()
	if !ok {
		return false
	}
	e.paused = true
	e.setPaused(true)
	e.until = nil
	e.statusMu.Lock()
	e.status.Break = info.Reason
	e.statusMu.Unlock()
	e.notifyMessage(info.Reason)
	// イベントを積んでから進行の結果を返す。結果を受け取った直後の観測で、
	// このイベントが数えられるようにするためである。
	if o := e.observer.Load(); o != nil && o.OnBreak != nil {
		o.OnBreak(info, e.machine.Frames())
	}
	e.finishStepBreak(info)
	if e.cfg.OnBreak != nil {
		e.cfg.OnBreak(info)
	}
	// ブレークポイントで止まったときはトレースを書き出す。
	e.dumpTraceOnBreak()
	return true
}

// clearBreak は止まった理由の表示を消す。
func (e *Emulator) clearBreak() {
	e.statusMu.Lock()
	e.status.Break = ""
	e.statusMu.Unlock()
}

// setCycleStepping は命令の途中で止まっていることを Status へ反映する。
func (e *Emulator) setCycleStepping(v bool) {
	e.statusMu.Lock()
	e.status.MidInstruction = v
	e.statusMu.Unlock()
}

// attachDebugger は読み込んだ本体をデバッガへ渡す。
//
// ROM ごとに保存した名前とブレークポイントを読む。読めないときは
// 空の状態で続ける。
func (e *Emulator) attachDebugger(m *nes.NES, romPath string) {
	load := e.cfg.LoadSymbols
	if load == nil {
		load = debug.LoadSymbols
	}
	syms, err := load(e.symbolsPath(m))
	if err != nil {
		e.notifyError(err)
		syms = debug.NewSymbols()
	}
	// ブレークポイントの条件式が .dbg の名前を使えるよう、Attach の前に読む。
	// パスの分からない読み込み（Fork）と、共有する Symbols を読み込み済みの
	// ときは読まない。
	var notes []string
	if romPath != "" && !syms.ProjectLoaded() {
		notes = debug.LoadProject(syms, romPath,
			debug.ProjectPathsFor(romPath, e.cfg.Dirs.Data, cart.ROMKeyString(m.ROM.Hash[:])))
	}
	e.statusMu.Lock()
	e.projectNotes = notes
	e.statusMu.Unlock()
	e.dbg.CycleHook = nil
	e.dbg.OnCycleBreak = e.onCycleBreak
	e.dbg.Attach(m, syms)
	// ヘッダの解釈で補正した点（マッパー番号の上位 4 bit のマスク、
	// 4 画面ビットの無視）を warn.compat へ記録する（設計書 09 編 §9.8）。
	for _, w := range m.ROM.Warnings {
		e.dbg.Logger().Warnf("ROM のヘッダ: %s", w)
	}
	if e.cfg.Debug.BreakOnUninitializedRAMRead {
		e.dbg.AddBreakpoint(debug.Breakpoint{Kind: debug.BreakEvent,
			Event: debug.EventUninitializedRAMRead, Enabled: true, Temporary: true})
	}
	for _, addr := range e.startupBreaks {
		e.dbg.AddBreakpoint(debug.Breakpoint{Kind: debug.BreakExec,
			AddrStart: addr, AddrEnd: addr, Enabled: true, Temporary: true})
	}
}

// SetStartupBreakpoints は ROM を読み込むたびに置く実行ブレークポイントを
// 決める（引数 --break-at）。保存しない一時的なブレークポイントとして置く。
// ROM を読み込む前に呼ぶ。
func (e *Emulator) SetStartupBreakpoints(addrs []uint16) {
	e.WithMachine(func(m *nes.NES) {
		e.startupBreaks = append([]uint16(nil), addrs...)
		if m != nil {
			for _, addr := range addrs {
				e.dbg.AddBreakpoint(debug.Breakpoint{Kind: debug.BreakExec,
					AddrStart: addr, AddrEnd: addr, Enabled: true, Temporary: true})
			}
		}
	})
}

// saveSymbols は名前とブレークポイントをファイルへ書き出す。
func (e *Emulator) saveSymbols() {
	if e.machine == nil {
		return
	}
	if err := e.dbg.Symbols().Save(e.symbolsPath(e.machine)); err != nil {
		e.notifyError(err)
	}
}

// SaveSymbols は名前とブレークポイントを書き出す。UI が変更の後に呼ぶ。
func (e *Emulator) SaveSymbols() {
	e.WithMachine(func(*nes.NES) { e.saveSymbols() })
}

// symbolsPath は ROM の名前とブレークポイントの保存先を返す。
func (e *Emulator) symbolsPath(m *nes.NES) string {
	return filepath.Join(e.cfg.Dirs.SymbolsDir(e.cfg.SymbolsDir), cart.ROMKeyString(m.ROM.Hash[:])+".json")
}

// dumpTraceOnBreak はトレースを有効にしているとき、止まった時点までの
// 記録をファイルへ書き出す。
func (e *Emulator) dumpTraceOnBreak() {
	if !e.dbg.Features().Tracing || e.dbg.Tracer().Len() == 0 {
		return
	}
	path, err := e.traceDumpPath()
	if err != nil {
		e.notifyError(err)
		return
	}
	if err := writeTrace(path, e.dbg.Tracer()); err != nil {
		e.notifyError(err)
		return
	}
	e.notifyMessage(fmt.Sprintf("トレースを %s へ書き出しました", path))
}

// Poke は副作用を起こさずにメモリを書き換える。
//
// ムービーを扱っている間は断る。記録した入力と結果の対応が崩れる
// ためである（設計書 09 編 §9.4.5）。
func (e *Emulator) Poke(space debug.Space, addr int, v uint8, withSideEffects bool) error {
	var err error
	ok := e.WithMachine(func(m *nes.NES) {
		if m == nil {
			err = errNoROM
			return
		}
		if e.recorder != nil || e.player != nil {
			err = errMovieEdit
			return
		}
		err = debug.WriteMemory(m, space, addr, v, withSideEffects)
		if err == nil {
			// 常時記録は利用者のムービー記録ではないため、編集を断らずに介入として
			// 残す（設計書 14 編 §14.16.2）。
			e.intervene(movie.Record{Kind: movie.KindPoke, Space: uint8(space), Addr: uint32(addr), Value: uint32(v), Flag: withSideEffects})
		}
	})
	if !ok {
		return errors.New("emu: エミュレーションが停止している")
	}
	return err
}

// newDebugger は設定からデバッガを作る。
func newDebugger(cfg Config) *debug.Debugger {
	// nil は設定を与えられなかったことを表し、既定のカテゴリを使う。
	// 空のスライスはすべて無効にした指定である。
	cats, err := debug.ParseCategories(cfg.Debug.LogCategories)
	if err != nil || cfg.Debug.LogCategories == nil {
		cats = debug.DefaultCategories
	}
	logger := debug.NewLogger(cats, cfg.LogWriter)
	return debug.New(logger, cfg.Debug.TraceRingSize, uint32(cfg.Debug.ChangeDecayFrames))
}

// traceDumpPath はトレースの書き出し先を決める。
func (e *Emulator) traceDumpPath() (string, error) {
	dir := e.cfg.Dirs.TraceDir(e.cfg.TraceDir)
	name := e.Status().ROMName
	if name == "" {
		name = "trace"
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d.log", name, e.machine.Cycles())), nil
}

// writeTrace はトレースのリングをファイルへ書き出す。
func writeTrace(path string, t *debug.Tracer) error {
	var buf bytesBuffer
	if _, err := t.WriteTo(&buf); err != nil {
		return err
	}
	return writeFileAtomic(path, buf.b)
}

// bytesBuffer は書き込みを溜める。
type bytesBuffer struct{ b []byte }

func (w *bytesBuffer) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

// DumpTrace はトレースのリングを path へ書き出す。
func (e *Emulator) DumpTrace(path string) error {
	var err error
	ok := e.WithMachine(func(*nes.NES) {
		err = writeTrace(path, e.dbg.Tracer())
	})
	if !ok {
		return errors.New("emu: エミュレーションが停止している")
	}
	return err
}

// DumpTraceDefault はトレースのリングを既定の置き場所へ書き出し、
// 書き出したパスを返す。
func (e *Emulator) DumpTraceDefault() (string, error) {
	var (
		path string
		err  error
	)
	ok := e.WithMachine(func(m *nes.NES) {
		if m == nil {
			err = errNoROM
			return
		}
		if path, err = e.traceDumpPath(); err != nil {
			return
		}
		err = writeTrace(path, e.dbg.Tracer())
	})
	if !ok {
		return "", errors.New("emu: エミュレーションが停止している")
	}
	return path, err
}

// StartTraceFile はトレースを既定の置き場所のファイルへ常時出力し始め、
// ファイルのパスを返す（設計書 09 編 §9.7）。
//
// 出力にはトレースの記録が要るため、呼び出し側はデバッガの Tracing を
// 有効にする。
func (e *Emulator) StartTraceFile() (string, error) {
	return e.startTraceFile("")
}

// StartTraceLog はトレースを path へ常時出力し始める（引数 --trace-log）。
// トレースの記録も有効にする。
func (e *Emulator) StartTraceLog(path string) error {
	_, err := e.startTraceFile(path)
	return err
}

// startTraceFile はトレースの常時出力を始める。path が空のとき既定の置き場所を使う。
func (e *Emulator) startTraceFile(path string) (string, error) {
	var err error
	ok := e.WithMachine(func(m *nes.NES) {
		if m == nil {
			err = errNoROM
			return
		}
		e.closeTraceFile()
		if path == "" {
			if path, err = e.traceDumpPath(); err != nil {
				return
			}
			path = strings.TrimSuffix(path, ".log") + "-live.log"
		}
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return
		}
		var f *os.File
		if f, err = os.Create(path); err != nil {
			return
		}
		e.traceFile = f
		err = e.dbg.Tracer().SetOutput(f)
		if err == nil {
			f := e.dbg.Features()
			f.Tracing = true
			e.dbg.SetFeatures(f)
		}
	})
	if !ok {
		return "", errors.New("emu: エミュレーションが停止している")
	}
	return path, err
}

// StopTraceFile はトレースの常時出力をやめ、ファイルを閉じる。
func (e *Emulator) StopTraceFile() error {
	var err error
	e.WithMachine(func(*nes.NES) { err = e.closeTraceFile() })
	return err
}

// TraceFileActive はトレースを常時出力しているかを返す。
func (e *Emulator) TraceFileActive() bool {
	var on bool
	e.WithMachine(func(*nes.NES) { on = e.traceFile != nil })
	return on
}

// closeTraceFile は常時出力のバッファを書き出してファイルを閉じる。
func (e *Emulator) closeTraceFile() error {
	if e.traceFile == nil {
		return nil
	}
	err := e.dbg.Tracer().SetOutput(nil)
	if cerr := e.traceFile.Close(); err == nil {
		err = cerr
	}
	e.traceFile = nil
	return err
}
