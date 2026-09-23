package ppu

// スプライトの評価とフェッチ。
//
// 評価は「範囲内のスプライトを 8 個集める」という結果だけを書かず、
// 調査結果の手順 1 から 4 をそのままステートマシンにする。結果だけを
// 実装するとオーバーフローフラグのバグが再現されず、フラグをタイミング源に
// 使うゲームが動かない。

// spriteUnit は 1 つのスプライト出力ユニット。
type spriteUnit struct {
	patternLo uint8
	patternHi uint8
	attr      uint8
	// xCounter は出力を始めるまでの残りドット数。
	xCounter uint8
	// active はこのユニットが実在するスプライトを持つかを表す。
	active bool
}

// evalStep は評価のステートマシンの手順。
type evalStep uint8

const (
	// evalCopyY は手順 1。Y 座標を読んで空きスロットへ入れる。
	evalCopyY evalStep = iota
	// evalCopyRest は手順 1a。範囲内だった残りの 3 バイトを写す。
	evalCopyRest
	// evalOverflow は手順 3。8 個そろった後の探索。
	evalOverflow
	// evalDone は手順 4。64 個を見終えた後の空回り。
	evalDone
)

// evalState はスプライト評価の途中の状態。
type evalState struct {
	// addr は次に読む OAM のアドレス。
	addr uint8
	step evalStep
	// copied は現在のスプライトで写したバイト数。
	copied int
	// overflowCopied は手順 3a で読み進めたバイト数。
	overflowCopied int
	// writeDisable は secondary OAM が満杯であることを表す。
	writeDisable bool
	// latch は奇数ドットで読んだ値。
	latch uint8
}

// secondarySize は secondary OAM の大きさ。8 スプライト分。
const secondarySize = 32

// maxSpritesPerLine は 1 走査線に表示できるスプライトの数。
const maxSpritesPerLine = 8

// spriteDot は走査線内のスプライト処理を 1 ドット分進める。
//
// visible が false のときはプリレンダー行であり、評価を行わない。評価は
// 次の行の描画に効くため、最初の走査線にスプライトが描かれない。
func (p *PPU) spriteDot(visible bool) {
	switch {
	case p.dot == 0:
		// 走査線の開始。前の行の評価結果を現在行へ移す。
		p.sprite0OnCurrent = p.sprite0OnNext
		p.sprite0OnNext = false
	case p.dot <= 64:
		p.clearSecondaryDot()
	case p.dot <= 256:
		if visible {
			p.evaluateSpritesDot()
		}
	case p.dot <= 320:
		p.fetchSpriteDot()
	}
}

// clearSecondaryDot は dot 1-64 で secondary OAM を $FF で埋める。
//
// 偶数ドットで 1 バイトずつ書く。この期間の $2004 の読み出しが $FF を
// 返すのは、内部で「OAM を読んで secondary OAM に書く」処理が動いており、
// 読み出しを $FF に固定する信号が有効になっているためである。
func (p *PPU) clearSecondaryDot() {
	if p.dot&1 == 0 {
		p.secondary[(p.dot-1)/2] = 0xFF
	}
	if p.dot == 1 {
		p.spriteCount = 0
		p.eval = evalState{}
	}
	if p.dot == 64 {
		// 評価は dot 65 の時点の oamAddr から始まる。
		p.eval.addr = p.oamAddr
	}
}

// inRange はスプライトの Y 座標が現在の走査線に掛かるかを返す。
func (p *PPU) inRange(y uint8) bool {
	diff := p.scanline - int(y)
	return diff >= 0 && diff < p.ctrl.SpriteHeight()
}

// evaluateSpritesDot は dot 65-256 の評価を 1 ドット分進める。
//
// 奇数ドットで primary OAM から読み、偶数ドットで secondary OAM へ書く。
// secondary OAM が満杯のときは書く代わりに読む。
func (p *PPU) evaluateSpritesDot() {
	if p.dot&1 == 1 {
		p.eval.latch = p.oam[p.eval.addr]
		return
	}

	switch p.eval.step {
	case evalCopyY:
		p.evalStepCopyY()
	case evalCopyRest:
		p.evalStepCopyRest()
	case evalOverflow:
		p.evalStepOverflow()
	case evalDone:
		// 手順 4。コピーを試みて失敗し、スプライト番号を進める。
		// HBLANK までこれを繰り返す。手順 1 へは戻らない。
		p.eval.addr = (p.eval.addr + 4) & 0xFC
	}
}

// evalStepCopyY は手順 1 と 2。Y 座標を空きスロットへ入れる。
func (p *PPU) evalStepCopyY() {
	y := p.eval.latch
	if !p.eval.writeDisable {
		p.secondary[p.spriteCount*4] = y
	}

	if p.inRange(y) {
		// 手順 1a。残りの 3 バイトも写す。
		if p.eval.addr>>2 == 0 {
			p.sprite0OnNext = true
		}
		p.eval.step = evalCopyRest
		p.eval.copied = 1
		p.eval.addr++
		return
	}

	// 手順 2。次のスプライトへ進む。
	p.advanceSprite()
}

