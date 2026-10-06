package cpu

// 各演算は func(c *CPU, addr uint16) の形を取る。addr はアドレッシングモードが
// 解決した実効アドレスである。実効アドレスに対する最後のバスアクセスは
// ここで行う。

// --- ロード ---

func opLDA(c *CPU, addr uint16) {
	c.A = c.read(addr)
	c.setZN(c.A)
}

func opLDX(c *CPU, addr uint16) {
	c.X = c.read(addr)
	c.setZN(c.X)
}

func opLDY(c *CPU, addr uint16) {
	c.Y = c.read(addr)
	c.setZN(c.Y)
}

// --- ストア ---

func opSTA(c *CPU, addr uint16) { c.write(addr, c.A) }
func opSTX(c *CPU, addr uint16) { c.write(addr, c.X) }
func opSTY(c *CPU, addr uint16) { c.write(addr, c.Y) }

// --- 転送 ---

func opTAX(c *CPU, _ uint16) { c.X = c.A; c.setZN(c.X) }
func opTAY(c *CPU, _ uint16) { c.Y = c.A; c.setZN(c.Y) }
func opTXA(c *CPU, _ uint16) { c.A = c.X; c.setZN(c.A) }
func opTYA(c *CPU, _ uint16) { c.A = c.Y; c.setZN(c.A) }
func opTSX(c *CPU, _ uint16) { c.X = c.S; c.setZN(c.X) }

// opTXS はフラグを変更しない。算術演算ではないためである。
func opTXS(c *CPU, _ uint16) { c.S = c.X }

// --- 加減算 ---

// adc は A に値と C を加える。
//
// オーバーフローは (result ^ A) & (result ^ M) & $80 で判定する。
// 符号が同じ 2 つの値を加えて符号が変わったときに立つ。
func (c *CPU) adc(m uint8) {
	sum := uint16(c.A) + uint16(m)
	if c.C {
		sum++
	}
	result := uint8(sum)
	c.C = sum > 0xFF
	c.V = (result^c.A)&(result^m)&0x80 != 0
	c.A = result
	c.setZN(c.A)
}

func opADC(c *CPU, addr uint16) { c.adc(c.read(addr)) }

// opSBC は A から値を引く。A + ~M + C と等しい。
func opSBC(c *CPU, addr uint16) { c.adc(^c.read(addr)) }

// --- 増減 ---

// opINC と opDEC は C と V を変更しない。
func opINC(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v++
		c.setZN(v)
		return v
	})
}

func opDEC(c *CPU, addr uint16) {
	c.rmw(addr, func(v uint8) uint8 {
		v--
		c.setZN(v)
		return v
	})
}

func opINX(c *CPU, _ uint16) { c.X++; c.setZN(c.X) }
func opINY(c *CPU, _ uint16) { c.Y++; c.setZN(c.Y) }
func opDEX(c *CPU, _ uint16) { c.X--; c.setZN(c.X) }
func opDEY(c *CPU, _ uint16) { c.Y--; c.setZN(c.Y) }

// --- シフト ---

func (c *CPU) asl(v uint8) uint8 {
	c.C = v&0x80 != 0
	v <<= 1
	c.setZN(v)
	return v
}

func (c *CPU) lsr(v uint8) uint8 {
	c.C = v&0x01 != 0
	v >>= 1
	c.setZN(v)
	return v
}

func (c *CPU) rol(v uint8) uint8 {
	carry := c.C
	c.C = v&0x80 != 0
	v <<= 1
	if carry {
		v |= 0x01
	}
	c.setZN(v)
	return v
}

func (c *CPU) ror(v uint8) uint8 {
	carry := c.C
	c.C = v&0x01 != 0
	v >>= 1
	if carry {
		v |= 0x80
	}
	c.setZN(v)
	return v
}

