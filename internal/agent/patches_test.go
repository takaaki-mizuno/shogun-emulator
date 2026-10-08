package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/nes/cart"
)

// TestHeadlessInstanceDoesNotTouchPatches は headless の Instance が利用者の
// オーバーレイを読むが書き戻さないことを確かめる（設計書 14 編）。
//
// 変更の無い有効なオーバーレイは、Instance を閉じるときの保存で消される。
// 利用者の保存先をそのまま使うと、利用者のファイルが消える。
func TestHeadlessInstanceDoesNotTouchPatches(t *testing.T) {
	h := newTestHost(t, KindServe)
	rom := writeTestROM(t)
	data, err := os.ReadFile(rom)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := cart.LoadROM(data)
	if err != nil {
		t.Fatal(err)
	}
	userDir := h.opts.EmuConfig.Dirs.PatchesDir(h.opts.EmuConfig.PatchesDir)
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := cart.ROMKeyString(parsed.Hash[:]) + ".json"
	overlay := filepath.Join(userDir, name)
	want := []byte(`{"version":1,"enabled":true,"prg":[],"chr":[]}` + "\n")
	if err := os.WriteFile(overlay, want, 0o644); err != nil {
		t.Fatal(err)
	}

	c := h.Connect()
	mustCall(t, h, c, "instance.create", map[string]any{"rom": rom}, nil)
	// 利用者のオーバーレイの写しを読んでいる。
	if _, err := os.Stat(filepath.Join(h.saveDir, workPatchesDir, name)); err != nil {
		t.Errorf("Instance の保存先にオーバーレイの写しが無い: %v", err)
	}
	mustCall(t, h, c, "instance.close", nil, nil)

	got, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("利用者のオーバーレイが消えた: %v", err)
	}
	if string(got) != string(want) {
		t.Error("利用者のオーバーレイの内容が変わった")
	}
	entries, err := os.ReadDir(userDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("利用者の保存先のファイル数 = %d、期待 1", len(entries))
	}
}
