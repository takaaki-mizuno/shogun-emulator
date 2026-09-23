package input

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// fakeSource はテストから押下状態を差し替えられる供給元。
type fakeSource struct{ buttons [PortCount]uint8 }

func (s *fakeSource) Buttons(port int) uint8 { return s.buttons[port] }

// readAll は strobe をかけてから 8 回読み、ビット列を uint8 にまとめる。
func readAll(c *StandardController) uint8 {
	c.Strobe(1)
	c.Strobe(0)
	var v uint8
	for i := range ButtonCount {
		v |= (c.Read() & 1) << i
	}
	return v
}

// TestReadsButtonsInOrder はボタンが定めた順に読まれることを確かめる。
func TestReadsButtonsInOrder(t *testing.T) {
	src := &fakeSource{}
	c := NewStandardController(src, 0)

	tests := []struct {
		name    string
		buttons uint8
	}{
		{"A のみ", ButtonA},
		{"Right のみ", ButtonRight},
		{"A と Start", ButtonA | ButtonStart},
		{"すべて", 0xFF},
		{"なし", 0x00},
	}
	for _, tt := range tests {
		src.buttons[0] = tt.buttons
		if got := readAll(c); got != tt.buttons {
			t.Errorf("%s: 読み出し = %#08b, 期待 %#08b", tt.name, got, tt.buttons)
		}
	}
}

// TestReadsOnesAfterEightReads は 8 回読み終えた後に 1 が返ることを
// 確かめる。接続されているコントローラの数を数えるプログラムがこれを見る。
func TestReadsOnesAfterEightReads(t *testing.T) {
	src := &fakeSource{}
	c := NewStandardController(src, 0)
	src.buttons[0] = 0x00

	c.Strobe(1)
	c.Strobe(0)
	for range ButtonCount {
		if got := c.Read(); got != 0 {
			t.Fatalf("ボタンを押していないのに %d が返った", got)
		}
	}
	for i := range 8 {
		if got := c.Read(); got != 1 {
			t.Errorf("%d 回目の余分な読み出し = %d, 期待 1", i+1, got)
		}
	}
}

// TestStrobeHighKeepsReturningA は strobe が high の間 A を返し続け、
// シフトしないことを確かめる。
func TestStrobeHighKeepsReturningA(t *testing.T) {
	src := &fakeSource{}
	c := NewStandardController(src, 0)
	src.buttons[0] = ButtonA

	c.Strobe(1)
	for i := range 16 {
		if got := c.Read(); got != 1 {
			t.Fatalf("%d 回目 = %d, 期待 1（A を押している）", i+1, got)
		}
	}

	// strobe を下ろすと、直前に取り込んだ値から順に出る。
	c.Strobe(0)
	if got := c.Read(); got != 1 {
		t.Errorf("A = %d, 期待 1", got)
	}
	if got := c.Read(); got != 0 {
		t.Errorf("B = %d, 期待 0", got)
	}
}

// TestStrobeHighSeesLatestButtons は strobe が high の間にボタン状態が
// 変わったとき、最新の値が読まれることを確かめる。
//
// 実機のシフトレジスタは strobe が high の間ボタンから継続的に
// ロードされる。1 フレームに複数回ポーリングするプログラムがこれに依存する。
func TestStrobeHighSeesLatestButtons(t *testing.T) {
	src := &fakeSource{}
	c := NewStandardController(src, 0)

	src.buttons[0] = 0x00
	c.Strobe(1)
	if got := c.Read(); got != 0 {
		t.Fatalf("押していない状態で %d が返った", got)
	}

	// strobe を下ろさずに A を押す
	src.buttons[0] = ButtonA
	if got := c.Read(); got != 1 {
		t.Errorf("strobe 中の変化が読まれない: %d, 期待 1", got)
	}

	// 下ろした直後の読み出しも最新の値から始まる
	c.Strobe(0)
	if got := c.Read(); got != 1 {
		t.Errorf("strobe を下ろした後の最初の読み出し = %d, 期待 1", got)
	}
}

// TestPeekDoesNotShift は Peek が状態を変えないことを確かめる。
func TestPeekDoesNotShift(t *testing.T) {
	src := &fakeSource{}
	c := NewStandardController(src, 0)
	src.buttons[0] = ButtonB // bit 1

	c.Strobe(1)
	c.Strobe(0)
	for range 4 {
		if got := c.Peek(); got != 0 {
			t.Fatalf("Peek = %d, 期待 0（A は押していない）", got)
		}
	}
	if got := c.Read(); got != 0 {
		t.Errorf("A = %d, 期待 0", got)
	}
	if got := c.Read(); got != 1 {
		t.Errorf("B = %d, 期待 1", got)
	}
}

// TestPeekDoesNotReadSource は Peek が供給元を参照しないことを確かめる。
//
// デバッガの表示が、エミュレーションの経過と無関係に変わらないようにする。
func TestPeekDoesNotReadSource(t *testing.T) {
	src := &fakeSource{}
	c := NewStandardController(src, 0)
	c.Strobe(1)
	src.buttons[0] = ButtonA
	if got := c.Peek(); got != 0 {
		t.Errorf("Peek = %d, 期待 0（取り込み前の値を返す）", got)
	}
}

// TestPortSelectsSource はポート番号に応じた押下状態を読むことを確かめる。
func TestPortSelectsSource(t *testing.T) {
	src := &fakeSource{}
	src.buttons[0] = ButtonA
	src.buttons[1] = ButtonB

	if got := readAll(NewStandardController(src, 0)); got != ButtonA {
		t.Errorf("ポート 1 = %#08b, 期待 %#08b", got, ButtonA)
	}
	if got := readAll(NewStandardController(src, 1)); got != ButtonB {
		t.Errorf("ポート 2 = %#08b, 期待 %#08b", got, ButtonB)
	}
}

// TestNilSourceMeansNothingPressed は供給元が nil のとき何も押されて
// いないものとして扱うことを確かめる。
func TestNilSourceMeansNothingPressed(t *testing.T) {
	c := NewStandardController(nil, 0)
	if got := readAll(c); got != 0 {
		t.Errorf("読み出し = %#08b, 期待 0", got)
	}
}

// TestNoDeviceReadsZero は未接続のポートが 0 を返すことを確かめる。
func TestNoDeviceReadsZero(t *testing.T) {
	var d Device = NoDevice{}
	d.Strobe(1)
	d.Strobe(0)
	for range 16 {
		if got := d.Read(); got != 0 {
			t.Fatalf("未接続のポートが %d を返した", got)
		}
	}
}

// TestStateRoundtrip はシフトレジスタと strobe が往復することを確かめる。
func TestStateRoundtrip(t *testing.T) {
	src := &fakeSource{}
	src.buttons[0] = ButtonA | ButtonSelect
	a := NewStandardController(src, 0)
	a.Strobe(1)
	a.Strobe(0)
	a.Read()
	a.Read()

	w := state.NewWriter()
	a.SaveState(w)

	b := NewStandardController(src, 0)
	if err := b.LoadState(state.NewReader(w.Data())); err != nil {
		t.Fatal(err)
	}

	for i := range 8 {
		x, y := a.Read(), b.Read()
		if x != y {
			t.Fatalf("%d 回目の読み出しが一致しない: %d と %d", i+1, x, y)
		}
	}
}
