package state

// Pattern は電源投入時に RAM を埋めるパターンを表す。
//
// 実機では内蔵 RAM・OAM・パレット RAM・CIRAM・CHR-RAM の初期値が定まらない。
// エミュレータはこれを明示的に決め、シードをステートとムービーに記録する。
// これにより「未初期化 RAM に依存するプログラムの挙動を観測できること」と
// 「再現性」を両立する。
type Pattern uint8

const (
	// PatternZero はすべて 0x00 で埋める。
	PatternZero Pattern = iota
	// PatternFF はすべて 0xFF で埋める。
	PatternFF
	// PatternAlternating は 0x00 と 0xFF を 8 バイトごとに繰り返す。
	PatternAlternating
	// PatternRandom はシードから決まる擬似乱数で埋める。
	PatternRandom
)

// String は Pattern の名前を返す。設定ファイルの値と対応する。
func (p Pattern) String() string {
	switch p {
	case PatternZero:
		return "zero"
	case PatternFF:
		return "ff"
	case PatternAlternating:
		return "pattern"
	case PatternRandom:
		return "random"
	}
	return "unknown"
}

// ParsePattern は名前から Pattern を返す。
func ParsePattern(s string) (Pattern, bool) {
	switch s {
	case "zero":
		return PatternZero, true
	case "ff":
		return PatternFF, true
	case "pattern":
		return PatternAlternating, true
	case "random":
		return PatternRandom, true
	}
	return PatternZero, false
}

// FillPattern は dst をパターンに従って埋める。
//
// PatternRandom のとき、seed と salt から決まる擬似乱数で埋める。同じ
// seed と salt からは常に同じ内容が得られる。salt には埋める対象を区別する
// 値（RAM・OAM・CIRAM など）を渡す。同じ seed で複数の領域を埋めるときに
// 同一の並びが繰り返されるのを避けるためである。
//
// math/rand のグローバル関数を使わない。エミュレーションの決定論を保つため、
// 状態を持つ生成器をこの関数の内部に閉じる。
func FillPattern(dst []uint8, p Pattern, seed uint64, salt uint64) {
	switch p {
	case PatternZero:
		clear(dst)
	case PatternFF:
		for i := range dst {
			dst[i] = 0xFF
		}
	case PatternAlternating:
		for i := range dst {
			if i&0x08 == 0 {
				dst[i] = 0x00
			} else {
				dst[i] = 0xFF
			}
		}
	case PatternRandom:
		s := splitMix64{state: seed ^ mix(salt)}
		for i := 0; i < len(dst); i += 8 {
			v := s.next()
			for j := 0; j < 8 && i+j < len(dst); j++ {
				dst[i+j] = uint8(v >> (8 * j))
			}
		}
	default:
		clear(dst)
	}
}

// splitMix64 は SplitMix64 の実装。
//
// 自前で持つのは、Go の標準ライブラリの生成器の内部実装が将来変わっても
// セーブステートとムービーの再現性を保つためである。生成列が処理系や
// バージョンに依存してはならない。
type splitMix64 struct {
	state uint64
}

func (s *splitMix64) next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// mix は salt を撹拌する。
func mix(v uint64) uint64 {
	v ^= v >> 33
	v *= 0xFF51AFD7ED558CCD
	v ^= v >> 33
	v *= 0xC4CEB9FE1A85EC53
	v ^= v >> 33
	return v
}

// 埋める対象を区別する salt。
const (
	SaltRAM     uint64 = 1
	SaltOAM     uint64 = 2
	SaltPalette uint64 = 3
	SaltCIRAM   uint64 = 4
	SaltCHRRAM  uint64 = 5
	SaltPRGRAM  uint64 = 6
)
