package emu

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
)

// overlayFileVersion はオーバーレイのファイルの形式のバージョン。
const overlayFileVersion = 1

// overlayFile は patches/<rom-hash>.json の内容（設計書 06 編 §6.8）。
//
// 変更はオフセットの昇順に並べる。同じ内容から同じファイルができる。
type overlayFile struct {
	Version int          `json:"version"`
	Enabled bool         `json:"enabled"`
	PRG     []patchEntry `json:"prg"`
	CHR     []patchEntry `json:"chr"`
}

// patchEntry は 1 バイト分の変更。
type patchEntry struct {
	Offset uint32 `json:"offset"`
	Value  uint8  `json:"value"`
}

// OverlayStatus はオーバーレイの状態。UI の表示に使う。
type OverlayStatus struct {
	Enabled bool
	// PRG と CHR は変更したバイト数。
	PRG, CHR int
}

// patchesPath は ROM のオーバーレイの保存先を返す。
func (e *Emulator) patchesPath(rom *cart.ROM) string {
	dir, err := config.PatchesDir(e.cfg.PatchesDir)
	if err != nil {
		dir = config.PatchesDirName
	}
	return filepath.Join(dir, cart.ROMKeyString(rom.Hash[:])+".json")
}

// loadOverlay はファイルからオーバーレイを読み込んで ROM へ当てる。
// ファイルが無いときは何もしない。
func loadOverlay(path string, rom *cart.ROM) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var f overlayFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("emu: オーバーレイ %s を読めない: %w", filepath.Base(path), err)
	}
	if f.Version != overlayFileVersion {
		return fmt.Errorf("emu: オーバーレイ %s の形式のバージョン %d を扱えない", filepath.Base(path), f.Version)
	}
	return rom.Overlay().Load(toPatches(f.PRG), toPatches(f.CHR), f.Enabled)
}

// saveOverlay はオーバーレイをファイルへ書き出す。
//
// 変更が無く有効のときはファイルを消す。何も編集していない ROM の
// ためにファイルを残さない。
func saveOverlay(path string, rom *cart.ROM) error {
	o := rom.Overlay()
	if o.Empty() && o.Enabled() {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	prg, chr := o.Patches()
	f := overlayFile{Version: overlayFileVersion, Enabled: o.Enabled(), PRG: fromPatches(prg), CHR: fromPatches(chr)}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

func toPatches(list []patchEntry) []cart.Patch {
	out := make([]cart.Patch, 0, len(list))
	for _, p := range list {
		out = append(out, cart.Patch{Offset: p.Offset, Value: p.Value})
	}
	return out
}

func fromPatches(list []cart.Patch) []patchEntry {
	out := make([]patchEntry, 0, len(list))
	for _, p := range list {
		out = append(out, patchEntry{Offset: p.Offset, Value: p.Value})
	}
	return out
}

// storeOverlay は読み込んでいる ROM のオーバーレイを保存する。
// エミュレーションゴルーチンから呼ぶ。
func (e *Emulator) storeOverlay() {
	if e.machine == nil {
		return
	}
	if err := saveOverlay(e.patchesPath(e.machine.ROM), e.machine.ROM); err != nil {
		e.notifyError(err)
	}
}

// SaveOverlay はオーバーレイを保存する。UI が編集の操作を終えるたびに呼ぶ。
func (e *Emulator) SaveOverlay() {
	e.WithMachine(func(*nes.NES) { e.storeOverlay() })
}

// withEditableMachine はムービーを扱っていないときだけ fn を実行する。
//
// オーバーレイの変更はエミュレーション結果を変える。記録した入力と
// 結果の対応を崩さないため、ムービーの記録中と再生中は断る。
func (e *Emulator) withEditableMachine(fn func(m *nes.NES) error) error {
	var err error
	ok := e.WithMachine(func(m *nes.NES) {
		switch {
		case m == nil:
			err = errNoROM
		case e.recorder != nil || e.player != nil:
			err = errMovieEdit
		default:
			err = fn(m)
		}
	})
	if !ok {
		return errors.New("emu: エミュレーションが停止している")
	}
	return err
}

// SetOverlayEnabled はオーバーレイの有効・無効を切り替えて保存する。
func (e *Emulator) SetOverlayEnabled(on bool) error {
	return e.withEditableMachine(func(m *nes.NES) error {
		m.ROM.Overlay().SetEnabled(on)
		e.storeOverlay()
		return nil
	})
}

// ClearOverlay はオーバーレイの変更をすべて捨てて保存する。
func (e *Emulator) ClearOverlay() error {
	return e.withEditableMachine(func(m *nes.NES) error {
		m.ROM.Overlay().Clear()
		e.storeOverlay()
		return nil
	})
}

// OverlayStatus はオーバーレイの状態を返す。
func (e *Emulator) OverlayStatus() OverlayStatus {
	var st OverlayStatus
	e.WithMachine(func(m *nes.NES) {
		if m == nil {
			return
		}
		o := m.ROM.Overlay()
		prg, chr := o.Patches()
		st = OverlayStatus{Enabled: o.Enabled(), PRG: len(prg), CHR: len(chr)}
	})
	return st
}

// SetAPUMute はミュートする APU のチャンネルをビットで指定する。
//
// 出力段の設定であり、エミュレーション状態を変えない（設計書 05 編 §5.9）。
// ROM を読み込み直しても保つ。
func (e *Emulator) SetAPUMute(mask uint8) {
	e.WithMachine(func(m *nes.NES) {
		e.apuMute = mask
		if m != nil {
			m.APU.SetMute(mask)
		}
	})
}
