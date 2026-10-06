package debug

// PPU レジスタへの書き込みの記録（設計書 14 編 §14.10.5）。
//
// 画面分割やスクロールの不具合は、どのスキャンラインで $2000・$2005・
// $2006 へ書いたかで決まる。エージェントが自力で原因を見つけられるよう、
// 直前に完成したフレームの書き込みを時刻つきで返す。

// ppuLogLimit は 1 フレームに記録する上限。
const ppuLogLimit = 512

// ppuLogIdleFrames は要求が無いまま記録を続けるフレーム数。これを過ぎたら
// フックを外す。
const ppuLogIdleFrames = 600

// PPUWrite は PPU レジスタへの書き込み 1 件。
type PPUWrite struct {
	// Reg はミラーを畳んだアドレス（$2000–$2007、$4014）。
	Reg      uint16
	Value    uint8
	Scanline int
	Dot      int
	// PC は書き込んだ命令の先頭のアドレス。
	PC uint16
}

// PPUWriteFrame は 1 フレーム分の記録。
type PPUWriteFrame struct {
	Frame  uint64
	Writes []PPUWrite
	// Truncated は上限を超えて捨てた書き込みがあることを表す。
	Truncated bool
	// Complete はフレームの最初から記録したことを表す。記録を始めた
	// 直後のフレームは false。
	Complete bool
}

// ppuWriteLog は記録の状態。エミュレーションゴルーチンだけが触る。
type ppuWriteLog struct {
	cur, last   PPUWriteFrame
	hasLast     bool
	idle        int
	instrPC     uint16
	curComplete bool
}

// isLoggedPPUReg は記録の対象のレジスタかを返し、畳んだアドレスを返す。
// $2003（OAMADDR）、$2000・$2001・$2005・$2006、$4014 を対象とする。
// $2007 は 1 フレームに数百回書かれ、他の記録を埋めるため対象にしない。
func isLoggedPPUReg(addr uint16) (uint16, bool) {
	if addr == 0x4014 {
		return addr, true
	}
	if addr < 0x2000 || addr >= 0x4000 {
		return 0, false
	}
	r := 0x2000 | addr&7
	switch r {
	case 0x2000, 0x2001, 0x2003, 0x2005, 0x2006:
		return r, true
	}
	return 0, false
}

// RequestPPUWrites は PPU 書き込みの記録を有効にし、直前に完成した
// フレームの記録を返す。記録が無いとき ok は false。
//
// 呼ぶたびに無効にするまでのフレーム数を数え直す。エミュレーション
// ゴルーチンで呼ぶ。
func (d *Debugger) RequestPPUWrites() (PPUWriteFrame, bool) {
	if d.ppuLog == nil {
		d.ppuLog = &ppuWriteLog{}
		d.updateHooks()
	}
	l := d.ppuLog
	l.idle = 0
	if !l.hasLast {
		return PPUWriteFrame{}, false
	}
	out := l.last
	out.Writes = append([]PPUWrite(nil), l.last.Writes...)
	return out, true
}

// PPUWriteLogEnabled は記録が有効かを返す。
func (d *Debugger) PPUWriteLogEnabled() bool { return d.ppuLog != nil }

// recordPPUWrite は書き込みを記録する。
func (d *Debugger) recordPPUWrite(addr uint16, v uint8) {
	reg, ok := isLoggedPPUReg(addr)
	if !ok {
		return
	}
	l := d.ppuLog
	if len(l.cur.Writes) >= ppuLogLimit {
		l.cur.Truncated = true
		return
	}
	l.cur.Writes = append(l.cur.Writes, PPUWrite{
		Reg: reg, Value: v, Scanline: d.n.PPU.Scanline(), Dot: d.n.PPU.Dot(), PC: l.instrPC,
	})
}

// finishPPUFrame はフレームの完成で記録を入れ替える。要求が無いまま
// ppuLogIdleFrames を過ぎたら記録をやめ、フックを外す。
func (d *Debugger) finishPPUFrame() {
	l := d.ppuLog
	l.cur.Frame = d.n.Frames()
	l.cur.Complete = l.curComplete
	l.last, l.cur = l.cur, PPUWriteFrame{Writes: l.last.Writes[:0]}
	l.hasLast = true
	l.curComplete = true
	l.idle++
	if l.idle >= ppuLogIdleFrames {
		d.ppuLog = nil
		d.updateHooks()
	}
}
