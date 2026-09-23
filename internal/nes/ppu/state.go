package ppu

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// SaveState は状態を書く。
//
// region・bus・frame・queue を除く全フィールドを保存する。これらは
// エミュレーション状態ではなく、組み立て時に決まる参照である。
func (p *PPU) SaveState(w *state.Writer) {
	end := w.Section("ppu")

	endReg := w.Section("registers")
	w.U8(uint8(p.ctrl))
	w.U8(uint8(p.mask))
	w.U8(uint8(p.status))
	w.U8(p.oamAddr)
	w.U16(p.v)
	w.U16(p.t)
	w.U8(p.x)
	w.Bool(p.w)
	w.U8(p.readBuffer)
	w.U8(p.ioLatch)
	w.U64(p.dots)
	for _, e := range p.latchExpiry {
		w.U64(e)
	}
	endReg()

	endPos := w.Section("position")
	w.Int(p.scanline)
	w.Int(p.dot)
	w.Bool(p.oddFrame)
	w.U64(p.frames)
	w.U16(p.busAddr)
	endPos()

	// 復元直後の 1 タイル分の描画が崩れないように、フェッチ結果と
	// シフタをすべて保存する。
	endFetch := w.Section("fetch")
	w.U8(p.ntLatch)
	w.U8(p.atLatch)
	w.U8(p.bgLoLatch)
	w.U8(p.bgHiLatch)
	w.U16(p.bgShiftLo)
	w.U16(p.bgShiftHi)
	w.U8(p.atShiftLo)
	w.U8(p.atShiftHi)
	w.Bool(p.atLatchLo)
	w.Bool(p.atLatchHi)
	endFetch()

	endSprite := w.Section("sprites")
	w.RawBytes(p.secondary[:])
	w.Int(p.spriteCount)
	w.Bool(p.sprite0OnNext)
	w.Bool(p.sprite0OnCurrent)
	for i := range p.sprites {
		u := &p.sprites[i]
		w.U8(u.patternLo)
		w.U8(u.patternHi)
		w.U8(u.attr)
		w.U8(u.xCounter)
		w.Bool(u.active)
	}
	// 評価の途中の状態を保存する。省くとオーバーフローフラグを
	// タイミング源に使うゲームで復元後の挙動が変わる。
	w.U8(p.eval.addr)
	w.U8(uint8(p.eval.step))
	w.Int(p.eval.copied)
	w.Int(p.eval.overflowCopied)
	w.Bool(p.eval.writeDisable)
	w.U8(p.eval.latch)
	endSprite()

	endMem := w.Section("memory")
	w.RawBytes(p.oam[:])
	w.RawBytes(p.palette[:])
	w.RawBytes(p.ciram[:])
	endMem()

	endMisc := w.Section("misc")
	w.U32(p.warmupDots)
	w.Bool(p.nmiLine)
	w.Bool(p.suppressVBlank)
	w.Bool(p.skipDot)
	endMisc()

	end()
}

// LoadState は状態を読む。
func (p *PPU) LoadState(r *state.Reader) error {
	end := r.RequireSection("ppu")

	endReg := r.RequireSection("registers")
	p.ctrl = Control(r.U8())
	p.mask = Mask(r.U8())
	p.status = Status(r.U8())
	p.oamAddr = r.U8()
	p.v = r.U16()
	p.t = r.U16()
	p.x = r.U8()
	p.w = r.Bool()
	p.readBuffer = r.U8()
	p.ioLatch = r.U8()
	p.dots = r.U64()
	for i := range p.latchExpiry {
		p.latchExpiry[i] = r.U64()
	}
	p.updateMinLatchExpiry()
	endReg()

	endPos := r.RequireSection("position")
	p.scanline = r.Int()
	p.dot = r.Int()
	p.oddFrame = r.Bool()
	p.frames = r.U64()
	p.busAddr = r.U16()
	endPos()

	endFetch := r.RequireSection("fetch")
	p.ntLatch = r.U8()
	p.atLatch = r.U8()
	p.bgLoLatch = r.U8()
	p.bgHiLatch = r.U8()
	p.bgShiftLo = r.U16()
	p.bgShiftHi = r.U16()
	p.atShiftLo = r.U8()
	p.atShiftHi = r.U8()
	p.atLatchLo = r.Bool()
	p.atLatchHi = r.Bool()
	endFetch()

	endSprite := r.RequireSection("sprites")
	r.RawBytes(p.secondary[:])
	p.spriteCount = r.Int()
	p.sprite0OnNext = r.Bool()
	p.sprite0OnCurrent = r.Bool()
	for i := range p.sprites {
		u := &p.sprites[i]
		u.patternLo = r.U8()
		u.patternHi = r.U8()
		u.attr = r.U8()
		u.xCounter = r.U8()
		u.active = r.Bool()
	}
	p.eval.addr = r.U8()
	p.eval.step = evalStep(r.U8())
	p.eval.copied = r.Int()
	p.eval.overflowCopied = r.Int()
	p.eval.writeDisable = r.Bool()
	p.eval.latch = r.U8()
	endSprite()

	endMem := r.RequireSection("memory")
	r.RawBytes(p.oam[:])
	r.RawBytes(p.palette[:])
	r.RawBytes(p.ciram[:])
	endMem()

	endMisc := r.RequireSection("misc")
	p.warmupDots = r.U32()
	p.nmiLine = r.Bool()
	p.suppressVBlank = r.Bool()
	p.skipDot = r.Bool()
	endMisc()

	end()
	return r.Err()
}
