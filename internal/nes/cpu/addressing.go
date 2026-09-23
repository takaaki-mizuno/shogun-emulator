package cpu

// addrMode はアドレッシングモード。
type addrMode uint8

const (
	// modeImplied はオペランドを持たない。次の命令バイトを読んで捨てる。
	modeImplied addrMode = iota
	// modeAccumulator はアキュムレータを対象にする。バスの動きは modeImplied と同じ。
	modeAccumulator
	modeImmediate
	modeZeroPage
	modeZeroPageX
	modeZeroPageY
	modeAbsolute
	modeAbsoluteX
	modeAbsoluteY
	modeIndirectX
	modeIndirectY
	modeRelative
	// modeIndirect は JMP (a) のみが使う。
	modeIndirect
	// modeJSR は JSR a。下位バイトだけを先に読み、上位バイトは push の後に読む。
	modeJSR
)

// operandLen はオペランドのバイト数を返す。
func (m addrMode) operandLen() int {
	switch m {
	case modeImplied, modeAccumulator:
		return 0
	case modeImmediate, modeZeroPage, modeZeroPageX, modeZeroPageY,
		modeIndirectX, modeIndirectY, modeRelative:
		return 1
	}
	return 2
}

// accessKind は実効アドレスに対するアクセスの種別。
//
// 同じアドレッシングモードでもリードとライトでサイクル数が変わるため、
// アドレス解決はこの種別を見る。
type accessKind uint8

const (
	// accessRead は実効アドレスから読む。
	accessRead accessKind = iota
	// accessWrite は実効アドレスへ書く。
	accessWrite
	// accessRMW は読んで書き戻し、変更後の値を書く。
	accessRMW
	// accessNone は実効アドレスをデータとして扱わない。
	accessNone
)

// resolve はアドレッシングモードのサイクルシーケンスを実行し、実効アドレスを返す。
//
// 実効アドレスに対する最後のアクセス（リード・ライト・RMW）は演算側が行う。
// ダミーリードとダミーライトを省略しない。nestest はサイクル数を検証する。
func (c *CPU) resolve(op *opcode) uint16 {
	switch op.mode {
	case modeImplied, modeAccumulator:
		return c.addrImplied()
	case modeImmediate:
		return c.addrImmediate()
	case modeZeroPage:
		return c.addrZeroPage()
	case modeZeroPageX:
		return c.addrZeroPageIndexed(c.X)
	case modeZeroPageY:
		return c.addrZeroPageIndexed(c.Y)
	case modeAbsolute:
		return c.addrAbsolute()
	case modeAbsoluteX:
		return c.addrAbsoluteIndexed(c.X, op.access)
	case modeAbsoluteY:
		return c.addrAbsoluteIndexed(c.Y, op.access)
	case modeIndirectX:
		return c.addrIndirectX()
	case modeIndirectY:
		return c.addrIndirectY(op.access)
	case modeRelative:
		return c.addrRelative()
	case modeIndirect:
		return c.addrIndirect()
	case modeJSR:
		return c.addrJSR()
	}
	return 0
}

// addrImplied は次の命令バイトを読んで捨てる。PC は進めない。
func (c *CPU) addrImplied() uint16 {
	c.read(c.PC)
	return 0
}

// addrImmediate は値の置かれたアドレスを返す。
//
// ここではバスにアクセスしない。値を読むのは演算側であり、その 1 回の
// リードが 2 サイクル目になる。
func (c *CPU) addrImmediate() uint16 {
	addr := c.PC
	c.PC++
	return addr
}

// addrZeroPage はゼロページのアドレスをフェッチする。
func (c *CPU) addrZeroPage() uint16 {
	return uint16(c.fetch())
}

// addrZeroPageIndexed はゼロページのアドレスにインデックスを加算する。
//
// アドレスをフェッチした後、そのアドレスから読んで捨てる。実効アドレスの
// 上位バイトは常に $00 であり、ページ境界のラップは処理されない。
func (c *CPU) addrZeroPageIndexed(index uint8) uint16 {
	base := c.fetch()
	c.read(uint16(base))
	return uint16(base + index)
}

