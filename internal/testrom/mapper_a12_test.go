package testrom_test

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// buildMMC3LoopROM は無限ループするだけの MMC3 の ROM を組む。
//
// テスト ROM を使わずに構成を作るのは、$2000 と $2001 をこちらで決めて
// A12 の立ち上がり回数を数えるためである。テスト ROM を走らせると、
// その ROM 自身がレジスタを書き換える。
func buildMMC3LoopROM() []uint8 {
	const prgSize = 128 * 1024
	out := make([]uint8, 16+prgSize)
	// PRG 8 単位、CHR-ROM 無し（CHR-RAM 8 KiB）、マッパー 4。
	copy(out, []uint8{'N', 'E', 'S', 0x1A, 8, 0, 0x40, 0x00})
	prg := out[16:]
	// 最後の 8 KiB バンクが $E000-$FFFF に現れる。
	last := prgSize - 8*1024
	prg[last+0x0000] = 0x4C // JMP $E000
	prg[last+0x0001] = 0x00
	prg[last+0x0002] = 0xE0
	prg[last+0x1FFC] = 0x00 // RESET = $E000
	prg[last+0x1FFD] = 0xE0
	return out
}

// TestMMC3A12RisesPerFrame は 1 フレームあたりの A12 立ち上がり回数を
// 確かめる。
//
// 背景とスプライトが別のパターンテーブルを使うとき、走査線ごとに
// 1 回だけ立ち上がる。可視 240 行とプリレンダー行で 241 回になる。
// 短い low を数えると走査線あたり 3 回になり、IRQ が 3 倍の速さで
// 発生する。
func TestMMC3A12RisesPerFrame(t *testing.T) {
	rom, err := cart.LoadROM(buildMMC3LoopROM())
	if err != nil {
		t.Fatal(err)
	}
	n, err := nes.New(rom, region.NTSC)
	if err != nil {
		t.Fatal(err)
	}
	n.PowerOn(nes.Deterministic())
	m, ok := n.Cart.(*cart.MMC3)
	if !ok {
		t.Fatalf("MMC3 ではない（%T）", n.Cart)
	}
	// PPU のウォームアップが終わるまで $2000 への書き込みが効かない。
	n.RunFrames(3)

	for _, tt := range []struct {
		name string
		ctrl uint8
		want uint64
	}{
		// 背景が $0000、スプライトが $1000。スプライトのフェッチで
		// 走査線ごとに 1 回立ち上がる。
		{"背景 $0000 / スプライト $1000", 0x08, 241},
		// 両方が $0000。A12 が上がらない。
		{"背景 $0000 / スプライト $0000", 0x00, 0},
		// 両方が $1000。ネームテーブルのフェッチで A12 が下がる時間が
		// 短く、走査線ごとの立ち上がりを数えない。フレームの先頭で
		// 1 回だけ数える。VBlank の間はフェッチが無く、A12 が下がった
		// ままになるためである。同じパターンテーブルを使うゲームで
		// スキャンライン IRQ が使えないことに対応する。
		{"背景 $1000 / スプライト $1000", 0x18, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n.Bus.Write(0x2000, tt.ctrl)
			n.Bus.Write(0x2001, 0x1E)
			n.RunFrames(1)
			before := m.A12Rises()
			n.RunFrames(1)
			if got := m.A12Rises() - before; got != tt.want {
				t.Errorf("立ち上がり = %d 回/フレーム, 期待 %d 回", got, tt.want)
			}
		})
	}
}
