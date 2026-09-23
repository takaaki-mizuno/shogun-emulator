package ppu

// 背景のフェッチは 8 ドットの周期で 4 回のアクセスを行う。1 回のアクセスは
// 2 ドットを要する。ドットの値ごとに分岐を並べるのではなく、区間の判定と
// 8 ドット周期の位相で処理を決める。341 通りの分岐を並べると設計書の表との
// 対応が追えなくなるためである。

// renderScanlineDot は可視走査線とプリレンダー行の 1 ドットを処理する。
//
// visible が false のときはプリレンダー行である。フェッチは行うが
// ピクセルは出力しない。
func (p *PPU) renderScanlineDot(visible bool) {
	if !visible && p.dot == 1 {
		p.leaveVBlank()
	}

	if visible && p.dot >= 1 && p.dot <= 256 {
		p.renderPixel()
	}

	if !p.mask.RenderingEnabled() {
		return
	}

	if !visible && p.dot == 0 {
		// レンダリングの開始。OAM のリフレッシュバグがここで起こる。
		p.refreshOAMOnRenderStart()
	}

	// シフタは背景フェッチの区間で毎ドット進む
	if p.inBackgroundFetch() {
		p.shiftBackground()
	}
	if visible && p.dot >= 1 && p.dot <= 256 {
		p.shiftSprites()
	}

	p.spriteDot(visible)
	p.fetchDot()
	p.updateScrollRegisters(visible)
}

// inBackgroundFetch は背景のシフタが動く区間かを返す。
//
// dot 2-257 と dot 322-337 でシフタが進む。ピクセルを出力する dot 1-256 の
// 1 ドット前からタイルの先頭が並ぶように区間を定める。
func (p *PPU) inBackgroundFetch() bool {
	return (p.dot >= 2 && p.dot <= 257) || (p.dot >= 322 && p.dot <= 337)
}

// fetchDot はフェッチ区間の 1 ドット分のバスアクセスを行う。
func (p *PPU) fetchDot() {
	switch {
	case p.dot >= 1 && p.dot <= 256, p.dot >= 321 && p.dot <= 336:
		p.backgroundFetchDot()
	case p.dot >= 257 && p.dot <= 320:
		// スプライトフェッチは spriteDot が行う。
		// oamAddr はこの区間で 0 にリセットされる。
		p.oamAddr = 0
	case p.dot >= 337 && p.dot <= 340:
		// ネームテーブルバイトを 2 回フェッチする。値は使わない。
		// MMC5 がこの 3 連続のネームテーブルフェッチを走査線カウンタの
		// クロックに使うため、バスアクセスを省略しない。
		if p.dot%2 == 1 {
			p.fetchAddress(p.nametableAddress())
		} else {
			p.ntLatch = p.fetchValue()
		}
	}
}

// backgroundFetchDot は 8 ドット周期の位相に従って背景をフェッチする。
//
// 位相は 1 から 8 で、奇数ドットでアドレスをバスへ出し、偶数ドットで値を
// 読む。2 段に分けるのは、マッパーが監視する A12 の遷移を実機と同じ回数・
// 同じタイミングで起こすためである。
func (p *PPU) backgroundFetchDot() {
	phase := (p.dot - 1) % 8
	switch phase {
	case 0:
		p.fetchAddress(p.nametableAddress())
	case 1:
		p.ntLatch = p.fetchValue()
	case 2:
		p.fetchAddress(p.attributeAddress())
	case 3:
		// 属性の 1 バイトから、このタイルに当たる 2 bit をここで選ぶ。
		// シフタへ転送する時点では coarse X が次のタイルへ進んでおり、
		// その v で選ぶと隣の 2×2 の属性を使ってしまう（設計書 04 編 §4.5.1）。
		shift := (p.v>>4)&0x04 | p.v&0x02
		p.atLatch = p.fetchValue() >> shift & 0x03
	case 4:
		p.fetchAddress(p.patternAddress(0))
	case 5:
		p.bgLoLatch = p.fetchValue()
	case 6:
		p.fetchAddress(p.patternAddress(8))
	case 7:
		p.bgHiLatch = p.fetchValue()
	}
}

// nametableAddress は現在の v に対応するネームテーブルのアドレスを返す。
func (p *PPU) nametableAddress() uint16 {
	return 0x2000 | p.v&0x0FFF
}

// attributeAddress は現在の v に対応する属性テーブルのアドレスを返す。
//
// 属性テーブルは 1 バイトで 4x4 タイル分の色を持つ。coarse X の上位 3 bit と
// coarse Y の上位 3 bit で 1 バイトを選ぶ。
func (p *PPU) attributeAddress() uint16 {
	return 0x23C0 | p.v&0x0C00 | (p.v>>4)&0x38 | (p.v>>2)&0x07
}

// patternAddress はパターンテーブルのアドレスを返す。offset は 0 で下位
// プレーン、8 で上位プレーン。
func (p *PPU) patternAddress(offset uint16) uint16 {
	return p.ctrl.BGPatternBase() | uint16(p.ntLatch)<<4 | (p.v>>12)&0x07 | offset
}

