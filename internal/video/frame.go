package video

// Width と Height はフレームバッファの大きさ。
//
// 高さは PPU のカウンタ上の可視走査線数に合わせる。実際に表示する高さは
// リージョンごとに異なり、表示側が切り取る。
const (
	Width  = 256
	Height = 240
)

// Frame は 1 フレーム分のピクセル。
//
// 各ピクセルは下位 6 bit がパレット値、bit 6-8 がエンファシス（赤・緑・青の
// 順）である。
type Frame struct {
	Pixels [Width * Height]uint16
}

// パレット値とエンファシスのビット位置。
const (
	// PaletteMask はパレット値の部分。
	PaletteMask uint16 = 0x003F
	// EmphasisShift はエンファシスの開始ビット。
	EmphasisShift = 6
	// EmphasisMask はエンファシスの部分。
	EmphasisMask uint16 = 0x01C0
)

// NewFrame は空のフレームを返す。
func NewFrame() *Frame { return &Frame{} }

// Set は 1 ピクセルを書く。範囲外の座標は無視する。
//
// 範囲を確かめるのは、PPU の走査位置の計算が誤っていてもパニックで止まらず、
// テストが差分として報告できるようにするためである。
func (f *Frame) Set(x, y int, v uint16) {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return
	}
	f.Pixels[y*Width+x] = v
}

// At は 1 ピクセルを読む。範囲外の座標では 0 を返す。
func (f *Frame) At(x, y int) uint16 {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return 0
	}
	return f.Pixels[y*Width+x]
}

// PaletteIndex はピクセルのパレット値を返す。
func PaletteIndex(v uint16) uint8 { return uint8(v & PaletteMask) }

// EmphasisBits はピクセルのエンファシスを返す。bit 0 が赤、1 が緑、2 が青。
func EmphasisBits(v uint16) uint8 { return uint8((v & EmphasisMask) >> EmphasisShift) }

// Clear はフレーム全体を指定の値で埋める。
func (f *Frame) Clear(v uint16) {
	for i := range f.Pixels {
		f.Pixels[i] = v
	}
}

// CopyFrom は別のフレームの内容を複製する。
func (f *Frame) CopyFrom(src *Frame) { f.Pixels = src.Pixels }
