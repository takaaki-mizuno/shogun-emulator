package apu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// フレームカウンタのモード。
const (
	// modeFourStep は 4 ステップシーケンス。フレーム IRQ を出す。
	modeFourStep uint8 = 0
	// modeFiveStep は 5 ステップシーケンス。IRQ を出さない。
	modeFiveStep uint8 = 1
)

// frameCounter は低周波クロックを生成する。
//
// 名前に frame と付くが映像信号とは無関係で、約 240 Hz（NTSC）で
// 独立に走る。エンベロープ・リニアカウンタ・レングスカウンタ・
// スイープのクロック源である。
type frameCounter struct {
	region *region.Region

	mode       uint8
	irqInhibit bool
	irqFlag    bool

	// apuCycles はシーケンスの先頭からの APU サイクル数。
	apuCycles uint32

	// pendingWrite は $4017 への書き込みの遅延反映。
	//
	// 書き込みは即座に効かず、3 または 4 CPU サイクル後にタイマーを
	// リセットする。この遅延に依存するテスト ROM がある。
	pendingWrite bool
	pendingValue uint8
	pendingDelay int
}

// clockKind は 1 回のステップで生成する信号。
type clockKind struct {
	quarter bool
	half    bool
}

// newFrameCounter はフレームカウンタを作る。
func newFrameCounter(r *region.Region) frameCounter {
	return frameCounter{region: r}
}

// steps は現在のモードのステップ境界を返す。
func (f *frameCounter) steps() []uint32 {
	return f.region.FrameCounterSteps[f.mode]
}

// write は $4017 への書き込みを受け取る。
//
// タイマーのリセットは、実機では書き込みから 3 または 4 CPU サイクル後に
// 起こる。どちらになるかは書き込んだサイクルの位相で決まる。
//
// 数える値が 1 つ大きいのは、書き込みが起きた CPU サイクルの step が
// すでに終わっているためである。バスは CPU のアクセスより前に APU を
// 進める。`4-jitter`・`09.reset_timing`・`apu_reset/4017_timing` が
// この値を検証する。
func (f *frameCounter) write(v uint8, apuTick bool) {
	f.pendingWrite = true
	f.pendingValue = v
	if apuTick {
		f.pendingDelay = 5
	} else {
		f.pendingDelay = 4
	}

	// IRQ 抑止は書き込みの時点で即座に効く。
	if v&0x40 != 0 {
		f.irqInhibit = true
		f.irqFlag = false
	}
}

// step は 1 CPU サイクル進め、生成した信号を返す。
//
// apuTick は、この CPU サイクルで APU サイクルが 1 つ進むかを表す。
// 1 つの APU サイクルは 2 つの CPU サイクルからなる。調査文書の表では
// 前半を GET、後半を PUT と呼ぶ。カウントが所定値に達するのは GET の
// 側であり、quarter frame と half frame の信号はその 1 CPU サイクル後、
// PUT の側で出る。フレーム割り込みフラグは GET と PUT の両方で立つ。
func (f *frameCounter) step(apuTick bool) clockKind {
	var out clockKind

	if f.pendingWrite {
		f.pendingDelay--
		if f.pendingDelay <= 0 {
			out = f.applyWrite()
		}
	}

	if apuTick {
		f.apuCycles++
		f.onGetCycle()
		return out
	}

	k := f.onPutCycle()
	out.quarter = out.quarter || k.quarter
	out.half = out.half || k.half
	return out
}

// applyWrite は遅延していた $4017 の書き込みを反映する。
func (f *frameCounter) applyWrite() clockKind {
	f.pendingWrite = false
	f.mode = f.pendingValue >> 7 & 1
	f.irqInhibit = f.pendingValue&0x40 != 0
	if f.irqInhibit {
		f.irqFlag = false
	}
	f.apuCycles = 0

	// 5 ステップモードでは、リセットと同時に quarter frame と
	// half frame の両方を生成する。
	if f.mode == modeFiveStep {
		return clockKind{quarter: true, half: true}
	}
	return clockKind{}
}

// onGetCycle は APU サイクルの前半で行う処理。
//
// カウントが 0 に戻るのはここである。4 ステップモードでは、このときにも
// フレーム割り込みフラグを立てる。
//
// 調査文書の表のとおり、最終ステップの APU サイクルとその次の APU
// サイクルの前半でフラグを立てる。後半での 1 回と合わせて 3 箇所になる。
func (f *frameCounter) onGetCycle() {
	steps := f.steps()
	if f.mode == modeFourStep {
		if f.apuCycles == steps[3] {
			f.setIRQ()
		}
		if f.apuCycles == steps[4] {
			f.setIRQ()
			f.apuCycles = 0
		}
		return
	}
	if f.apuCycles == steps[5] {
		f.apuCycles = 0
	}
}

// onPutCycle は APU サイクルの後半で行う処理。
//
// quarter frame と half frame の信号はここで出る。
func (f *frameCounter) onPutCycle() clockKind {
	steps := f.steps()
	if f.mode == modeFourStep {
		switch f.apuCycles {
		case steps[0]:
			return clockKind{quarter: true}
		case steps[1]:
			return clockKind{quarter: true, half: true}
		case steps[2]:
			return clockKind{quarter: true}
		case steps[3]:
			f.setIRQ()
			return clockKind{quarter: true, half: true}
		}
		return clockKind{}
	}

	switch f.apuCycles {
	case steps[0]:
		return clockKind{quarter: true}
	case steps[1]:
		return clockKind{quarter: true, half: true}
	case steps[2]:
		return clockKind{quarter: true}
	case steps[4]:
		return clockKind{quarter: true, half: true}
	}
	return clockKind{}
}

// setIRQ は抑止されていなければフレーム割り込みフラグを立てる。
func (f *frameCounter) setIRQ() {
	if !f.irqInhibit {
		f.irqFlag = true
	}
}

// clearIRQ は $4015 の読み出しでフラグを落とす。
func (f *frameCounter) clearIRQ() { f.irqFlag = false }

// reset は電源投入とリセットの状態にする。
//
// 実機では、最初のコードが実行される 10 CPU サイクル前に $4017 へ 0 が
// 書かれたのと同じ状態になっている。シーケンスの先頭から始める。
func (f *frameCounter) reset() {
	f.apuCycles = 0
	f.irqFlag = false
	f.pendingWrite = false
	f.pendingDelay = 0
	f.pendingValue = 0
}

func (f *frameCounter) saveState(w *state.Writer) {
	end := w.Section("frame")
	w.U8(f.mode)
	w.Bool(f.irqInhibit)
	w.Bool(f.irqFlag)
	w.U32(f.apuCycles)
	w.Bool(f.pendingWrite)
	w.U8(f.pendingValue)
	w.Int(f.pendingDelay)
	end()
}

func (f *frameCounter) loadState(r *state.Reader) {
	end := r.RequireSection("frame")
	f.mode = r.U8()
	f.irqInhibit = r.Bool()
	f.irqFlag = r.Bool()
	f.apuCycles = r.U32()
	f.pendingWrite = r.Bool()
	f.pendingValue = r.U8()
	f.pendingDelay = r.Int()
	end()
}
