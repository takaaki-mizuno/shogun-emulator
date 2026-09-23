package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// testROMPath はリポジトリ内のテスト ROM のパスを返す。
//
// 無いときはテストを飛ばす。テスト ROM は配布物に含めず、
// `go run ./tools/fetch-test-roms` で取得する。
func testROMPath(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "roms", rel)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("テスト ROM が無い: %s", rel)
	}
	return path
}

// TestScreenIsPaintedByFyne は Fyne が実際に描いた結果に NES の画面が
// 現れることを確かめる。
//
// 画面の内容を image.RGBA へ変換するところまでは screen_test.go が
// 確かめる。ここでは canvas.Image を Fyne の描画系へ通し、拡大した
// 結果が出てくることを見る。ウィンドウを開かずに済ませるため、
// ソフトウェア描画を使う。
func TestScreenIsPaintedByFyne(t *testing.T) {
	test.NewApp()

	e := emu.New(emu.Config{
		Emulation: config.EmulationConfig{Region: config.RegionNTSC, RAMInitPattern: "zero"},
		Input:     config.InputConfig{Port1Device: config.DeviceStandard},
		NewPacer:  func(*region.Region) emu.Pacer { return emu.NewNoPacer() },
	})
	e.Start()
	defer e.Stop()

	if err := e.LoadROM(testROMPath(t, "full_palette/full_palette.nes")); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Video.Scale = 2
	s := newScreen(e.Frames, video.DefaultPalette(), cfg.Video)

	// 画面が描かれるまで待って取り込む。
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.refresh()
		if !isUniform(s.rgba) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("画面に変化が現れない")
		}
		time.Sleep(2 * time.Millisecond)
	}

	w, h := s.PixelSize()
	s.CanvasObject().Resize(fyne.NewSize(float32(w), float32(h)))
	img := software.Render(s.CanvasObject(), theme.DefaultTheme())

	if got := img.Bounds(); got.Dx() != w || got.Dy() != h {
		t.Errorf("描画結果の大きさ = %dx%d, 期待 %dx%d", got.Dx(), got.Dy(), w, h)
	}
	if isUniformImage(img) {
		t.Fatal("描画結果が単色である。画面が出ていない")
	}

	// 拡大は最近傍である。2 倍のとき隣り合う 2 ピクセルが同じ色になる。
	if c0, c1 := img.At(0, 0), img.At(1, 0); c0 != c1 {
		t.Errorf("2 倍の拡大で隣接ピクセルの色が違う: %v と %v", c0, c1)
	}

	if dir := os.Getenv("SHOGUN_FRAME_PNG"); dir != "" {
		writeRendered(t, filepath.Join(dir, "ui-screen.png"), img)
	}
}

// isUniform は画像が単色かを返す。
func isUniform(img *image.RGBA) bool {
	first := img.Pix[0:4]
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] != first[0] || img.Pix[i+1] != first[1] || img.Pix[i+2] != first[2] {
			return false
		}
	}
	return true
}

// isUniformImage は image.Image が単色かを返す。
func isUniformImage(img image.Image) bool {
	b := img.Bounds()
	first := color.RGBAModel.Convert(img.At(b.Min.X, b.Min.Y))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)) != first {
				return false
			}
		}
	}
	return true
}

// writeRendered は描画結果を PNG として書く。目視の確認に使う。
func writeRendered(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestScreenshotWritesPNG はスクリーンショットのホットキーが PNG を
// 書くことを確かめる。保存先は設定で指定した場所にする。
func TestScreenshotWritesPNG(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)
	u.pal = video.DefaultPalette()
	u.screen = newScreen(u.emu.Frames, u.pal, u.cfg.Video)
	u.status = newStatusBar(u.emu.Frames)

	dir := t.TempDir()
	u.cfg.Paths.ScreenshotDir = dir

	// ROM が無いときは何もしない。
	if err := u.saveScreenshot(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("ROM が無いのに %d 個のファイルができた", len(entries))
	}

	if err := u.emu.LoadROM(testROMPath(t, "full_palette/full_palette.nes")); err != nil {
		t.Fatal(err)
	}
	u.screen.SetPictureHeight(u.emu.Status().PictureHeight)
	u.screen.refresh()

	if err := u.saveScreenshot(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("できたファイルの数 = %d, 期待 1", len(entries))
	}

	f, err := os.Open(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfgImg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	// オーバースキャンを反映した大きさになる。拡大率は掛けない。
	if cfgImg.Width != video.Width || cfgImg.Height != video.Height-16 {
		t.Errorf("大きさ = %dx%d, 期待 %dx%d",
			cfgImg.Width, cfgImg.Height, video.Width, video.Height-16)
	}
}

// TestRefreshIsDrivenByAnimation は画面の更新が Fyne のフレーム駆動に
// 載っていることを確かめる。
//
// 自前のゴルーチンからタイマーで更新すると、終了処理と重なったときに
// UI オブジェクトを UI スレッド以外から触ることになる。
func TestRefreshIsDrivenByAnimation(t *testing.T) {
	test.NewApp()
	u := newTestUI(t)
	u.app = test.NewApp()
	u.pal = video.DefaultPalette()
	u.screen = newScreen(u.emu.Frames, u.pal, u.cfg.Video)
	u.status = newStatusBar(u.emu.Frames)

	u.startRefreshing()
	if u.refreshAnim == nil || u.refreshAnim.Tick == nil {
		t.Fatal("更新のアニメーションが登録されていない")
	}
	if u.refreshAnim.RepeatCount != fyne.AnimationRepeatForever {
		t.Errorf("繰り返しの指定 = %d, 期待 %d",
			u.refreshAnim.RepeatCount, fyne.AnimationRepeatForever)
	}

	// tick でフレームが取り込まれること
	f := video.NewFrame()
	f.Clear(0x20) // 白
	u.emu.Frames.Put(f)

	u.refreshAnim.Tick(0)
	if got := u.screen.rgba.RGBAAt(0, 0); got != (color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}) {
		t.Errorf("tick 後の (0,0) = %+v, 期待 白", got)
	}

	u.stopRefreshing()
	if u.refreshAnim != nil {
		t.Error("停止した後もアニメーションが残っている")
	}
}
