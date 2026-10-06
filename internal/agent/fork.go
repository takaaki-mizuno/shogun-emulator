package agent

import (
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// Fork は src の現在の Machine State を複製した新しい Instance を作る
// （設計書 14 編 §14.3.2）。新しい Instance の Control は owner が持つ。
//
// 複製はセーブステートを経由する。往復テスト（設計書 12 編 §12.4）で
// 複製が完全であることが確かめられている経路を使うためである。
func (h *Host) Fork(owner *Conn, src *Instance) (*Instance, []string, error) {
	if src.romData == nil {
		return nil, nil, Errorf(KindNotLoaded, "Instance %s は ROM を読み込んでいない", src.ID)
	}
	if err := h.reserve(); err != nil {
		return nil, nil, err
	}
	var notes []string
	if src.Emu.Status().MidInstruction {
		notes = append(notes, "命令の途中で止まっていたため、命令を完了させてから複製した")
	}
	st, err := src.Emu.SaveState()
	if err != nil {
		return nil, nil, Errorf(KindInternalError, "状態を取り出せない: %v", err)
	}
	var bps []debug.Breakpoint
	var freezes []debug.Freeze
	var an analysisSettings
	src.Emu.WithDebugger(func(d *debug.Debugger) {
		bps = d.Breakpoints()
		freezes = d.Freezes()
		an = analysisSettings{diag: d.DiagConfig(), trace: d.AgentTraceState()}
		an.profile, an.profiling = d.ActiveProfile()
	})

	e, err := h.newEmulator(src.opts)
	if err != nil {
		return nil, nil, err
	}
	if err := e.LoadROMData(src.romData, src.romName); err != nil {
		e.Stop()
		return nil, nil, Errorf(KindInternalError, "ROM を読み込めない: %v", err)
	}
	if err := e.LoadState(st); err != nil {
		e.Stop()
		return nil, nil, Errorf(KindInternalError, "状態を復元できない: %v", err)
	}
	copyInstanceState(src, e, bps, freezes, an)
	// 常時記録を複製し、以降は別々に記録する（設計書 14 編 §14.3.2）。
	if j, blob, err := src.Emu.Journal(); err == nil {
		e.SetJournal(j, blob)
	}

	inst, err := h.register(e, owner)
	if err != nil {
		e.Stop()
		return nil, nil, err
	}
	inst.romName, inst.romPath, inst.romData, inst.opts = src.romName, src.romPath, src.romData, src.opts
	h.attachShared(inst)
	// scenario.export は複製した時点の状態から始める。
	inst.setExportStart(inst.newAnchor(src.romPath, true))
	h.watchEmulator(inst)
	return inst, notes, nil
}

// copyInstanceState は Machine State 以外で Fork の時点の内容を複製する
// ものを写す（設計書 14 編 §14.3.2 の表）。
//
// 各項目は、その機能を作るフェーズでここに加える。
//   - ブレークポイント: 写す（このフェーズ）
//   - ウォッチ: ROM ごとの Symbols が持つため共有している。Instance ごとに
//     分けるのはフェーズ 16
//   - Freeze: 写す
//   - Diagnostic の有効・無効、トレースとプロファイルの設定: 写す（内容は写さない）
//   - 常時記録: 写す（Fork の本体で行う）
//
// Symbol と Game State Definition は複製せず、ROM ごとの共有物を使う。
// イベントキューは空で始める。
func copyInstanceState(src *Instance, dst *emu.Emulator, bps []debug.Breakpoint, freezes []debug.Freeze, an analysisSettings) {
	dst.WithDebugger(func(d *debug.Debugger) {
		// Diagnostic の指定、トレースの指定、プロファイルの指定を写す。記録の
		// 内容は写さない（設計書 14 編 §14.3.2）。
		d.SetDiagConfig(an.diag)
		if an.trace.Enabled {
			d.SetAgentTrace(an.trace)
		}
		if an.profiling {
			d.StartProfile(an.profile)
		}
		d.SetFreezes(freezes)
		for _, b := range d.Breakpoints() {
			d.RemoveBreakpoint(b.ID)
		}
		for _, b := range bps {
			d.AddBreakpoint(b)
		}
	})
}

// analysisSettings は Fork で写す解析の指定。
type analysisSettings struct {
	diag      debug.DiagConfig
	trace     debug.AgentTrace
	profile   debug.ProfileOptions
	profiling bool
}
