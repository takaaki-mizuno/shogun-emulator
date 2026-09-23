package cpu

// opcode は 1 つの opcode の定義。
type opcode struct {
	// mnemonic はニモニック。トレース出力に使う。
	mnemonic string
	// alias は別名。無ければ空文字列。
	alias string
	mode  addrMode
	// access は実効アドレスに対するアクセスの種別。
	// アドレス解決のサイクル数とトレースの注記の有無を決める。
	access accessKind
	// official は公式命令かを表す。トレース出力では非公式命令に * を付ける。
	official bool
	exec     func(c *CPU, addr uint16)
}

// opcodes は 256 個の opcode の表。
//
// 内容は docs/research/02_cpu_6502.md の 6.1 節と 7.1 節の表から転記する。
// 命令の実行を「アドレッシングモードのサイクルシーケンス」と「演算」の
// 組み合わせで表すため、組み合わせが正しければサイクル数も正しくなる。
var opcodes [256]opcode

// undefinedOpcode は未実装の opcode のエントリ。
var undefinedOpcode = opcode{
	mnemonic: "???",
	mode:     modeImplied,
	access:   accessNone,
	official: false,
	exec:     opUndefined,
}

// set は 1 エントリを登録する。二重登録はプログラムの誤りとして止める。
func set(code uint8, mnemonic string, mode addrMode, access accessKind, exec func(*CPU, uint16)) {
	if opcodes[code].exec != nil {
		panic("cpu: opcode の二重登録")
	}
	opcodes[code] = opcode{
		mnemonic: mnemonic,
		mode:     mode,
		access:   access,
		official: true,
		exec:     exec,
	}
}

// setUn は非公式命令の 1 エントリを登録する。
//
// alias には別の文書で使われる名前を入れる。逆アセンブラが併記する。
func setUn(code uint8, mnemonic, alias string, mode addrMode, access accessKind, exec func(*CPU, uint16)) {
	if opcodes[code].exec != nil {
		panic("cpu: opcode の二重登録")
	}
	opcodes[code] = opcode{
		mnemonic: mnemonic,
		alias:    alias,
		mode:     mode,
		access:   access,
		official: false,
		exec:     exec,
	}
}

// setUnAll は同じ命令を複数の opcode に登録する。
func setUnAll(mnemonic, alias string, mode addrMode, access accessKind, exec func(*CPU, uint16), codes ...uint8) {
	for _, code := range codes {
		setUn(code, mnemonic, alias, mode, access, exec)
	}
}

