package emu

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/state"
)

// stateExt はセーブステートの拡張子。
const stateExt = ".state"

// SlotCount はスロットの数。
const SlotCount = 10

// SlotInfo はスロット 1 つの情報。一覧の表示に使う。
type SlotInfo struct {
	// Index はスロット番号。
	Index int
	// Exists はステートが保存されているかを表す。
	Exists bool
	// SavedAt は保存した時刻。
	SavedAt time.Time
	// Frames は保存した時点の累積フレーム数。
	Frames uint64
	// Screenshot は保存時の画面の PNG。持たないとき長さ 0。
	Screenshot []uint8
	// Err は読めなかった理由。
	Err error
}

// Slot は選択中のスロット番号を返す。
func (e *Emulator) Slot() int {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	return e.status.Slot
}

// SetSlot は選択中のスロットを変える。範囲外の値は無視する。
func (e *Emulator) SetSlot(n int) {
	if n < 0 || n >= SlotCount {
		return
	}
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Slot = n
}

// NextSlot は次のスロットを選ぶ。末尾の次は先頭へ戻る。
func (e *Emulator) NextSlot() int {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Slot = (e.status.Slot + 1) % SlotCount
	return e.status.Slot
}

// PrevSlot は前のスロットを選ぶ。先頭の前は末尾へ戻る。
func (e *Emulator) PrevSlot() int {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	e.status.Slot = (e.status.Slot + SlotCount - 1) % SlotCount
	return e.status.Slot
}

// SaveSlot は選択中のスロットへ保存する。
func (e *Emulator) SaveSlot(n int) error {
	path, err := e.slotPath(n)
	if err != nil {
		return err
	}
	return e.SaveToFile(path)
}

// LoadSlot はスロットから復元する。
func (e *Emulator) LoadSlot(n int) error {
	path, err := e.slotPath(n)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("emu: スロット %d は空である", n)
	}
	return e.LoadFromFile(path)
}

// SaveToFile は状態をファイルへ保存する。
//
// スクリーンショットを添えるかは設定 state.saveScreenshot で決まる。
func (e *Emulator) SaveToFile(path string) error {
	data, pc, err := e.saveState(e.cfg.State.SaveScreenshot)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	// 保存した位置を添える。命令の途中で要求されたときは、命令境界まで
	// 進めてから保存している（設計書 08 編 §8.3.2）。
	e.notifyMessage(fmt.Sprintf("%s へ命令境界（$%04X）で保存しました", filepath.Base(path), pc))
	return nil
}

// LoadFromFile はファイルから状態を復元する。
func (e *Emulator) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := e.LoadState(data); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	e.notifyMessage(fmt.Sprintf("%s から復元しました", filepath.Base(path)))
	return nil
}

// SlotInfos はスロットの一覧を返す。
func (e *Emulator) SlotInfos() ([]SlotInfo, error) {
	dir, err := e.slotDir()
	if err != nil {
		return nil, err
	}
	out := make([]SlotInfo, 0, SlotCount)
	for i := range SlotCount {
		out = append(out, readSlot(dir, i))
	}
	return out, nil
}

// readSlot は 1 つのスロットの情報を読む。
//
// 本体を組み立てずにヘッダだけを読む。一覧の表示のために ROM を
// 読み込み直す必要はない。
func readSlot(dir string, index int) SlotInfo {
	info := SlotInfo{Index: index}
	path := slotFile(dir, index)
	st, err := os.Stat(path)
	if err != nil {
		return info
	}
	info.Exists = true
	info.SavedAt = st.ModTime()

	data, err := os.ReadFile(path)
	if err != nil {
		info.Err = err
		return info
	}
	h, err := state.ReadHeader(data)
	if err != nil {
		info.Err = err
		return info
	}
	info.Frames = h.Frames
	info.Screenshot = h.Screenshot
	return info
}

// slotDir は現在の ROM のスロットを置くディレクトリを返す。
func (e *Emulator) slotDir() (string, error) {
	key := e.ROMKey()
	if key == "" {
		return "", errNoROM
	}
	return filepath.Join(e.cfg.Dirs.StateDir(e.cfg.Paths.StateDir), key), nil
}

// slotPath はスロットのファイルのパスを返す。
func (e *Emulator) slotPath(n int) (string, error) {
	if n < 0 || n >= SlotCount {
		return "", fmt.Errorf("emu: スロット番号が範囲外である（%d）", n)
	}
	dir, err := e.slotDir()
	if err != nil {
		return "", err
	}
	return slotFile(dir, n), nil
}

// slotFile はスロットのファイル名を組み立てる。
func slotFile(dir string, n int) string {
	return filepath.Join(dir, fmt.Sprintf("%d%s", n, stateExt))
}

// ROMKey は読み込んでいる ROM のハッシュから作った識別子を返す。
//
// 読み込んでいないとき空文字列を返す。
func (e *Emulator) ROMKey() string {
	e.statusMu.Lock()
	defer e.statusMu.Unlock()
	return e.status.ROMKey
}
