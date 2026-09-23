package input

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// StandardController は標準コントローラ。
//
// 内部は 8 bit のシフトレジスタである。strobe が high の間はボタンから
// 継続的にロードされ、low にすると 1 回の読み出しごとに 1 bit ずつ出る。
type StandardController struct {
	src  Source
	port int

	// buttons は直近に取り込んだ押下状態。
	buttons uint8
	// shiftReg は読み出しで出ていくビット列。
	shiftReg uint8
	// strobe は $4016 の bit 0 の状態。
	strobe bool
}

// NewStandardController は port のコントローラを作る。
//
// src が nil のときは何も押されていない供給元を使う。テストと、入力を
// 繋がない実行（テスト ROM ランナー）のためである。
func NewStandardController(src Source, port int) *StandardController {
	if src == nil {
		src = NoSource{}
	}
	return &StandardController{src: src, port: port}
}

// SetSource は押下状態の供給元を差し替える。
//
// 入力ムービーの再生を始めるときと終えるときに呼ぶ。
func (c *StandardController) SetSource(src Source) {
	if src == nil {
		src = NoSource{}
	}
	c.src = src
}

// Strobe は $4016 への書き込みの下位 3 bit を受け取る。
func (c *StandardController) Strobe(v uint8) {
	high := v&1 != 0
	if high {
		c.reload()
	}
	c.strobe = high
}

// Read は 1 bit を返す。
//
// strobe が high の間は取り込み直して A の状態を返し続ける。シフトは
// 行わない。low のときは 1 bit 出してシフトし、空いた上位ビットへ 1 を
// 入れる。これにより 8 回読み終えた後は 1 が返る。
func (c *StandardController) Read() uint8 {
	if c.strobe {
		c.reload()
	}
	v := c.shiftReg & 1
	if !c.strobe {
		c.shiftReg = c.shiftReg>>1 | 0x80
	}
	return v
}

// Peek は副作用を発生させずに次に読まれる値を返す。
//
// 供給元を参照しない。デバッガの表示のために UI スレッドの値を読むと、
// エミュレーションの経過と無関係に表示が変わる。
func (c *StandardController) Peek() uint8 {
	if c.strobe {
		return c.buttons & 1
	}
	return c.shiftReg & 1
}

// Kind はデバイス種別の名前を返す。
func (c *StandardController) Kind() string { return "standard" }

// reload は供給元から押下状態を取り込む。
func (c *StandardController) reload() {
	c.buttons = c.src.Buttons(c.port)
	c.shiftReg = c.buttons
}

// SaveState は状態を書く。供給元は書かない。
func (c *StandardController) SaveState(w *state.Writer) {
	end := w.Section("standard")
	w.U8(c.buttons)
	w.U8(c.shiftReg)
	w.Bool(c.strobe)
	end()
}

// LoadState は状態を読む。
func (c *StandardController) LoadState(r *state.Reader) error {
	end := r.RequireSection("standard")
	c.buttons = r.U8()
	c.shiftReg = r.U8()
	c.strobe = r.Bool()
	end()
	return r.Err()
}
