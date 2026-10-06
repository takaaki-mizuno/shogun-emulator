package ppu

// レジスタの番号。バスが addr & 0x0007 を渡す。
const (
	regCtrl    = 0 // $2000
	regMask    = 1 // $2001
	regStatus  = 2 // $2002
	regOAMAddr = 3 // $2003
	regOAMData = 4 // $2004
	regScroll  = 5 // $2005
	regAddr    = 6 // $2006
	regData    = 7 // $2007
)

// ReadRegister は $2000-$2007 を読む。
//
// どのポートの読み出しでも ioLatch を更新する。書き込み専用ポートの
// 読み出しは ioLatch の現在値を返す。実機では I/O バスの動的ラッチが
// 直前の値を保持しているためである。
func (p *PPU) ReadRegister(reg uint16) uint8 {
	switch reg {
	case regStatus:
		// 上位 3 bit だけをバスへ駆動する。下位 5 bit はラッチの値が出る。
		p.refreshLatch(p.readStatus(), 0xE0)
	case regOAMData:
		p.refreshLatch(p.readOAMData(), 0xFF)
	case regData:
		v, mask := p.readData()
		p.refreshLatch(v, mask)
	}
	// 書き込み専用ポートの読み出しはラッチを駆動しない。駆動すると
	// 読み続けるだけで保持が続き、減衰が観測できなくなる。
	return p.ioLatch
}

// PeekRegister は副作用を起こさずに $2000-$2007 を読む。
func (p *PPU) PeekRegister(reg uint16) uint8 {
	switch reg {
	case regStatus:
		return uint8(p.status)&0xE0 | p.ioLatch&0x1F
	case regOAMData:
		return p.oam[p.oamAddr]
	case regData:
		addr := p.v & 0x3FFF
		if addr >= 0x3F00 {
			return p.palette[paletteIndex(addr)]&0x3F | p.ioLatch&0xC0
		}
		return p.readBuffer
	}
	return p.ioLatch
}

// WriteRegister は $2000-$2007 へ書く。
func (p *PPU) WriteRegister(reg uint16, v uint8) {
	p.refreshLatch(v, 0xFF)

	switch reg {
	case regCtrl:
		if p.warmupDots > 0 {
			p.compat(CompatWarmupWrite, 0x2000|reg)
			return
		}
		p.writeControl(v)
	case regMask:
		if p.warmupDots > 0 {
			p.compat(CompatWarmupWrite, 0x2000|reg)
			return
		}
		p.mask = Mask(v)
	case regStatus:
		// 読み出し専用。ioLatch のみ更新する。
	case regOAMAddr:
		p.oamAddr = v
	case regOAMData:
		p.writeOAMData(v)
	case regScroll:
		if p.warmupDots > 0 {
			p.compat(CompatWarmupWrite, 0x2000|reg)
			return
		}
		p.writeScroll(v)
	case regAddr:
		if p.warmupDots > 0 {
			p.compat(CompatWarmupWrite, 0x2000|reg)
			return
		}
		if p.Compat != nil && p.renderingActive() {
			p.compat(CompatRenderAccess, 0x2006)
		}
		p.writeAddr(v)
	case regData:
		p.writeData(v)
	}
}

// readStatus は $2002 を読む。
//
// 上位 3 bit を status から、下位 5 bit を ioLatch から合成する。読み出しで
// VBlank をクリアし、w を false にする。
//
// VBlank フラグがセットされる 1 ドット前に読んだときは、そのフレームの
// セットを飛ばす。セットと同一ドットまたはその直後に読んだときは、フラグを
// 1 として返してクリアする。クリアにより NMI 線が戻るため、CPU が線の
// 立ち下がりを採取する前に解除され、そのフレームの NMI は発生しない。
//
// dot は「次に処理するドット」を指す。CPU の読み出しはサイクルの終わりに
// 起こるため、読み出しが起きたドットは dot - 1 である。フラグが立つのは
// dot 1 の処理であり、その 1 ドット前に読むのは dot が 1 のときになる。
func (p *PPU) readStatus() uint8 {
	if p.scanline == p.region.VBlankStartScanline() && p.dot == 1 {
		p.suppressVBlank = true
	}

	out := uint8(p.status)&0xE0 | p.ioLatch&0x1F
	p.status &^= StatusVBlank
	p.updateNMILine()
	p.w = false
	return out
}

// writeControl は $2000 へ書く。
//
// NMI 許可を 0 から 1 にしたとき、VBlank フラグが立っていれば NMI 線を
// アサートする。線は 2 つの論理積で駆動されるためである。
func (p *PPU) writeControl(v uint8) {
	p.ctrl = Control(v)
	p.t = p.t&0xF3FF | p.ctrl.NametableSelect()
	p.updateNMILine()
}

// writeScroll は $2005 へ書く。
func (p *PPU) writeScroll(v uint8) {
	if !p.w {
		// 1 回目。coarse X と fine X
		p.t = p.t&0xFFE0 | uint16(v>>3)
		p.x = v & 0x07
		p.w = true
		return
	}
	// 2 回目。fine Y と coarse Y
	p.t = p.t&0x8C1F | uint16(v&0x07)<<12 | uint16(v&0xF8)<<2
	p.w = false
}

