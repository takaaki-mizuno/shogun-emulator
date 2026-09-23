package emu

import (
	"sync/atomic"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
)

// InputState は UI スレッドとエミュレーションゴルーチンが共有する押下状態。
//
// エミュレーションコアに置かないのは、これがエミュレーション状態ではなく
// UI スレッドとの境界であることによる（設計書 07 編 §7.3）。
type InputState struct {
	ports [input.PortCount]atomic.Uint32
}

// Buttons は port のボタンの押下状態を返す。エミュレーションゴルーチンが呼ぶ。
func (s *InputState) Buttons(port int) uint8 {
	if port < 0 || port >= input.PortCount {
		return 0
	}
	return uint8(s.ports[port].Load())
}

// Set は port のボタンの押下状態を置き換える。UI スレッドが呼ぶ。
func (s *InputState) Set(port int, buttons uint8) {
	if port < 0 || port >= input.PortCount {
		return
	}
	s.ports[port].Store(uint32(buttons))
}

// SetButton は port の 1 つのボタンの押下状態を変える。
//
// 読んで変えて書くため、同じポートを複数のゴルーチンから操作すると
// 取りこぼす。キーイベントを扱うのは UI スレッドだけである。
func (s *InputState) SetButton(port int, bit uint8, pressed bool) {
	if port < 0 || port >= input.PortCount {
		return
	}
	v := s.Buttons(port)
	if pressed {
		v |= bit
	} else {
		v &^= bit
	}
	s.Set(port, v)
}

// SetAction はプレイヤー入力のアクションを押下状態へ反映する。
//
// アクションがプレイヤー入力でないときは false を返す。呼び出し側は
// その場合にホットキーとして処理する。
func (s *InputState) SetAction(a config.Action, pressed bool) bool {
	port, bit, ok := PlayerButton(a)
	if !ok {
		return false
	}
	s.SetButton(port, bit, pressed)
	return true
}

// Clear はすべてのボタンを離した状態にする。
//
// ウィンドウがフォーカスを失ったときに呼ぶ。キーを押したまま別の
// ウィンドウへ移ると、離したことが通知されず押しっぱなしになる。
func (s *InputState) Clear() {
	for i := range s.ports {
		s.ports[i].Store(0)
	}
}

// buttonBits はボタン名からコントローラのビットへの対応。
//
// 添字は config.PlayerButtonOrder の並びに合わせる。
var buttonBits = map[config.ButtonName]uint8{
	config.ButtonA:      input.ButtonA,
	config.ButtonB:      input.ButtonB,
	config.ButtonSelect: input.ButtonSelect,
	config.ButtonStart:  input.ButtonStart,
	config.ButtonUp:     input.ButtonUp,
	config.ButtonDown:   input.ButtonDown,
	config.ButtonLeft:   input.ButtonLeft,
	config.ButtonRight:  input.ButtonRight,
}

// PlayerButton はプレイヤー入力のアクションをポート番号（0 起点）と
// コントローラのビットに対応づける。
//
// この対応を emu に置くのは、internal/config がエミュレーションコアを
// 参照しないためである（設計書 01 編 §1.4）。
func PlayerButton(a config.Action) (port int, bit uint8, ok bool) {
	name, ok := a.ButtonName()
	if !ok {
		return 0, 0, false
	}
	bit, ok = buttonBits[name]
	if !ok {
		return 0, 0, false
	}
	return a.Player() - 1, bit, true
}
