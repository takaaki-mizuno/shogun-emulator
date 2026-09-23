package ui

import (
	"image"
	"testing"

	"fyne.io/fyne/v2/container"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/apu"
)

// advanceFrame は一時停止したまま 1 フレーム進め、処理を終えるまで待つ。
func advanceFrame(u *UI) {
	u.emu.FrameAdvance()
	u.emu.WithMachine(func(*nes.NES) {})
}

// poke はテストのためにメモリを書き換える。
func poke(t *testing.T, u *UI, s debug.Space, addr int, v uint8) {
	t.Helper()
	if err := u.emu.Poke(s, addr, v, false); err != nil {
		t.Fatal(err)
	}
}

// TestPPUViewersInBothLayouts は 5 つのビューアを別ウィンドウとタブの両方で
// 開け、閉じるとフックが外れることを確かめる（フェーズ 11 計画 §3.9）。
func TestPPUViewersInBothLayouts(t *testing.T) {
	u := newDebugTestUI(t)
	viewers := []Viewer{u.patterns(), u.nametables(), u.sprites(), u.palettes(), u.apus()}
	for _, v := range viewers {
		u.host.Show(v)
	}
	if hooksOf(t, u).OnFrameComplete == nil {
		t.Error("PPU 系のビューアを開いても OnFrameComplete が無い")
	}
	advanceFrame(u)
	for _, v := range viewers {
		v.Refresh()
	}

	// タブの配置へ移す。Content を作り直しても購読は 1 つずつに保たれる。
	docked := newDockedHost(container.NewAppTabs())
	switchHost(u.host, docked)
	u.host = docked
	if got := len(docked.Visible()); got != len(viewers) {
		t.Fatalf("タブのビューアの数 = %d, 期待 %d", got, len(viewers))
	}
	advanceFrame(u)
	for _, v := range docked.Visible() {
		v.Refresh()
	}
	docked.Close()
	if h := hooksOf(t, u); anyHook(h) {
		t.Errorf("すべて閉じた後もフックが残っている: %+v", h)
	}
}

// TestPatternViewerAndTileEditor はパターンテーブルの描画と、ピクセルエディタの
// 編集がオーバーレイへ届くことを確かめる（設計書 09 編 §9.4.1）。
func TestPatternViewerAndTileEditor(t *testing.T) {
	u := newDebugTestUI(t)
	// タイル 1 の 1 行目の下位プレーンを塗る（CHR-ROM なのでオーバーレイへ行く）。
	poke(t, u, debug.SpacePPU, 0x0010, 0xFF)
	v := u.patterns()
	u.host.Show(v)
	advanceFrame(u)
	v.Refresh()
	if got := v.view.Image().RGBAAt(8, 0); got != greyLevels[1] {
		t.Errorf("タイル 1 の左上 = %v, 期待 %v", got, greyLevels[1])
	}
	if got := v.view.Image().RGBAAt(8, 1); got != greyLevels[0] {
		t.Errorf("タイル 1 の 2 行目 = %v, 期待 %v", got, greyLevels[0])
	}

	// 8×16 の並びでは 2 行目の左端がタイル 1 になる。
	v.tall = true
	if _, tile := v.tileAt(0, 1); tile != 1 {
		t.Errorf("8×16 の並びの (0, 1) = タイル %d, 期待 1", tile)
	}
	if _, tile := v.tileAt(1, 0); tile != 2 {
		t.Errorf("8×16 の並びの (1, 0) = タイル %d, 期待 2", tile)
	}
	v.tall = false

	// ピクセルエディタで色 3 を置く。
	v.tap(8, 0)
	ed := u.tileEditors[0x0010]
	if ed == nil || !u.host.IsVisible(ed) {
		t.Fatal("ピクセルエディタが開いていない")
	}
	ed.current = 3
	ed.paint(0, 1)
	ed.endStroke()
	var lo, hi uint8
	u.emu.WithMachine(func(n *nes.NES) {
		lo, hi = n.PPU.PeekVRAM(0x0011), n.PPU.PeekVRAM(0x0019)
	})
	if lo&0x80 == 0 || hi&0x80 == 0 {
		t.Errorf("色 3 を置いた後の $0011 = $%02X, $0019 = $%02X", lo, hi)
	}
	if st := u.emu.OverlayStatus(); st.CHR != 3 {
		t.Errorf("オーバーレイの CHR の変更 = %d, 期待 3", st.CHR)
	}
	ed.undoLast()
	u.emu.WithMachine(func(n *nes.NES) { lo = n.PPU.PeekVRAM(0x0011) })
	if lo != 0 {
		t.Errorf("元に戻した後の $0011 = $%02X", lo)
	}

	// オーバーレイを無効にすると元の内容に戻る。
	if err := u.emu.SetOverlayEnabled(false); err != nil {
		t.Fatal(err)
	}
	advanceFrame(u)
	v.Refresh()
	if got := v.view.Image().RGBAAt(8, 0); got != greyLevels[0] {
		t.Errorf("オーバーレイを無効にした後のタイル 1 = %v", got)
	}
}

