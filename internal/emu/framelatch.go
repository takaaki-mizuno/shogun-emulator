package emu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/input"

// frameLatch はフレームの開始時に取り込んだ押下状態。
//
// コントローラはこの値を読む。フレームの途中で値が変わらないため、
// 同じ入力からは常に同じ結果になる。入力ムービーのフレーム N の入力が
// 「フレーム N の scanline 0 / dot 0 でラッチされ、そのフレーム中は
// 変化しない」という定めを満たす（設計書 08 編 §8.7.1）。
//
// エミュレーションゴルーチンだけが書き換える。UI スレッドと共有する
// InputState をコントローラへ直接渡すと、フレームの途中で値が変わり、
// 記録した入力と再生した結果が一致しなくなる。
type frameLatch struct {
	buttons [input.PortCount]uint8
}

// Buttons は port のラッチした押下状態を返す。
func (l *frameLatch) Buttons(port int) uint8 {
	if port < 0 || port >= input.PortCount {
		return 0
	}
	return l.buttons[port]
}

// set はラッチの内容を差し替える。
func (l *frameLatch) set(b [input.PortCount]uint8) { l.buttons = b }

// get はラッチの内容を返す。
func (l *frameLatch) get() [input.PortCount]uint8 { return l.buttons }