func init() {
	// ロード
	set(0xA9, "LDA", modeImmediate, accessRead, opLDA)
	set(0xA5, "LDA", modeZeroPage, accessRead, opLDA)
	set(0xB5, "LDA", modeZeroPageX, accessRead, opLDA)
	set(0xAD, "LDA", modeAbsolute, accessRead, opLDA)
	set(0xBD, "LDA", modeAbsoluteX, accessRead, opLDA)
	set(0xB9, "LDA", modeAbsoluteY, accessRead, opLDA)
	set(0xA1, "LDA", modeIndirectX, accessRead, opLDA)
	set(0xB1, "LDA", modeIndirectY, accessRead, opLDA)

	set(0xA2, "LDX", modeImmediate, accessRead, opLDX)
	set(0xA6, "LDX", modeZeroPage, accessRead, opLDX)
	set(0xB6, "LDX", modeZeroPageY, accessRead, opLDX)
	set(0xAE, "LDX", modeAbsolute, accessRead, opLDX)
	set(0xBE, "LDX", modeAbsoluteY, accessRead, opLDX)

	set(0xA0, "LDY", modeImmediate, accessRead, opLDY)
	set(0xA4, "LDY", modeZeroPage, accessRead, opLDY)
	set(0xB4, "LDY", modeZeroPageX, accessRead, opLDY)
	set(0xAC, "LDY", modeAbsolute, accessRead, opLDY)
	set(0xBC, "LDY", modeAbsoluteX, accessRead, opLDY)

	// ストア
	set(0x85, "STA", modeZeroPage, accessWrite, opSTA)
	set(0x95, "STA", modeZeroPageX, accessWrite, opSTA)
	set(0x8D, "STA", modeAbsolute, accessWrite, opSTA)
	set(0x9D, "STA", modeAbsoluteX, accessWrite, opSTA)
	set(0x99, "STA", modeAbsoluteY, accessWrite, opSTA)
	set(0x81, "STA", modeIndirectX, accessWrite, opSTA)
	set(0x91, "STA", modeIndirectY, accessWrite, opSTA)

	set(0x86, "STX", modeZeroPage, accessWrite, opSTX)
	set(0x96, "STX", modeZeroPageY, accessWrite, opSTX)
	set(0x8E, "STX", modeAbsolute, accessWrite, opSTX)

	set(0x84, "STY", modeZeroPage, accessWrite, opSTY)
	set(0x94, "STY", modeZeroPageX, accessWrite, opSTY)
	set(0x8C, "STY", modeAbsolute, accessWrite, opSTY)

	// 転送
	set(0xAA, "TAX", modeImplied, accessNone, opTAX)
	set(0xA8, "TAY", modeImplied, accessNone, opTAY)
	set(0x8A, "TXA", modeImplied, accessNone, opTXA)
	set(0x98, "TYA", modeImplied, accessNone, opTYA)
	set(0xBA, "TSX", modeImplied, accessNone, opTSX)
	set(0x9A, "TXS", modeImplied, accessNone, opTXS)

	// 加算
	set(0x69, "ADC", modeImmediate, accessRead, opADC)
	set(0x65, "ADC", modeZeroPage, accessRead, opADC)
	set(0x75, "ADC", modeZeroPageX, accessRead, opADC)
	set(0x6D, "ADC", modeAbsolute, accessRead, opADC)
	set(0x7D, "ADC", modeAbsoluteX, accessRead, opADC)
	set(0x79, "ADC", modeAbsoluteY, accessRead, opADC)
	set(0x61, "ADC", modeIndirectX, accessRead, opADC)
	set(0x71, "ADC", modeIndirectY, accessRead, opADC)

	// 減算
	set(0xE9, "SBC", modeImmediate, accessRead, opSBC)
	set(0xE5, "SBC", modeZeroPage, accessRead, opSBC)
	set(0xF5, "SBC", modeZeroPageX, accessRead, opSBC)
	set(0xED, "SBC", modeAbsolute, accessRead, opSBC)
	set(0xFD, "SBC", modeAbsoluteX, accessRead, opSBC)
	set(0xF9, "SBC", modeAbsoluteY, accessRead, opSBC)
	set(0xE1, "SBC", modeIndirectX, accessRead, opSBC)
	set(0xF1, "SBC", modeIndirectY, accessRead, opSBC)

	// 論理積
	set(0x29, "AND", modeImmediate, accessRead, opAND)
	set(0x25, "AND", modeZeroPage, accessRead, opAND)
	set(0x35, "AND", modeZeroPageX, accessRead, opAND)
	set(0x2D, "AND", modeAbsolute, accessRead, opAND)
	set(0x3D, "AND", modeAbsoluteX, accessRead, opAND)
	set(0x39, "AND", modeAbsoluteY, accessRead, opAND)
	set(0x21, "AND", modeIndirectX, accessRead, opAND)
	set(0x31, "AND", modeIndirectY, accessRead, opAND)

	// 論理和
	set(0x09, "ORA", modeImmediate, accessRead, opORA)
	set(0x05, "ORA", modeZeroPage, accessRead, opORA)
	set(0x15, "ORA", modeZeroPageX, accessRead, opORA)
	set(0x0D, "ORA", modeAbsolute, accessRead, opORA)
	set(0x1D, "ORA", modeAbsoluteX, accessRead, opORA)
	set(0x19, "ORA", modeAbsoluteY, accessRead, opORA)
	set(0x01, "ORA", modeIndirectX, accessRead, opORA)
	set(0x11, "ORA", modeIndirectY, accessRead, opORA)

	// 排他的論理和
	set(0x49, "EOR", modeImmediate, accessRead, opEOR)
	set(0x45, "EOR", modeZeroPage, accessRead, opEOR)
	set(0x55, "EOR", modeZeroPageX, accessRead, opEOR)
	set(0x4D, "EOR", modeAbsolute, accessRead, opEOR)
	set(0x5D, "EOR", modeAbsoluteX, accessRead, opEOR)
	set(0x59, "EOR", modeAbsoluteY, accessRead, opEOR)
	set(0x41, "EOR", modeIndirectX, accessRead, opEOR)
	set(0x51, "EOR", modeIndirectY, accessRead, opEOR)

	// 比較
	set(0xC9, "CMP", modeImmediate, accessRead, opCMP)
	set(0xC5, "CMP", modeZeroPage, accessRead, opCMP)
	set(0xD5, "CMP", modeZeroPageX, accessRead, opCMP)
	set(0xCD, "CMP", modeAbsolute, accessRead, opCMP)
	set(0xDD, "CMP", modeAbsoluteX, accessRead, opCMP)
	set(0xD9, "CMP", modeAbsoluteY, accessRead, opCMP)
	set(0xC1, "CMP", modeIndirectX, accessRead, opCMP)
	set(0xD1, "CMP", modeIndirectY, accessRead, opCMP)

	set(0xE0, "CPX", modeImmediate, accessRead, opCPX)
	set(0xE4, "CPX", modeZeroPage, accessRead, opCPX)
	set(0xEC, "CPX", modeAbsolute, accessRead, opCPX)

	set(0xC0, "CPY", modeImmediate, accessRead, opCPY)
	set(0xC4, "CPY", modeZeroPage, accessRead, opCPY)
	set(0xCC, "CPY", modeAbsolute, accessRead, opCPY)

	set(0x24, "BIT", modeZeroPage, accessRead, opBIT)
	set(0x2C, "BIT", modeAbsolute, accessRead, opBIT)

	// シフト
	set(0x0A, "ASL", modeAccumulator, accessNone, opASLA)
	set(0x06, "ASL", modeZeroPage, accessRMW, opASL)
	set(0x16, "ASL", modeZeroPageX, accessRMW, opASL)
	set(0x0E, "ASL", modeAbsolute, accessRMW, opASL)
	set(0x1E, "ASL", modeAbsoluteX, accessRMW, opASL)

	set(0x4A, "LSR", modeAccumulator, accessNone, opLSRA)
	set(0x46, "LSR", modeZeroPage, accessRMW, opLSR)
	set(0x56, "LSR", modeZeroPageX, accessRMW, opLSR)
	set(0x4E, "LSR", modeAbsolute, accessRMW, opLSR)
	set(0x5E, "LSR", modeAbsoluteX, accessRMW, opLSR)

	set(0x2A, "ROL", modeAccumulator, accessNone, opROLA)
	set(0x26, "ROL", modeZeroPage, accessRMW, opROL)
	set(0x36, "ROL", modeZeroPageX, accessRMW, opROL)
	set(0x2E, "ROL", modeAbsolute, accessRMW, opROL)
	set(0x3E, "ROL", modeAbsoluteX, accessRMW, opROL)

	set(0x6A, "ROR", modeAccumulator, accessNone, opRORA)
	set(0x66, "ROR", modeZeroPage, accessRMW, opROR)
	set(0x76, "ROR", modeZeroPageX, accessRMW, opROR)
	set(0x6E, "ROR", modeAbsolute, accessRMW, opROR)
	set(0x7E, "ROR", modeAbsoluteX, accessRMW, opROR)

	// 増減
	set(0xE6, "INC", modeZeroPage, accessRMW, opINC)
	set(0xF6, "INC", modeZeroPageX, accessRMW, opINC)
	set(0xEE, "INC", modeAbsolute, accessRMW, opINC)
	set(0xFE, "INC", modeAbsoluteX, accessRMW, opINC)

	set(0xC6, "DEC", modeZeroPage, accessRMW, opDEC)
	set(0xD6, "DEC", modeZeroPageX, accessRMW, opDEC)
	set(0xCE, "DEC", modeAbsolute, accessRMW, opDEC)
	set(0xDE, "DEC", modeAbsoluteX, accessRMW, opDEC)

	set(0xE8, "INX", modeImplied, accessNone, opINX)
	set(0xC8, "INY", modeImplied, accessNone, opINY)
	set(0xCA, "DEX", modeImplied, accessNone, opDEX)
	set(0x88, "DEY", modeImplied, accessNone, opDEY)

	// 分岐
	set(0x10, "BPL", modeRelative, accessNone, opBPL)
	set(0x30, "BMI", modeRelative, accessNone, opBMI)
	set(0x50, "BVC", modeRelative, accessNone, opBVC)
	set(0x70, "BVS", modeRelative, accessNone, opBVS)
	set(0x90, "BCC", modeRelative, accessNone, opBCC)
	set(0xB0, "BCS", modeRelative, accessNone, opBCS)
	set(0xD0, "BNE", modeRelative, accessNone, opBNE)
	set(0xF0, "BEQ", modeRelative, accessNone, opBEQ)

	// ジャンプ
	set(0x4C, "JMP", modeAbsolute, accessNone, opJMP)
	set(0x6C, "JMP", modeIndirect, accessNone, opJMP)
	set(0x20, "JSR", modeJSR, accessNone, opJSR)
	set(0x60, "RTS", modeImplied, accessNone, opRTS)
	set(0x00, "BRK", modeImplied, accessNone, opBRK)
	set(0x40, "RTI", modeImplied, accessNone, opRTI)

	// スタック
	set(0x48, "PHA", modeImplied, accessNone, opPHA)
	set(0x08, "PHP", modeImplied, accessNone, opPHP)
	set(0x68, "PLA", modeImplied, accessNone, opPLA)
	set(0x28, "PLP", modeImplied, accessNone, opPLP)

	// フラグ
	set(0x18, "CLC", modeImplied, accessNone, opCLC)
	set(0x38, "SEC", modeImplied, accessNone, opSEC)
	set(0x58, "CLI", modeImplied, accessNone, opCLI)
	set(0x78, "SEI", modeImplied, accessNone, opSEI)
	set(0xD8, "CLD", modeImplied, accessNone, opCLD)
	set(0xF8, "SED", modeImplied, accessNone, opSED)
	set(0xB8, "CLV", modeImplied, accessNone, opCLV)

	// その他
	set(0xEA, "NOP", modeImplied, accessNone, opNOP)

	registerUnofficial()

	// 表が埋まっていることをここで確かめる。実機では 256 個すべての
	// opcode が定まった動作をする。埋め忘れを起動時に見つける。
	for i := range opcodes {
		if opcodes[i].exec == nil {
			opcodes[i] = undefinedOpcode
		}
	}
}

