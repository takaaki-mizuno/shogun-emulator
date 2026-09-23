package debug

import (
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// visibleScanlines は可視スキャンラインの数。
const visibleScanlines = 240

// Snapshot はビューアが参照するメモリの写し（設計書 09 編 §9.3）。
//
// フレームの終わり（または指定したスキャンラインの開始時）に取る。
// UI スレッドはエミュレーションの状態を直接読まず、これを読む。
type Snapshot struct {
	// Frame は取ったときの累積フレーム数。
	Frame uint64
	// Scanline は取ったスキャンライン。フレームの終わりのとき -1。
	Scanline int
	// CHR は PPU の $0000-$1FFF を現在のバンク構成で読んだ値。
	CHR [8192]uint8
	// Nametables は PPU の $2000-$2FFF をミラーリングを通して読んだ値。
	Nametables [4096]uint8
	CIRAM      [4096]uint8
	OAM        [256]uint8
	Palette    [32]uint8
	// Ctrl と Mask は $2000 と $2001 の値。
	Ctrl, Mask uint8
	// ScrollV と FineX はフレームの描画開始時の v と x（設計書 04 編 §4.10.1）。
	ScrollV   uint16
	FineX     uint8
	Mirroring cart.Mirroring
	CPUState  cpu.State
	// BankView は CHR のバンク構成。
	BankView []cart.BankView
	// CHRROM は CHR が ROM であることを表す。編集がオーバーレイへ行く。
	CHRROM bool
	// SpriteHeight はスプライトの高さ。8 または 16。
	SpriteHeight int
	// ScanlineSpriteCount はスキャンラインごとの範囲内のスプライト数。
	// 9 以上はオーバーフローした行である。
	ScanlineSpriteCount [visibleScanlines]uint8
	// SpriteDrawn は各スプライトが少なくとも 1 行で描かれたかを表す。
	SpriteDrawn [64]bool
}

// SnapshotSet はスナップショットの二重バッファ。
//
// エミュレーションゴルーチンが working へ書き、書き終えたら complete と
// 入れ替える。UI スレッドは complete を読む。コピーの量は 17 KiB 程度で
// あり、60 fps で毎秒 1 MiB になる（設計書 09 編 §9.3）。
type SnapshotSet struct {
	mu       sync.Mutex
	complete *Snapshot
	working  *Snapshot
	// ready は complete に内容が入っていることを表す。
	ready bool
	// line は取得位置。-1 でフレーム末。
	line int
}

// Line は取得位置を返す。-1 はフレーム末を表す。
func (s *SnapshotSet) Line() int { return s.line }

// NewSnapshotSet は二重バッファを作る。
func NewSnapshotSet() *SnapshotSet {
	return &SnapshotSet{complete: &Snapshot{}, working: &Snapshot{}, line: -1}
}

// Capture は現在の状態を写して公開する。エミュレーションゴルーチンが呼ぶ。
func (s *SnapshotSet) Capture(n *nes.NES, scanline int) {
	w := s.working
	w.Frame = n.Frames()
	w.Scanline = scanline
	for i := range w.CHR {
		w.CHR[i] = n.PPU.PeekVRAM(uint16(i))
	}
	for i := range w.Nametables {
		w.Nametables[i] = n.PPU.PeekVRAM(0x2000 + uint16(i))
	}
	copy(w.CIRAM[:], n.PPU.CIRAM())
	copy(w.OAM[:], n.PPU.OAM())
	copy(w.Palette[:], n.PPU.Palette())
	w.Ctrl, w.Mask = uint8(n.PPU.Control()), uint8(n.PPU.Mask())
	w.ScrollV, w.FineX = n.PPU.FrameScroll()
	w.CPUState = n.CPU.State(n.PPU.Scanline(), n.PPU.Dot())
	info := n.Cart.Info()
	w.Mirroring = info.Mirroring
	w.BankView = append(w.BankView[:0], info.CHRBanks...)
	w.CHRROM = len(n.ROM.CHR) > 0
	w.SpriteHeight = n.PPU.SpriteHeight()
	countSpritesPerLine(&w.OAM, w.SpriteHeight, &w.ScanlineSpriteCount, &w.SpriteDrawn)

	s.mu.Lock()
	s.complete, s.working = s.working, s.complete
	s.ready = true
	s.mu.Unlock()
}

// Latest は直近のスナップショットの写しを dst へ書く。無いとき false。
//
// 写して返すのは、UI スレッドが読んでいる間にエミュレーションゴルーチン
// が次のスナップショットで上書きしないようにするためである。
func (s *SnapshotSet) Latest(dst *Snapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return false
	}
	bank := dst.BankView[:0]
	*dst = *s.complete
	dst.BankView = append(bank, s.complete.BankView...)
	return true
}

// countSpritesPerLine は OAM からスキャンラインごとのスプライト数と、
// 描かれたスプライトを求める。
//
// スプライトは Y+1 の行から描かれる。各行で範囲内のスプライトを OAM の
// 順に最大 8 個まで描かれたものとする。PPU のスプライト評価と同じ範囲の
// 判定を OAM の写しに対して行う。
func countSpritesPerLine(oam *[256]uint8, height int, out *[visibleScanlines]uint8, drawn *[64]bool) {
	*out = [visibleScanlines]uint8{}
	*drawn = [64]bool{}
	for i := 0; i < 64; i++ {
		top := int(oam[i*4]) + 1
		for line := top; line < top+height && line < visibleScanlines; line++ {
			if out[line] < maxSpritesPerLine {
				drawn[i] = true
			}
			if out[line] < 255 {
				out[line]++
			}
		}
	}
}

// maxSpritesPerLine は 1 行に描かれるスプライトの最大数。
const maxSpritesPerLine = 8
