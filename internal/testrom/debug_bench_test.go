package testrom_test

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// BenchmarkFrameWithDebugger はデバッガの使い方ごとの 1 フレームの実行時間を
// 測る（フェーズ 10 計画 §3.15）。60 fps を保つには 1 フレーム 16.6 ms に
// 収める必要がある。
//
//	go test -run '^$' -bench FrameWithDebugger ./internal/testrom
func BenchmarkFrameWithDebugger(b *testing.B) {
	const rom = "dpcmletterbox/dpcmletterbox.nes"
	cases := []struct {
		name  string
		setup func(d *debug.Debugger)
	}{
		{"なし", nil},
		{"ビューア3つ", func(d *debug.Debugger) {
			// CPU デバッガ・メモリビューア・スナップショットを使うビューア。
			d.SetFeatures(debug.Features{CPUView: true, ChangeTracking: true})
			d.AcquireSnapshots(-1)
		}},
		{"PPUビューア5つ", func(d *debug.Debugger) {
			// パターンテーブル（行 30）・ネームテーブル・スプライト・パレット
			// （フレーム末）・APU の 5 つを開いた状態。フレーム末の購読は共有される。
			d.AcquireSnapshots(-1)
			d.AcquireSnapshots(30)
		}},
		{"ブレークポイント10個", func(d *debug.Debugger) {
			for i := range 10 {
				a := uint16(0x0700 + i)
				kind := debug.BreakExec
				if i%2 == 1 {
					kind = debug.BreakWrite
				}
				if kind == debug.BreakExec {
					a = uint16(0xFF00 + i)
				}
				d.AddBreakpoint(debug.Breakpoint{Kind: kind, AddrStart: a, AddrEnd: a, Enabled: true})
			}
		}},
		{"トレース", func(d *debug.Debugger) {
			d.SetFeatures(debug.Features{Tracing: true})
		}},
		// Agent Interface の解析（計画フェーズ 20 §3.11）。項目ごとに分ける。
		{"Diagnostic既定", func(d *debug.Debugger) { d.SetDiagConfig(debug.DefaultDiagConfig()) }},
		{"Diagnostic全項目", func(d *debug.Debugger) { d.SetDiagConfig(allDiagnostics()) }},
		{"トレース(bus)", func(d *debug.Debugger) {
			d.SetAgentTrace(debug.AgentTrace{Enabled: true, RingSize: debug.DefaultTraceRingSize, BusRingSize: 2_000_000})
		}},
		{"プロファイル", func(d *debug.Debugger) { d.StartProfile(debug.ProfileOptions{}) }},
		{"解析すべて", func(d *debug.Debugger) {
			d.SetDiagConfig(allDiagnostics())
			d.SetAgentTrace(debug.AgentTrace{Enabled: true, RingSize: debug.DefaultTraceRingSize, BusRingSize: 2_000_000})
			d.StartProfile(debug.ProfileOptions{})
		}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			testrom.RequireROM(b, rom)
			n, _ := newMovieMachine(b, rom, nes.Deterministic())
			if c.setup != nil {
				d := debug.New(debug.NewLogger(debug.DefaultCategories, nil), debug.DefaultTraceRingSize, 0)
				d.Attach(n, debug.NewSymbols())
				c.setup(d)
			}
			// 最初の数フレームは初期化の処理で重さが違うため除く。
			for range 30 {
				n.RunFrame()
			}
			b.ResetTimer()
			for b.Loop() {
				n.RunFrame()
			}
			b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/1000, "ms/frame")
		})
	}
}
