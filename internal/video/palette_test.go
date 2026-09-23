package video

import (
	"image"
	"image/color"
	"testing"
)

// TestDefaultPaletteIsComplete は既定のパレットが 512 色すべてを持つことを
// 確かめる。埋め込みに失敗すると全色が黒になる。
func TestDefaultPaletteIsComplete(t *testing.T) {
	p := DefaultPalette()
	nonBlack := 0
	for _, c := range p.table {
		if c.A != 0xFF {
			t.Fatalf("不透明でない色がある: %+v", c)
		}
		if c.R != 0 || c.G != 0 || c.B != 0 {
			nonBlack++
		}
	}
	if nonBlack == 0 {
		t.Fatal("全色が黒である。パレットの埋め込みが読めていない")
	}
}

// TestDefaultPaletteKnownColors は調査文書の表の値がそのまま入ることを
// 確かめる。パレットの生成と読み込みの経路が保たれていることを見る。
func TestDefaultPaletteKnownColors(t *testing.T) {
	p := DefaultPalette()
	tests := []struct {
		name  string
		index uint16
		want  color.RGBA
	}{
		{"$00 灰", 0x00, color.RGBA{0x57, 0x57, 0x57, 0xFF}},
		{"$0F 黒", 0x0F, color.RGBA{0x00, 0x00, 0x00, 0xFF}},
		{"$20 白", 0x20, color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}},
		{"$11 青", 0x11, color.RGBA{0x00, 0x41, 0xD9, 0xFF}},
	}
	for _, tt := range tests {
		if got := p.Color(tt.index); got != tt.want {
			t.Errorf("%s: Color(%#02x) = %+v, 期待 %+v", tt.name, tt.index, got, tt.want)
		}
	}
}

// TestEmphasisAttenuatesOtherComponents はエンファシスが「強調しなかった
// 成分を暗くする」ことを確かめる。
func TestEmphasisAttenuatesOtherComponents(t *testing.T) {
	// 白 (FFFFFF) を使うと 3 成分の変化を同時に見られる。
	white := []uint8{0xFF, 0xFF, 0xFF}
	p, err := LoadPalette(append(white, make([]uint8, BaseFileSize-3)...))
	if err != nil {
		t.Fatal(err)
	}

	base := p.Color(0)
	if base != (color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Fatalf("エンファシス無しで %+v", base)
	}

	// 赤を強調すると緑と青が暗くなり、赤は変わらない。
	red := p.Color(uint16(1) << EmphasisShift)
	if red.R != 0xFF {
		t.Errorf("赤の強調で赤成分が %d に変わった", red.R)
	}
	if red.G >= 0xFF || red.G != red.B {
		t.Errorf("赤の強調で緑と青が期待どおりに減衰していない: %+v", red)
	}

	// 3 つすべてを強調すると全成分が暗くなる。
	all := p.Color(uint16(7) << EmphasisShift)
	if all.R >= 0xFF || all.G >= 0xFF || all.B >= 0xFF {
		t.Errorf("全強調で全成分が暗くなっていない: %+v", all)
	}
	// 減衰は重ねない。2 成分の強調でも 1 成分と同じ率になる。
	if all.R != red.G {
		t.Errorf("減衰が重なっている: 全強調 %d, 1 つの強調 %d", all.R, red.G)
	}
}

// TestLoadPaletteFullFile は 512 色のファイルをそのまま使うことを確かめる。
func TestLoadPaletteFullFile(t *testing.T) {
	data := make([]uint8, FullFileSize)
	for i := range ColorCount * EmphasisCount {
		data[i*3] = uint8(i & 0xFF)
	}
	p, err := LoadPalette(data)
	if err != nil {
		t.Fatal(err)
	}
	// エンファシス 3（赤と緑）の色 $05 は添字 3*64+5 = 197。
	idx := uint16(3)<<EmphasisShift | 0x05
	if got := p.Color(idx).R; got != uint8(197&0xFF) {
		t.Errorf("Color(%#x).R = %d, 期待 %d", idx, got, 197&0xFF)
	}
}

// TestLoadPaletteRejectsWrongSize は大きさの違うファイルを拒むことを確かめる。
func TestLoadPaletteRejectsWrongSize(t *testing.T) {
	for _, n := range []int{0, 191, 193, 1535, 1537} {
		if _, err := LoadPalette(make([]uint8, n)); err == nil {
			t.Errorf("%d バイトのファイルを受け入れてしまった", n)
		}
	}
}

// TestApplyWritesOnlyTheGivenRect は指定した範囲だけを変換することを
// 確かめる。オーバースキャンで隠した範囲を変換しないためである。
func TestApplyWritesOnlyTheGivenRect(t *testing.T) {
	p := DefaultPalette()
	f := NewFrame()
	f.Clear(0x0F) // 黒
	f.Set(10, 10, 0x20)

	src := image.Rect(8, 8, 16, 16)
	dst := image.NewRGBA(image.Rect(0, 0, src.Dx(), src.Dy()))
	p.ApplyRect(f, src, dst)

	if got := dst.RGBAAt(2, 2); got != (color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Errorf("(10,10) が dst の (2,2) に来ていない: %+v", got)
	}
	if got := dst.RGBAAt(0, 0); got != (color.RGBA{0x00, 0x00, 0x00, 0xFF}) {
		t.Errorf("dst の (0,0) = %+v, 期待 黒", got)
	}
}

// TestApplyUsesBoundsAsFrameRect は Apply が dst の境界をフレーム上の
// 範囲として扱うことを確かめる。
func TestApplyUsesBoundsAsFrameRect(t *testing.T) {
	p := DefaultPalette()
	f := NewFrame()
	f.Clear(0x0F)
	f.Set(0, 8, 0x20)

	dst := image.NewRGBA(image.Rect(0, 8, Width, Height-8))
	p.Apply(f, dst)
	if got := dst.RGBAAt(0, 8); got != (color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Errorf("(0,8) = %+v, 期待 白", got)
	}
}