// evalStepCopyRest は手順 1a。範囲内のスプライトの残り 3 バイトを写す。
func (p *PPU) evalStepCopyRest() {
	if !p.eval.writeDisable {
		p.secondary[p.spriteCount*4+p.eval.copied] = p.eval.latch
	}
	p.eval.copied++
	p.eval.addr++

	if p.eval.copied < 4 {
		return
	}

	// 1 スプライト分を写し終えた。
	if !p.eval.writeDisable {
		p.spriteCount++
	}
	p.eval.copied = 0

	if p.eval.addr == 0 {
		// 手順 2a。64 個すべてを見た。
		p.eval.step = evalDone
		return
	}
	if p.spriteCount < maxSpritesPerLine {
		// 手順 2b。
		p.eval.step = evalCopyY
		return
	}
	// 手順 2c。ちょうど 8 個そろった。以降 secondary OAM への書き込みを
	// 読み出しに変える。
	p.eval.writeDisable = true
	p.eval.step = evalOverflow
	p.eval.overflowCopied = 0
}

// evalStepOverflow は手順 3。8 個そろった後の探索。
//
// 範囲内と判定したとき、次の 3 エントリを読み進める。範囲外のときは
// スプライト番号とバイト位置の両方を桁上がりなしで進める。この
// バイト位置のインクリメントが実機のバグであり、OAM を斜めに走査して
// タイル番号・属性・X 座標を Y 座標として評価してしまう。
func (p *PPU) evalStepOverflow() {
	if p.eval.overflowCopied > 0 {
		// 手順 3a の続き。次の 3 バイトを読み進める。
		p.eval.overflowCopied++
		p.eval.addr++
		if p.eval.overflowCopied >= 4 {
			p.eval.overflowCopied = 0
		}
		if p.eval.addr == 0 {
			p.eval.step = evalDone
		}
		return
	}

	if p.inRange(p.eval.latch) {
		// 手順 3a。オーバーフローフラグを立てる。
		p.status |= StatusSpriteOverflow
		p.eval.overflowCopied = 1
		p.eval.addr++
		if p.eval.addr == 0 {
			p.eval.step = evalDone
		}
		return
	}

	// 手順 3b。n と m を桁上がりなしで進める。
	sprite := int(p.eval.addr >> 2)
	byteIndex := int(p.eval.addr&0x03+1) & 0x03
	sprite++
	if sprite > 63 {
		// n が 0 にオーバーフローした。
		p.eval.addr = uint8(byteIndex)
		p.eval.step = evalDone
		return
	}
	p.eval.addr = uint8(sprite<<2 | byteIndex)
}

// advanceSprite は次のスプライトの Y 座標へアドレスを進める。
func (p *PPU) advanceSprite() {
	p.eval.addr = (p.eval.addr + 4) & 0xFC
	if p.eval.addr == 0 {
		// 手順 2a。64 個すべてを見た。
		p.eval.step = evalDone
		return
	}
	if p.spriteCount >= maxSpritesPerLine {
		p.eval.writeDisable = true
		p.eval.step = evalOverflow
		p.eval.overflowCopied = 0
		return
	}
	p.eval.step = evalCopyY
}

// fetchSpriteDot は dot 257-320 のスプライトフェッチを 1 ドット分進める。
//
// 1 スプライトあたり 8 ドットで 4 回のアクセスを行う。最初の 2 回は値を
// 使わないネームテーブルのフェッチである。範囲内のスプライトが 8 個
// 未満のときもフェッチを行う。マッパーが監視する A12 の遷移を実機と
// 同じ回数起こすためである。
func (p *PPU) fetchSpriteDot() {
	slot := (p.dot - 257) / 8
	phase := (p.dot - 257) % 8

	switch phase {
	case 0, 2:
		p.fetchAddress(p.nametableAddress())
	case 1:
		p.fetchValue()
	case 3:
		p.fetchValue()
		// 属性と X 座標を secondary OAM からロードする
		p.loadSpriteAttributes(slot)
	case 4:
		p.fetchAddress(p.spritePatternAddress(slot, 0))
	case 5:
		p.loadSpritePattern(slot, false)
	case 6:
		p.fetchAddress(p.spritePatternAddress(slot, 8))
	case 7:
		p.loadSpritePattern(slot, true)
	}
}

// loadSpriteAttributes は属性と X 座標をユニットへ入れる。
func (p *PPU) loadSpriteAttributes(slot int) {
	u := &p.sprites[slot]
	u.attr = p.secondary[slot*4+2]
	u.xCounter = p.secondary[slot*4+3]
	u.active = slot < p.spriteCount
}

