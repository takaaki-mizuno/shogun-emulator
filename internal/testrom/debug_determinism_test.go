package testrom_test

import (
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/testrom"
)

// neverTrue は評価しても成り立たない条件式。ブレークポイントの判定の
// 経路を通しつつ、再生を止めないために使う。
const neverTrue = "A == $100"

// attachFullDebugger はデバッガのすべての機能を有効にして本体へつなぐ。
//
// ブレークポイントは各種類を置き、条件式で止まらないようにする。
// ログは全カテゴリを有効にし、ビューア向けの保持まで通す。
func attachFullDebugger(t *testing.T, n *nes.NES) *debug.Debugger {
	t.Helper()
	cats, err := debug.ParseCategories(debug.CategoryNames())
	if err != nil {
		t.Fatal(err)
	}
	d := debug.New(debug.NewLogger(cats, nil), 1<<16, 0)
	d.Attach(n, debug.NewSymbols())
	add := func(b debug.Breakpoint) {
		b.Enabled = true
		id := d.AddBreakpoint(b)
		if err := d.SetBreakpointCondition(id, neverTrue); err != nil {
			t.Fatal(err)
		}
	}
	add(debug.Breakpoint{Kind: debug.BreakExec, AddrStart: 0x8000, AddrEnd: 0xFFFF})
	add(debug.Breakpoint{Kind: debug.BreakRead, AddrStart: 0x0000, AddrEnd: 0xFFFF})
	add(debug.Breakpoint{Kind: debug.BreakWrite, AddrStart: 0x0000, AddrEnd: 0xFFFF})
	add(debug.Breakpoint{Kind: debug.BreakPPUPosition, Scanline: 100, Dot: 50})
	for e := debug.EventNMI; e <= debug.EventMMC3IRQReloadWithoutClocks; e++ {
		add(debug.Breakpoint{Kind: debug.BreakEvent, Event: e})
	}
	d.SetFeatures(debug.Features{Tracing: true, CPUView: true, ChangeTracking: true})
	// ビューア 5 つがフレーム末と任意のスキャンラインを購読した状態にする。
	d.AcquireSnapshots(-1)
	d.AcquireSnapshots(100)
	d.AcquireSnapshots(200)
	// APU 状態ビューアでミュートした状態にする。出力段だけに効く。
	n.APU.SetMute(0x1F)
	return d
}

// TestMovieUnchangedByDebugger はデバッガのブレークポイント・トレース・
// 変更追跡・スナップショット・ログを有効にしても、ムービーの再生結果が
// 変わらないことを確かめる（設計書 09 編 §9.1）。
//
// フックはエミュレーションの状態を読むだけであり、書き換えないことを
// ハッシュの一致で確かめる。
func TestMovieUnchangedByDebugger(t *testing.T) {
	for _, tc := range determinismCases {
		t.Run(tc.rom, func(t *testing.T) {
			testrom.RequireROM(t, tc.rom)
			m := loadGoldenMovie(t, tc.rom, tc.movie, tc.frames)
			plain := replayMovie(t, tc.rom, m)

			var d *debug.Debugger
			withDebugger := replayMovieWith(t, tc.rom, m, func(n *nes.NES) {
				d = attachFullDebugger(t, n)
			})
			if plain != withDebugger {
				t.Errorf("デバッガを有効にすると結果が変わる（%x と %x）", plain, withDebugger)
			}
			if d.HasHit() {
				info, _ := d.TakeHit()
				t.Errorf("成り立たない条件のブレークポイントで止まった: %s", info.Reason)
			}
			if d.Tracer().Count() == 0 {
				t.Error("トレースが記録されていない")
			}
		})
	}
}

// TestSprite0HitEventBreak はスプライト 0 ヒットのイベントブレークポイントが
// フラグの立った命令の後で止めることを確かめる（設計書 09 編 §9.6）。
func TestSprite0HitEventBreak(t *testing.T) {
	const rom = "sprite_hit_tests_2005.10.05/01.basics.nes"
	testrom.RequireROM(t, rom)
	n, _ := newMovieMachine(t, rom, nes.Deterministic())
	d := debug.New(debug.NewLogger(0, nil), 1024, 0)
	d.Attach(n, debug.NewSymbols())
	d.AddBreakpoint(debug.Breakpoint{Kind: debug.BreakEvent, Event: debug.EventSprite0Hit, Enabled: true})

	// 10 秒分を上限とする。
	limit := n.Cycles() + 10*1789773
	for !d.HasHit() {
		if n.Cycles() > limit {
			t.Fatal("スプライト 0 ヒットで止まらなかった")
		}
		n.StepInstruction()
	}
	info, _ := d.TakeHit()
	if n.Bus.Peek(0x2002)&0x40 == 0 {
		t.Errorf("止まった位置でスプライト 0 ヒットのフラグが立っていない（%s）", info.Reason)
	}
	if info.Scanline < 0 || info.Scanline >= 240 {
		t.Errorf("止まった行 = %d, 可視領域の外", info.Scanline)
	}
}