// addrAbsolute は 16 bit のアドレスをフェッチする。
func (c *CPU) addrAbsolute() uint16 {
	lo := c.fetch()
	hi := c.fetch()
	return uint16(hi)<<8 | uint16(lo)
}

// addrAbsoluteIndexed は 16 bit のアドレスにインデックスを加算する。
//
// 上位バイトのフェッチと同時に下位バイトへインデックスを加算するため、
// ページ境界をまたぐとき上位バイトが $100 小さい不正なアドレスができる。
// リードではそのアドレスから読んだ後に上位バイトを修正して読み直す。
// ライトと RMW は書き込みを取り消せないため、ページ境界に関係なく必ず
// ダミーリードを行う。
func (c *CPU) addrAbsoluteIndexed(index uint8, access accessKind) uint16 {
	lo := c.fetch()
	hi := c.fetch()
	base := uint16(hi)<<8 | uint16(lo)
	addr := base + uint16(index)
	crossed := addr&0xFF00 != base&0xFF00
	c.indexBase, c.indexCrossed = base, crossed
	if crossed || access != accessRead {
		// 上位バイトが修正される前の不正なアドレスを読む
		c.read(base&0xFF00 | addr&0x00FF)
	}
	return addr
}

// addrIndirectX はゼロページのポインタに X を加算してアドレスを読む。
//
// ポインタは常にゼロページから読まれる。ゼロページ境界のラップは
// 処理されない。
func (c *CPU) addrIndirectX() uint16 {
	ptr := c.fetch()
	c.read(uint16(ptr))
	ptr += c.X
	lo := c.read(uint16(ptr))
	hi := c.read(uint16(ptr + 1))
	return uint16(hi)<<8 | uint16(lo)
}

// addrIndirectY はゼロページのポインタが指すアドレスに Y を加算する。
//
// 上位バイトのフェッチと同時に下位バイトへ Y を加算するため、
// addrAbsoluteIndexed と同じページ境界の扱いになる。
func (c *CPU) addrIndirectY(access accessKind) uint16 {
	ptr := c.fetch()
	lo := c.read(uint16(ptr))
	hi := c.read(uint16(ptr + 1))
	base := uint16(hi)<<8 | uint16(lo)
	addr := base + uint16(c.Y)
	crossed := addr&0xFF00 != base&0xFF00
	c.indexBase, c.indexCrossed = base, crossed
	if crossed || access != accessRead {
		c.read(base&0xFF00 | addr&0x00FF)
	}
	return addr
}

// addrRelative は分岐先のアドレスを返す。
func (c *CPU) addrRelative() uint16 {
	offset := int8(c.fetch())
	return uint16(int32(c.PC) + int32(offset))
}

// addrIndirect は JMP (a) のポインタをたどる。
//
// ポインタの上位バイトは下位バイトのみをインクリメントして計算する。
// $xxFF を指したときページをまたがない。
func (c *CPU) addrIndirect() uint16 {
	ptr := c.addrAbsolute()
	lo := c.read(ptr)
	hi := c.read(ptr&0xFF00 | uint16(uint8(ptr)+1))
	return uint16(hi)<<8 | uint16(lo)
}

// addrJSR は JSR の下位バイトだけをフェッチする。
//
// 上位バイトは push の後にフェッチされる。順序が異なるため、他の
// absolute と同じ経路にしない。
func (c *CPU) addrJSR() uint16 {
	return uint16(c.fetch())
}

// rmw は read-modify-write のバスアクセスを行う。
//
// 「読む → 元の値をそのまま書く → 変更後の値を書く」の順でアクセスする。
// この二重ライトにより、INC $2007 が PPUDATA に 2 回書き込む挙動が
// 再現される。
func (c *CPU) rmw(addr uint16, f func(uint8) uint8) {
	v := c.read(addr)
	c.write(addr, v)    // 元の値を書き戻す
	c.write(addr, f(v)) // 変更後の値を書く
}
