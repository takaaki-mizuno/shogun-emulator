package emu

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
)

// batteryFlushDelay は最後の変化から書き出すまでの時間。
//
// 書き込みのたびにファイルへ書くと、セーブの多いゲームでディスクへの
// 書き込みが頻発する。逆に長く待つと、強制終了でセーブが失われる。
const batteryFlushDelay = 3 * time.Second

// batteryExt はセーブデータの拡張子。
const batteryExt = ".sav"

// battery はカートリッジの不揮発メモリをファイルへ保存する。
//
// 変化の検出を内容の比較で行う。カートリッジに「書かれた」ことを
// 知らせる仕組みを持たせないのは、internal/nes に時刻を持ち込まず、
// バスの書き込み経路に保存のための分岐を入れないためである。
// 1 フレームに 1 回 8 KiB から 32 KiB を比較する費用は十分に小さい。
type battery struct {
	path string
	// ram はカートリッジの不揮発メモリ。カートリッジと同じ配列を指す。
	ram []uint8
	// last は直前に比較した内容。
	last []uint8
	// dirty は書き出していない変化があることを表す。
	dirty bool
	// changedAt は最後に変化を見つけた時刻。
	changedAt time.Time
	// now は現在時刻を返す。テストで差し替える。
	now func() time.Time
}

// newBattery は不揮発メモリを持つカートリッジのための保存先を作る。
//
// 不揮発メモリを持たないときは nil を返す。ファイルがあれば読み込む。
// 無いときはカートリッジの初期値（$00 で埋めた状態）のままにする。
func newBattery(dir string, hash []uint8, c cart.Cartridge) (*battery, error) {
	ram := c.BatteryRAM()
	if len(ram) == 0 {
		return nil, nil
	}
	b := &battery{
		path: filepath.Join(dir, cart.ROMKeyString(hash)+batteryExt),
		ram:  ram,
		now:  time.Now,
	}
	data, err := os.ReadFile(b.path)
	switch {
	case os.IsNotExist(err):
		// 初回。カートリッジ側が $00 で埋めている。
	case err != nil:
		return nil, fmt.Errorf("emu: セーブデータを読めない: %w", err)
	default:
		if err := c.SetBatteryRAM(data); err != nil {
			return nil, err
		}
	}
	b.last = bytes.Clone(b.ram)
	return b, nil
}

// poll は内容の変化を調べ、落ち着いたところで書き出す。
//
// フレームの完成ごとに呼ぶ。
func (b *battery) poll() error {
	if b == nil {
		return nil
	}
	if b.detectChange() {
		return nil
	}
	if !b.dirty || b.now().Sub(b.changedAt) < batteryFlushDelay {
		return nil
	}
	return b.flush()
}

// detectChange は前回の比較から内容が変わったかを調べ、変わっていれば
// 記録する。
func (b *battery) detectChange() bool {
	if bytes.Equal(b.ram, b.last) {
		return false
	}
	copy(b.last, b.ram)
	b.dirty = true
	b.changedAt = b.now()
	return true
}

// flush は内容をファイルへ書き出す。
//
// 一時ファイルへ書いてから rename する。書き込みの途中で電源が切れても、
// 前回の内容が残る。
func (b *battery) flush() error {
	if b == nil || !b.dirty {
		return nil
	}
	if err := writeFileAtomic(b.path, b.ram); err != nil {
		return err
	}
	b.dirty = false
	return nil
}

// close は残っている変化を書き出す。
//
// 最後の比較から後の変化も拾う。ROM を取り外すまで 1 フレームも
// 完成していない場合があり、比較を待つと直前の書き込みが失われる。
func (b *battery) close() error {
	if b == nil {
		return nil
	}
	b.detectChange()
	return b.flush()
}