func opASL(c *CPU, addr uint16) { c.rmw(addr, c.asl) }
func opLSR(c *CPU, addr uint16) { c.rmw(addr, c.lsr) }
func opROL(c *CPU, addr uint16) { c.rmw(addr, c.rol) }
func opROR(c *CPU, addr uint16) { c.rmw(addr, c.ror) }
func opASLA(c *CPU, _ uint16)   { c.A = c.asl(c.A) }
func opLSRA(c *CPU, _ uint16)   { c.A = c.lsr(c.A) }
func opROLA(c *CPU, _ uint16)   { c.A = c.rol(c.A) }
func opRORA(c *CPU, _ uint16)   { c.A = c.ror(c.A) }

// --- 論理 ---

func opAND(c *CPU, addr uint16) { c.A &= c.read(addr); c.setZN(c.A) }
func opORA(c *CPU, addr uint16) { c.A |= c.read(addr); c.setZN(c.A) }
func opEOR(c *CPU, addr uint16) { c.A ^= c.read(addr); c.setZN(c.A) }

// opBIT は Z を A & M で決め、N と V には M の bit 7 と bit 6 をそのまま入れる。
func opBIT(c *CPU, addr uint16) {
	m := c.read(addr)
	c.Z = c.A&m == 0
	c.N = m&0x80 != 0
	c.V = m&0x40 != 0
}

// --- 比較 ---

// compare は減算を行いフラグだけを更新する。レジスタと V は変更しない。
func (c *CPU) compare(reg, m uint8) {
	c.C = reg >= m
	c.setZN(reg - m)
}

func opCMP(c *CPU, addr uint16) { c.compare(c.A, c.read(addr)) }
func opCPX(c *CPU, addr uint16) { c.compare(c.X, c.read(addr)) }
func opCPY(c *CPU, addr uint16) { c.compare(c.Y, c.read(addr)) }

// --- 分岐 ---

// branch は条件が成立したとき PC を移す。
//
// 成立したとき次の opcode をフェッチする位置を 1 回読んで捨てる。
// ページ境界をまたぐとき、上位バイトが修正される前の不正なアドレスを
// もう 1 回読む。これにより 2 / 3 / 4 サイクルになる。
//
// 割り込みのポーリング点が他の命令と異なる（設計書 03 編 §3.5.1）。
//
//	2 サイクル目（オペランドフェッチ）の前     常に行う
//	分岐成立時の 3 サイクル目の前              行わない
//	ページ越え時の PCH 修正サイクルの前        行う
//
// 分岐が成立しないときは 2 サイクル命令であり、一般の規則（最後から
// 2 番目のサイクルの終わり）がそのまま 1 つ目のポーリング点になる。
func (c *CPU) branch(target uint16, taken bool) {
	if !taken {
		return
	}

	// この時点で 2 サイクル分が終わっており、prev が 2 サイクル目の前の
	// ポーリング点に対応する。
	nmi, irq := c.pollNMIPrev, c.pollIRQPrev

	c.dummyRead(c.PC)
	if target&0xFF00 != c.PC&0xFF00 {
		c.dummyRead(c.PC&0xFF00 | target&0x00FF)
		// PCH 修正サイクルの前のポーリング結果を論理和する。
		nmi = nmi || c.pollNMIPrev
		irq = irq || c.pollIRQPrev
	}
	c.PC = target

	c.setPollResult(nmi, irq)
}

func opBPL(c *CPU, addr uint16) { c.branch(addr, !c.N) }
func opBMI(c *CPU, addr uint16) { c.branch(addr, c.N) }
func opBVC(c *CPU, addr uint16) { c.branch(addr, !c.V) }
func opBVS(c *CPU, addr uint16) { c.branch(addr, c.V) }
func opBCC(c *CPU, addr uint16) { c.branch(addr, !c.C) }
func opBCS(c *CPU, addr uint16) { c.branch(addr, c.C) }
func opBNE(c *CPU, addr uint16) { c.branch(addr, !c.Z) }
func opBEQ(c *CPU, addr uint16) { c.branch(addr, c.Z) }

