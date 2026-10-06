// Package movie は入力ムービーの形式・記録・再生を提供する。
//
// ムービーはフレーム単位の入力の並びである。同じ初期状態と同じ入力から
// 同じ結果を得られること（決定論）の上に成り立つ（設計書 08 編 §8.7）。
package movie

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// Magic はムービーの先頭に置く 4 バイト。
const Magic = "SHGM"

// FormatVersion はムービーの形式のバージョン。介入のレコードを含むとき 2、
// 含まないとき 1 で書く（設計書 08 編 §8.7.2）。
const FormatVersion = 2

// formatVersionV1 は介入のレコードを持たない形式のバージョン。介入を含まない
// ムービーをバージョン 1 のまま書き、既存のムービーと期待値を保つ。
const formatVersionV1 = 1

// StartKind はムービーの開始方法。
type StartKind uint8

const (
	// StartPowerOn は電源投入から始める。
	StartPowerOn StartKind = iota
	// StartSaveState は埋め込んだセーブステートから始める。
	StartSaveState
)

// String は開始方法の名前を返す。
func (k StartKind) String() string {
	if k == StartSaveState {
		return "savestate"
	}
	return "power-on"
}

// Header はムービーの先頭に置く情報。
type Header struct {
	// FormatVersion はムービーの形式のバージョン。
	FormatVersion uint16
	// Version と Commit は記録したエミュレータの識別情報。
	Version string
	Commit  string
	// ROMHash は PRG と CHR を連結した SHA-1。
	ROMHash [state.HashSize]uint8
	// ROMName は表示のみに使う ROM のファイル名。
	ROMName string
	// Region はリージョンの名前。
	Region string
	// Mapper と Submapper はマッパーの識別。
	Mapper    uint16
	Submapper uint8
	// Init は電源投入時の初期状態。
	Init state.Init
	// Start は開始方法。
	Start StartKind
	// StateBlob は Start が StartSaveState のときの埋め込みステート。
	StateBlob []uint8
	// Ports はポート 1 と 2 のデバイス種別。
	Ports [2]string
	// TotalFrames は記録した総フレーム数。
	TotalFrames uint64
	// Rerecords は再記録回数。巻き戻して録り直した回数である。
	Rerecords uint64
	// Author と Comment は表示のみに使う。
	Author  string
	Comment string
	// ChecksumInterval はチェックサムを記録するフレーム間隔。0 で無効。
	ChecksumInterval int
}

// Kind はレコードの種別。
type Kind uint8

const (
	// KindInput はそのフレームの入力。
	KindInput Kind = iota
	// KindReset はリセットボタンの押下。
	KindReset
	// KindHardReset は電源の入れ直し。
	KindHardReset
	// KindChecksum は状態のハッシュ。
	KindChecksum
	// KindPoke はメモリの書き換え（バージョン 2）。
	KindPoke
	// KindSetRegister は CPU のレジスタの書き換え（バージョン 2）。
	KindSetRegister
	// KindFreeze は値の固定（バージョン 2）。
	KindFreeze
	// KindUnfreeze は固定の解除（バージョン 2）。
	KindUnfreeze
	// KindOverlay はオーバーレイの有効・無効の切り替え（バージョン 2）。
	KindOverlay
)

// IsIntervention は入力以外で Machine State を変えた操作（介入）のレコードかを
// 返す（設計書 14 編 §14.16.2）。
func (k Kind) IsIntervention() bool { return k >= KindPoke && k <= KindOverlay }

// EndOfFrame は介入のレコードのサイクル数で「フレームの終わり」を表す値。
// 記録の最後のフレームの後（次のフレームの開始を待っている間）に行った介入に
// 使う。再生では記録の終わりで適用する。
const EndOfFrame = ^uint32(0)

