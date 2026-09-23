package testrom

import (
	"fmt"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// fakeStateful は往復テストの枠組みを確かめるための模擬のエミュレータ。
//
// leak を真にすると、保存していないフィールドが挙動に影響する状態を作る。
// 往復テストがそれを検出できることを確かめるために使う。
type fakeStateful struct {
	fakeMachine
	counter uint32
	// hidden は直列化に含めないフィールド。
	hidden uint32
	leak   bool
}

func newFakeStateful(leak bool) *fakeStateful {
	f := &fakeStateful{leak: leak}
	f.mem = map[uint16]uint8{}
	return f
}

func (f *fakeStateful) RunFrame() {
	f.counter += 1 + f.hidden
	if f.leak {
		// 1 フレーム目を進めた時点で hidden が立つ。復元した側では
		// hidden が 0 のままになり、以降の進み方が変わる。
		f.hidden = 1
	}
	f.frame++
}

func (f *fakeStateful) SaveState() []byte {
	w := state.NewWriter()
	end := w.Section("fake")
	w.U32(f.counter)
	end()
	return w.Data()
}

func (f *fakeStateful) LoadState(b []byte) error {
	r := state.NewReader(b)
	end := r.RequireSection("fake")
	f.counter = r.U32()
	end()
	return r.Err()
}

// fatalTB は Fatalf の呼び出しを記録する testing.TB。
type fatalTB struct {
	testing.TB
	messages []string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

// TestRoundtripPasses は保存漏れが無いとき往復テストが通ることを確かめる。
func TestRoundtripPasses(t *testing.T) {
	fake := &fatalTB{TB: t}
	newMachine := func(testing.TB, string) StatefulMachine { return newFakeStateful(false) }
	Roundtrip(fake, newMachine, "dummy.nes", 5, 5)
	if len(fake.messages) != 0 {
		t.Errorf("保存漏れが無いのに失敗した: %v", fake.messages)
	}
}

// TestRoundtripDetectsLeak は保存漏れを検出できることを確かめる。
// 往復テスト自体が働いていることの確認である。
func TestRoundtripDetectsLeak(t *testing.T) {
	fake := &fatalTB{TB: t}
	newMachine := func(testing.TB, string) StatefulMachine { return newFakeStateful(true) }
	Roundtrip(fake, newMachine, "dummy.nes", 5, 5)
	if len(fake.messages) == 0 {
		t.Fatal("保存漏れを検出できなかった")
	}
	if !strings.Contains(fake.messages[0], "fake") {
		t.Errorf("差分の位置がメッセージに出ていない: %s", fake.messages[0])
	}
}

// TestRoundtripFrames は -short のときフレーム数が減ることを確かめる。
func TestRoundtripFrames(t *testing.T) {
	if RoundtripFrames(true) >= RoundtripFrames(false) {
		t.Error("-short でフレーム数が減っていない")
	}
}
