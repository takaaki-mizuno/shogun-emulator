package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// diagROM は testdata/agent/diag のテスト用 ROM と .dbg を一時ディレクトリへ
// 写し、そのパスを返す。
func diagROM(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	src := "../../testdata/agent/diag"
	for _, ext := range []string{".nes", ".dbg"} {
		data, err := os.ReadFile(filepath.Join(src, name+ext))
		if err != nil {
			t.Fatalf("テスト用 ROM が無い（testdata/agent/diag/build.sh でビルドする）: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+ext), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	os.Chtimes(filepath.Join(dir, name+".nes"), now, now)
	os.Chtimes(filepath.Join(dir, name+".dbg"), now.Add(time.Second), now.Add(time.Second))
	return filepath.Join(dir, name+".nes")
}

// newDiagSession は diag の ROM の Instance を作る。
func newDiagSession(t *testing.T, name string) *agentSession {
	t.Helper()
	h := newTestHost(t, KindServe)
	s := &agentSession{t: t, h: h, conn: h.Connect()}
	var info InstanceInfo
	s.must("instance.create", map[string]any{"rom": diagROM(t, name), "deterministic": true}, &info)
	if len(info.Notes) != 0 {
		t.Fatalf("読み込みの知らせ = %v", info.Notes)
	}
	return s
}

// triggerLine は ROM のソースで trigger: の次の命令の行番号を返す。
func triggerLine(t *testing.T, name, label string) int {
	t.Helper()
	f, err := os.Open(filepath.Join("../../testdata/agent/diag/src", name+".s"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	line, found := 0, false
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if found && text != "" && !strings.HasPrefix(text, ";") {
			return line
		}
		if text == label+":" {
			found = true
		}
	}
	t.Fatalf("%s.s に %s: が無い", name, label)
	return 0
}

type diagListResult struct {
	Items  []DiagListItem    `json:"items"`
	Totals map[string]uint64 `json:"totals"`
}

// TestDiagnosticROMs は 11 項目がそれぞれのテスト ROM で検知され、位置に
// Symbol とソース行が付き、対照の ROM で 1 件も検知されないことを確かめる
// （フェーズ 20 の完了判定）。
func TestDiagnosticROMs(t *testing.T) {
	cases := []struct {
		rom, kind, label string
		pc               string // label が無いとき（RAM の実行）
	}{
		{"vram_during_render", "vram_access_during_render", "trigger", ""},
		{"warmup", "ppu_write_before_warmup", "trigger", ""},
		{"uninit_read", "uninitialized_ram_read", "trigger", ""},
		{"stack_overflow", "stack_overflow", "trigger", ""},
		{"stack_underflow", "stack_underflow", "trigger", ""},
		{"nmi_reentry", "nmi_reentry", "", ""},
		{"execute_data", "execute_data", "data_code", ""},
		{"execute_ram", "execute_ram", "", "$0200"},
		{"unstable_opcode", "unstable_opcode", "trigger", ""},
		{"oamaddr_dma", "oamaddr_nonzero_at_dma", "trigger", ""},
		{"palette_0d", "palette_color_0d", "trigger", ""},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			s := newDiagSession(t, tc.rom)
			s.must("diag.configure", map[string]any{"enable": "all"}, nil)
			s.must("exec.step", map[string]any{"frames": 10}, nil)
			var r diagListResult
			s.must("diag.list", nil, &r)
			var hit *DiagListItem
			for i, it := range r.Items {
				if it.Kind != tc.kind {
					t.Errorf("対象でない %s を検知した: %+v", it.Kind, it)
					continue
				}
				hit = &r.Items[i]
			}
			if hit == nil {
				t.Fatalf("%s を検知しない: %+v", tc.kind, r)
			}
			if tc.pc != "" && hit.PC != tc.pc {
				t.Errorf("PC = %s、期待 %s", hit.PC, tc.pc)
			}
			if tc.label != "" {
				if hit.Symbol != tc.label {
					t.Errorf("Symbol = %q、期待 %q（%+v）", hit.Symbol, tc.label, hit)
				}
				want := tc.rom + ".s:" + strconv.Itoa(triggerLine(t, tc.rom, tc.label))
				if hit.Source != want {
					t.Errorf("ソース = %q、期待 %q", hit.Source, want)
				}
			}
			if hit.Detail == "" || hit.PRGOffset == nil && tc.pc == "" {
				t.Errorf("説明か PRG のオフセットが無い: %+v", hit)
			}
		})
	}
	t.Run("対照", func(t *testing.T) {
		s := newDiagSession(t, "control")
		s.must("diag.configure", map[string]any{"enable": "all"}, nil)
		s.must("exec.step", map[string]any{"frames": 30}, nil)
		var r diagListResult
		s.must("diag.list", nil, &r)
		if len(r.Items) != 0 || len(r.Totals) != 0 {
			t.Errorf("対照の ROM で検知した: %+v", r)
		}
	})
}

// TestDiagnosticDefaults は既定の有効・無効が §14.20.1 の表どおりであり、
// 既定で無効な種類は検知しないことを確かめる。
func TestDiagnosticDefaults(t *testing.T) {
	s := newDiagSession(t, "palette_0d")
	var cfg struct {
		Kinds []DiagKindState `json:"kinds"`
	}
	s.must("diag.configure", nil, &cfg)
	off := map[string]bool{"execute_ram": true, "palette_color_0d": true}
	if len(cfg.Kinds) != 11 {
		t.Fatalf("種類の数 = %d", len(cfg.Kinds))
	}
	for _, k := range cfg.Kinds {
		if k.Enabled == off[k.Kind] || k.Default != k.Enabled || k.Stop {
			t.Errorf("既定 = %+v", k)
		}
	}
	s.must("exec.step", map[string]any{"frames": 10}, nil)
	var r diagListResult
	s.must("diag.list", nil, &r)
	if len(r.Items) != 0 {
		t.Errorf("既定で無効な palette_color_0d を検知した: %+v", r.Items)
	}
	if err := s.call("diag.configure", map[string]any{"items": map[string]any{"nope": map[string]any{"enabled": true}}}, nil); err == nil {
		t.Error("知らない種類を受け付けた")
	}
}

// TestDiagnosticStop は stop: true の種類を検知した命令を終えた命令境界で
// 止まり、stop_reason が diagnostic になることを確かめる。
func TestDiagnosticStop(t *testing.T) {
	for _, tc := range []struct{ rom, kind string }{
		{"stack_overflow", "stack_overflow"},
		{"uninit_read", "uninitialized_ram_read"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newDiagSession(t, tc.rom)
			s.must("diag.configure", map[string]any{"items": map[string]any{tc.kind: map[string]any{"stop": true}}}, nil)
			var ob Observation
			s.must("exec.step", map[string]any{"frames": 10}, &ob)
			if ob.StopReason != StopDiagnostic {
				t.Fatalf("止まらない: %+v", ob)
			}
			raw, _ := json.Marshal(ob.StopDetail)
			var detail DiagInfo
			json.Unmarshal(raw, &detail)
			if detail.Kind != tc.kind || detail.Symbol != "trigger" {
				t.Errorf("stop_detail = %+v", detail)
			}
			// 止まった位置は trigger の命令の直後の命令境界。
			var trig struct {
				Name string `json:"name"`
				CPU  string `json:"cpu"`
			}
			s.must("symbol.lookup", map[string]any{"name": "trigger"}, &trig)
			var cpu CPUInfo
			s.must("cpu.get", nil, &cpu)
			start, _ := strconv.ParseUint(strings.TrimPrefix(trig.CPU, "$"), 16, 16)
			pc, _ := strconv.ParseUint(strings.TrimPrefix(cpu.PC, "$"), 16, 16)
			if pc <= start || pc > start+3 || ob.CPU.MidInstruction {
				t.Errorf("止まった位置 %s、trigger %s", cpu.PC, trig.CPU)
			}
			// 続けて進めると止まらずに進む（同じ位置で止まり直さない）。
			s.must("exec.step", map[string]any{"frames": 2}, &ob)
			if ob.StopReason == StopDiagnostic {
				t.Errorf("同じ検知で止まり直した: %+v", ob)
			}
		})
	}
}

// TestDiagnosticReporting は Observation の diagnostics・イベント・ROM の
// 読み直しでの数え直しを確かめる。
func TestDiagnosticReporting(t *testing.T) {
	s := newDiagSession(t, "control")
	s.must("diag.configure", map[string]any{"enable": "all"}, nil)
	var ob Observation
	s.must("exec.step", map[string]any{"frames": 2}, &ob)
	if len(ob.Diagnostics) != 0 {
		t.Fatalf("対照で diagnostics = %+v", ob.Diagnostics)
	}
	// RAM に置いた命令を毎フレーム実行させる: update を書き換えずに、
	// $0200 に RTS を置いて PC を移す。
	s.must("mem.write", map[string]any{"loc": "$0200", "value": 0x60}, nil)
	s.must("debug.bp.add", map[string]any{"kind": "exec", "loc": "helper"}, nil)
	s.must("exec.step", map[string]any{"frames": 5}, &ob)
	if ob.StopReason != StopBreakpoint {
		t.Fatalf("helper で止まらない: %+v", ob)
	}
	s.must("debug.bp.remove", map[string]any{"id": 1}, nil)
	var cpu CPUInfo
	s.must("cpu.get", nil, &cpu)
	// JSR $0200 の代わりに、スタックへ戻り先を積んで PC を $0200 にする。
	s.must("cpu.set", map[string]any{"pc": 0x0200}, nil)
	ob = Observation{}
	s.must("exec.step_unit", map[string]any{"unit": "instruction", "count": 1}, &ob)
	if len(ob.Diagnostics) != 1 || ob.Diagnostics[0].Kind != "execute_ram" || ob.Diagnostics[0].Count != 1 ||
		ob.Diagnostics[0].First.PC != "$0200" {
		t.Fatalf("Observation の diagnostics = %+v", ob.Diagnostics)
	}
	ob = Observation{}
	s.must("exec.step_unit", map[string]any{"unit": "instruction", "count": 1}, &ob)
	if len(ob.Diagnostics) != 0 {
		t.Errorf("前回の Observation 以降に無いのに diagnostics = %+v", ob.Diagnostics)
	}
	var ev PollResult
	s.must("events.poll", map[string]any{"kinds": []string{EventDiagnostic}}, &ev)
	if len(ev.Events) != 1 {
		t.Fatalf("diagnostic イベント = %+v", ev.Events)
	}
	// 同じ組は 2 回目からイベントにしない。
	s.must("cpu.set", map[string]any{"pc": 0x0200}, nil)
	s.must("exec.step_unit", map[string]any{"unit": "instruction", "count": 1}, &ob)
	s.must("events.poll", map[string]any{"since": ev.LastSeq, "kinds": []string{EventDiagnostic}}, &ev)
	if len(ev.Events) != 0 {
		t.Errorf("同じ組のイベントを積んだ: %+v", ev.Events)
	}
	var r diagListResult
	s.must("diag.list", nil, &r)
	if len(r.Items) != 1 || r.Items[0].Count != 2 {
		t.Errorf("diag.list = %+v", r)
	}
	// ROM を読み直すと数え直す。指定は残る。
	s.must("rom.reload", nil, nil)
	s.must("diag.list", nil, &r)
	if len(r.Items) != 0 {
		t.Errorf("読み直した後の diag.list = %+v", r)
	}
	var cfg struct {
		Kinds []DiagKindState `json:"kinds"`
	}
	s.must("diag.configure", nil, &cfg)
	for _, k := range cfg.Kinds {
		if !k.Enabled {
			t.Errorf("読み直すと指定が消えた: %+v", k)
		}
	}
}

// TestDiagnosticHooksNil はどの項目も有効でないとき Diagnostic のための
// フックが nil であることを確かめる（§14.20.2）。
func TestDiagnosticHooksNil(t *testing.T) {
	s := newDiagSession(t, "control")
	inst := s.h.Instances()[0]
	check := func(want bool) {
		t.Helper()
		inst.Emu.WithDebugger(func(d *debug.Debugger) {
			n := d.Machine()
			h := n.Hooks()
			got := h.OnCPURead != nil || h.OnCPUWrite != nil || h.OnBeforeExec != nil || h.OnInterrupt != nil ||
				n.CPU.Compat != nil || n.PPU.Compat != nil
			if got != want {
				t.Errorf("フック = %v、期待 %v（%+v）", got, want, h)
			}
		})
	}
	check(true)
	s.must("diag.configure", map[string]any{"enable": "none"}, nil)
	check(false)
	s.must("diag.configure", map[string]any{"enable": []string{"palette_color_0d"}}, nil)
	inst.Emu.WithDebugger(func(d *debug.Debugger) {
		h := d.Machine().Hooks()
		if h.OnCPURead != nil || h.OnCPUWrite != nil || h.OnBeforeExec != nil || d.Machine().PPU.Compat == nil {
			t.Errorf("palette_color_0d だけのフック = %+v", h)
		}
	})
}

// TestDiagnosticFork は Fork で有効・無効と stop が写ることを確かめる。
func TestDiagnosticFork(t *testing.T) {
	s := newDiagSession(t, "control")
	s.must("diag.configure", map[string]any{"enable": []string{"execute_ram"}, "items": map[string]any{"execute_ram": map[string]any{"stop": true}}}, nil)
	var fork ForkResult
	s.must("instance.fork", map[string]any{"instance": "i1"}, &fork)
	var cfg struct {
		Kinds []DiagKindState `json:"kinds"`
	}
	s.must("diag.configure", map[string]any{"instance": string(fork.ID)}, &cfg)
	for _, k := range cfg.Kinds {
		want := k.Kind == "execute_ram"
		if k.Enabled != want || k.Stop != want {
			t.Errorf("Fork の指定 = %+v", k)
		}
	}
}

// TestTraceQuery は trace.query の各絞り込みを確かめる。
func TestTraceQuery(t *testing.T) {
	s := newDiagSession(t, "control")
	s.must("exec.step", map[string]any{"frames": 3}, nil)
	var res map[string]any
	if err := s.call("trace.query", map[string]any{"kinds": []string{"write"}}, nil); err == nil {
		t.Error("トレースを始める前の write の絞り込みを受け付けた")
	}
	var st TraceStatus
	s.must("trace.enable", map[string]any{"ring_size": 100000}, &st)
	if !st.Enabled || st.RingSize != 100000 || st.BusRingSize != 0 {
		t.Fatalf("trace.enable = %+v", st)
	}
	s.must("exec.step", map[string]any{"frames": 5}, nil)
	if err := s.call("trace.query", map[string]any{"kinds": []string{"write"}}, nil); err == nil || !strings.Contains(err.Message, "bus: true") {
		t.Errorf("bus なしの write の絞り込み: %v", err)
	}
	type entry struct {
		Frame     uint64 `json:"frame"`
		PC        string `json:"pc"`
		Symbol    string `json:"symbol"`
		Source    string `json:"source"`
		Disasm    string `json:"disasm"`
		Kind      string `json:"kind"`
		Interrupt string `json:"interrupt"`
		Access    *struct {
			Addr  string `json:"addr"`
			Write bool   `json:"write"`
		} `json:"access"`
	}
	type queryResult struct {
		Count     int       `json:"count"`
		Truncated bool      `json:"truncated"`
		Entries   []entry   `json:"entries"`
		Text      string    `json:"text"`
		Covered   [2]uint64 `json:"covered_frames"`
	}
	var q queryResult
	// jsr: 1 フレームに wait_nmi・update・helper の 3 回。
	q = queryResult{}
	s.must("trace.query", map[string]any{"kinds": []string{"jsr"}, "frames": []uint64{5, 6}, "format": "json"}, &q)
	if q.Count != 6 {
		t.Errorf("jsr の数 = %d: %+v", q.Count, q.Entries)
	}
	for _, e := range q.Entries {
		if e.Kind != "jsr" || !strings.HasPrefix(e.Disasm, "JSR") || e.Source == "" || e.Frame < 5 || e.Frame > 6 {
			t.Errorf("jsr = %+v", e)
		}
	}
	// pc: Symbol の名前でその範囲。
	q = queryResult{}
	s.must("trace.query", map[string]any{"pc": "update", "frames": []uint64{5, 5}, "format": "json"}, &q)
	if q.Count != 3 {
		t.Errorf("update の命令の数 = %d: %+v", q.Count, q.Entries)
	}
	for _, e := range q.Entries {
		if !strings.HasPrefix(e.Symbol, "update") {
			t.Errorf("update の外: %+v", e)
		}
	}
	// interrupt と rti。
	q = queryResult{}
	s.must("trace.query", map[string]any{"kinds": []string{"interrupt", "rti"}, "frames": []uint64{5, 6}, "format": "json"}, &q)
	nmi, rti := 0, 0
	for _, e := range q.Entries {
		if e.Kind == "interrupt" && e.Interrupt == "nmi" && e.Symbol == "nmi" {
			nmi++
		}
		if e.Kind == "rti" {
			rti++
		}
	}
	if nmi != 2 || rti != 2 {
		t.Errorf("interrupt %d、rti %d: %+v", nmi, rti, q.Entries)
	}
	// branch_taken: wait_nmi のループ。
	q = queryResult{}
	s.must("trace.query", map[string]any{"kinds": []string{"branch_taken"}, "frames": []uint64{5, 5}, "format": "json", "limit": 5}, &q)
	if q.Count != 5 || !q.Truncated || !strings.HasPrefix(q.Entries[0].Symbol, "wait_nmi") {
		t.Errorf("branch_taken = %+v", q)
	}
	// text: nestest 形式にソース行。
	q = queryResult{}
	s.must("trace.query", map[string]any{"pc": "helper", "limit": 2}, &q)
	if !strings.Contains(q.Text, "control.s:") || !strings.Contains(q.Text, "helper") || strings.Count(q.Text, "\n") != 1 {
		t.Errorf("text = %q", q.Text)
	}
	// bus: true で write と addr。
	s.must("trace.enable", map[string]any{"ring_size": 100000, "bus": true}, &st)
	s.must("exec.step", map[string]any{"frames": 3}, nil)
	var cur Observation
	s.must("obs.get", nil, &cur)
	q = queryResult{}
	s.must("trace.query", map[string]any{"kinds": []string{"write"}, "addr": "frame_work", "format": "json"}, &q)
	// INC は読み・書き戻し（ダミーライト）・書きの順に動くため、1 回で 2 回書く。
	if q.Count != 6 {
		t.Errorf("frame_work への書き込み = %d: %+v", q.Count, q.Entries)
	}
	for _, e := range q.Entries {
		if e.Access == nil || !e.Access.Write || e.Symbol != "update" {
			t.Errorf("write = %+v", e)
		}
	}
	q = queryResult{}
	s.must("trace.query", map[string]any{"kinds": []string{"read"}, "addr": "nmi_count", "frames": []uint64{cur.Frame - 1, cur.Frame - 1}, "format": "json", "limit": 5000}, &q)
	if q.Count < 10 {
		t.Errorf("nmi_count の読み出し = %d", q.Count)
	}
	// 範囲がリングの外: 残っている範囲を返す。
	q = queryResult{}
	s.must("trace.query", map[string]any{"frames": []uint64{0, cur.Frame}, "format": "json", "limit": 1}, &res)
	if notes, _ := res["notes"].([]any); len(notes) == 0 {
		t.Errorf("リングの外を知らせない: %v", res)
	}
	s.must("trace.disable", nil, &st)
	if st.Enabled || st.Recorded == 0 {
		t.Errorf("trace.disable = %+v", st)
	}
}

// TestTraceSummary は trace.summary の呼び出し回数がテスト用 ROM の構造と
// 一致することを確かめる。
func TestTraceSummary(t *testing.T) {
	s := newDiagSession(t, "control")
	s.must("exec.step", map[string]any{"frames": 3}, nil)
	s.must("trace.enable", nil, nil)
	s.must("exec.step", map[string]any{"frames": 6}, nil)
	var sum struct {
		Frames       [2]uint64     `json:"frames"`
		Instructions int           `json:"instructions"`
		Functions    []FuncSummary `json:"functions"`
		Interrupts   struct {
			NMI uint64 `json:"nmi"`
		} `json:"interrupts"`
		HotSpots []struct {
			Symbol string  `json:"symbol"`
			Share  float64 `json:"share"`
		} `json:"hot_spots"`
		Notes []string `json:"notes"`
	}
	var ob Observation
	s.must("obs.get", nil, &ob)
	from := ob.Frame - 4
	s.must("trace.summary", map[string]any{"frames": []uint64{from, from + 2}}, &sum)
	if sum.Frames != [2]uint64{from, from + 2} || sum.Instructions == 0 {
		t.Fatalf("範囲 = %+v", sum)
	}
	byName := map[string]FuncSummary{}
	for _, f := range sum.Functions {
		byName[f.Symbol] = f
	}
	for _, name := range []string{"update", "helper", "nmi"} {
		f := byName[name]
		if f.Calls != 3 {
			t.Errorf("%s の呼び出し = %+v", name, f)
		}
	}
	if u, h := byName["update"], byName["helper"]; u.CyclesIncl <= h.CyclesIncl || u.CyclesExcl+h.CyclesIncl != u.CyclesIncl ||
		len(h.Callers) != 1 || h.Callers[0] != "update" || u.Source == "" {
		t.Errorf("update = %+v、helper = %+v", u, h)
	}
	if !byName["nmi"].Interrupt || sum.Interrupts.NMI != 3 {
		t.Errorf("nmi = %+v、割り込み %d", byName["nmi"], sum.Interrupts.NMI)
	}
	if len(sum.HotSpots) == 0 || !strings.HasPrefix(sum.HotSpots[0].Symbol, "wait_nmi") || sum.HotSpots[0].Share < 0.1 {
		t.Errorf("hot_spots = %+v", sum.HotSpots)
	}
}

type profileResult struct {
	FrameCount uint64 `json:"frame_count"`
	Busy       struct {
		Ratio statJSON `json:"ratio"`
	} `json:"busy"`
	Over90 []map[string]any `json:"over_90"`
	NMI    struct {
		Count  uint64   `json:"count"`
		Cycles statJSON `json:"cycles"`
	} `json:"nmi"`
	VBlank struct {
		Frames uint64 `json:"frames"`
	} `json:"vblank_ppu"`
	Functions []FuncSummary `json:"functions"`
	Lag       struct {
		Count  uint64   `json:"count"`
		Frames []uint64 `json:"frames"`
	} `json:"lag_frames"`
	Idle  idleSummary `json:"idle_detection"`
	Notes []string    `json:"notes"`
}

// TestProfile は処理落ちを起こす ROM でプロファイルが処理落ちのフレームを
// 数え、アイドルループの判定 3 段階がそれぞれ働くことを確かめる。
func TestProfile(t *testing.T) {
	s := newDiagSession(t, "lag")
	s.must("exec.step", map[string]any{"frames": 5}, nil)
	var start struct {
		Idle idleSummary `json:"idle_detection"`
	}
	s.must("profile.start", nil, &start)
	if start.Idle.Method != debug.IdleBySymbol || len(start.Idle.Ranges) != 1 || !strings.HasPrefix(start.Idle.Ranges[0], "wait_vblank") {
		t.Fatalf("判定 2（Symbol）= %+v", start.Idle)
	}
	s.must("exec.step", map[string]any{"frames": 41}, nil)
	var rep profileResult
	s.must("profile.report", nil, &rep)
	if rep.FrameCount < 39 {
		t.Fatalf("測ったフレーム = %d", rep.FrameCount)
	}
	// 4 フレームに 1 回の heavy は 1 フレームを超えるため、次のフレームは
	// アイドルループに入らない。
	if rep.Lag.Count < 8 || rep.Lag.Count > 12 {
		t.Errorf("処理落ち = %+v", rep.Lag)
	}
	if rep.Busy.Ratio.Max < 0.9 || rep.Busy.Ratio.Min > 0.2 || len(rep.Over90) == 0 {
		t.Errorf("忙しさ = %+v、over_90 %d", rep.Busy, len(rep.Over90))
	}
	if rep.NMI.Count < 30 || rep.NMI.Cycles.Max == 0 {
		t.Errorf("NMI = %+v", rep.NMI)
	}
	found := false
	for _, f := range rep.Functions {
		if f.Symbol == "heavy" && f.Calls >= 9 && f.CyclesIncl > 30000*9 {
			found = true
		}
	}
	if !found {
		t.Errorf("heavy が関数に無い: %+v", rep.Functions)
	}
	s.must("profile.stop", nil, nil)
	s.must("exec.step", map[string]any{"frames": 5}, nil)
	var after profileResult
	s.must("profile.report", nil, &after)
	if after.FrameCount != rep.FrameCount {
		t.Errorf("止めた後も測っている: %d → %d", rep.FrameCount, after.FrameCount)
	}

	// 判定 1: 引数。
	s.must("profile.start", map[string]any{"idle": "wait_vblank"}, &start)
	if start.Idle.Method != debug.IdleByArgument {
		t.Errorf("判定 1 = %+v", start.Idle)
	}

	// 判定 3: 自動（JMP *）。
	s2 := newDiagSession(t, "palette_0d")
	s2.must("profile.start", nil, &start)
	if start.Idle.Method != debug.IdleByAuto || len(start.Idle.Ranges) != 0 {
		t.Fatalf("判定 3 の開始 = %+v", start.Idle)
	}
	s2.must("exec.step", map[string]any{"frames": 5}, nil)
	var auto profileResult
	s2.must("profile.report", nil, &auto)
	// forever の JMP * を見つける。
	var fv struct {
		CPU string `json:"cpu"`
	}
	s2.must("symbol.lookup", map[string]any{"name": "forever"}, &fv)
	hasForever := false
	for _, r := range auto.Idle.Ranges {
		if strings.HasPrefix(r, fv.CPU+"..") {
			hasForever = true
		}
	}
	if auto.Idle.Method != debug.IdleByAuto || !hasForever || auto.Lag.Count > 1 {
		t.Errorf("判定 3 = %+v、処理落ち %+v", auto.Idle, auto.Lag)
	}
	if err := s2.call("profile.report", map[string]any{"instance": "nope"}, nil); err == nil {
		t.Error("無い Instance を受け付けた")
	}
}

// TestAnalysisHooksRemoved はトレースとプロファイルを止めるとフックが nil に
// 戻ることを確かめる（計画フェーズ 20 §3.1）。
func TestAnalysisHooksRemoved(t *testing.T) {
	s := newDiagSession(t, "control")
	s.must("diag.configure", map[string]any{"enable": "none"}, nil)
	inst := s.h.Instances()[0]
	hooked := func() bool {
		var on bool
		inst.Emu.WithDebugger(func(d *debug.Debugger) {
			h := d.Machine().Hooks()
			on = h.OnCPURead != nil || h.OnCPUWrite != nil || h.OnBeforeExec != nil || h.OnInterrupt != nil
		})
		return on
	}
	if hooked() {
		t.Fatal("最初からフックがある")
	}
	s.must("trace.enable", map[string]any{"bus": true, "ring_size": 1000, "bus_ring_size": 1000}, nil)
	if !hooked() {
		t.Fatal("トレースのフックが無い")
	}
	s.must("trace.disable", nil, nil)
	if hooked() {
		t.Error("trace.disable の後もフックがある")
	}
	s.must("profile.start", nil, nil)
	if !hooked() {
		t.Fatal("プロファイルのフックが無い")
	}
	s.must("profile.stop", nil, nil)
	if hooked() {
		t.Error("profile.stop の後もフックがある")
	}
}
