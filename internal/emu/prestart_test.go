package emu

import (
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// TestWithMachineBeforeStart は Start の前に呼んだ WithMachine がその場で
// 実行され、待ち続けないことを確かめる。GUI は Start の前に連射の設定を
// 渡すため、ここで止まると起動しない。
func TestWithMachineBeforeStart(t *testing.T) {
	e := New(testConfig())
	done := make(chan bool, 1)
	go func() {
		e.SetTurbo(0, map[string]int{"a": 15})
		e.SetStartupBreakpoints([]uint16{0x8000})
		ran := false
		e.WithMachine(func(*nes.NES) { ran = true })
		done <- ran
	}()
	select {
	case ran := <-done:
		if !ran {
			t.Error("処理が実行されていない")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start の前の WithMachine が戻らない")
	}
	if e.turbo.rates[0][0] != 15 {
		t.Errorf("連射のレートが設定されていない: %v", e.turbo.rates[0])
	}
	e.Start()
	e.Stop()
}
