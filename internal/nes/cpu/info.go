package cpu

// Mode はデバッガへ見せるアドレッシングモード。
type Mode uint8

// アドレッシングモードの一覧。
const (
	ModeImplied Mode = iota
	ModeAccumulator
	ModeImmediate
	ModeZeroPage
	ModeZeroPageX
	ModeZeroPageY
	ModeAbsolute
	ModeAbsoluteX
	ModeAbsoluteY
	ModeIndirectX
	ModeIndirectY
	ModeRelative
	ModeIndirect
	// ModeJSR は JSR の絶対アドレス。
	ModeJSR
)

// OpcodeInfo は opcode 1 つの静的な情報。
//
// 逆アセンブラの本体はデバッガ（internal/debug）が持つ。CPU は命令表
// だけを見せる。トレース出力に要る最小限の逆アセンブル（Disassemble）は
// nestest との比較のために CPU に残す。
type OpcodeInfo struct {
	Mnemonic string
	// Alias は非公式命令の別名。無ければ空文字列。
	Alias string
	Mode  Mode
	// Official は公式命令かを表す。
	Official bool
	// Length は命令の長さ（バイト）。
	Length int
	// Accesses は実効アドレスのデータを読み書きするかを表す。
	// JMP と JSR の飛び先のように、アドレスをデータとして使わない
	// 命令では false になる。
	Accesses bool
}

// Info は opcode の情報を返す。
func Info(code uint8) OpcodeInfo {
	op := &opcodes[code]
	return OpcodeInfo{
		Mnemonic: op.mnemonic,
		Alias:    op.alias,
		Mode:     Mode(op.mode),
		Official: op.official,
		Length:   1 + op.mode.operandLen(),
		Accesses: op.access != accessNone,
	}
}