// registerUnofficial は非公式命令 105 個を登録する。
//
// 内容は docs/research/02_cpu_6502.md の 7.1 節の 256 エントリの表から
// 転記する。ニモニックは nestest.log の表記に合わせる。トレースを
// nestest.log と行単位で比較することが CPU の検証手段であり、別表記では
// 比較できないためである。別名は alias に入れる。
func registerUnofficial() {
	// RMW 系（RMW 演算と ALU 演算の合成）
	// アドレッシングモードの並びは (d,X) / d / a / (d),Y / d,X / a,Y / a,X
	registerRMWCombo("SLO", "ASO", opSLO, 0x03, 0x07, 0x0F, 0x13, 0x17, 0x1B, 0x1F)
	registerRMWCombo("RLA", "RLN", opRLA, 0x23, 0x27, 0x2F, 0x33, 0x37, 0x3B, 0x3F)
	registerRMWCombo("SRE", "LSE", opSRE, 0x43, 0x47, 0x4F, 0x53, 0x57, 0x5B, 0x5F)
	registerRMWCombo("RRA", "RRD", opRRA, 0x63, 0x67, 0x6F, 0x73, 0x77, 0x7B, 0x7F)
	registerRMWCombo("DCP", "DCM", opDCP, 0xC3, 0xC7, 0xCF, 0xD3, 0xD7, 0xDB, 0xDF)
	registerRMWCombo("ISB", "ISC", opISC, 0xE3, 0xE7, 0xEF, 0xF3, 0xF7, 0xFB, 0xFF)

	// ロード系
	setUn(0xA3, "LAX", "", modeIndirectX, accessRead, opLAX)
	setUn(0xA7, "LAX", "", modeZeroPage, accessRead, opLAX)
	setUn(0xAF, "LAX", "", modeAbsolute, accessRead, opLAX)
	setUn(0xB3, "LAX", "", modeIndirectY, accessRead, opLAX)
	setUn(0xB7, "LAX", "", modeZeroPageY, accessRead, opLAX)
	setUn(0xBF, "LAX", "", modeAbsoluteY, accessRead, opLAX)

	// ストア系
	setUn(0x83, "SAX", "AXS", modeIndirectX, accessWrite, opSAX)
	setUn(0x87, "SAX", "AXS", modeZeroPage, accessWrite, opSAX)
	setUn(0x8F, "SAX", "AXS", modeAbsolute, accessWrite, opSAX)
	setUn(0x97, "SAX", "AXS", modeZeroPageY, accessWrite, opSAX)

	// 即値系
	setUnAll("ANC", "", modeImmediate, accessRead, opANC, 0x0B, 0x2B)
	setUn(0x4B, "ALR", "ASR", modeImmediate, accessRead, opALR)
	setUn(0x6B, "ARR", "", modeImmediate, accessRead, opARR)
	setUn(0xCB, "AXS", "SBX", modeImmediate, accessRead, opAXS)
	// $EB は公式の $E9 と同一動作
	setUn(0xEB, "SBC", "USBC", modeImmediate, accessRead, opSBC)

	// NOP 系
	setUnAll("NOP", "", modeImplied, accessNone, opNOP,
		0x1A, 0x3A, 0x5A, 0x7A, 0xDA, 0xFA)
	setUnAll("NOP", "SKB", modeImmediate, accessRead, opSKB,
		0x80, 0x82, 0x89, 0xC2, 0xE2)
	setUnAll("NOP", "IGN", modeZeroPage, accessRead, opIGN,
		0x04, 0x44, 0x64)
	setUnAll("NOP", "IGN", modeZeroPageX, accessRead, opIGN,
		0x14, 0x34, 0x54, 0x74, 0xD4, 0xF4)
	setUn(0x0C, "NOP", "IGN", modeAbsolute, accessRead, opIGN)
	setUnAll("NOP", "IGN", modeAbsoluteX, accessRead, opIGN,
		0x1C, 0x3C, 0x5C, 0x7C, 0xDC, 0xFC)

	// 不安定な非公式命令
	setUn(0x8B, "XAA", "ANE", modeImmediate, accessRead, opXAA)
	setUn(0xAB, "LAX", "LXA", modeImmediate, accessRead, opLAXImmediate)
	setUn(0x9B, "TAS", "SHS", modeAbsoluteY, accessWrite, opTAS)
	setUn(0x93, "AHX", "SHA", modeIndirectY, accessWrite, opAHX)
	setUn(0x9F, "AHX", "SHA", modeAbsoluteY, accessWrite, opAHX)
	setUn(0x9C, "SHY", "SAY", modeAbsoluteX, accessWrite, opSHY)
	setUn(0x9E, "SHX", "XAS", modeAbsoluteY, accessWrite, opSHX)
	setUn(0xBB, "LAS", "LAE", modeAbsoluteY, accessRead, opLAS)

	// STP
	setUnAll("STP", "JAM", modeImplied, accessNone, opSTP,
		0x02, 0x12, 0x22, 0x32, 0x42, 0x52, 0x62, 0x72,
		0x92, 0xB2, 0xD2, 0xF2)
}

