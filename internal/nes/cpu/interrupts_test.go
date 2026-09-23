package cpu

import "testing"

// atCycle は指定したバスアクセス回数の後に割り込み線を操作する仕掛けを
// fakeBus へ取り付ける。
//
// 「命令の最後から 2 番目のサイクルの終わり」という位置を検証するには、
// サイクル単位で線を動かす必要がある。
func atCycle(b *fakeBus, n int, f func(*fakeBus)) {
	b.onAccess = func(count int) {
		if count == n {
			f(b)
		}
	}
}

// TestIRQTakenWhenAssertedBeforeSecondToLastCycle は最後から 2 番目の
// サイクルの終わりに IRQ がアサートされていれば、その命令の直後に
// 割り込みが入ることを確かめる。
func TestIRQTakenWhenAssertedBeforeSecondToLastCycle(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.load(vectorIRQ, 0x00, 0x90)
	// NOP を並べる。1 命令 2 サイクル。
	b.load(0x8000, 0xEA, 0xEA, 0xEA, 0xEA)

	// 1 サイクル目（opcode フェッチ）の終わりにアサートする。
	// これが最後から 2 番目のサイクルにあたる。
	atCycle(b, 1, func(b *fakeBus) { b.irq = true })

	c.StepInstruction()

	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（この命令の直後に IRQ が入ること）", c.PC)
	}
}

// TestIRQDelayedWhenAssertedInLastCycle は最後のサイクルでアサートされた
// IRQ がその命令では受け付けられず、次の命令の後に入ることを確かめる。
//
// 「最終サイクルでポーリングする」と実装すると、この 1 命令の差が出ない。
func TestIRQDelayedWhenAssertedInLastCycle(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xEA, 0xEA, 0xEA)

	// 2 サイクル目（最後のサイクル）の終わりにアサートする。
	atCycle(b, 2, func(b *fakeBus) { b.irq = true })

	c.StepInstruction()
	if c.PC != 0x8001 {
		t.Fatalf("PC = $%04X, 期待 $8001（この命令では受け付けないこと）", c.PC)
	}

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（次の命令の後に入ること）", c.PC)
	}
}

// TestIRQMaskedByIFlagAtPollPoint はポーリング点での I の値で
// マスクが決まることを確かめる。
func TestIRQMaskedByIFlagAtPollPoint(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = true
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xEA, 0xEA)

	c.StepInstruction()
	if c.PC != 0x8001 {
		t.Errorf("PC = $%04X, 期待 $8001（I が立っているのでマスクされること）", c.PC)
	}
}

// TestCLILatency は CLI の後 1 命令が実行されてから IRQ が入ることを
// 確かめる。
//
// CLI は割り込みポーリングの後に I を変更する。したがって CLI の直後の
// ポーリング点では I がまだ立っており、次の命令の後まで割り込みが延びる。
func TestCLILatency(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = true
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0x58, 0xEA, 0xEA) // CLI, NOP, NOP

	c.StepInstruction() // CLI
	if c.PC != 0x8001 {
		t.Fatalf("CLI の直後に PC = $%04X。割り込みが入るのは早すぎる", c.PC)
	}

	c.StepInstruction() // NOP。この命令の後に IRQ が入る
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（CLI の次の命令の後に入ること）", c.PC)
	}
}

// TestSEILatency は SEI の直後の 1 命令の前にはまだ IRQ が入りうることを
// 確かめる。
func TestSEILatency(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0x78, 0xEA) // SEI, NOP

	c.StepInstruction() // SEI。ポーリング点では I がまだ下りている
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（SEI の効果は 1 命令遅れること）", c.PC)
	}
}

// TestRTIAppliesIBeforePolling は RTI が I を即座に反映し、その結果が
// ポーリングに反映されることを確かめる。
func TestRTIAppliesIBeforePolling(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = true
	b.irq = true
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0x40) // RTI
	c.S = 0xFA
	// P は I = 0、戻り先は $8100
	b.load(0x01FB, 0x00, 0x00, 0x81)

	c.StepInstruction()

	// RTI は I を即座に下ろすため、RTI の直後に IRQ が入る
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（RTI は I を即座に反映すること）", c.PC)
	}
}

// TestBranchNotTakenPollsSecondToLastCycle は分岐が成立しないときの
// ポーリング点を確かめる。2 サイクル命令なので一般の規則と同じになる。
func TestBranchNotTakenPollsSecondToLastCycle(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	c.Z = true // BNE は成立しない
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xD0, 0x10, 0xEA)

	atCycle(b, 1, func(b *fakeBus) { b.irq = true })

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000", c.PC)
	}
}