// writeAddr は $2006 へ書く。
func (p *PPU) writeAddr(v uint8) {
	if !p.w {
		// 1 回目。上位バイト。bit 14 は 0 になる。
		p.t = p.t&0x00FF | uint16(v&0x3F)<<8
		p.w = true
		return
	}
	// 2 回目。下位バイトを入れて v へ転送する。
	p.t = p.t&0xFF00 | uint16(v)
	p.v = p.t
	p.w = false
	// 転送した値がアドレスバスに現れる。マッパーが A12 を監視しており、
	// レンダリングを止めている間の $2006 への書き込みでもスキャンライン
	// IRQ のカウンタが進む。
	p.putAddressOnBus(p.v)
}

// readOAMData は $2004 を読む。
//
// レンダリング中の dot 1-64 は 0xFF を返す。この期間はスプライト評価が
// secondary OAM を初期化しており、読み出しにその値が現れる。
func (p *PPU) readOAMData() uint8 {
	if p.renderingActive() {
		switch {
		case p.dot >= 1 && p.dot <= 64:
			return 0xFF
		case p.dot <= 256:
			// 評価が参照している値が現れる。
			return p.eval.latch
		}
	}
	v := p.oam[p.oamAddr]
	if p.oamAddr&0x03 == oamAttributeByte {
		// 属性バイトの bit 2-4 に対応する記憶素子が存在しない。
		// 読み出すと常に 0 になる。
		v &= oamAttributeMask
	}
	return v
}

// writeOAMData は $2004 へ書く。
//
// レンダリング中の書き込みは OAM を変更しない。
func (p *PPU) writeOAMData(v uint8) {
	if p.renderingActive() {
		return
	}
	p.oam[p.oamAddr] = v
	p.oamAddr++
}

// OAM の属性バイトの位置と、実在するビット。
const (
	oamAttributeByte = 2
	oamAttributeMask = 0xE3
)

// readData は $2007 を読む。返り値はバスへ駆動する値と、そのビットの範囲。
//
// 読み出しは内部リードバッファの内容を返し、そのあとでバッファを更新する。
// パレット領域は即座に値を返す。上位 2 bit は駆動されないため、ラッチの値が
// そのまま出る。
func (p *PPU) readData() (value, mask uint8) {
	addr := p.v & 0x3FFF
	if (p.Warn != nil || p.Compat != nil) && p.renderingActive() {
		p.warn("レンダリング中に $2007 を読んだ（v=$%04X）", p.v)
		p.compat(CompatRenderAccess, 0x2007)
	}
	p.putAddressOnBus(addr)
	var out uint8
	m := uint8(0xFF)
	if addr >= 0x3F00 {
		out = p.palette[paletteIndex(addr)] & 0x3F
		if p.mask.Greyscale() {
			out &= 0x30
		}
		m = 0x3F
		// 下敷きのメモリを読んでバッファへ入れる。パレット領域の下には
		// ネームテーブルの $2F00-$2FFF が写っている。
		p.readBuffer = p.readNametable(addr & 0x2FFF)
	} else {
		out = p.readBuffer
		p.readBuffer = p.readVRAM(addr)
	}
	p.advanceVRAMAddress()
	// 進めた後のアドレスもバスに現れる。$0FFF を読んで $1000 へ進む
	// 読み出しは、この時点で A12 を立ち上げる。
	p.putAddressOnBus(p.v & 0x3FFF)
	return out, m
}

// writeData は $2007 へ書く。
func (p *PPU) writeData(v uint8) {
	if (p.Warn != nil || p.Compat != nil) && p.renderingActive() {
		p.warn("レンダリング中に $2007 へ書いた（v=$%04X）", p.v)
		p.compat(CompatRenderAccess, 0x2007)
	}
	p.putAddressOnBus(p.v & 0x3FFF)
	p.writeVRAM(p.v&0x3FFF, v)
	p.advanceVRAMAddress()
	p.putAddressOnBus(p.v & 0x3FFF)
}

// advanceVRAMAddress は $2007 アクセス後に v を進める。
//
// レンダリング中は incrementX と incrementY を同時に行う。実機では
// $2007 のアクセスがレンダリング用のアドレス更新回路を動かすためである。
func (p *PPU) advanceVRAMAddress() {
	if p.renderingActive() {
		p.incrementX()
		p.incrementY()
		return
	}
	p.v = (p.v + p.ctrl.VRAMIncrement()) & 0x7FFF
}

// renderingActive はレンダリングのためのメモリアクセスが動いているかを返す。
//
// 可視走査線とプリレンダー行で、かつ背景かスプライトのいずれかが有効な
// 期間である。
func (p *PPU) renderingActive() bool {
	if !p.mask.RenderingEnabled() {
		return false
	}
	return p.scanline < p.region.VisibleScanlines || p.scanline == p.region.PreRenderScanline()
}

// incrementX は coarse X を進める。31 で次のネームテーブルへ移る。
func (p *PPU) incrementX() {
	if p.v&0x001F == 31 {
		p.v &^= 0x001F
		p.v ^= 0x0400
		return
	}
	p.v++
}

// incrementY は fine Y と coarse Y を進める。
//
// coarse Y が 29 のときネームテーブルを切り替える。31 のときは切り替え
// ない。属性テーブルの領域まで走査が進んだ場合に相当する。
func (p *PPU) incrementY() {
	if p.v&0x7000 != 0x7000 {
		p.v += 0x1000
		return
	}
	p.v &^= 0x7000
	y := (p.v & 0x03E0) >> 5
	switch y {
	case 29:
		y = 0
		p.v ^= 0x0800
	case 31:
		y = 0
	default:
		y++
	}
	p.v = p.v&^uint16(0x03E0) | y<<5
}
