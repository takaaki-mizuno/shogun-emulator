package agent

import (
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// Stop Reason の名前（設計書 14 編 §14.8.4 の表の全 11 種）。
const (
	StopFramesDone   = "frames_done"
	StopSequenceDone = "sequence_done"
	StopCondition    = "condition"
	StopMaxFrames    = "max_frames"
	StopBreakpoint   = "breakpoint"
	StopDiagnostic   = "diagnostic"
	StopStepDone     = "step_done"
	StopCancelled    = "cancelled"
	StopTimeout      = "timeout"
	StopControlLost  = "control_lost"
	StopCPUHalted    = "cpu_halted"
)

// stopName は emu の停止理由を Stop Reason にする。
//
// 条件で止まる進行（run_until）以外で条件が成り立つことは無い。フレーム数を
// 進め切ったときの名前は、進行の種類によって呼び出し側が選ぶ。
func stopName(r emu.StopReason) string {
	switch r {
	case emu.StopFramesDone:
		return StopFramesDone
	case emu.StopStepDone:
		return StopStepDone
	case emu.StopBreakpoint:
		return StopBreakpoint
	case emu.StopCancelled:
		return StopCancelled
	case emu.StopCPUHalted:
		return StopCPUHalted
	case emu.StopCondition:
		return StopCondition
	}
	return StopCancelled
}

// BreakDetail は stop_reason が breakpoint のときの stop_detail。
type BreakDetail struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	PC       string `json:"pc"`
	Scanline int    `json:"scanline"`
	Dot      int    `json:"dot"`
}

// breakDetail はブレークポイントの情報を stop_detail にする。
func breakDetail(info *debug.BreakInfo) *BreakDetail {
	if info == nil {
		return nil
	}
	return &BreakDetail{
		ID: info.Breakpoint.ID, Kind: debug.BreakKindName(info.Breakpoint.Kind), Reason: info.Reason,
		PC: hex16(info.PC), Scanline: info.Scanline, Dot: info.Dot,
	}
}
