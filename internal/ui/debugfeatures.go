package ui

import (
	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// debugUsers は開いているビューアごとに、デバッガのどの機能を使って
// いるかを数える。
//
// 最後のビューアを閉じたときに機能を止め、フックを nil に戻す。開いて
// いない機能のフックを残すと、通常のプレイが遅くなる（設計書 09 編 §9.6）。
type debugUsers struct {
	cpu     int
	memory  int
	tracing bool
}

// features は数から機能の組を作る。
func (d *debugUsers) features() debug.Features {
	return debug.Features{
		Tracing:        d.tracing,
		CPUView:        d.cpu > 0,
		ChangeTracking: d.memory > 0,
	}
}

// applyDebugFeatures はデバッガへ機能の組を送る。
func (u *UI) applyDebugFeatures() {
	f := u.debugUsers.features()
	u.emu.WithDebugger(func(d *debug.Debugger) { d.SetFeatures(f) })
}
