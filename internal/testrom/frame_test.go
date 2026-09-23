package testrom_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// frameCases は描画を検証する ROM とフレーム数。
//
// 背景を描く ROM を選ぶ。スプライトを使う ROM は次のフェーズで加える。
var frameCases = []struct {
	rom    string
	frames int
}{
	{"blargg_ppu_tests_2005.09.15b/palette_ram.nes", 180},
	{"blargg_ppu_tests_2005.09.15b/vram_access.nes", 180},
	{"branch_timing_tests/1.Branch_Basics.nes", 300},
	{"full_palette/full_palette.nes", 120},
	{"other/nestest.nes", 120},
	// スプライトを使う場面
	{"sprite_hit_tests_2005.10.05/04.flip.nes", 240},
	{"sprite_hit_tests_2005.10.05/08.double_height.nes", 240},
	{"sprite_overflow_tests/1.Basics.nes", 240},
	{"scanline/scanline.nes", 300},
	// DMC IRQ を使って背景の上下を隠す。DMA と IRQ のタイミングを
	// 画面の形として見る。
	{"dpcmletterbox/dpcmletterbox.nes", 300},
}

// TestFrameHashes は描画したフレームのハッシュを golden と照合する。
//
// 表示を実装する前に描画の正しさを固定するための手段である。
// 環境変数 SHOGUN_UPDATE_FRAMES を設定すると golden を書き直し、
// SHOGUN_FRAME_PNG を設定すると PNG を出力する。
func TestFrameHashes(t *testing.T) {
	root := testrom.RepoRoot(t)
	path := testrom.FrameHashPath(root)
	want, err := testrom.LoadFrameHashes(path)
	if err != nil {
		t.Fatalf("フレームハッシュの表を読めない: %v", err)
	}

	update := os.Getenv("SHOGUN_UPDATE_FRAMES") != ""
	pngDir := os.Getenv("SHOGUN_FRAME_PNG")
	got := map[string]string{}

	for _, tc := range frameCases {
		key := tc.rom
		t.Run(key, func(t *testing.T) {
			romPath := testrom.RequireROM(t, tc.rom)
			m := newMachine(t, romPath).(machine)
			testrom.RunFrames(m, tc.frames)

			f := lastFrame(t, m.NES)
			h := testrom.FrameHash(f)
			got[key] = h

			if pngDir != "" {
				name := filepath.Base(key) + ".png"
				if err := testrom.WritePNG(filepath.Join(pngDir, name), f); err != nil {
					t.Fatalf("PNG を書けない: %v", err)
				}
			}

			if update {
				t.Logf("ハッシュを記録した: %s", h)
				return
			}
			w, ok := want[key]
			if !ok {
				t.Skipf("golden が無い。SHOGUN_UPDATE_FRAMES=1 で記録する（今回のハッシュ %s）", h)
			}
			if h != w {
				t.Errorf("フレームハッシュが一致しない\n実際: %s\n期待: %s", h, w)
			}
		})
	}

	if update {
		for k, v := range got {
			want[k] = v
		}
		if err := testrom.SaveFrameHashes(path, want); err != nil {
			t.Fatalf("フレームハッシュの表を書けない: %v", err)
		}
		t.Logf("%s を更新した", path)
	}
}

// lastFrame は直前に完成したフレームを返す。
func lastFrame(t *testing.T, n *nes.NES) *video.Frame {
	t.Helper()
	f := n.TakeFrame()
	if f == nil {
		t.Fatal("完成したフレームが無い")
	}
	return f
}

// checkFrameHash はフレームのハッシュを golden と照合する。
//
// 表に無いときは飛ばす。SHOGUN_UPDATE_FRAMES を設定すると記録する。
func checkFrameHash(t *testing.T, key string, f *video.Frame) {
	t.Helper()
	root := testrom.RepoRoot(t)
	path := testrom.FrameHashPath(root)
	want, err := testrom.LoadFrameHashes(path)
	if err != nil {
		t.Fatalf("フレームハッシュの表を読めない: %v", err)
	}
	h := testrom.FrameHash(f)

	if os.Getenv("SHOGUN_UPDATE_FRAMES") != "" {
		want[key] = h
		if err := testrom.SaveFrameHashes(path, want); err != nil {
			t.Fatalf("フレームハッシュの表を書けない: %v", err)
		}
		t.Logf("%s のハッシュを記録した: %s", key, h)
		return
	}
	w, ok := want[key]
	if !ok {
		t.Skipf("golden が無い。SHOGUN_UPDATE_FRAMES=1 で記録する（今回のハッシュ %s）", h)
	}
	if h != w {
		t.Errorf("%s のフレームハッシュが一致しない\n実際: %s\n期待: %s", key, h, w)
	}
}
