package cart

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
)

// Format はヘッダの形式。
type Format uint8

const (
	// FormatArchaicINES はバイト 7-15 の意味が定まっていない初期の iNES。
	FormatArchaicINES Format = iota
	// FormatINES は iNES 1.0。
	FormatINES
	// FormatNES20 は NES 2.0。
	FormatNES20
)

// String は形式の名前を返す。
func (f Format) String() string {
	switch f {
	case FormatArchaicINES:
		return "archaic iNES"
	case FormatINES:
		return "iNES"
	case FormatNES20:
		return "NES 2.0"
	}
	return "unknown"
}

// タイミングモード。ヘッダのバイト 12 の下位 2 bit。
const (
	TimingNTSC     uint8 = 0
	TimingPAL      uint8 = 1
	TimingMultiple uint8 = 2
	TimingDendy    uint8 = 3
)

// ヘッダとサイズの単位。
const (
	headerSize  = 16
	trainerSize = 512
	prgUnit     = 16 * 1024
	chrUnit     = 8 * 1024
)

// magic は `.nes` ファイルの先頭 4 バイト "NES\x1A"。
var magic = [4]uint8{0x4E, 0x45, 0x53, 0x1A}

// ErrNotINES はファイルが iNES 形式でないことを表す。
var ErrNotINES = errors.New("cart: iNES ヘッダのマジックが一致しない")

// ROM は解析済みの ROM イメージ。
type ROM struct {
	Format    Format
	Mapper    uint16
	Submapper uint8

	PRG     []uint8
	CHR     []uint8
	Trainer []uint8
	MiscROM []uint8

	PRGRAMSize   int
	PRGNVRAMSize int
	CHRRAMSize   int
	CHRNVRAMSize int

	Mirroring   Mirroring
	FourScreen  bool
	HasBattery  bool
	ConsoleType uint8
	TimingMode  uint8

	// Hash は PRG と CHR を連結した SHA-1。セーブステートの照合に使う。
	Hash [20]uint8

	// Warnings はヘッダの解釈で補正した点。呼び出し側がログに出す。
	Warnings []string

	// overlay は PRG と CHR への変更。Overlay で作る。
	overlay *Overlay
}

// LoadROM は `.nes` ファイルの内容を解析する。
func LoadROM(data []uint8) (*ROM, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("cart: ファイルが短すぎる（%d バイト）", len(data))
	}
	h := data[:headerSize]
	if [4]uint8(h[0:4]) != magic {
		return nil, ErrNotINES
	}

	r := &ROM{}
	r.Format = detectFormat(h, len(data))

	// バイト 6: 下位 4 bit がフラグ、上位 4 bit がマッパー番号の下位ニブル
	flags6 := h[6]
	r.Mirroring = MirrorHorizontal
	if flags6&0x01 != 0 {
		r.Mirroring = MirrorVertical
	}
	r.HasBattery = flags6&0x02 != 0
	hasTrainer := flags6&0x04 != 0
	r.FourScreen = flags6&0x08 != 0

	mapper := uint16(flags6 >> 4)
	switch r.Format {
	case FormatArchaicINES:
		// バイト 7 以降にリッパーの署名が入っていることがある。
		// 上位ニブルを採用すると誤ったマッパー番号になる。
		if h[7]>>4 != 0 {
			r.warn("archaic iNES と判定したため、マッパー番号の上位 4 bit（$%X）を無視した", h[7]>>4)
		}
	case FormatINES:
		mapper |= uint16(h[7] & 0xF0)
		r.ConsoleType = h[7] & 0x03
	case FormatNES20:
		mapper |= uint16(h[7] & 0xF0)
		mapper |= uint16(h[8]&0x0F) << 8
		r.Submapper = h[8] >> 4
		r.ConsoleType = h[7] & 0x03
		r.TimingMode = h[12] & 0x03
	}
	r.Mapper = mapper

	prgSize, chrSize := romSizes(h, r.Format)
	if prgSize == 0 {
		return nil, errors.New("cart: PRG-ROM のサイズが 0 である")
	}

	r.prgramSizes(h)
	r.chrramSizes(h, chrSize)

	pos := headerSize
	if hasTrainer {
		if pos+trainerSize > len(data) {
			return nil, fmt.Errorf("cart: トレーナーがファイルの末尾を超える")
		}
		r.Trainer = clone(data[pos : pos+trainerSize])
		pos += trainerSize
	}

	if pos+prgSize > len(data) {
		return nil, fmt.Errorf("cart: PRG-ROM %d バイトがファイルの末尾を超える（残り %d バイト）",
			prgSize, len(data)-pos)
	}
	r.PRG = clone(data[pos : pos+prgSize])
	pos += prgSize

	if pos+chrSize > len(data) {
		return nil, fmt.Errorf("cart: CHR-ROM %d バイトがファイルの末尾を超える（残り %d バイト）",
			chrSize, len(data)-pos)
	}
	r.CHR = clone(data[pos : pos+chrSize])
	pos += chrSize

	if pos < len(data) {
		r.MiscROM = clone(data[pos:])
	}

	// PRG と CHR を連結した SHA-1。ヘッダを含めないのは、ヘッダだけが
	// 異なる同一の ROM を同じものとして扱うためである。
	hash := sha1.New()
	hash.Write(r.PRG)
	hash.Write(r.CHR)
	copy(r.Hash[:], hash.Sum(nil))

	return r, nil
}

// detectFormat はヘッダの形式を判別する。
func detectFormat(h []uint8, fileSize int) Format {
	switch {
	case h[7]&0x0C == 0x08 && nes20SizeFits(h, fileSize):
		return FormatNES20
	case h[7]&0x0C == 0x04:
		return FormatArchaicINES
	case h[7]&0x0C == 0x00 && allZero(h[12:16]):
		return FormatINES
	default:
		return FormatArchaicINES
	}
}

