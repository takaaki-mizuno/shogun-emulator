package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestParseInput は入力の書き方を確かめる（設計書 14 編 §14.8.5）。
func TestParseInput(t *testing.T) {
	cases := []struct {
		in   string
		want uint8
	}{
		{"", 0}, {"A", 0x01}, {"b", 0x02}, {"Select", 0x04}, {"START", 0x08},
		{"Up", 0x10}, {"down", 0x20}, {"L", 0x40}, {"r", 0x80},
		{"R+A", 0x81}, {"Up+B+A", 0x13}, {"Left+Right", 0xC0}, {"$81", 0x81}, {" a + b ", 0x03},
	}
	for _, c := range cases {
		got, err := ParseInput(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseInput(%q) = $%02X, %v, 期待 $%02X", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"X", "A+Jump", "$1FF", "$zz", "A++B"} {
		_, err := ParseInput(bad)
		if err == nil {
			t.Errorf("ParseInput(%q) が誤りにならない", bad)
			continue
		}
		if AsError(err).Kind != KindInvalidParams {
			t.Errorf("ParseInput(%q) の種類 = %s", bad, AsError(err).Kind)
		}
	}
	if e := AsError(func() error { _, err := ParseInput("A+Jump"); return err }()); e.Position != 2 {
		t.Errorf("誤りの位置 = %d, 期待 2", e.Position)
	}
}

// TestTestROMLayout はテスト用 ROM の組み立てを逆アセンブルで確かめる。
func TestTestROMLayout(t *testing.T) {
	s := newSession(t)
	var r struct {
		Lines []DisasmLine `json:"lines"`
	}
	s.must("cpu.disasm", map[string]any{"loc": romAddr("loop"), "count": 2}, &r)
	if r.Lines[0].Text != "JMP "+romAddr("loop") || r.Lines[1].Text != "INC $10" {
		t.Fatalf("逆アセンブル = %+v", r.Lines)
	}
}

// TestStepAdvancesFramesWithInput は exec.step が指定のフレーム数を進め、
// 入力をそのフレームでラッチし、キーボードの入力を使わないことを確かめる。
func TestStepAdvancesFramesWithInput(t *testing.T) {
	s := newSession(t)
	inst := s.h.Instances()[0]
	// キーボードの入力は Agent-Paced の間は使わない。
	inst.Emu.Input.Set(0, 0xFF)
	f0, n0 := s.warm()

	var ob Observation
	s.must("exec.step", map[string]any{"frames": 3}, &ob)
	if ob.StopReason != StopFramesDone || ob.Frame != f0+3 {
		t.Fatalf("exec.step = %+v（開始 %d）", ob, f0)
	}
	if v := s.readByte("$0011"); v != 0 {
		t.Errorf("キーボードの入力が混ざった: $11 = $%02X", v)
	}
	s.must("exec.step", map[string]any{"frames": 1, "input": "A+Right"}, &ob)
	if ob.Frame != f0+4 {
		t.Errorf("1 フレーム進めた後のフレーム = %d, 期待 4", ob.Frame)
	}
	// A が bit 7、Right が bit 0（ROM が読んだ順）。
	if v := s.readByte("$0011"); v != 0x81 {
		t.Errorf("$11 = $%02X, 期待 $81", v)
	}
	s.must("exec.step", nil, &ob)
	if v := s.readByte("$0011"); v != 0 {
		t.Errorf("入力を外した次のフレームの $11 = $%02X, 期待 $00", v)
	}
	if v := s.readByte("$0010"); v != n0+5 {
		t.Errorf("NMI の回数 $10 = %d, 期待 %d", v, n0+5)
	}
	if err := s.call("exec.step", map[string]any{"frames": 0x10000}, nil); err == nil || err.Kind != KindInvalidParams {
		t.Errorf("範囲外の frames: %v", err)
	}
	if err := s.call("exec.step", map[string]any{"input": "Jump"}, nil); err == nil || err.Kind != KindInvalidParams {
		t.Errorf("不正な入力: %v", err)
	}
	var none Observation
	s.must("exec.step", map[string]any{"observe": false}, &none)
	if none.CPU != nil || none.StopReason != StopFramesDone {
		t.Errorf("observe: false の結果 = %+v", none)
	}
}

// TestInputSequence は入力の列を流し、各要素の要約を返すことを確かめる。
func TestInputSequence(t *testing.T) {
	s := newSession(t)
	f0, _ := s.warm()
	s.must("debug.watch.add", map[string]any{"loc": "$0011"}, nil)
	var r SequenceResult
	s.must("exec.input_sequence", map[string]any{
		"steps":        []map[string]any{{"frames": 2, "input": "B"}, {"frames": 1, "input": "Start"}, {"frames": 1}},
		"observe_each": true,
	}, &r)
	if r.StopReason != StopSequenceDone || r.CompletedSteps != 3 || r.Frame != f0+4 || len(r.PerStep) != 3 {
		t.Fatalf("結果 = %+v", r)
	}
	want := []int64{0x40, 0x10, 0x00}
	for i, ps := range r.PerStep {
		if ps.Watch["$0011"] != want[i] || ps.Image != nil {
			t.Errorf("per_step[%d] = %+v, 期待 $11 = $%02X", i, ps.Watch, want[i])
		}
	}
	// ブレークポイントで止まったらそこで終える。
	s.must("debug.bp.add", map[string]any{"kind": "exec", "loc": romAddr("nmi")}, nil)
	s.must("exec.input_sequence", map[string]any{"steps": []map[string]any{{"frames": 5}, {"frames": 5}}}, &r)
	if r.StopReason != StopBreakpoint || r.CompletedSteps != 0 {
		t.Errorf("ブレークポイントでの結果 = %+v", r)
	}
	detail, _ := json.Marshal(r.StopDetail)
	if !strings.Contains(string(detail), `"pc":"`+romAddr("nmi")+`"`) {
		t.Errorf("stop_detail = %s", detail)
	}
}

// TestRunUntil は条件が成り立つフレームで止まることを確かめる。
func TestRunUntil(t *testing.T) {
	s := newSession(t)
	var ob Observation
	s.must("exec.run_until", map[string]any{"condition": "[$0010] == 5"}, &ob)
	if ob.StopReason != StopCondition || s.readByte("$0010") != 5 {
		t.Fatalf("条件で止まらない: %+v（$10 = %d）", ob, s.readByte("$0010"))
	}
	f := ob.Frame
	// 開始時点で成り立っていても、少なくとも 1 フレーム進める。
	s.must("exec.run_until", map[string]any{"condition": "[$0010] >= 5"}, &ob)
	if ob.Frame != f+1 {
		t.Errorf("開始直後に止まった: フレーム %d（開始 %d）", ob.Frame, f)
	}
	s.must("exec.run_until", map[string]any{"condition": "[$0010] == 200", "max_frames": 10}, &ob)
	if ob.StopReason != StopMaxFrames || ob.Frame != f+11 {
		t.Errorf("上限: %+v", ob)
	}
	// 命令ごとの判定は NMI の中の書き込みの直後で止まる。
	next := s.readByte("$0010") + 1
	s.must("exec.run_until", map[string]any{"condition": "[$0010] == " + itoa(next), "check": "instruction"}, &ob)
	if ob.StopReason != StopCondition || ob.CPU.PC != romAddr("after_inc") {
		t.Errorf("命令ごとの判定: %+v %+v", ob, ob.CPU)
	}
	err := s.call("exec.run_until", map[string]any{"condition": "[$0010] == == 1"}, nil)
	if err == nil || err.Kind != KindInvalidExpression || err.Position <= 0 {
		t.Errorf("式の誤り: %+v", err)
	}
	s.must("exec.run_until", map[string]any{"condition": "[$0011] == $FF", "max_frames": 216000, "timeout_ms": 1}, &ob)
	if ob.StopReason != StopTimeout {
		t.Errorf("timeout: %+v", ob)
	}
}

// TestRunUntilCancel は実行中の run_until を取り消すと cancelled で返る
// ことを確かめる。
func TestRunUntilCancel(t *testing.T) {
	s := newSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan any, 1)
	go func() {
		p, _ := json.Marshal(map[string]any{"condition": "[$0011] == $FF", "max_frames": 216000})
		res, err := s.h.Dispatch(ctx, s.conn, "exec.run_until", p)
		if err != nil {
			done <- err
			return
		}
		done <- res
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case res := <-done:
		ob, ok := res.(*Observation)
		if !ok || ob.StopReason != StopCancelled {
			t.Fatalf("結果 = %#v", res)
		}
		if ob.Frame == 0 {
			t.Error("進んでいない")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取り消しても戻らない")
	}
	// 取り消した後も一時停止しており、次の進行を受け付ける。
	var ob Observation
	s.must("exec.step", nil, &ob)
	if ob.StopReason != StopFramesDone {
		t.Errorf("取り消しの後の進行 = %+v", ob)
	}
}

// TestStepUnit は単位ごとの進行を確かめる。
func TestStepUnit(t *testing.T) {
	s := newSession(t)
	var ob Observation
	s.must("exec.step_unit", map[string]any{"unit": "instruction", "count": 2}, &ob)
	if ob.StopReason != StopStepDone || ob.CPU.PC != "$8002" {
		t.Errorf("2 命令 = %+v", ob.CPU)
	}
	s.must("exec.step_unit", map[string]any{"to": romAddr("loop")}, &ob)
	if ob.CPU.PC != romAddr("loop") {
		t.Errorf("位置まで = %+v", ob.CPU)
	}
	s.must("exec.step_unit", map[string]any{"unit": "cycle"}, &ob)
	if !ob.CPU.MidInstruction {
		t.Errorf("サイクル単位で命令の途中に止まらない: %+v", ob.CPU)
	}
	s.must("exec.step_unit", map[string]any{"unit": "scanline", "count": 3}, &ob)
	if ob.StopReason != StopStepDone {
		t.Errorf("スキャンライン: %+v", ob)
	}
	if err := s.call("exec.step_unit", map[string]any{"unit": "bogus"}, nil); err == nil {
		t.Error("知らない単位を受け付けた")
	}
}

// TestBreakpointsAndNoBreak はブレークポイントで止まることと、break: false で
// 止まらないことを確かめる。
func TestBreakpointsAndNoBreak(t *testing.T) {
	s := newSession(t)
	s.warm()
	var bp BreakpointInfo
	s.must("debug.bp.add", map[string]any{"kind": "write", "loc": "$0011"}, &bp)
	if bp.ID == 0 || bp.Kind != "write" || bp.Start != "$0011" {
		t.Fatalf("debug.bp.add = %+v", bp)
	}
	var ob Observation
	s.must("exec.step", map[string]any{"frames": 5}, &ob)
	if ob.StopReason != StopBreakpoint {
		t.Errorf("書き込みで止まらない: %+v", ob)
	}
	s.must("exec.step", map[string]any{"frames": 5, "break": false}, &ob)
	if ob.StopReason != StopFramesDone {
		t.Errorf("break: false で止まった: %+v", ob)
	}
	var list []BreakpointInfo
	s.must("debug.bp.enable", map[string]any{"id": bp.ID, "enabled": false}, &list)
	if len(list) != 1 || list[0].Enabled {
		t.Errorf("無効にした一覧 = %+v", list)
	}
	s.must("exec.step", map[string]any{"frames": 2}, &ob)
	if ob.StopReason != StopFramesDone {
		t.Errorf("無効にしたブレークポイントで止まった: %+v", ob)
	}
	s.must("debug.bp.remove", map[string]any{"id": bp.ID}, &list)
	if len(list) != 0 {
		t.Errorf("取り除いた一覧 = %+v", list)
	}
	for _, bad := range []map[string]any{
		{"kind": "nope"}, {"kind": "exec"}, {"kind": "event", "event": "nope"},
		{"kind": "exec", "loc": "$8000", "condition": "A =="},
	} {
		if err := s.call("debug.bp.add", bad, nil); err == nil {
			t.Errorf("%v を受け付けた", bad)
		}
	}
}

// warm は NMI が動き始めるまで進め、そのときのフレームと $10 を返す。
// quiet は Diagnostic を止め、それまでのイベントを読み捨てる。テスト用の ROM は
// PPU の起動を待たずに書くなど Diagnostic を起こすため、フックやイベントの数を
// 確かめるテストで使う。
func (s *agentSession) quiet() uint64 {
	s.t.Helper()
	s.must("diag.configure", map[string]any{"enable": "none"}, nil)
	var r PollResult
	s.must("events.poll", map[string]any{"max": 1000}, &r)
	return r.LastSeq
}

func (s *agentSession) warm() (uint64, int) {
	s.t.Helper()
	var ob Observation
	s.must("exec.run_until", map[string]any{"condition": "[$0010] >= 1"}, &ob)
	if ob.StopReason != StopCondition {
		s.t.Fatalf("NMI が動かない: %+v", ob)
	}
	return ob.Frame, s.readByte("$0010")
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestCPUHalted は STP で CPU が止まると cpu_halted を返すことを確かめる。
func TestCPUHalted(t *testing.T) {
	h := newTestHost(t, KindServe)
	s := &agentSession{t: t, h: h, conn: h.Connect()}
	a := newAsm(0x8000)
	a.op(0xEA, 0x02) // NOP; STP
	prg := make([]uint8, 32*1024)
	copy(prg, a.done(t))
	prg[0x7FFC], prg[0x7FFD] = 0x00, 0x80
	data := append([]uint8{0x4E, 0x45, 0x53, 0x1A, 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, prg...)
	data = append(data, make([]uint8, 8*1024)...)
	path := filepath.Join(t.TempDir(), "stp.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	s.must("instance.create", map[string]any{"rom": path}, nil)
	var ob Observation
	s.must("exec.step", map[string]any{"frames": 2}, &ob)
	if ob.StopReason != StopCPUHalted {
		t.Errorf("stop_reason = %s, 期待 cpu_halted", ob.StopReason)
	}
}