// TestBranchTakenDoesNotPollThirdCycle は分岐が成立したときに
// 3 サイクル目の前でポーリングしないことを確かめる。
//
// 一般の規則（最後から 2 番目のサイクル）をそのまま当てると、3 サイクルの
// 分岐では 2 サイクル目の終わりを見てしまう。分岐命令はそこを見ない。
func TestBranchTakenDoesNotPollThirdCycle(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	c.Z = false // BNE は成立する
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xD0, 0x10) // BNE $8012（同じページ内）
	b.load(0x8012, 0xEA, 0xEA)

	// 2 サイクル目の終わりにアサートする。分岐成立時はこの点を見ない。
	atCycle(b, 2, func(b *fakeBus) { b.irq = true })

	c.StepInstruction()
	if c.PC != 0x8012 {
		t.Fatalf("PC = $%04X, 期待 $8012（分岐成立後に割り込みが入るのは早すぎる）", c.PC)
	}

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（次の命令の後に入ること）", c.PC)
	}
}

// TestBranchTakenPollsSecondCycle は分岐が成立しても 2 サイクル目の前の
// ポーリング点は有効であることを確かめる。
func TestBranchTakenPollsSecondCycle(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	c.Z = false
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xD0, 0x10)
	b.load(0x8012, 0xEA)

	// 1 サイクル目の終わり = 2 サイクル目の前
	atCycle(b, 1, func(b *fakeBus) { b.irq = true })

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（2 サイクル目の前のポーリングは有効）", c.PC)
	}
}

// TestBranchPageCrossPollsFixupCycle はページ越えのときに PCH 修正
// サイクルの前でポーリングすることを確かめる。
func TestBranchPageCrossPollsFixupCycle(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	c.Z = false
	b.load(vectorIRQ, 0x00, 0x90)
	// $80F0 から +$20 で $8112。ページが変わる（4 サイクル）
	b.load(0x80F0, 0xD0, 0x20)
	b.load(0x8112, 0xEA)
	c.PC = 0x80F0

	// 3 サイクル目の終わり = PCH 修正サイクルの前
	atCycle(b, 3, func(b *fakeBus) { b.irq = true })

	c.StepInstruction()
	if c.PC != 0x9000 {
		t.Errorf("PC = $%04X, 期待 $9000（PCH 修正サイクルの前のポーリングは有効）", c.PC)
	}
}

// TestInterruptSequenceDoesNotPoll は割り込みシーケンス中にポーリングを
// 行わず、ハンドラの最初の 1 命令が必ず実行されることを確かめる。
func TestInterruptSequenceDoesNotPoll(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.irq = true // 常にアサートし続ける
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xEA)
	// ハンドラの先頭は SEI。これが必ず実行されなければならない。
	b.load(0x9000, 0x78, 0xEA)

	c.StepInstruction() // NOP → IRQ シーケンス
	if c.PC != 0x9000 {
		t.Fatalf("PC = $%04X, 期待 $9000", c.PC)
	}

	c.StepInstruction() // ハンドラの SEI
	if c.PC != 0x9001 {
		t.Errorf("PC = $%04X, 期待 $9001（ハンドラの最初の 1 命令は必ず実行されること）", c.PC)
	}
}

// TestNMIHijacksBRK は BRK の実行中に NMI がアサートされたとき、
// B フラグを立てたまま NMI ベクタへ分岐することを確かめる。
func TestNMIHijacksBRK(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x00) // BRK
	b.load(vectorNMI, 0x00, 0xA0)
	b.load(vectorIRQ, 0x00, 0x90)

	// BRK の 4 サイクル目までに NMI を立ち下げる
	atCycle(b, 3, func(b *fakeBus) { b.nmiLine = false })

	c.StepInstruction()

	if c.PC != 0xA000 {
		t.Errorf("PC = $%04X, 期待 $A000（NMI ベクタへ分岐すること）", c.PC)
	}
	p := b.mem[0x01FB]
	if p&0x10 == 0 {
		t.Errorf("push された P = $%02X。B フラグは立ったままであること", p)
	}
}

