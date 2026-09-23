package bus

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// TestOAMDMACycleCount は OAM DMA が 513 または 514 サイクルを
// 消費することを確かめる。
//
// 開始が get サイクルか put サイクルかで 1 サイクル変わる。
func TestOAMDMACycleCount(t *testing.T) {
	for _, phase := range []int{0, 1} {
		b := newTestBus(t, region.NTSC)
		b.PowerOn(phase)

		// 転送元のページを埋める
		for i := range 256 {
			b.Poke(uint16(0x0200+i), uint8(i))
		}

		// $4014 への書き込み自体で 1 サイクル、その後 DMA が走る。
		before := b.Cycles()
		b.Write(0x4014, 0x02)
		afterWrite := b.Cycles()
		// 次のバスアクセスの先頭で DMA が処理される。
		b.Read(0x0000)
		after := b.Cycles()

		writeCycles := afterWrite - before
		// 次のアクセス 1 サイクル分を除いた残りが DMA の消費である。
		dmaCycles := after - afterWrite - 1

		t.Logf("位相 %d: 書き込み %d サイクル、DMA %d サイクル", phase, writeCycles, dmaCycles)
		if dmaCycles != 513 && dmaCycles != 514 {
			t.Errorf("位相 %d: DMA が %d サイクル。513 か 514 を期待", phase, dmaCycles)
		}
	}
}

// TestOAMDMACopiesPage は転送がページの先頭から前方向に行われることを
// 確かめる。
func TestOAMDMACopiesPage(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)
	for i := range 256 {
		b.Poke(uint16(0x0300+i), uint8(255-i))
	}
	// OAM のアドレスを 0 にしてから転送する。
	b.Write(0x2003, 0x00)
	b.Write(0x4014, 0x03)
	b.Read(0x0000)

	oam := b.ppu.OAM()
	for i := range 256 {
		if oam[i] != uint8(255-i) {
			t.Fatalf("OAM[%d] = %d, 期待 %d", i, oam[i], 255-i)
		}
	}
}

// TestOAMDMAHaltsOnlyOnRead は DMA がライトサイクルでは停止せず、
// 次のリードサイクルまで待つことを確かめる。
func TestOAMDMAHaltsOnlyOnRead(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)

	b.Write(0x4014, 0x02)

	// 続けて書き込みを行っても DMA は始まらない。
	before := b.Cycles()
	b.Write(0x0000, 0x00)
	b.Write(0x0001, 0x00)
	if got := b.Cycles() - before; got != 2 {
		t.Errorf("ライト 2 回で %d サイクル進んだ。期待 2（DMA は始まらない）", got)
	}

	// リードで始まる。
	before = b.Cycles()
	b.Read(0x0000)
	if got := b.Cycles() - before; got < 500 {
		t.Errorf("リードで %d サイクルしか進まない。DMA が始まっていない", got)
	}
}

// TestRepeatedReadClocksControllerExtra は停止中の読み直しで
// コントローラのシフトレジスタが余分に進むことを確かめる。
//
// 実機で「Right が押されたように見える」症状の元である。
func TestRepeatedReadClocksControllerExtra(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)
	src := &fixedSource{}
	connectControllers(b, src)

	// A と B だけを押す。読み出しは 1, 1, 0, 0, ... となる。
	src.buttons[0] = input.ButtonA | input.ButtonB
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)

	// 停止を伴わない読み出し
	got := []uint8{b.Read(0x4016) & 1, b.Read(0x4016) & 1, b.Read(0x4016) & 1}
	if got[0] != 1 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("停止なしの読み出し = %v, 期待 [1 1 0]", got)
	}

	// 読み直しを起こす。$4016 を読もうとしたところで DMC DMA を要求する。
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)
	b.RequestDMCFetch(0x8000, true)
	first := b.Read(0x4016) & 1
	second := b.Read(0x4016) & 1

	if b.ConflictCount() == 0 {
		t.Fatal("読み直しが起きていない")
	}
	// 停止中に余分にクロックされるため、A の次に B ではなく
	// その先のビットが出る。
	t.Logf("読み直しあり: %d, %d（読み直し %d 回）", first, second, b.ConflictCount())
	if first == 1 && second == 1 {
		t.Error("シフトレジスタが余分にクロックされていない")
	}
}

