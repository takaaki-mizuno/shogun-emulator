package testrom

import (
	"errors"
	"fmt"
)

// Machine はテスト ROM ランナーが必要とする操作。
//
// エミュレーションコアの型を直接参照しないのは、コアの型が固まる前から
// テストの枠組みを用意するためである。フェーズ 1 以降、*nes.NES がこの
// インタフェースを満たす。
type Machine interface {
	// RunFrame は 1 フレーム分進める。
	RunFrame()
	// Reset はリセットを掛ける。電源は切らない。
	Reset()
	// Peek は観測のためにメモリを読む。
	//
	// 通常の読み出しではなく Peek を使う。テストの観測が
	// エミュレーションの状態を変えてはならない。
	Peek(addr uint16) uint8
}

// blargg 形式のテスト ROM が結果を書き出すアドレス。
const (
	// statusAddr は実行状態と結果コードを置くアドレス。
	statusAddr = 0x6000
	// signatureAddr はシグネチャの先頭アドレス。
	signatureAddr = 0x6001
	// messageAddr は結果の文字列の先頭アドレス。
	messageAddr = 0x6004

	// statusRunning は実行中を表す値。
	statusRunning = 0x80
	// statusNeedsReset はリセットを求める値。
	statusNeedsReset = 0x81

	// maxMessageLen は読み取る文字列の上限。
	//
	// 上限を設けるのは、ゼロ終端が現れないまま読み続けることを避けるため
	// である。テスト ROM が $6000 の領域を壊した場合に起こりうる。
	maxMessageLen = 1024

	// resetDelayFrames はリセットを求められたときに待つフレーム数。
	//
	// 実機で 100 ms 以上待つ必要がある。60 フレームで約 1 秒になるため、
	// 10 フレームで足りる。
	resetDelayFrames = 10
)

// signature は結果が書かれたことを示すバイト列。
var signature = [3]uint8{0xDE, 0xB0, 0x61}

// ErrTimeout は制限フレーム数までに結果が出なかったことを表す。
var ErrTimeout = errors.New("testrom: 制限フレーム数までに結果が出なかった")

// Result はテスト ROM の結果。
type Result struct {
	// Code は $6000 の値。0 のとき合格。
	Code uint8
	// Message は $6004 以降のゼロ終端文字列。
	Message string
	// Frames は結果が出るまでに進めたフレーム数。
	Frames int
	// Resets は途中でリセットを掛けた回数。
	Resets int
}

// OK は合格かを返す。
func (r Result) OK() bool { return r.Code == 0 }

// String は結果を人間が読める形にする。
func (r Result) String() string {
	if r.OK() {
		return fmt.Sprintf("合格（%d フレーム）", r.Frames)
	}
	if r.Message == "" {
		return fmt.Sprintf("コード %d（%d フレーム）", r.Code, r.Frames)
	}
	return fmt.Sprintf("コード %d: %s（%d フレーム）", r.Code, r.Message, r.Frames)
}

// RunBlargg は blargg 形式のテスト ROM を実行し、結果を返す。
//
// $6001-$6003 にシグネチャが書かれるまで結果を読まない。電源投入直後の
// 未初期化の値を結果として読まないためである。$6000 が $80 のあいだ実行を
// 続け、$81 のときリセットを掛ける。
func RunBlargg(m Machine, timeoutFrames int) (Result, error) {
	var resets int
	// justReset はリセットの直後を表す。$6000 の値はプログラムが書き換える
	// まで $81 のままであり、値が変わるのを待たずに再度リセットすると、
	// レジスタを設定し終える前にリセットが入る。
	justReset := false

	for frame := range timeoutFrames {
		m.RunFrame()
		if !hasSignature(m) {
			continue
		}
		s := m.Peek(statusAddr)
		if justReset {
			if s == statusNeedsReset {
				continue
			}
			justReset = false
		}
		switch s {
		case statusRunning:
			continue
		case statusNeedsReset:
			RunFrames(m, resetDelayFrames)
			m.Reset()
			resets++
			justReset = true
		default:
			return Result{
				Code:    s,
				Message: readMessage(m),
				Frames:  frame + 1,
				Resets:  resets,
			}, nil
		}
	}
	return Result{Frames: timeoutFrames, Resets: resets}, ErrTimeout
}

// RunFrames は n フレーム進める。
func RunFrames(m Machine, n int) {
	for range n {
		m.RunFrame()
	}
}

// hasSignature は結果が書かれたことを示すシグネチャがあるかを返す。
func hasSignature(m Machine) bool {
	for i, want := range signature {
		if m.Peek(signatureAddr+uint16(i)) != want {
			return false
		}
	}
	return true
}

// readMessage は $6004 からゼロ終端までを読む。
func readMessage(m Machine) string {
	buf := make([]byte, 0, 64)
	for i := range maxMessageLen {
		c := m.Peek(messageAddr + uint16(i))
		if c == 0 {
			break
		}
		buf = append(buf, c)
	}
	return string(buf)
}