// TestNMIDoesNotHijackBRKAfterCycleFour は 4 サイクル目より後に
// NMI がアサートされたとき、BRK が IRQ ベクタへ分岐することを確かめる。
//
// ベクタが決まるのはサイクル 4 と 5 の間である。この場合 BRK は $FFFE を
// 使って BRK ハンドラへ入り、その直後に NMI が処理される。BRK の
// 最後から 2 番目のサイクルのポーリングで NMI が検出されるためである。
// したがって NMI が push する戻り先が BRK ハンドラの先頭になる。
func TestNMIDoesNotHijackBRKAfterCycleFour(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x00)
	b.load(vectorNMI, 0x00, 0xA0)
	b.load(vectorIRQ, 0x00, 0x90)

	// 5 サイクル目の後に立ち下げる。ベクタの決定はもう済んでいる。
	atCycle(b, 5, func(b *fakeBus) { b.nmiLine = false })

	c.StepInstruction()

	// BRK が 3 バイト、続く NMI が 3 バイトを積む
	if c.S != 0xF7 {
		t.Errorf("S = $%02X, 期待 $F7（BRK と NMI で 6 バイト積まれること）", c.S)
	}
	// NMI が push した戻り先が BRK ハンドラの先頭であること
	if hi, lo := b.mem[0x01FA], b.mem[0x01F9]; hi != 0x90 || lo != 0x00 {
		t.Errorf("NMI の戻り先 = $%02X%02X, 期待 $9000（BRK は $FFFE を使うこと）", hi, lo)
	}
	if c.PC != 0xA000 {
		t.Errorf("PC = $%04X, 期待 $A000（BRK の直後に NMI が処理されること）", c.PC)
	}
}

// TestNMIHijacksIRQ は IRQ シーケンス中に NMI がアサートされたとき
// NMI ベクタへ分岐することを確かめる。
func TestNMIHijacksIRQ(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.irq = true
	b.load(vectorNMI, 0x00, 0xA0)
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xEA)

	// NOP が 2 サイクル、IRQ シーケンスの 4 サイクル目までに立ち下げる
	atCycle(b, 5, func(b *fakeBus) { b.nmiLine = false })

	c.StepInstruction()

	if c.PC != 0xA000 {
		t.Errorf("PC = $%04X, 期待 $A000（NMI が IRQ をハイジャックすること）", c.PC)
	}
	p := b.mem[0x01FB]
	if p&0x10 != 0 {
		t.Errorf("push された P = $%02X。IRQ 由来なので B フラグは立たないこと", p)
	}
}

// TestNMITakesPriorityOverIRQ は両方が検出されたとき NMI が処理され、
// IRQ の検出が破棄されることを確かめる。
func TestNMITakesPriorityOverIRQ(t *testing.T) {
	c, b := newTestCPU(t)
	c.I = false
	b.irq = true
	b.nmiLine = false // 最初から立ち下げておく
	b.load(vectorNMI, 0x00, 0xA0)
	b.load(vectorIRQ, 0x00, 0x90)
	b.load(0x8000, 0xEA, 0xEA)

	c.StepInstruction()
	if c.PC != 0xA000 {
		t.Errorf("PC = $%04X, 期待 $A000（NMI が優先されること）", c.PC)
	}
}

// TestNMIRetriggersOnNewEdge は NMI が処理された後、新しい立ち下がりで
// 再び発生することを確かめる。
func TestNMIRetriggersOnNewEdge(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(vectorNMI, 0x00, 0xA0)
	b.load(0x8000, 0xEA)
	b.load(0xA000, 0x40) // RTI

	b.nmiLine = false
	c.StepInstruction() // NOP → NMI
	if c.PC != 0xA000 {
		t.Fatalf("PC = $%04X, 期待 $A000", c.PC)
	}

	// 線を戻してから、もう一度立ち下げる
	b.nmiLine = true
	c.StepInstruction() // RTI で $8001 へ戻る
	b.nmiLine = false
	b.load(0x8001, 0xEA)
	c.StepInstruction() // NOP → 新しいエッジで NMI

	if c.PC != 0xA000 {
		t.Errorf("PC = $%04X, 期待 $A000（新しい立ち下がりで再発すること）", c.PC)
	}
}

// TestHaltedCPUIgnoresInterrupts は STP で停止した CPU が割り込みで
// 復帰しないことを確かめる。復帰にはリセットが必要である。
func TestHaltedCPUIgnoresInterrupts(t *testing.T) {
	c, b := newTestCPU(t)
	b.load(0x8000, 0x02) // STP
	b.load(vectorNMI, 0x00, 0xA0)

	c.StepInstruction()
	if !c.Halted() {
		t.Fatal("STP で停止していない")
	}

	b.nmiLine = false
	before := c.PC
	c.StepInstruction()
	if c.PC != before {
		t.Errorf("停止中に PC が $%04X から $%04X へ動いた", before, c.PC)
	}

	b.load(vectorReset, 0x00, 0x80)
	c.Reset()
	if c.Halted() {
		t.Error("リセットで停止が解除されていない")
	}
}