// TestNametableViewerMapsCellsAndScroll はネームテーブルの位置の計算と
// スクロール枠の位置を確かめる（設計書 09 編 §9.4.2）。
func TestNametableViewerMapsCellsAndScroll(t *testing.T) {
	var snap debug.Snapshot
	snap.Nametables[0x400+2*32+33%32] = 0x42 // 右上の面の (1, 2)
	snap.Nametables[0x400+0x3C0] = 0b11_10_01_00
	c := cell(&snap, 33, 2)
	if c.addr != 0x2441 || c.tile != 0x42 {
		t.Errorf("右上の面の (1, 2) = $%04X タイル $%02X", c.addr, c.tile)
	}
	// 属性バイトは左上・右上・左下・右下の順に 2 bit ずつ下位から並ぶ。
	// (1, 2) は 16×16 の領域の左下にあたる。
	if c.attrAddr != 0x27C0 || c.palette != 2 {
		t.Errorf("属性 = $%04X パレット %d, 期待 $27C0 パレット 2", c.attrAddr, c.palette)
	}
	for _, tc := range []struct{ tx, ty, want int }{
		{32, 0, 0}, {34, 0, 1}, {32, 2, 2}, {35, 3, 3},
	} {
		if got := int(cell(&snap, tc.tx, tc.ty).palette); got != tc.want {
			t.Errorf("(%d, %d) のパレット = %d, 期待 %d", tc.tx-32, tc.ty, got, tc.want)
		}
	}

	// v: ネームテーブル 3、coarse X 5、coarse Y 7、fine Y 2。fine X 3。
	v := uint16(3<<10 | 5 | 7<<5 | 2<<12)
	x, y := scrollOrigin(v, 3)
	if x != 256+5*8+3 || y != 240+7*8+2 {
		t.Errorf("スクロール枠の左上 = (%d, %d)", x, y)
	}

	// 枠が右下の端をまたぐときは左上へ折り返して描く。
	img := image.NewRGBA(image.Rect(0, 0, 512, 480))
	drawRectOutline(img, image.Rect(400, 400, 656, 640), colorScrollFrame)
	if img.RGBAAt(0, 400) != colorScrollFrame || img.RGBAAt(143, 0) != colorScrollFrame {
		t.Error("折り返した枠が描かれていない")
	}
}

// TestNametableViewerEditsTile はネームテーブルへの書き込みが CIRAM に届くことを確かめる。
func TestNametableViewerEditsTile(t *testing.T) {
	u := newDebugTestUI(t)
	v := u.nametables()
	u.host.Show(v)
	v.poke(0x2005, 0x33)
	advanceFrame(u)
	v.Refresh()
	if got := cell(&v.src.snap, 5, 0).tile; got != 0x33 {
		t.Errorf("書いたタイル番号 = $%02X, 期待 $33", got)
	}
}

// TestSpriteViewerDragAndEdit はドラッグと数値の編集で OAM が変わることを確かめる
// （設計書 09 編 §9.4.3）。
func TestSpriteViewerDragAndEdit(t *testing.T) {
	u := newDebugTestUI(t)
	for i := range 64 {
		poke(t, u, debug.SpaceOAM, i*4, 0xFF)
	}
	poke(t, u, debug.SpaceOAM, 0, 20)
	poke(t, u, debug.SpaceOAM, 3, 10)
	v := u.sprites()
	u.host.Show(v)
	advanceFrame(u)
	v.Refresh()
	if !v.src.snap.SpriteDrawn[0] || v.src.snap.SpriteDrawn[1] {
		t.Errorf("描かれたスプライト = %v", v.src.snap.SpriteDrawn[:2])
	}

	v.pick(12, 25)
	if v.selected != 0 {
		t.Fatalf("選んだスプライト = %d, 期待 0", v.selected)
	}
	v.drag(0, 0, 5.5, 3)
	var oam [4]uint8
	u.emu.WithMachine(func(n *nes.NES) { copy(oam[:], n.PPU.OAM()) })
	if oam[0] != 23 || oam[3] != 15 {
		t.Errorf("ドラッグした後の Y, X = %d, %d, 期待 23, 15", oam[0], oam[3])
	}
	v.setField(2, "$C1")
	u.emu.WithMachine(func(n *nes.NES) { copy(oam[:], n.PPU.OAM()) })
	if oam[2] != 0xC1 {
		t.Errorf("属性 = $%02X, 期待 $C1", oam[2])
	}

	// メイン画面に矩形を重ねる。
	u.screen = newScreen(u.emu.Frames, u.pal, u.cfg.Video)
	v.boxes = true
	v.updateBoxes()
	if n := len(u.screen.overlay.boxes); n != 1 {
		t.Errorf("重ねた矩形の数 = %d, 期待 1", n)
	}
	u.host.Hide(v)
	if n := len(u.screen.overlay.boxes); n != 0 {
		t.Errorf("閉じた後も矩形が %d 個残っている", n)
	}
}

