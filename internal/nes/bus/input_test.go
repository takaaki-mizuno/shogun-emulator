package bus

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// fixedSource は固定の押下状態を返す供給元。
type fixedSource struct{ buttons [input.PortCount]uint8 }

func (s *fixedSource) Buttons(port int) uint8 { return s.buttons[port] }

// connectControllers はバスへ標準コントローラを繋ぐ。
func connectControllers(b *Bus, src input.Source) {
	for i := range input.PortCount {
		b.SetPort(i, input.NewStandardController(src, i))
	}
}

// TestControllerUpperBitsComeFromOpenBus は $4016 と $4017 の上位 3 bit が
// オープンバスから来ることを確かめる。
//
// $4016 の読み出しは命令のオペコードフェッチの直後に起こる。上位 3 bit が
// 直前に読まれた値（多くは $40 = LDA $4016 の上位バイト）になることを
// 期待するプログラムがある。
func TestControllerUpperBitsComeFromOpenBus(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	src := &fixedSource{}
	connectControllers(b, src)

	// RAM を読んでオープンバスに値を残す
	b.Write(0x0000, 0xE0)
	b.Read(0x0000)

	src.buttons[0] = input.ButtonA
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)

	got := b.Read(0x4016)
	if got&0xE0 != 0xE0 {
		t.Errorf("上位 3 bit = %#02x, 期待 $E0（オープンバスの値）", got&0xE0)
	}
	if got&0x1F != 1 {
		t.Errorf("下位 5 bit = %#02x, 期待 $01（A を押している）", got&0x1F)
	}
}

// TestControllerReadDoesNotUpdateOpenBus はコントローラの読み出しが
// オープンバスを書き換えないことを確かめる。
//
// 書き換えると、上位 3 bit が次の読み出しで下位のビット列に汚染される。
func TestControllerReadDoesNotUpdateOpenBus(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	src := &fixedSource{}
	connectControllers(b, src)

	b.Write(0x0000, 0x40)
	b.Read(0x0000)

	src.buttons[0] = 0xFF
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)
	for range 4 {
		if got := b.Read(0x4016) & 0xE0; got != 0x40 {
			t.Fatalf("上位 3 bit = %#02x, 期待 $40", got)
		}
	}
}

// TestStrobeGoesToBothPorts は $4016 への書き込みが両方のポートへ
// 届くことを確かめる。ポート 2 へ書き込む方法は実機に無い。
func TestStrobeGoesToBothPorts(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	src := &fixedSource{}
	connectControllers(b, src)

	src.buttons[0] = input.ButtonA
	src.buttons[1] = input.ButtonA
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)

	if got := b.Read(0x4016) & 1; got != 1 {
		t.Errorf("ポート 1 = %d, 期待 1", got)
	}
	if got := b.Read(0x4017) & 1; got != 1 {
		t.Errorf("ポート 2 = %d, 期待 1", got)
	}
}

// TestWriteToPort2AddressDoesNotStrobe は $4017 への書き込みが APU の
// フレームカウンタへ行き、コントローラを strobe しないことを確かめる。
func TestWriteToPort2AddressDoesNotStrobe(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	src := &fixedSource{}
	connectControllers(b, src)

	src.buttons[0] = 0xFF
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)
	b.Read(0x4016) // A を読む

	b.Write(0x4017, 0x80)
	if got := b.Read(0x4016) & 1; got != 1 {
		t.Errorf("$4017 への書き込みでシフトレジスタが戻された")
	}
}

// TestPeekControllerHasNoSideEffect は Peek が読み出しを進めないことを
// 確かめる。デバッガがメモリを表示するだけで入力が壊れてはならない。
func TestPeekControllerHasNoSideEffect(t *testing.T) {
	b := newTestBus(t, region.NTSC)
	src := &fixedSource{}
	connectControllers(b, src)

	src.buttons[0] = input.ButtonB // bit 1 のみ
	b.Write(0x4016, 1)
	b.Write(0x4016, 0)

	for range 4 {
		if got := b.Peek(0x4016) & 1; got != 0 {
			t.Fatalf("Peek = %d, 期待 0", got)
		}
	}
	if got := b.Read(0x4016) & 1; got != 0 {
		t.Errorf("A = %d, 期待 0", got)
	}
	if got := b.Read(0x4016) & 1; got != 1 {
		t.Errorf("B = %d, 期待 1", got)
	}
}

// TestInputStateRoundtrip は入力の状態がセーブステートを往復することを
// 確かめる。
func TestInputStateRoundtrip(t *testing.T) {
	src := &fixedSource{}
	src.buttons[0] = input.ButtonA | input.ButtonRight

	a := newTestBus(t, region.NTSC)
	connectControllers(a, src)
	a.Write(0x4016, 1)
	a.Write(0x4016, 0)
	a.Read(0x4016)

	w := state.NewWriter()
	a.SaveState(w)

	b := newTestBus(t, region.NTSC)
	connectControllers(b, src)
	if err := b.LoadState(state.NewReader(w.Data())); err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		x, y := a.Read(0x4016)&1, b.Read(0x4016)&1
		if x != y {
			t.Fatalf("%d 回目の読み出しが一致しない: %d と %d", i+1, x, y)
		}
	}
}

// TestLoadStateRejectsDifferentDeviceKind は保存時と違う種別のデバイスが
// 繋がっているステートを拒むことを確かめる。
func TestLoadStateRejectsDifferentDeviceKind(t *testing.T) {
	a := newTestBus(t, region.NTSC)
	connectControllers(a, &fixedSource{})
	w := state.NewWriter()
	a.SaveState(w)

	b := newTestBus(t, region.NTSC) // ポートは未接続のまま
	if err := b.LoadState(state.NewReader(w.Data())); err == nil {
		t.Error("デバイス種別が違うステートを受け入れてしまった")
	}
}
