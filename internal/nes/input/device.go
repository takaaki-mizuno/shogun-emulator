// Package input はコントローラポートに接続するデバイスを提供する。
//
// 押下状態そのものはこのパッケージが持たない。UI スレッドと共有する
// ビットマスクは Source として外から受け取る。エミュレーションコアに
// 並行処理の仕組みを持ち込まないためである。
package input

import "github.com/takaakimizuno/shogun-emulator/internal/nes/state"

// ボタンのビット位置。$4016 から読み出される順に並ぶ。
const (
	ButtonA uint8 = 1 << iota
	ButtonB
	ButtonSelect
	ButtonStart
	ButtonUp
	ButtonDown
	ButtonLeft
	ButtonRight
)

// ButtonCount は標準コントローラのボタン数。
const ButtonCount = 8

// PortCount はコントローラポートの数。
const PortCount = 2

// Source は押下状態の供給元。
//
// エミュレーションゴルーチンから、strobe と読み出しのたびに呼ばれる。
// 実装は UI スレッドと共有する値を返すか、入力ムービーの値を返す。
type Source interface {
	// Buttons は port のボタンの押下状態を返す。bit 0 が A。
	Buttons(port int) uint8
}

// Device はコントローラポートに接続するデバイス。
type Device interface {
	// Strobe は $4016 への書き込みの下位 3 bit を受け取る。
	Strobe(v uint8)

	// Read は下位 5 bit を返す。上位 3 bit はバスがオープンバスから合成する。
	Read() uint8

	// Peek は副作用を発生させずに現在の出力を返す。
	Peek() uint8

	// Kind は保存したデバイス種別との照合に使う名前を返す。
	Kind() string

	state.Snapshotter
}

// NoSource は何も押されていないことを返す供給元。
type NoSource struct{}

// Buttons は常に 0 を返す。
func (NoSource) Buttons(int) uint8 { return 0 }

// NoDevice は何も接続されていないポート。
//
// 読み出しは常に 0 を返す。実機でもポートに何も挿さっていなければ
// データ線は low のままである。
type NoDevice struct{}

// Strobe は何もしない。
func (NoDevice) Strobe(uint8) {}

// Read は常に 0 を返す。
func (NoDevice) Read() uint8 { return 0 }

// Peek は常に 0 を返す。
func (NoDevice) Peek() uint8 { return 0 }

// Kind はデバイス種別の名前を返す。
func (NoDevice) Kind() string { return "none" }

// SaveState は書くものを持たない。
func (NoDevice) SaveState(w *state.Writer) {
	end := w.Section("none")
	end()
}

// LoadState は読むものを持たない。
func (NoDevice) LoadState(r *state.Reader) error {
	end := r.RequireSection("none")
	end()
	return r.Err()
}
