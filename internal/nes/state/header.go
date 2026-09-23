package state

import (
	"bytes"
	"errors"
	"fmt"
)

// Magic はセーブステートの先頭に置く 4 バイト。
const Magic = "SHGS"

// FormatVersion はセーブステートの形式のバージョン。
//
// 保存する状態が増えるたびに上げる。古い形式との対応を保たないのは、
// 精度を高める過程で保存対象が変わり続けるためである（設計書 08 編 §8.2.2）。
const FormatVersion = 3

// HashSize は ROM ハッシュのバイト数。PRG と CHR を連結した SHA-1。
const HashSize = 20

// Init は電源投入時に実機で値が定まらない状態を決める。
//
// これを明示的に持つことで、未初期化の値に依存するプログラムの挙動を
// 観測でき、かつ同じ Init から常に同じ結果を得られる（設計書 08 編 §8.5.2）。
type Init struct {
	// RAMPattern は RAM を埋めるパターン。
	RAMPattern Pattern
	// RAMSeed はパターンが Random のときの種。
	RAMSeed uint64
	// CPUPPUAlignment は CPU と PPU の位相差。0..2。
	CPUPPUAlignment int
	// DMAGetPutPhase は DMA の get/put 位相。0..1。
	DMAGetPutPhase int
	// PPUVBlankFlag は電源投入時の VBlank フラグ。
	PPUVBlankFlag bool
}

// Header はセーブステートの先頭に置く識別情報。
//
// ロード時に照合する。別の ROM やマッパーのステートを読み込むと、復元が
// 成立せず原因の分からない異常動作になる。
type Header struct {
	// FormatVersion はステートの形式のバージョン。
	FormatVersion uint16
	// Version と Commit はエミュレータの識別情報。
	Version string
	Commit  string
	// ROMHash は PRG と CHR を連結した SHA-1。
	ROMHash [HashSize]uint8
	// Mapper と Submapper はマッパーの識別。
	Mapper    uint16
	Submapper uint8
	// Region はリージョンの名前。
	Region string
	// Init は電源投入時の初期状態。
	Init Init
	// Cycles と Frames は電源投入からの累積。
	Cycles uint64
	Frames uint64
	// OverlayHash は保存時のオーバーレイのハッシュ（設計書 06 編 §6.8）。
	// 一致しなくてもロードは続ける。
	OverlayHash [8]uint8
	// Screenshot は保存時の画面の PNG。持たないとき長さ 0。
	Screenshot []uint8
}

// headerSection はヘッダを収めるセクションの名前。
const headerSection = "header"

// Write はヘッダを書く。
func (h *Header) Write(w *Writer) {
	end := w.Section(headerSection)
	w.RawBytes([]uint8(Magic))
	w.U16(h.FormatVersion)
	w.String(h.Version)
	w.String(h.Commit)
	w.RawBytes(h.ROMHash[:])
	w.U16(h.Mapper)
	w.U8(h.Submapper)
	w.String(h.Region)
	w.U8(uint8(h.Init.RAMPattern))
	w.U64(h.Init.RAMSeed)
	w.Int(h.Init.CPUPPUAlignment)
	w.Int(h.Init.DMAGetPutPhase)
	w.Bool(h.Init.PPUVBlankFlag)
	w.U64(h.Cycles)
	w.U64(h.Frames)
	w.RawBytes(h.OverlayHash[:])
	w.Bytes(h.Screenshot)
	end()
}

// Read はヘッダを読む。
//
// マジックと形式のバージョンはここで確かめる。後続の読み出しが意味を
// 持たないためである。他の項目の照合は Verify で行う。
func (h *Header) Read(r *Reader) error {
	end := r.RequireSection(headerSection)
	magic := make([]uint8, len(Magic))
	r.RawBytes(magic)
	if err := r.Err(); err != nil {
		return err
	}
	if !bytes.Equal(magic, []uint8(Magic)) {
		return fmt.Errorf("state: マジックが一致しない（期待 %q、実際 %q）", Magic, magic)
	}
	h.FormatVersion = r.U16()
	if h.FormatVersion != FormatVersion {
		return fmt.Errorf("state: 形式のバージョンが違う（期待 %d、実際 %d）",
			FormatVersion, h.FormatVersion)
	}
	h.Version = r.String()
	h.Commit = r.String()
	r.RawBytes(h.ROMHash[:])
	h.Mapper = r.U16()
	h.Submapper = r.U8()
	h.Region = r.String()
	h.Init.RAMPattern = Pattern(r.U8())
	h.Init.RAMSeed = r.U64()
	h.Init.CPUPPUAlignment = r.Int()
	h.Init.DMAGetPutPhase = r.Int()
	h.Init.PPUVBlankFlag = r.Bool()
	h.Cycles = r.U64()
	h.Frames = r.U64()
	r.RawBytes(h.OverlayHash[:])
	h.Screenshot = r.Bytes()
	end()
	return r.Err()
}

// ReadHeader はステートのバイト列からヘッダだけを読む。
//
// スロットの一覧表示が、本体を組み立てずに保存時刻とスクリーンショットを
// 取り出すために使う。
func ReadHeader(b []uint8) (Header, error) {
	var h Header
	err := h.Read(NewReader(b))
	return h, err
}

// Verify は want と照合する。一致しない項目があるときエラーを返す。
//
// エラーには「何が」「期待値」「実際の値」を含める。利用者が別の ROM の
// ステートを読み込んだことに気づけるようにするためである。
func (h *Header) Verify(want *Header) error {
	if h.Version != want.Version {
		return fmt.Errorf("state: エミュレータのバージョンが違う（期待 %q、実際 %q）",
			want.Version, h.Version)
	}
	if h.Commit != want.Commit {
		return fmt.Errorf("state: コミットハッシュが違う（期待 %q、実際 %q）",
			want.Commit, h.Commit)
	}
	if h.ROMHash != want.ROMHash {
		return fmt.Errorf("state: ROM ハッシュが一致しない（期待 %x、実際 %x）",
			want.ROMHash[:4], h.ROMHash[:4])
	}
	if h.Mapper != want.Mapper || h.Submapper != want.Submapper {
		return fmt.Errorf("state: マッパーが違う（期待 %d.%d、実際 %d.%d）",
			want.Mapper, want.Submapper, h.Mapper, h.Submapper)
	}
	if h.Region != want.Region {
		return fmt.Errorf("state: リージョンが違う（期待 %q、実際 %q）", want.Region, h.Region)
	}
	return nil
}

// ErrNoScreenshot はステートにスクリーンショットが入っていないことを表す。
var ErrNoScreenshot = errors.New("state: スクリーンショットが入っていない")