// Record はムービーのレコード 1 つ。
//
// すべてのレコードが種別バイトで始まる。入力レコードに種別バイトを
// 付けないと、続くバイト列が入力かイベントかを区別できない。
type Record struct {
	Kind Kind
	// Buttons は Kind が KindInput のときのポート 1 と 2 の押下状態。
	Buttons [2]uint8
	// Hash は Kind が KindChecksum のときの状態のハッシュ。
	Hash [8]uint8

	// 以下は介入のレコードの内容。
	// Cycle はフレームの開始から数えた CPU サイクル数。
	Cycle uint32
	// Space はポークの空間（「09 デバッガ設計」§9.4.5 の空間の番号）。
	Space uint8
	// Addr はポークの空間内のオフセット、Freeze の CPU アドレス。
	Addr uint32
	// Value はポーク（1 バイト）・レジスタ（2 バイト）・Freeze（4 バイト）の値。
	Value uint32
	// Size は Freeze のバイト数。
	Size uint8
	// Reg はレジスタの番号（0: A、1: X、2: Y、3: S、4: PC、5: P）。
	Reg uint8
	// Flag はポークの副作用の有無、オーバーレイの有効。
	Flag bool
}

// Movie はヘッダとレコードの並び。
type Movie struct {
	Header  Header
	Records []Record
}

// セクションの名前。
const (
	headerSection = "movie"
	logSection    = "log"
)

// Encode はムービーをバイト列にする。
func (m *Movie) Encode() []uint8 {
	w := state.NewWriter()

	endHeader := w.Section(headerSection)
	h := &m.Header
	w.RawBytes([]uint8(Magic))
	version := uint16(formatVersionV1)
	for _, rec := range m.Records {
		if rec.Kind.IsIntervention() {
			version = FormatVersion
			break
		}
	}
	w.U16(version)
	w.String(h.Version)
	w.String(h.Commit)
	w.RawBytes(h.ROMHash[:])
	w.String(h.ROMName)
	w.String(h.Region)
	w.U16(h.Mapper)
	w.U8(h.Submapper)
	w.U8(uint8(h.Init.RAMPattern))
	w.U64(h.Init.RAMSeed)
	w.Int(h.Init.CPUPPUAlignment)
	w.Int(h.Init.DMAGetPutPhase)
	w.Bool(h.Init.PPUVBlankFlag)
	w.U8(uint8(h.Start))
	w.Bytes(h.StateBlob)
	w.String(h.Ports[0])
	w.String(h.Ports[1])
	w.U64(h.TotalFrames)
	w.U64(h.Rerecords)
	w.String(h.Author)
	w.String(h.Comment)
	w.Int(h.ChecksumInterval)
	endHeader()

	endLog := w.Section(logSection)
	w.Bytes(encodeRecords(m.Records))
	endLog()

	return w.Data()
}

// encodeRecords はレコードの並びをバイト列にする。
func encodeRecords(records []Record) []uint8 {
	out := make([]uint8, 0, len(records)*3)
	for _, rec := range records {
		out = append(out, uint8(rec.Kind))
		switch rec.Kind {
		case KindInput:
			out = append(out, rec.Buttons[0], rec.Buttons[1])
		case KindChecksum:
			out = append(out, rec.Hash[:]...)
		case KindPoke:
			out = le32(out, rec.Cycle)
			out = append(out, rec.Space)
			out = le32(out, rec.Addr)
			out = append(out, uint8(rec.Value), b2u(rec.Flag))
		case KindSetRegister:
			out = le32(out, rec.Cycle)
			out = append(out, rec.Reg, uint8(rec.Value), uint8(rec.Value>>8))
		case KindFreeze:
			out = le32(out, rec.Cycle)
			out = append(out, uint8(rec.Addr), uint8(rec.Addr>>8), rec.Size)
			out = le32(out, rec.Value)
		case KindUnfreeze:
			out = le32(out, rec.Cycle)
			out = append(out, uint8(rec.Addr), uint8(rec.Addr>>8))
		case KindOverlay:
			out = le32(out, rec.Cycle)
			out = append(out, b2u(rec.Flag))
		}
	}
	return out
}

func le32(b []uint8, v uint32) []uint8 {
	return append(b, uint8(v), uint8(v>>8), uint8(v>>16), uint8(v>>24))
}