// TestOAMDMAUsesSecondPageOfRMW は `INC $4014` のような RMW 命令で、
// 2 回目に書かれたページから転送されることを確かめる。
//
// RMW 命令は同じアドレスへ 2 回書く。1 回目は読んだ値、2 回目が
// 変更後の値である。停止はライトサイクルでは成立しないため、
// 2 回目の書き込みまで待つことになる。
func TestOAMDMAUsesSecondPageOfRMW(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)
	for i := range 256 {
		b.Poke(uint16(0x0200+i), 0x11)
		b.Poke(uint16(0x0300+i), 0x22)
	}

	// RMW 命令を模す。$02 を書いた直後に $03 を書く。
	b.Write(0x4014, 0x02)
	b.Write(0x4014, 0x03)
	b.Read(0x0000)

	if got := b.ppu.OAM()[0]; got != 0x22 {
		t.Errorf("OAM[0] = %#02x, 期待 $22（2 回目に書かれたページ）", got)
	}
}

// TestRepeatedReadAdvancesPPUAddress は停止中の読み直しで $2007 の
// アドレスが余分に進むことを確かめる。
func TestRepeatedReadAdvancesPPUAddress(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)

	// $2006 でアドレスを $2000 に設定する。
	b.Write(0x2006, 0x20)
	b.Write(0x2006, 0x00)

	// 読み直しなしで 1 回読む
	b.Read(0x2007)
	plain := b.ppu.VRAMAddress()

	b.Write(0x2006, 0x20)
	b.Write(0x2006, 0x00)
	b.RequestDMCFetch(0x8000, true)
	b.Read(0x2007)
	withDMA := b.ppu.VRAMAddress()

	if withDMA <= plain {
		t.Errorf("読み直しありのアドレス = %#04x、なし = %#04x。余分に進んでいない", withDMA, plain)
	}
	t.Logf("読み直しなし %#04x、あり %#04x（読み直し %d 回）", plain, withDMA, b.ConflictCount())
}

// TestRepeatedReadClearsFrameIRQ は停止中の読み直しで $4015 の
// フレーム割り込みフラグが消えることを確かめる。
func TestRepeatedReadClearsFrameIRQ(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)

	// フレーム IRQ が立つまで進める。
	for range 40000 {
		b.Read(0x0000)
		if b.apu.IRQAsserted() {
			break
		}
	}
	if !b.apu.IRQAsserted() {
		t.Skip("フレーム IRQ が立たない")
	}

	// $4015 を読もうとしたところで停止させる。読み直しでフラグが消える。
	b.RequestDMCFetch(0x8000, true)
	b.Read(0x4015)
	if b.ConflictCount() == 0 {
		t.Fatal("読み直しが起きていない")
	}
	if b.apu.IRQAsserted() {
		t.Error("読み直しでフレーム IRQ が消えていない")
	}
}

// TestRegisterConflictCanBeDisabled は設定で読み直しを止められることを
// 確かめる。2A07（PAL）ではこの挙動が修正されている。
func TestRegisterConflictCanBeDisabled(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)
	b.SetDMCRegisterConflicts(false)

	b.RequestDMCFetch(0x8000, true)
	b.Read(0x4016)
	if b.ConflictCount() != 0 {
		t.Errorf("無効にしたのに %d 回読み直した", b.ConflictCount())
	}

	// PAL では設定に関わらず行わない。
	p := newTestBus(t, region.PAL)
	p.PowerOn(0)
	p.RequestDMCFetch(0x8000, true)
	p.Read(0x4016)
	if p.ConflictCount() != 0 {
		t.Errorf("PAL で %d 回読み直した", p.ConflictCount())
	}
}

// TestRepeatedReadWarns は DMC DMA による読み直しを warn.compat へ記録する
// ことを確かめる（設計書 09 編 §9.8）。
func TestRepeatedReadWarns(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	b.PowerOn(0)
	var warnings int
	b.Warn = func(string, ...any) { warnings++ }

	b.Read(0x4016)
	if warnings != 0 {
		t.Fatalf("読み直しが無いのに %d 回記録した", warnings)
	}
	b.RequestDMCFetch(0x8000, true)
	b.Read(0x4016)
	if b.ConflictCount() == 0 {
		t.Fatal("読み直しが起きていない")
	}
	if warnings == 0 {
		t.Error("読み直しを記録していない")
	}
}