// nes20SizeFits は NES 2.0 として解釈したサイズがファイルに収まるかを返す。
//
// バイト 7 の bit 3-2 が `10` でも NES 2.0 でないファイルがある。サイズが
// 収まるかを追加の条件にすることで、そのようなファイルを iNES として扱う。
func nes20SizeFits(h []uint8, fileSize int) bool {
	prg := nes20Size(h[4], h[9]&0x0F, prgUnit)
	chr := nes20Size(h[5], h[9]>>4, chrUnit)
	need := headerSize + prg + chr
	if h[6]&0x04 != 0 {
		need += trainerSize
	}
	return need <= fileSize
}

// nes20Size は NES 2.0 のサイズ表記を解釈する。
//
// MSB ニブルが $F のときは指数・乗数表記になる。
func nes20Size(lsb uint8, msb uint8, unit int) int {
	if msb == 0x0F {
		exp := (lsb >> 2) & 0x3F
		mult := int(lsb&0x03)*2 + 1
		if exp >= 48 {
			// 1 << 48 を超える指数は現実の ROM に存在しない。
			// オーバーフローを避けるために 0 を返す。
			return 0
		}
		return (1 << exp) * mult
	}
	return (int(msb)<<8 | int(lsb)) * unit
}

// romSizes は PRG-ROM と CHR-ROM のサイズを返す。
func romSizes(h []uint8, f Format) (prg, chr int) {
	if f == FormatNES20 {
		return nes20Size(h[4], h[9]&0x0F, prgUnit), nes20Size(h[5], h[9]>>4, chrUnit)
	}
	return int(h[4]) * prgUnit, int(h[5]) * chrUnit
}

// prgramSizes は PRG-RAM と PRG-NVRAM のサイズを決める。
func (r *ROM) prgramSizes(h []uint8) {
	if r.Format == FormatNES20 {
		r.PRGRAMSize = shiftSize(h[10] & 0x0F)
		r.PRGNVRAMSize = shiftSize(h[10] >> 4)
		return
	}
	// iNES にはサイズを表す手段がない。バッテリーがあるか、PRG-RAM を持つ
	// 構成のマッパーであれば 8 KiB を割り当てる。
	size := defaultPRGRAMSize(r.Mapper)
	if r.HasBattery {
		r.PRGNVRAMSize = size
		return
	}
	r.PRGRAMSize = size
}

// chrramSizes は CHR-RAM と CHR-NVRAM のサイズを決める。
func (r *ROM) chrramSizes(h []uint8, chrROMSize int) {
	if r.Format == FormatNES20 {
		r.CHRRAMSize = shiftSize(h[11] & 0x0F)
		r.CHRNVRAMSize = shiftSize(h[11] >> 4)
		return
	}
	// CHR-ROM が無いカートリッジは CHR-RAM を持つ。
	if chrROMSize == 0 {
		r.CHRRAMSize = chrUnit
	}
}

// shiftSize は NES 2.0 のシフトカウントをバイト数にする。
//
// バイト数は 64 << シフトカウントである。0 のときそのメモリを持たない。
func shiftSize(shift uint8) int {
	if shift == 0 {
		return 0
	}
	if shift > maxShiftCount {
		return 0
	}
	return 64 << shift
}

// maxShiftCount は受け付けるシフトカウントの上限。
//
// 64 << 24 は 1 GiB である。これを超えるメモリを持つカートリッジは
// 存在せず、32 bit 環境での桁あふれを避けるために打ち切る。
const maxShiftCount = 24

// defaultPRGRAMSize は iNES で PRG-RAM のサイズが分からないときの既定値。
func defaultPRGRAMSize(mapper uint16) int {
	if mapper == 1 {
		// MMC1 は 32 KiB の構成が存在する。少なく割り当てると
		// SRAM を使うゲームが動かない。
		return 32 * 1024
	}
	return 8 * 1024
}

// warn は補正した点を記録する。
func (r *ROM) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// UseFourScreen は 4 画面構成を採用する。
//
// マッパーが 4 画面を扱えるときにファクトリから呼ぶ。扱えないマッパーで
// ヘッダの bit 3 が立っている ROM があるため、採用はマッパー側の判断とする。
func (r *ROM) UseFourScreen() {
	r.Mirroring = MirrorFourScreen
}

// IgnoreFourScreen は 4 画面構成を無視する。
func (r *ROM) IgnoreFourScreen() {
	r.FourScreen = false
	r.warn("マッパー %d は 4 画面構成を扱えないため、ヘッダの 4 画面ビットを無視した", r.Mapper)
}

// allZero はすべて 0 かを返す。
func allZero(b []uint8) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// clone はバイト列を複製する。元のファイルのバッファを保持しないためである。
func clone(b []uint8) []uint8 {
	out := make([]uint8, len(b))
	copy(out, b)
	return out
}

// romKeyHexDigits はファイル名に使うハッシュの桁数。
const romKeyHexDigits = 16

// ROMKeyString は ROM ハッシュからファイル名に使う文字列を返す。
//
// PRG-ROM と CHR-ROM の SHA-1 の先頭 16 桁を 16 進で表す。ヘッダを
// 含めないのは、同じゲームのダンプでヘッダだけが異なる場合に別の
// ゲームとして扱わないためである。
func ROMKeyString(hash []uint8) string {
	s := hex.EncodeToString(hash)
	if len(s) > romKeyHexDigits {
		s = s[:romKeyHexDigits]
	}
	return s
}
