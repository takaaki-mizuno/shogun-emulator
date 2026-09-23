package emu

import (
	"math"
	"math/bits"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// turboState は連射の状態（設計書 07 編 §7.5）。
//
// ボタンごとのレートと、押し始めたフレームを持つ。フレームの開始時に
// 押下状態を取り込むときに当てるため、ムービーには連射を当てた後の状態が
// 記録される。
type turboState struct {
	// rates はポートとボタン（input.ButtonA からのビット位置）ごとのレート。0 で無効。
	rates [input.PortCount][8]int
	// held は前のフレームで押していたボタン。
	held [input.PortCount]uint8
	// start はボタンを押し始めたフレーム。
	start [input.PortCount][8]uint64
}

// turboHalfPeriod は連射レート hz の半周期をフレーム数で返す。
func turboHalfPeriod(hz int) uint64 {
	return uint64(max(1, int(math.Round(60/(2*float64(hz))))))
}

// apply はフレーム frame に取り込む押下状態 b に連射を当てる。
func (t *turboState) apply(frame uint64, b [input.PortCount]uint8) [input.PortCount]uint8 {
	for port := range b {
		pressed := b[port]
		for bit := range 8 {
			mask := uint8(1) << bit
			hz := t.rates[port][bit]
			if hz <= 0 || pressed&mask == 0 {
				continue
			}
			if t.held[port]&mask == 0 {
				t.start[port][bit] = frame
			}
			if (frame-t.start[port][bit])/turboHalfPeriod(hz)%2 == 1 {
				b[port] &^= mask
			}
		}
		t.held[port] = pressed
	}
	return b
}

// SetTurbo はポート port（0 始まり）の連射レートをボタン名ごとに設定する。
// レートが 0 のボタンは連射しない。
func (e *Emulator) SetTurbo(port int, turbo map[string]int) {
	if port < 0 || port >= input.PortCount {
		return
	}
	var rates [8]int
	for name, hz := range turbo {
		mask, ok := buttonBits[name]
		if !ok {
			continue
		}
		rates[bits.TrailingZeros8(mask)] = hz
	}
	e.WithMachine(func(*nes.NES) { e.turbo.rates[port] = rates })
}