// TestPaletteViewerMirrorsBackdrop は $3F10 への書き込みが $3F00 にも現れる
// ことを確かめる（設計書 09 編 §9.4.4）。
func TestPaletteViewerMirrorsBackdrop(t *testing.T) {
	u := newDebugTestUI(t)
	v := u.palettes()
	u.host.Show(v)
	advanceFrame(u)
	v.Refresh()
	v.setColor(0x10, 0x21)
	var p0 uint8
	u.emu.WithMachine(func(n *nes.NES) { p0 = n.PPU.PeekVRAM(0x3F00) })
	if p0 != 0x21 {
		t.Errorf("$3F10 に書いた後の $3F00 = $%02X, 期待 $21", p0)
	}
	if got := v.entries.Image().RGBAAt(8, 8); got != u.pal.Color(0x21) {
		t.Errorf("backdrop の表示 = %v", got)
	}
}

// TestAPUViewerMute はミュートとソロが APU の出力段へ届くことを確かめる。
func TestAPUViewerMute(t *testing.T) {
	u := newDebugTestUI(t)
	v := u.apus()
	u.host.Show(v)
	v.setMask(0x1F &^ apu.MuteTriangle)
	var mute uint8
	u.emu.WithMachine(func(n *nes.NES) { mute = n.APU.Mute() })
	if mute != 0x1F&^apu.MuteTriangle {
		t.Errorf("ミュート = %05b", mute)
	}
	v.Refresh()
	if !v.mutes[0].Checked || v.mutes[2].Checked {
		t.Error("チェックがミュートと合っていない")
	}
	v.setMask(0)
}

// TestSnapshotLinePerViewer は取得位置をビューアごとに独立して持つことを確かめる
// （設計書 09 編 §9.3.1）。
func TestSnapshotLinePerViewer(t *testing.T) {
	u := newDebugTestUI(t)
	p, n := u.patterns(), u.nametables()
	u.host.Show(p)
	u.host.Show(n)
	p.src.setLine(30)
	advanceFrame(u)
	p.Refresh()
	n.Refresh()
	if p.src.snap.Scanline != 30 || n.src.snap.Scanline != -1 {
		t.Errorf("取得位置 = パターン %d, ネームテーブル %d", p.src.snap.Scanline, n.src.snap.Scanline)
	}
	if h := hooksOf(t, u); h.OnCycle == nil || h.OnFrameComplete == nil {
		t.Errorf("フック = %+v", h)
	}
	u.host.Hide(p)
	if hooksOf(t, u).OnCycle != nil {
		t.Error("スキャンラインの購読をやめても OnCycle が残っている")
	}
	u.host.Hide(n)
}

// BenchmarkPPUViewersDraw は 5 つのビューアを 1 回ずつ描き直す UI スレッドの費用を測る。
//
//	go test -run '^$' -bench PPUViewersDraw ./internal/ui
func BenchmarkPPUViewersDraw(b *testing.B) {
	u := newDebugTestUI(b)
	viewers := []Viewer{u.patterns(), u.nametables(), u.sprites(), u.palettes(), u.apus()}
	for _, v := range viewers {
		u.host.Show(v)
	}
	advanceFrame(u)
	b.ResetTimer()
	for b.Loop() {
		for _, v := range viewers {
			v.Refresh()
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/refresh")
}

// TestSpritePixelTallAndFlip は 8×16 のスプライトと反転の描画を確かめる。
func TestSpritePixelTallAndFlip(t *testing.T) {
	var snap debug.Snapshot
	snap.SpriteHeight = 16
	// $1000 側のタイル $02（上）と $03（下）。上の 1 行目の左端と、下の 8 行目の右端を塗る。
	snap.CHR[0x1000+2*16] = 0x80
	snap.CHR[0x1000+3*16+7] = 0x01
	s := spriteInfo{tile: 0x03} // bit 0 が 1 なので $1000 側、タイルは $02/$03
	if spritePixel(&snap, s, 0, 0) != 1 || spritePixel(&snap, s, 7, 15) != 1 {
		t.Error("8×16 の上下のタイルが描かれていない")
	}
	s.attr = 0xC0 // 水平・垂直反転
	if spritePixel(&snap, s, 7, 15) != 1 || spritePixel(&snap, s, 0, 0) != 1 {
		t.Error("反転した 8×16 の位置が違う")
	}
	if spritePixel(&snap, s, 0, 15) != 0 {
		t.Error("反転していない位置に色がある")
	}
}