// TestMemoryViewerReadHasNoSideEffect はメモリビューアの読み出しが $2002 の
// VBlank フラグを消さないことを確かめる（設計書 09 編 §9.4.5）。
func TestMemoryViewerReadHasNoSideEffect(t *testing.T) {
	const rom = "other/nestest.nes"
	testrom.RequireROM(t, rom)
	n, _ := newMovieMachine(t, rom, nes.Deterministic())

	// VBlank フラグが立つまで進める。ROM 自身が $2002 を読んで消す前の
	// 命令境界で止まる。
	limit := n.Cycles() + 1789773
	for n.Bus.Peek(0x2002)&0x80 == 0 {
		if n.Cycles() > limit {
			t.Fatal("VBlank フラグが立たない")
		}
		n.StepInstruction()
	}
	var b [8]uint8
	for range 3 {
		debug.ReadMemory(n, debug.SpaceCPU, 0x2000, b[:])
		if b[2]&0x80 == 0 {
			t.Fatal("読み出しで VBlank フラグが消えた")
		}
	}
	if n.Bus.Peek(0x2002)&0x80 == 0 {
		t.Error("読み出しの後に VBlank フラグが消えている")
	}
}

// replayInputs はムービーの入力だけを与えて再生し、最後の状態のハッシュを返す。
// チェックサムは照合しない。
func replayInputs(t *testing.T, romName string, m *movie.Movie, setup func(n *nes.NES)) [8]uint8 {
	t.Helper()
	n, pad := newMovieMachine(t, romName, m.Header.Init)
	if setup != nil {
		setup(n)
	}
	p := movie.NewPlayer(m)
	for {
		f, ok := p.BeginFrame()
		if !ok {
			break
		}
		pad.buttons = f.Buttons
		n.RunFrame()
	}
	return n.StateHash()
}

// TestMovieChangesWithOverlay はオーバーレイを有効にするとムービーの再生結果が
// 変わり、無効にすると元に戻ることを確かめる（フェーズ 11 計画 §3.7）。
// オーバーレイは ROM の内容を変えるため、結果が変わるのが正しい挙動である。
func TestMovieChangesWithOverlay(t *testing.T) {
	const rom = "other/nestest.nes"
	testrom.RequireROM(t, rom)
	m := loadGoldenMovie(t, rom, "nestest.movie", 240)
	plain := replayInputs(t, rom, m, nil)

	patch := func(enabled bool) func(n *nes.NES) {
		return func(n *nes.NES) {
			// リセットベクタの下位バイトを変え、電源を入れ直す。
			o := n.ROM.Overlay()
			last := len(n.ROM.PRG) - 4
			if err := o.SetPRG(last, n.ROM.PRG[last]+3); err != nil {
				t.Fatal(err)
			}
			o.SetEnabled(enabled)
			n.PowerOn(m.Header.Init)
		}
	}
	patched := replayInputs(t, rom, m, patch(true))
	if patched == plain {
		t.Error("オーバーレイを有効にしても結果が変わらない")
	}
	disabled := replayInputs(t, rom, m, patch(false))
	if disabled != plain {
		t.Errorf("オーバーレイを無効にしても結果が戻らない（%x と %x）", disabled, plain)
	}
}

// TestSnapshotFollowsMidFramePalette は画面の途中でパレットを書き換える ROM で、
// 取得位置によってパレットビューアの内容が変わることを確かめる
// （フェーズ 11 計画 §3.8・§3.10）。
func TestSnapshotFollowsMidFramePalette(t *testing.T) {
	const rom = "full_palette/full_palette.nes"
	testrom.RequireROM(t, rom)
	n, _ := newMovieMachine(t, rom, nes.Deterministic())
	d := debug.New(debug.NewLogger(0, nil), 16, 0)
	d.Attach(n, debug.NewSymbols())
	sets := map[int]*debug.SnapshotSet{}
	for _, line := range []int{20, 120, 220} {
		sets[line] = d.AcquireSnapshots(line)
	}
	for range 30 {
		n.RunFrame()
	}
	seen := map[[32]uint8]bool{}
	for line, set := range sets {
		var s debug.Snapshot
		if !set.Latest(&s) || s.Scanline != line {
			t.Fatalf("行 %d のスナップショットが無い", line)
		}
		seen[s.Palette] = true
	}
	if len(seen) < 2 {
		t.Error("取得位置を変えてもパレットが同じ")
	}
}
