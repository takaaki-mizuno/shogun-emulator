package bus

import (
	"fmt"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/input"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// SetPort は port（0 または 1）に接続するデバイスを差し替える。
func (b *Bus) SetPort(port int, d input.Device) {
	if port < 0 || port >= input.PortCount || d == nil {
		return
	}
	b.ports[port] = d
}

// Port は port に接続されているデバイスを返す。
func (b *Bus) Port(port int) input.Device {
	if port < 0 || port >= input.PortCount {
		return input.NoDevice{}
	}
	return b.ports[port]
}

// readController は $4016 / $4017 の読み出しを合成する。
//
// 下位 5 bit をデバイスから取り、上位 3 bit をオープンバスから取る。
// 実機ではポートのデータ線が 5 本しかなく、残りの 3 bit はバスに残った
// 値がそのまま読まれる。この 3 bit を期待するプログラムがある。
func (b *Bus) readController(port int) uint8 {
	v := b.ports[port].Read() & 0x1F
	return v | b.openBus&0xE0
}

// peekController は副作用を起こさずに $4016 / $4017 の値を組み立てる。
func (b *Bus) peekController(port int) uint8 {
	return b.ports[port].Peek()&0x1F | b.openBus&0xE0
}

// writeController は $4016 への書き込みを両方のポートへ配る。
//
// 実機では $4016 の書き込みが両方のポートの OUT0 線を駆動する。
// ポート 2 へ別に書き込む方法はない。
func (b *Bus) writeController(v uint8) {
	b.controllerLatch = v & 0x07
	for _, d := range b.ports {
		d.Strobe(v)
	}
}

// saveInput は入力の状態を書く。
func (b *Bus) saveInput(w *state.Writer) {
	end := w.Section("input")
	w.U8(b.controllerLatch)
	for _, d := range b.ports {
		w.String(d.Kind())
		d.SaveState(w)
	}
	end()
}

// loadInput は入力の状態を読む。
//
// デバイス種別が違うときはエラーにする。種別ごとに書き出す内容が
// 異なるため、読み違えると以降の読み出しがすべてずれる。
func (b *Bus) loadInput(r *state.Reader) error {
	end := r.RequireSection("input")
	b.controllerLatch = r.U8()
	for i, d := range b.ports {
		kind := r.String()
		if err := r.Err(); err != nil {
			return err
		}
		if kind != d.Kind() {
			r.Fail(fmt.Errorf("bus: ポート %d のデバイス種別が違う（ファイル %q、現在 %q）",
				i+1, kind, d.Kind()))
			return r.Err()
		}
		if err := d.LoadState(r); err != nil {
			return err
		}
	}
	end()
	return r.Err()
}