// --- ジャンプ ---

func opJMP(c *CPU, addr uint16) { c.PC = addr }

// opJSR はリターンアドレスを push してから上位バイトを読む。
//
// push するのは次の命令の 1 バイト前（opcode + 2）である。addr には
// 下位バイトだけが入っており、この時点で PC は上位バイトを指している。
func opJSR(c *CPU, addr uint16) {
	lo := uint8(addr)
	c.peekStack() // 内部処理の 1 サイクル
	c.push(uint8(c.PC >> 8))
	c.push(uint8(c.PC))
	hi := c.read(c.PC)
	c.PC = uint16(hi)<<8 | uint16(lo)
}

// opRTS は pull した後に PC をインクリメントする。
func opRTS(c *CPU, _ uint16) {
	c.peekStack()
	lo := c.pull()
	hi := c.pull()
	c.PC = uint16(hi)<<8 | uint16(lo)
	c.dummyRead(c.PC)
	c.PC++
}

// opRTI は pull した PC をインクリメントしない。
// I の変更は即座に反映する。ポーリングより前に I を復元するためである。
func opRTI(c *CPU, _ uint16) {
	c.peekStack()
	c.unpackP(c.pull())
	lo := c.pull()
	hi := c.pull()
	c.PC = uint16(hi)<<8 | uint16(lo)
}

// opBRK は B フラグを立てて割り込みシーケンスを実行する。
//
// addrImplied で 2 サイクル目のリードが済んでいる。実機では
// そのサイクルで PC が進むため、ここで進める。
func opBRK(c *CPU, _ uint16) {
	c.PC++
	c.push(uint8(c.PC >> 8))
	c.push(uint8(c.PC))
	vec := c.resolveVector(interruptBRK)
	c.push(c.packP(true))
	c.I = true
	lo := c.read(vec)
	hi := c.read(vec + 1)
	c.PC = uint16(hi)<<8 | uint16(lo)
}

// --- スタック ---

func opPHA(c *CPU, _ uint16) { c.push(c.A) }

// opPHP は B フラグを立てて P を push する。
func opPHP(c *CPU, _ uint16) { c.push(c.packP(true)) }

func opPLA(c *CPU, _ uint16) {
	c.peekStack()
	c.A = c.pull()
	c.setZN(c.A)
}

// opPLP は I の変更を 1 命令遅延させる。
func opPLP(c *CPU, _ uint16) {
	c.peekStack()
	c.unpackP(c.pull())
}

// --- フラグ ---

func opCLC(c *CPU, _ uint16) { c.C = false }
func opSEC(c *CPU, _ uint16) { c.C = true }
func opCLD(c *CPU, _ uint16) { c.D = false }
func opSED(c *CPU, _ uint16) { c.D = true }
func opCLV(c *CPU, _ uint16) { c.V = false }

// opCLI と opSEI は I の変更を 1 命令遅延させる。
// opCLI と opSEI は I を即座に変える。
//
// 効果が 1 命令遅れて見えるのは、割り込みのポーリングが命令の最後から
// 2 番目のサイクルの末尾で行われるためである（設計書 03 編 §3.6）。
// フラグの更新そのものを遅らせると、直後に割り込みが起きたときに
// スタックへ積まれる P が実機と違う値になる。
func opCLI(c *CPU, _ uint16) { c.I = false }
func opSEI(c *CPU, _ uint16) { c.I = true }

// --- その他 ---

func opNOP(c *CPU, _ uint16) {}

// opUndefined は未実装の opcode。
//
// 2 サイクル（opcode フェッチと次のバイトの読み捨て）を消費する。
// 実機では 256 個すべての opcode が定まった動作をする。ここで止めずに
// 進めるのは、未実装であることを記録しながらプログラムを動かし続ける
// ためである。
func opUndefined(c *CPU, _ uint16) {
	c.warn("未実装の opcode を実行した（PC=$%04X）", c.opPC)
}
