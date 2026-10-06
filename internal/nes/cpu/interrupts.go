package cpu

// interruptKind は割り込みの種別。
type interruptKind uint8

const (
	// interruptIRQ は IRQ 線によるもの。
	interruptIRQ interruptKind = iota
	// interruptNMI は NMI 線によるもの。
	interruptNMI
	// interruptBRK は BRK 命令によるもの。
	interruptBRK
	// interruptReset はリセット。
	interruptReset
)

// Interrupt はデバッガへ知らせる割り込みの種別。
type Interrupt uint8

const (
	// InterruptIRQ は IRQ 線による割り込み。
	InterruptIRQ Interrupt = iota
	// InterruptNMI は NMI 線による割り込み。
	InterruptNMI
	// InterruptBRK は BRK 命令。
	InterruptBRK
	// InterruptReset はリセット。
	InterruptReset
)

// String は種別の名前を返す。
func (k Interrupt) String() string {
	switch k {
	case InterruptIRQ:
		return "IRQ"
	case InterruptNMI:
		return "NMI"
	case InterruptBRK:
		return "BRK"
	case InterruptReset:
		return "RESET"
	}
	return "?"
}

// endCycle は 1 サイクルの終わりの処理を行う。
//
// 割り込み線を採取し、ポーリング結果を 1 サイクル分ずらして保持する。
// 割り込みが実際に効くのは「命令の最後から 2 番目のサイクルの終わり」の
// 割り込み線の状態であり、最後のサイクルを終えた時点でその結果が
// pollNMIPrev と pollIRQPrev に残る。
func (c *CPU) endCycle() {
	c.sampleInterruptLines()
	c.pollNMIPrev, c.pollIRQPrev = c.pollNMI, c.pollIRQ
	c.pollNMI = c.nmiPending
	c.pollIRQ = !c.I && c.bus.IRQAsserted()
}

// sampleInterruptLines は割り込み線を採取する。
//
// NMI はエッジ検出である。線は負論理であり、立ち下がりでアサートされる。
// 検出した pending は処理されるまで保持する。
func (c *CPU) sampleInterruptLines() {
	nmi := c.bus.NMILine()
	if c.nmiLinePrev && !nmi {
		c.nmiPending = true
	}
	c.nmiLinePrev = nmi
}

// pollResult は命令の終わりに使うポーリング結果を返す。
//
// 命令側が明示的に決めていればそれを使う。分岐命令だけがそうする。
func (c *CPU) pollResult() (nmi, irq bool) {
	if c.overridePoll {
		return c.overrideNMI, c.overrideIRQ
	}
	return c.pollNMIPrev, c.pollIRQPrev
}

// setPollResult は命令側がポーリング結果を決める。
func (c *CPU) setPollResult(nmi, irq bool) {
	c.overridePoll = true
	c.overrideNMI = nmi
	c.overrideIRQ = irq
}

// takePolledInterrupt はポーリングの結果に従って割り込みを処理する。
//
// NMI と IRQ が同時に検出されたとき NMI を処理し、IRQ の検出は破棄する。
// IRQ はレベル検出であるため、次のポーリングで再検出される。
func (c *CPU) takePolledInterrupt() {
	nmi, irq := c.pollResult()
	c.overridePoll = false

	switch {
	case nmi:
		c.serviceInterrupt(interruptNMI)
	case irq:
		c.serviceInterrupt(interruptIRQ)
	}
}

// resolveVector は使用するベクタのアドレスを返す。
//
// 呼ばれるのは割り込みシーケンスのサイクル 4 とサイクル 5 の間である。
// 実機でベクタが決まるのはこの位置であり、割り込みハイジャックはこの
// 1 箇所で表現できる。
//
// この時点で NMI が pending であれば、種別が何であっても NMI ベクタを
// 使う。BRK の実行中に NMI がアサートされたとき、B フラグを立てたまま
// NMI ハンドラへ分岐する挙動がこれで表される。IRQ と BRK はベクタが
// 同じであるため、IRQ が BRK をハイジャックしても分岐先は変わらない。
func (c *CPU) resolveVector(kind interruptKind) uint16 {
	if kind == interruptReset {
		return vectorReset
	}
	if c.nmiPending {
		c.nmiPending = false
		return vectorNMI
	}
	return vectorIRQ
}

// serviceInterrupt は割り込みシーケンスを実行する。7 サイクルを消費する。
//
// シーケンス自体はポーリングを行わない。割り込みハンドラの最初の 1 命令は
// 必ず実行される。
func (c *CPU) serviceInterrupt(kind interruptKind) {
	c.dummyRead(c.PC) // 1: opcode をフェッチして破棄
	c.dummyRead(c.PC) // 2: 同じアドレスを読んで破棄

	c.push(uint8(c.PC >> 8)) // 3
	c.push(uint8(c.PC))      // 4

	vec := c.resolveVector(kind)

	c.push(c.packP(kind == interruptBRK)) // 5
	c.I = true

	lo := c.read(vec)     // 6
	hi := c.read(vec + 1) // 7
	c.PC = uint16(hi)<<8 | uint16(lo)

	if c.OnInterrupt != nil {
		c.OnInterrupt(reportedInterrupt(kind, vec))
	}
}

// reportedInterrupt はデバッガへ知らせる種別を決める。
//
// 実際に使ったベクタで判定する。BRK や IRQ の途中で NMI がベクタを
// 横取りしたときは NMI として知らせる。
func reportedInterrupt(kind interruptKind, vec uint16) Interrupt {
	switch {
	case kind == interruptReset:
		return InterruptReset
	case vec == vectorNMI:
		return InterruptNMI
	case kind == interruptBRK:
		return InterruptBRK
	}
	return InterruptIRQ
}

// NMIPending は NMI の立ち下がりを検出して処理を待っているかを返す。
// デバッガの表示に使う。
func (c *CPU) NMIPending() bool { return c.nmiPending }