// registerRMWCombo は RMW 演算と ALU 演算を合成した非公式命令を
// 7 つのアドレッシングモードに登録する。
//
// codes の並びは (d,X) / d / a / (d),Y / d,X / a,Y / a,X であり、
// 調査結果の表の並びと同じにしてある。
func registerRMWCombo(mnemonic, alias string, exec func(*CPU, uint16), codes ...uint8) {
	modes := []addrMode{
		modeIndirectX, modeZeroPage, modeAbsolute, modeIndirectY,
		modeZeroPageX, modeAbsoluteY, modeAbsoluteX,
	}
	if len(codes) != len(modes) {
		panic("cpu: RMW 系非公式命令の opcode の数が合わない")
	}
	for i, code := range codes {
		setUn(code, mnemonic, alias, modes[i], accessRMW, exec)
	}
}

// StepInstruction は 1 命令を実行する。
func (c *CPU) StepInstruction() {
	if c.halted {
		// STP の後もバスは止まらない。無限ループにしない。
		c.read(c.PC)
		return
	}

	c.opPC = c.PC
	op := &opcodes[c.fetch()]
	addr := c.resolve(op)
	op.exec(c, addr)

	// ポーリング結果は各サイクルの終わりに更新されている（endCycle）。
	// ここでは最後から 2 番目のサイクルの結果を使って判定する。
	c.takePolledInterrupt()
}