func rd32(b []uint8) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func b2u(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// interventionSizes は介入のレコードの種別バイトを除いた大きさ（設計書 08 編
// §8.7.2 の表）。
var interventionSizes = map[Kind]int{KindPoke: 11, KindSetRegister: 7, KindFreeze: 11, KindUnfreeze: 6, KindOverlay: 5}

// decodeRecords はバイト列をレコードの並びに戻す。
func decodeRecords(b []uint8) ([]Record, error) {
	out := make([]Record, 0, len(b)/3)
	for i := 0; i < len(b); {
		k := Kind(b[i])
		i++
		rec := Record{Kind: k}
		switch k {
		case KindInput:
			if i+2 > len(b) {
				return nil, fmt.Errorf("movie: 入力レコードが途中で切れている（位置 %d）", i)
			}
			rec.Buttons[0] = b[i]
			rec.Buttons[1] = b[i+1]
			i += 2
		case KindChecksum:
			if i+len(rec.Hash) > len(b) {
				return nil, fmt.Errorf("movie: チェックサムが途中で切れている（位置 %d）", i)
			}
			copy(rec.Hash[:], b[i:])
			i += len(rec.Hash)
		case KindReset, KindHardReset:
		case KindPoke, KindSetRegister, KindFreeze, KindUnfreeze, KindOverlay:
			n := interventionSizes[k]
			if i+n > len(b) {
				return nil, fmt.Errorf("movie: 介入のレコードが途中で切れている（位置 %d）", i)
			}
			p := b[i : i+n]
			rec.Cycle = rd32(p)
			switch k {
			case KindPoke:
				rec.Space, rec.Addr, rec.Value, rec.Flag = p[4], rd32(p[5:]), uint32(p[9]), p[10] != 0
			case KindSetRegister:
				rec.Reg, rec.Value = p[4], uint32(p[5])|uint32(p[6])<<8
			case KindFreeze:
				rec.Addr, rec.Size, rec.Value = uint32(p[4])|uint32(p[5])<<8, p[6], rd32(p[7:])
			case KindUnfreeze:
				rec.Addr = uint32(p[4]) | uint32(p[5])<<8
			case KindOverlay:
				rec.Flag = p[4] != 0
			}
			i += n
		default:
			return nil, fmt.Errorf("movie: 知らないレコード種別 %d（位置 %d）", k, i-1)
		}
		out = append(out, rec)
	}
	return out, nil
}

// Decode はバイト列をムービーに戻す。
func Decode(b []uint8) (*Movie, error) {
	r := state.NewReader(b)
	m := &Movie{}
	h := &m.Header

	endHeader, ok := r.Section(headerSection)
	if !ok {
		return nil, fmt.Errorf("movie: ムービーの形式ではない")
	}
	magic := make([]uint8, len(Magic))
	r.RawBytes(magic)
	if err := r.Err(); err != nil {
		return nil, err
	}
	if string(magic) != Magic {
		return nil, fmt.Errorf("movie: マジックが一致しない（期待 %q、実際 %q）", Magic, magic)
	}
	h.FormatVersion = r.U16()
	if h.FormatVersion != formatVersionV1 && h.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("movie: 形式のバージョンが違う（期待 %d か %d、実際 %d）",
			formatVersionV1, FormatVersion, h.FormatVersion)
	}
	h.Version = r.String()
	h.Commit = r.String()
	r.RawBytes(h.ROMHash[:])
	h.ROMName = r.String()
	h.Region = r.String()
	h.Mapper = r.U16()
	h.Submapper = r.U8()
	h.Init.RAMPattern = state.Pattern(r.U8())
	h.Init.RAMSeed = r.U64()
	h.Init.CPUPPUAlignment = r.Int()
	h.Init.DMAGetPutPhase = r.Int()
	h.Init.PPUVBlankFlag = r.Bool()
	h.Start = StartKind(r.U8())
	h.StateBlob = r.Bytes()
	h.Ports[0] = r.String()
	h.Ports[1] = r.String()
	h.TotalFrames = r.U64()
	h.Rerecords = r.U64()
	h.Author = r.String()
	h.Comment = r.String()
	h.ChecksumInterval = r.Int()
	endHeader()
	if err := r.Err(); err != nil {
		return nil, err
	}

	endLog, ok := r.Section(logSection)
	if !ok {
		return nil, fmt.Errorf("movie: 入力ログが無い")
	}
	raw := r.Bytes()
	endLog()
	if err := r.Err(); err != nil {
		return nil, err
	}
	records, err := decodeRecords(raw)
	if err != nil {
		return nil, err
	}
	m.Records = records
	return m, nil
}
