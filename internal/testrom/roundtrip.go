package testrom

import (
	"bytes"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// StatefulMachine はセーブステートを扱えるエミュレータ。
type StatefulMachine interface {
	Machine
	// SaveState は現在の状態を直列化して返す。
	SaveState() []byte
	// LoadState は直列化された状態を復元する。
	LoadState([]byte) error
}

// Factory は ROM から電源投入直後のエミュレータを作る。
//
// 往復テストは同じ ROM から 2 台を作って比べる。作り方をテスト側から
// 渡すことで、このパッケージがエミュレーションコアを参照せずに済む。
type Factory func(t testing.TB, romPath string) StatefulMachine

// Roundtrip はセーブステートの保存漏れを検出する。
//
//	経路 1: warmup フレーム進め、保存し、さらに compare フレーム進める
//	経路 2: warmup フレーム進めた状態を復元し、compare フレーム進める
//
// 2 つの結果を直列化したバイト列で比べる。構造体を直接比べないのは、
// 直列化に含めていないフィールドを検出する必要があるためである。直列化に
// 含めていないフィールドが挙動に影響する場合、2 つの経路の状態が食い違う。
func Roundtrip(t testing.TB, newMachine Factory, romPath string, warmup, compare int) {
	t.Helper()

	a := newMachine(t, romPath)
	RunFrames(a, warmup)
	blob := a.SaveState()

	// 経路 1: そのまま進める
	RunFrames(a, compare)
	want := a.SaveState()

	// 経路 2: 復元してから進める
	b := newMachine(t, romPath)
	if err := b.LoadState(blob); err != nil {
		t.Fatalf("ステートを復元できない: %v", err)
	}
	RunFrames(b, compare)
	got := b.SaveState()

	if !bytes.Equal(want, got) {
		t.Fatalf("warmup %d、compare %d で状態が一致しない。最初の差分: %s",
			warmup, compare, state.FirstDiff(want, got))
	}
}

// RoundtripEveryFrame は frames フレーム分、毎フレームで往復を検証する。
//
// 1 フレームだけ進めて比べるため、差分が現れた原因のフレームが特定できる。
func RoundtripEveryFrame(t *testing.T, newMachine Factory, romPath string, frames int) {
	t.Helper()
	for frame := range frames {
		if t.Failed() {
			return
		}
		Roundtrip(t, newMachine, romPath, frame, 1)
	}
}

// RoundtripFrames は往復テストで進めるフレーム数を返す。
//
// -short を指定したときに減らすのは、全フレーム往復テストの実行時間が
// 長いためである。日常の実行では短く、CI では全量を回す。
func RoundtripFrames(short bool) int {
	if short {
		return 60
	}
	return 600
}