// updateScrollRegisters はレンダリング中の v の自動更新を行う。
func (p *PPU) updateScrollRegisters(visible bool) {
	switch {
	case p.dot == 256:
		p.incrementY()
	case p.dot == 257:
		// 水平成分をコピーする
		p.v = p.v&0x7BE0 | p.t&0x041F
	case p.dot >= 8 && p.dot <= 256 && p.dot%8 == 0:
		p.incrementX()
	case p.dot == 328 || p.dot == 336:
		p.incrementX()
	}

	// プリレンダー行の dot 280-304 で垂直成分をコピーする
	if !visible && p.dot >= 280 && p.dot <= 304 {
		p.v = p.v&0x041F | p.t&0x7BE0
	}
}

// shiftBackground はシフタを 1 bit 進め、8 ドットごとにラッチを転送する。
//
// 転送をシフトより前に行う。転送はシフタの下位 8 bit へ入れ、ピクセルは
// 上位 8 bit から選ぶ。この順序でタイルの先頭ピクセルが正しい位置に並ぶ。
// 逆にすると画面が 1 ピクセルずれる。スプライト 0 ヒットが背景との
// 重なりを 1 ピクセル単位で見るため、`sprite_hit_tests` の
// `02.alignment`・`03.corners`・`04.flip` がこの順序を検証する。
func (p *PPU) shiftBackground() {
	// dot 9, 17, 25, ..., 257 と 329, 337 でラッチの内容をシフタへ転送する
	if p.dot%8 == 1 {
		p.reloadShifters()
	}

	p.bgShiftLo <<= 1
	p.bgShiftHi <<= 1
	p.atShiftLo <<= 1
	p.atShiftHi <<= 1
	if p.atLatchLo {
		p.atShiftLo |= 1
	}
	if p.atLatchHi {
		p.atShiftHi |= 1
	}
}

// reloadShifters はフェッチしたタイルをシフタの下位 8 bit へ入れる。
//
// 属性は 8 ピクセルで同じ値であるため、2 bit を 1 bit のラッチ 2 個に
// 保持し、毎ドットシフタへ送り込む。
func (p *PPU) reloadShifters() {
	p.bgShiftLo = p.bgShiftLo&0xFF00 | uint16(p.bgLoLatch)
	p.bgShiftHi = p.bgShiftHi&0xFF00 | uint16(p.bgHiLatch)

	// atLatch はフェッチの時点で選んだ 2 bit である。
	p.atLatchLo = p.atLatch&0x01 != 0
	p.atLatchHi = p.atLatch&0x02 != 0
}

// backgroundPixel は背景のピクセルを 4 bit で返す。
//
// 下位 2 bit がパターン値、上位 2 bit が属性で選ばれたパレット番号である。
// パターン値が 0 のときは透明であり、パレット番号を付けずに 0 を返す。
func (p *PPU) backgroundPixel() uint8 {
	if !p.mask.BGEnabled() {
		return 0
	}
	// 画面左端 8 ピクセルのクリッピング
	if !p.mask.ShowBGLeft() && p.dot-1 < 8 {
		return 0
	}

	// fine X でシフタの上位 8 bit のどのビットを見るかが決まる
	sel := uint16(0x8000) >> p.x
	var pattern uint8
	if p.bgShiftLo&sel != 0 {
		pattern |= 0x01
	}
	if p.bgShiftHi&sel != 0 {
		pattern |= 0x02
	}
	if pattern == 0 {
		return 0
	}

	atSel := uint8(0x80) >> p.x
	var attr uint8
	if p.atShiftLo&atSel != 0 {
		attr |= 0x01
	}
	if p.atShiftHi&atSel != 0 {
		attr |= 0x02
	}
	return attr<<2 | pattern
}

// renderPixel は 1 ピクセルを出力する。
func (p *PPU) renderPixel() {
	if !p.mask.RenderingEnabled() {
		// レンダリング無効のとき、v の下位 14 bit がパレット領域を指して
		// いればそのアドレスの色を出力する。意図的に使うソフトがある。
		idx := 0
		if addr := p.v & 0x3FFF; addr >= 0x3F00 {
			idx = paletteIndex(addr)
		}
		p.frame.Set(p.dot-1, p.scanline, p.paletteValue(idx))
		return
	}

	bg := p.backgroundPixel()
	sp, spAttr, spIndex := p.spritePixel()

	p.detectSprite0Hit(bg, sp, spIndex)
	idx := p.multiplex(bg, sp, spAttr)
	p.frame.Set(p.dot-1, p.scanline, p.paletteValue(int(idx)))
}

// paletteValue はパレットの添字からフレームバッファに書く値を作る。
//
// 透明のピクセルは $3F00 の色になる。実機では透明時に EXT 入力が選ばれ、
// EXT は接地されているため 0 になる。
func (p *PPU) paletteValue(idx int) uint16 {
	v := uint16(p.palette[idx&0x1F] & 0x3F)
	if p.mask.Greyscale() {
		v &= 0x30
	}
	return v | uint16(p.mask.Emphasis(p.region))<<6
}
