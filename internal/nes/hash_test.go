package nes

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// buildTestNES は無限ループするだけの NROM の本体を組み立てる。
//
// テスト ROM を使わないのは、この検証が ROM の内容に依存しないため
// である。取得していない環境でも動く。
func buildTestNES(t *testing.T) *NES {
	t.Helper()
	const prgSize = 32 * 1024
	data := make([]uint8, 16+prgSize+8*1024)
	copy(data, []uint8{'N', 'E', 'S', 0x1A, 2, 1, 0x02, 0x00})
	prg := data[16 : 16+prgSize]
	prg[0] = 0x4C // JMP $8000
	prg[1] = 0x00
	prg[2] = 0x80
	prg[0x7FFC] = 0x00 // RESET = $8000
	prg[0x7FFD] = 0x80

	rom, err := cart.LoadROM(data)
	if err != nil {
		t.Fatalf("ROM を解析できない: %v", err)
	}
	n, err := New(rom, region.NTSC)
	if err != nil {
		t.Fatalf("本体を組み立てられない: %v", err)
	}
	n.PowerOn(Deterministic())
	return n
}

// TestStateHashIsStable は同じ状態から同じハッシュが得られることを
// 確かめる。
func TestStateHashIsStable(t *testing.T) {
	a := buildTestNES(t)
	b := buildTestNES(t)
	a.RunFrames(3)
	b.RunFrames(3)

	if a.StateHash() != b.StateHash() {
		t.Errorf("同じ手順で違うハッシュになった（%x と %x）", a.StateHash(), b.StateHash())
	}
}

// TestStateHashDetectsChange は対象を 1 バイト変えるとハッシュが変わる
// ことを確かめる。
func TestStateHashDetectsChange(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(n *NES)
	}{
		{"内蔵 RAM", func(n *NES) { n.Bus.RAM()[0x123] ^= 0xFF }},
		{"CIRAM", func(n *NES) { n.PPU.CIRAM()[0x10] ^= 0xFF }},
		{"OAM", func(n *NES) { n.PPU.OAM()[0x20] ^= 0xFF }},
		{"パレット RAM", func(n *NES) { n.PPU.Palette()[3] ^= 0x0F }},
		{"PRG-RAM", func(n *NES) { n.Cart.PRGRAM()[0] ^= 0xFF }},
		{"CPU のレジスタ", func(n *NES) { n.CPU.X ^= 0xFF }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			n := buildTestNES(t)
			n.RunFrames(2)
			before := n.StateHash()
			tt.change(n)
			if n.StateHash() == before {
				t.Errorf("%s を変えてもハッシュが変わらない", tt.name)
			}
		})
	}
}

// TestStateHashIgnoresScreen は画面の内容がハッシュに影響しないことを
// 確かめる。状態が一致していれば画面も一致するためである。
func TestStateHashIgnoresScreen(t *testing.T) {
	n := buildTestNES(t)
	n.RunFrames(2)
	before := n.StateHash()

	f := n.TakeFrame()
	if f == nil {
		t.Fatal("フレームが出てこない")
	}
	f.Set(10, 10, 0x3F)

	if n.StateHash() != before {
		t.Error("画面を変えたらハッシュが変わった")
	}
}