// spritePatternAddress はスプライトのパターンのアドレスを返す。
//
// 8x16 ではタイル番号の bit 0 がパターンテーブルを選び、bit 7-1 が
// 上半分のタイルを選ぶ。垂直反転では行を反転させることで、2 つの
// 副タイルの反転と位置の入れ替えが同時に起こる。
func (p *PPU) spritePatternAddress(slot int, plane uint16) uint16 {
	height := p.ctrl.SpriteHeight()
	y := p.secondary[slot*4]
	tile := p.secondary[slot*4+1]
	attr := p.secondary[slot*4+2]

	row := p.scanline - int(y)
	if slot >= p.spriteCount || row < 0 || row >= height {
		// 空きスロットはタイル $FF へのダミーフェッチを行う。
		row = 0
		tile = 0xFF
	}
	if attr&0x80 != 0 {
		row = height - 1 - row
	}

	if height == 8 {
		return p.ctrl.SpritePatternBase() | uint16(tile)<<4 | uint16(row) | plane
	}

	base := uint16(tile&0x01) << 12
	t := tile & 0xFE
	if row >= 8 {
		t++
		row -= 8
	}
	return base | uint16(t)<<4 | uint16(row) | plane
}

// loadSpritePattern はフェッチしたパターンをユニットへ入れる。
//
// 水平反転はここでビット順を逆にする。出力時は常に bit 7 を見て左へ
// シフトすればよくなる。
func (p *PPU) loadSpritePattern(slot int, high bool) {
	v := p.fetchValue()
	u := &p.sprites[slot]

	if slot >= p.spriteCount {
		// 空きスロットは透明な値を入れる。
		v = 0
	} else if u.attr&0x40 != 0 {
		v = reverseBits(v)
	}

	if high {
		u.patternHi = v
		return
	}
	u.patternLo = v
}

// reverseBits はビット順を逆にする。
func reverseBits(v uint8) uint8 {
	v = v>>4 | v<<4
	v = (v&0xCC)>>2 | (v&0x33)<<2
	v = (v&0xAA)>>1 | (v&0x55)<<1
	return v
}

// spritePixel はスプライトのピクセルを返す。
//
// 若いインデックスのユニットの不透明値を優先する。若いアドレスの
// スプライトが若い出力ユニットに割り当てられ、若いユニットの出力が
// 優先されるよう配線されているためである。
func (p *PPU) spritePixel() (value, attr uint8, index int) {
	if !p.mask.SpritesEnabled() {
		return 0, 0, -1
	}
	if !p.mask.ShowSpritesLeft() && p.dot-1 < 8 {
		return 0, 0, -1
	}

	for i := range p.sprites {
		u := &p.sprites[i]
		if u.xCounter > 0 || !u.active {
			continue
		}
		var pattern uint8
		if u.patternLo&0x80 != 0 {
			pattern |= 0x01
		}
		if u.patternHi&0x80 != 0 {
			pattern |= 0x02
		}
		if pattern == 0 {
			continue
		}
		return u.attr&0x03<<2 | pattern, u.attr, i
	}
	return 0, 0, -1
}

// shiftSprites は各ユニットのカウンタを進める。
//
// 出力が始まっていないユニットはカウンタを減らし、始まったユニットは
// パターンを 1 bit シフトする。
func (p *PPU) shiftSprites() {
	for i := range p.sprites {
		u := &p.sprites[i]
		if u.xCounter > 0 {
			u.xCounter--
			continue
		}
		u.patternLo <<= 1
		u.patternHi <<= 1
	}
}

// multiplex は背景とスプライトの優先度を決め、5 bit のパレット添字を返す。
//
// 決定表は設計書 04 編 §4.6 のとおり。最上位ビットが背景 0 / スプライト 1。
func (p *PPU) multiplex(bg, sp, spAttr uint8) uint8 {
	bgOpaque := bg != 0
	spOpaque := sp != 0

	switch {
	case !bgOpaque && !spOpaque:
		// どちらも透明。backdrop の色になる。
		return 0
	case !bgOpaque:
		return 0x10 | sp
	case !spOpaque:
		return bg
	case spAttr&0x20 == 0:
		// スプライトが背景より前
		return 0x10 | sp
	default:
		return bg
	}
}

// detectSprite0Hit はスプライト 0 ヒットを判定する。
func (p *PPU) detectSprite0Hit(bg, sp uint8, spIndex int) {
	if spIndex != 0 || !p.sprite0OnCurrent {
		return
	}
	if bg == 0 || sp == 0 {
		return
	}
	if !p.mask.BGEnabled() || !p.mask.SpritesEnabled() {
		return
	}
	if p.status.Sprite0Hit() {
		return
	}

	x := p.dot - 1
	if x == 255 {
		// 最後のピクセルでは発生しない。
		return
	}
	if x < 8 && (!p.mask.ShowBGLeft() || !p.mask.ShowSpritesLeft()) {
		return
	}
	p.status |= StatusSprite0Hit
	if p.Hooks.OnSprite0Hit != nil {
		p.Hooks.OnSprite0Hit()
	}
}

// refreshOAMOnRenderStart は OAM のハードウェアリフレッシュのバグを再現する。
//
// レンダリングの開始時に oamAddr が 8 以上のとき、oamAddr & $F8 から
// 8 バイトが OAM の先頭 8 バイトを上書きする。
func (p *PPU) refreshOAMOnRenderStart() {
	if p.oamAddr < 8 {
		return
	}
	src := int(p.oamAddr & 0xF8)
	copy(p.oam[0:8], p.oam[src:src+8])
	p.warn("OAM のリフレッシュバグが起きた（OAMADDR=$%02X）", p.oamAddr)
}
