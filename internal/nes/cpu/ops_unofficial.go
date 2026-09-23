package cpu

// 非公式命令。実機の 6502 では未使用 opcode が「無関係な複数のマイクロコード
// 行を同時に起動する」ため、文書化されていないが定まった動作をする。
//
// 実装は既存の部品の組み合わせで表す。新しいアドレッシングモードや新しい
// 演算をほとんど追加しない。組み合わせとして書けばサイクル数が自動的に
// 正しくなり、個別に書いたときに入り込むサイクル数の誤りを避けられる。

// --- RMW 系（RMW 演算と ALU 演算の合成）---

// opSLO は ASL の後に ORA を行う。
func opSLO(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v = c.asl(v)
		c.A |= v
		c.setZN(c.A)
		return v
	})
}

// opRLA は ROL の後に AND を行う。
func opRLA(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v = c.rol(v)
		c.A &= v
		c.setZN(c.A)
		return v
	})
}

// opSRE は LSR の後に EOR を行う。
func opSRE(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v = c.lsr(v)
		c.A ^= v
		c.setZN(c.A)
		return v
	})
}

// opRRA は ROR の後に ADC を行う。
func opRRA(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v = c.ror(v)
		c.adc(v)
		return v
	})
}

// opDCP は DEC の後に CMP を行う。
func opDCP(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v--
		c.compare(c.A, v)
		return v
	})
}

// opISC は INC の後に SBC を行う。
func opISC(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v++
		c.adc(^v)
		return v
	})
}

// --- ロード・ストア系 ---

// opLAX は A と X に同時にロードする。
func opLAX(c *CPU, addr uint16) {
	v := c.read(addr)
	c.A = v
	c.X = v
	c.setZN(v)
}

// opSAX は A & X をメモリへ書く。フラグは変化しない。
func opSAX(c *CPU, addr uint16) {
	c.write(addr, c.A&c.X)
}

// --- 即値系 ---

// opALR は AND の後に LSR A を行う。
func opALR(c *CPU, addr uint16) {
	c.A &= c.read(addr)
	c.A = c.lsr(c.A)
}

// opANC は AND の後に N を C へコピーする。
func opANC(c *CPU, addr uint16) {
	c.A &= c.read(addr)
	c.setZN(c.A)
	c.C = c.N
}

// opARR は AND の後に ROR A を行う。
//
// フラグの決め方が公式の ROR と異なる。C は結果の bit 6、V は結果の
// bit 6 と bit 5 の排他的論理和になる。加算回路が関与するためである。
func opARR(c *CPU, addr uint16) {
	c.A &= c.read(addr)
	carry := c.C
	c.A >>= 1
	if carry {
		c.A |= 0x80
	}
	c.setZN(c.A)
	c.C = c.A&0x40 != 0
	c.V = (c.A>>6)&1 != (c.A>>5)&1
}

// opAXS は X = (A & X) - i を行う。借りは発生しない。
// N・Z・C を更新し、V は変更しない。
func opAXS(c *CPU, addr uint16) {
	m := c.read(addr)
	t := c.A & c.X
	c.C = t >= m
	c.X = t - m
	c.setZN(c.X)
}

// --- NOP 系 ---

// opSKB は即値を読んで捨てる 2 バイトの NOP。
//
// 値を使わなくてもリードサイクルは発生する。
func opSKB(c *CPU, addr uint16) {
	c.read(addr)
}

// opIGN は実効アドレスを読んで捨てる。
//
// 読み捨てであってもリードサイクルが発生するため、PPUSTATUS のラッチ
// リセットや PPUADDR のインクリメントといった副作用が起きる。副作用を
// 無視して実装すると一部の ROM が壊れる。
func opIGN(c *CPU, addr uint16) {
	c.read(addr)
}

// --- 不安定な非公式命令 ---

// unstableConstant は不安定な命令で A に論理和される値。
//
// 実機では内部の定数がデータバスに残った値に依存して変わる。一般に
// 観測されている値を採る。
const unstableConstant = 0xEE

// opXAA は A = (A | $EE) & X & i を行う。
func opXAA(c *CPU, addr uint16) {
	c.warnUnstable("XAA")
	c.A = (c.A | unstableConstant) & c.X & c.read(addr)
	c.setZN(c.A)
}

// opLAXImmediate は A = X = i を行う。
//
// XAA と同じ形の不安定な命令だが、内部定数が $FF として観測される。
// A との論理積が効かないため、即値がそのまま入る。instr_test-v5 の
// 03-immediate はこの動作を期待する。
func opLAXImmediate(c *CPU, addr uint16) {
	c.warnUnstable("LAX #i")
	v := c.read(addr)
	c.A = v
	c.X = v
	c.setZN(v)
}

// opLAS は A = X = S = memory & S を行う。
func opLAS(c *CPU, addr uint16) {
	c.warnUnstable("LAS")
	v := c.read(addr) & c.S
	c.A = v
	c.X = v
	c.S = v
	c.setZN(v)
}

// opTAS は S = A & X とし、A & X & (上位バイト + 1) をメモリへ書く。
func opTAS(c *CPU, addr uint16) {
	c.warnUnstable("TAS")
	c.S = c.A & c.X
	c.unstableStore(addr, c.A&c.X)
}

// opAHX は A & X & (上位バイト + 1) をメモリへ書く。
func opAHX(c *CPU, addr uint16) {
	c.warnUnstable("AHX")
	c.unstableStore(addr, c.A&c.X)
}

// opSHX は X & (上位バイト + 1) をメモリへ書く。
func opSHX(c *CPU, addr uint16) {
	c.warnUnstable("SHX")
	c.unstableStore(addr, c.X)
}

// opSHY は Y & (上位バイト + 1) をメモリへ書く。
func opSHY(c *CPU, addr uint16) {
	c.warnUnstable("SHY")
	c.unstableStore(addr, c.Y)
}

// unstableStore は不安定なストア命令の書き込みを行う。
//
// 書き込む値はレジスタと「インデックス加算前のアドレスの上位バイト + 1」の
// 論理積になる。ページ境界をまたいだときは、書き込み先アドレスの上位
// バイトもその値に置き換わる。アドレスの上位バイトを保持するラッチが
// 正しく駆動されないためである。
func (c *CPU) unstableStore(addr uint16, reg uint8) {
	high := uint8(c.indexBase >> 8)
	v := reg & (high + 1)
	if c.indexCrossed {
		addr = uint16(v)<<8 | addr&0x00FF
	}
	c.write(addr, v)
}

// --- STP ---

// opSTP は CPU を停止させる。
//
// 無限ループにしない。以降 StepInstruction は PC のリードのみを行い、
// デバッガが「STP により停止」と表示できる状態にする。復帰にはリセットが
// 必要である。
func opSTP(c *CPU, _ uint16) {
	c.halted = true
	c.warn("STP により CPU が停止した（PC=$%04X）", c.opPC)
}

// warnUnstable は不安定な非公式命令の実行を記録する。
//
// 実機でも値が定まらない命令に到達したことは、ROM の挙動を調べるときの
// 手がかりになる。黙って実行すると後から追えなくなる。
func (c *CPU) warnUnstable(name string) {
	c.warn("不安定な非公式命令 %s を実行した（PC=$%04X）", name, c.opPC)
}
