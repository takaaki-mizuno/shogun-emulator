package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
)

// apuChannelNames は APU のチャンネルの名前。ミュートのビットの順。
var apuChannelNames = []string{"Pulse 1", "Pulse 2", "Triangle", "Noise", "DMC"}

// apuViewer は APU 状態ビューア（設計書 09 編 §9.4.7）。
//
// ミュートとソロは出力段（APU.SetMute）だけで扱い、エミュレーション状態を
// 変えない。
type apuViewer struct {
	u     *UI
	grid  *widget.TextGrid
	mutes [5]*widget.Check
	ring  *widget.ProgressBar
	ringL *widget.Label
	mask  uint8
	// updating はチェックを表示側から変えている最中であることを表す。
	updating bool
}

// apus は APU 状態ビューアを返す。無ければ作る。
func (u *UI) apus() *apuViewer {
	if u.apuViewer == nil {
		u.apuViewer = &apuViewer{u: u}
	}
	return u.apuViewer
}

func (v *apuViewer) Title() string { return "APU" }

func (v *apuViewer) Content() fyne.CanvasObject {
	v.grid = widget.NewTextGrid()
	controls := container.NewGridWithColumns(3)
	for i, name := range apuChannelNames {
		bit := uint8(1) << i
		v.mutes[i] = widget.NewCheck(name+" をミュート", func(on bool) {
			if v.updating {
				return
			}
			if on {
				v.setMask(v.mask | bit)
			} else {
				v.setMask(v.mask &^ bit)
			}
		})
		controls.Add(v.mutes[i])
		controls.Add(widget.NewButton(name+" だけ鳴らす", func() { v.setMask(0x1F &^ bit) }))
		controls.Add(widget.NewLabel(""))
	}
	all := widget.NewButton("すべて鳴らす", func() { v.setMask(0) })
	v.ring = widget.NewProgressBar()
	v.ringL = widget.NewLabel("")
	v.Refresh()
	bottom := container.NewVBox(
		container.NewBorder(nil, nil, widget.NewLabel("リングバッファ"), v.ringL, v.ring),
		controls, all,
	)
	return container.NewBorder(nil, bottom, nil, nil, container.NewScroll(v.grid))
}

// setMask はミュートを変える。
func (v *apuViewer) setMask(m uint8) {
	v.mask = m
	v.u.emu.SetAPUMute(m)
	v.drawChecks()
}

// drawChecks はミュートのチェックを合わせる。
func (v *apuViewer) drawChecks() {
	v.updating = true
	for i, c := range v.mutes {
		if c != nil {
			c.SetChecked(v.mask&(1<<i) != 0)
		}
	}
	v.updating = false
}

// Refresh は APU の現在値を読んで描き直す。
func (v *apuViewer) Refresh() {
	if v.grid == nil {
		return
	}
	var in apu.Inspection
	loaded := false
	v.u.emu.WithMachine(func(n *nes.NES) {
		if n != nil {
			in = n.APU.Inspect()
			loaded = true
		}
	})
	if !loaded {
		v.grid.SetText("ROM が読み込まれていない")
		return
	}
	v.mask = in.Mute
	v.drawChecks()
	v.grid.SetText(formatAPU(in))

	st := v.u.emu.Status()
	if st.AudioHigh > 0 {
		v.ring.SetValue(min(float64(st.AudioFill)/float64(st.AudioHigh), 1))
		v.ringL.SetText(fmt.Sprintf("%d / %d（音切れ %d）", st.AudioFill, st.AudioHigh, st.AudioUnderruns))
	} else {
		v.ring.SetValue(0)
		v.ringL.SetText("音声なし")
	}
}

// formatAPU は APU の現在値を表にする。
func formatAPU(in apu.Inspection) string {
	var b strings.Builder
	muted := func(i int) string {
		if in.Mute&(1<<i) != 0 {
			return "（ミュート）"
		}
		return ""
	}
	for i, p := range in.Pulse {
		env := fmt.Sprintf("エンベロープ %d（ループ %v）", p.EnvelopeParam, p.EnvelopeLoop)
		if p.Constant {
			env = "一定音量"
		}
		fmt.Fprintf(&b, "Pulse %d%s\n", i+1, muted(i))
		fmt.Fprintf(&b, "  周期 $%03X  デューティ %d  音量 %2d  %s\n", p.Period, p.Duty, p.Volume, env)
		fmt.Fprintf(&b, "  レングス %3d（停止 %v）  スイープ 有効 %v 反転 %v シフト %d 周期 %d  出力 %2d\n",
			p.Length, p.Halt, p.SweepEnabled, p.SweepNegate, p.SweepShift, p.SweepPeriod, p.Output)
	}
	t := in.Triangle
	fmt.Fprintf(&b, "Triangle%s\n", muted(2))
	fmt.Fprintf(&b, "  周期 $%03X  リニアカウンタ %3d（再読込 %3d）  レングス %3d  シーケンス %2d  出力 %2d\n",
		t.Period, t.LinearCounter, t.LinearReload, t.Length, t.Sequence, t.Output)
	n := in.Noise
	mode := "長周期"
	if n.Mode {
		mode = "短周期"
	}
	fmt.Fprintf(&b, "Noise%s\n", muted(3))
	fmt.Fprintf(&b, "  周期 %4d  モード %s  LFSR $%04X  音量 %2d  レングス %3d  出力 %2d\n",
		n.Period, mode, n.LFSR, n.Volume, n.Length, n.Output)
	d := in.DMC
	fmt.Fprintf(&b, "DMC%s\n", muted(4))
	fmt.Fprintf(&b, "  出力レベル %3d  レート %2d  サンプル $%04X（%d バイト）  現在 $%04X  残り %d\n",
		d.OutputLevel, d.Rate, d.SampleAddr, d.SampleLength, d.CurrentAddr, d.BytesRemaining)
	fmt.Fprintf(&b, "  ループ %v  IRQ 有効 %v  IRQ %v\n", d.Loop, d.IRQEnabled, d.IRQ)
	f := in.Frame
	fmode := "4 ステップ"
	if f.FiveStep {
		fmode = "5 ステップ"
	}
	fmt.Fprintf(&b, "フレームカウンタ\n  モード %s  APU サイクル %d  IRQ 禁止 %v  IRQ %v",
		fmode, f.APUCycles, f.IRQInhibit, f.IRQ)
	return b.String()
}

func (v *apuViewer) OnClose() {}
