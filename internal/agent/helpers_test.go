package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// writeTestROM は検証に使う NROM の ROM を書く。
//
//	$8000  INC $00
//	$8002  LDA $00
//	$8004  STA $0300
//	$8007  JMP $8000
func writeTestROM(t *testing.T) string {
	t.Helper()
	prg := make([]uint8, 32*1024)
	copy(prg, []uint8{
		0xE6, 0x00, // INC $00
		0xA5, 0x00, // LDA $00
		0x8D, 0x00, 0x03, // STA $0300
		0x4C, 0x00, 0x80, // JMP $8000
	})
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	path := filepath.Join(t.TempDir(), "agent.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// newTestHost は一時ディレクトリを保存先にした headless の Host を作る。
func newTestHost(t *testing.T, kind Kind) *Host {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	h := NewHost(Options{
		Kind: kind, Server: "shogun test", MaxInstances: 4,
		EmuConfig: emu.Config{
			Emulation: cfg.Emulation, Input: cfg.Input, Audio: cfg.Audio, Paths: cfg.Paths,
			State: cfg.State, Movie: cfg.Movie, Debug: cfg.Debug,
			Dirs: config.Paths{Config: dir, Data: dir, Cache: dir, Logs: dir, Screenshots: dir},
		},
	})
	t.Cleanup(h.Close)
	return h
}

// call は Agent Command を実行し、結果を v へ読む。
func call(t *testing.T, h *Host, c *Conn, method string, params any, v any) *Error {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		raw = data
	}
	res, aerr := h.Dispatch(context.Background(), c, method, raw)
	if aerr != nil {
		return aerr
	}
	if v != nil {
		data, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

// mustCall は Agent Command を実行し、誤りなら止める。
func mustCall(t *testing.T, h *Host, c *Conn, method string, params any, v any) {
	t.Helper()
	if err := call(t, h, c, method, params, v); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

// stateHash は Instance の状態のハッシュを返す。
func stateHash(t *testing.T, inst *Instance) [8]uint8 {
	t.Helper()
	var h [8]uint8
	if !inst.Emu.WithMachine(func(n *nes.NES) { h = n.StateHash() }) {
		t.Fatal("エミュレーションが止まっている")
	}
	return h
}

// stepFrames は Instance を n フレーム進める。
func stepFrames(t *testing.T, inst *Instance, n int) {
	t.Helper()
	if _, err := inst.Emu.StepAndWait(context.Background(), emu.StepFrame, n); err != nil {
		t.Fatal(err)
	}
}
