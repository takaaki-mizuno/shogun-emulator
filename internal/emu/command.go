package emu

import (
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// command は外部からエミュレーションゴルーチンへ渡す要求。
//
// 命令境界でのみ処理する。命令の途中では CPU の状態がホストの
// コールスタックにあり、状態を触れない（設計書 08 編 §8.3）。
type command interface{ isCommand() }

// cmdLoadMachine は組み立て済みの本体を差し替える。
//
// 本体の組み立て（ROM の読み込みと電源投入）は呼び出し側で行い、
// 所有権をチャネル経由で渡す。エミュレーションゴルーチン以外が
// 実行中の本体を触らないためである。
type cmdLoadMachine struct {
	machine *nes.NES
	// battery は不揮発メモリの保存先。持たない ROM では nil。
	battery *battery
	name    string
	// path は ROM のファイルのパス。分からないとき空。.dbg と Game State
	// Definition を探すために使う。
	path string
	done chan error
}

// cmdUnload は本体を取り外す。
type cmdUnload struct{ done chan error }

// cmdSetInput はポートの押下状態を設定する。
//
// 通常の入力は InputState を介して渡す。このコマンドは入力ムービーの
// 再生のように、フレームの境界で確実に反映させたい場合に使う。
type cmdSetInput struct {
	port    int
	buttons uint8
}

// cmdReset はリセットする。hard が true のとき電源を入れ直す。
type cmdReset struct {
	hard bool
	done chan error
}

// cmdSetSpeed は速度倍率を変える。
type cmdSetSpeed struct{ factor float64 }

// cmdSetMuted は消音を切り替える。
type cmdSetMuted struct{ muted bool }

// cmdPause は一時停止と再開を切り替える。
type cmdPause struct{ paused bool }

// cmdStep は一時停止中に指定した単位だけ進める。
type cmdStep struct {
	kind  StepKind
	count int
	// addr は RunToCursor の目標アドレス。
	addr uint16
	// done は止まったときに結果を受け取る。nil のとき知らせない
	// （GUI の操作）。容量 1 のチャネルを渡す。
	done chan StepResult
	// input は進める間のポート 1 と 2 の入力。nil のとき変えない。
	// エージェントの入力の Instance でだけ使う。
	input *[2]uint8
	// cond は止める条件。nil のとき使わない。instr が true なら命令境界
	// ごと、false ならフレームの開始ごとに判定する。StepFrame でだけ使う。
	cond  func() bool
	instr bool
	// noBreak はブレークポイントで止まらないことを表す。
	noBreak bool
}

// cmdAgentInput はエージェントの入力の経路を切り替える。
type cmdAgentInput struct{ on bool }

// cmdSaveState は状態を保存する。
type cmdSaveState struct {
	out chan saveResult
	// screenshot は保存時の画面を添えるかどうか。
	screenshot bool
}

// saveResult は保存の結果。
type saveResult struct {
	data []byte
	// pc は保存した命令境界のプログラムカウンタ。
	pc  uint16
	err error
}

// cmdLoadState は状態を復元する。
type cmdLoadState struct {
	data []byte
	done chan error
}

// cmdRewind は巻き戻す。
type cmdRewind struct {
	frames int
	done   chan error
}

// cmdStartRecording はムービーの記録を始める。
type cmdStartRecording struct {
	path string
	done chan error
}

// cmdStopRecording はムービーの記録を止めて書き出す。
type cmdStopRecording struct{ done chan error }

// cmdPlayMovie はムービーの再生を始める。
type cmdPlayMovie struct {
	m    *movie.Movie
	done chan error
}

// cmdStopMovie はムービーの記録と再生を止める。
type cmdStopMovie struct{ done chan error }

// cmdSetRewinding は押している間の巻き戻しを切り替える。
type cmdSetRewinding struct{ on bool }

// cmdFunc は命令境界で任意の処理を実行する。
//
// デバッガとスクリーンショットが、命令境界の一貫した状態を読むために使う。
type cmdFunc struct {
	fn   func(*nes.NES)
	done chan struct{}
}

func (cmdLoadMachine) isCommand()    {}
func (cmdUnload) isCommand()         {}
func (cmdSetInput) isCommand()       {}
func (cmdReset) isCommand()          {}
func (cmdSetSpeed) isCommand()       {}
func (cmdSetMuted) isCommand()       {}
func (cmdPause) isCommand()          {}
func (cmdStep) isCommand()           {}
func (cmdAgentInput) isCommand()     {}
func (cmdSaveState) isCommand()      {}
func (cmdLoadState) isCommand()      {}
func (cmdFunc) isCommand()           {}
func (cmdRewind) isCommand()         {}
func (cmdStartRecording) isCommand() {}
func (cmdStopRecording) isCommand()  {}
func (cmdPlayMovie) isCommand()      {}
func (cmdStopMovie) isCommand()      {}
func (cmdSetRewinding) isCommand()   {}

// commandDone は完了を伝えるチャネルを返す。持たないコマンドでは nil。
//
// apply がこれを使う。1 か所にまとめるのは、コマンドを増やしたときの
// 追加漏れを型スイッチの網羅として見つけやすくするためである。
func commandDone(c command) chan error {
	switch v := c.(type) {
	case cmdLoadMachine:
		return v.done
	case cmdUnload:
		return v.done
	case cmdReset:
		return v.done
	case cmdLoadState:
		return v.done
	case cmdRewind:
		return v.done
	case cmdStartRecording:
		return v.done
	case cmdStopRecording:
		return v.done
	case cmdPlayMovie:
		return v.done
	case cmdStopMovie:
		return v.done
	case cmdReplay:
		return v.done
	}
	return nil
}
