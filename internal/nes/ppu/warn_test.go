package ppu

import "testing"

// TestWarnsOnDataAccessDuringRendering はレンダリング中の $2007 アクセスを
// warn.compat へ記録することを確かめる（設計書 09 編 §9.8）。
func TestWarnsOnDataAccessDuringRendering(t *testing.T) {
	p, _ := newTestPPU(t)
	var warnings int
	p.Warn = func(string, ...any) { warnings++ }

	// レンダリングしていないときは記録しない。
	p.WriteRegister(7, 0x00)
	p.ReadRegister(7)
	if warnings != 0 {
		t.Fatalf("レンダリングしていないのに %d 回記録した", warnings)
	}

	p.mask = Mask(0x1E)
	p.scanline, p.dot = 10, 100
	p.WriteRegister(7, 0x00)
	if warnings != 1 {
		t.Errorf("レンダリング中の書き込みの記録 = %d, 期待 1", warnings)
	}
	p.ReadRegister(7)
	if warnings != 2 {
		t.Errorf("レンダリング中の読み出しの記録 = %d, 期待 2", warnings)
	}
}

// TestWarnsOnForbiddenColor は色 $0D の書き込みを記録することを確かめる。
func TestWarnsOnForbiddenColor(t *testing.T) {
	p, _ := newTestPPU(t)
	var warnings int
	p.Warn = func(string, ...any) { warnings++ }

	p.WriteRegister(6, 0x3F)
	p.WriteRegister(6, 0x00)
	p.WriteRegister(7, 0x0F)
	if warnings != 0 {
		t.Fatalf("色 $0F で %d 回記録した", warnings)
	}
	p.WriteRegister(7, 0x0D)
	if warnings != 1 {
		t.Errorf("色 $0D の記録 = %d, 期待 1", warnings)
	}
}

// TestFrameScrollRecordsStartOfFrame はプリレンダー行のドット 304 で
// スクロール位置を記録することを確かめる（設計書 04 編 §4.10.1）。
func TestFrameScrollRecordsStartOfFrame(t *testing.T) {
	p, _ := newTestPPU(t)
	// $2005 で X=$25, Y=$47 を書き、$2000 でネームテーブル 1 を選ぶ。
	p.WriteRegister(0, 0x01)
	p.WriteRegister(5, 0x25)
	p.WriteRegister(5, 0x47)
	p.WriteRegister(1, 0x08)
	p.scanline, p.dot = p.region.PreRenderScanline(), 250
	for range 60 {
		p.Step()
	}
	v, x := p.FrameScroll()
	if x != 0x25&7 {
		t.Errorf("fine X = %d, 期待 %d", x, 0x25&7)
	}
	if got := v & 0x1F; got != 0x25>>3 {
		t.Errorf("coarse X = %d, 期待 %d", got, 0x25>>3)
	}
	if got := v >> 5 & 0x1F; got != 0x47>>3 {
		t.Errorf("coarse Y = %d, 期待 %d", got, 0x47>>3)
	}
	if got := v >> 12 & 7; got != 0x47&7 {
		t.Errorf("fine Y = %d, 期待 %d", got, 0x47&7)
	}
	if got := v >> 10 & 3; got != 1 {
		t.Errorf("ネームテーブル = %d, 期待 1", got)
	}
}
