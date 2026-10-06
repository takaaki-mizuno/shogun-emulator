package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
)

// TestReproReplaysToSameState は介入（mem.write・cpu.set・mem.freeze・フレームの
// 途中の書き込み）を含む操作の後に repro.export し、その SHGM を GUI と同じ
// 通常のエミュレータで再生すると、最後のフレームの状態ハッシュが一致すること
// を確かめる（フェーズ 18 の完了判定）。
func TestReproReplaysToSameState(t *testing.T) {
	s, rom := newGameSession(t, false)
	inst := s.h.Instances()[0]
	s.must("exec.step", map[string]any{"frames": 30, "input": "Right"}, nil)
	s.must("mem.write", map[string]any{"loc": "$0310", "value": []int{1, 2, 3}}, nil)
	s.must("exec.step", map[string]any{"frames": 5}, nil)
	// フレームの途中で止めて書く。
	s.must("exec.step_unit", map[string]any{"unit": "scanline", "count": 100}, nil)
	s.must("mem.write", map[string]any{"loc": "player_y", "value": 0x33}, nil)
	s.must("cpu.set", map[string]any{"a": "$5A"}, nil)
	s.must("exec.step", map[string]any{"frames": 3, "input": "A"}, nil)
	s.must("mem.freeze", map[string]any{"loc": "$0311", "value": 9}, nil)
	s.must("exec.step", map[string]any{"frames": 4}, nil)
	// 最後の介入の後に進めずに書き出す（フレームの終わりの介入になる）。
	s.must("mem.write", map[string]any{"loc": "$0312", "value": 0x77}, nil)
	want := stateHash(t, inst)
	wantFrame := inst.Emu.Status().Frames

	var st struct {
		Interventions int  `json:"interventions"`
		ReReachable   bool `json:"re_reachable"`
	}
	s.must("record.status", nil, &st)
	if st.Interventions < 5 || !st.ReReachable {
		t.Errorf("record.status = %+v", st)
	}
	dir := filepath.Join(t.TempDir(), "bug.repro")
	var res ReproResult
	s.must("repro.export", map[string]any{"path": dir, "note": "テスト"}, &res)
	for _, f := range []string{"repro.shgm", "commands.jsonl", "final.png", "README.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s が無い: %v", f, err)
		}
	}
	cmds, _ := os.ReadFile(filepath.Join(dir, "commands.jsonl"))
	if !strings.Contains(string(cmds), `"method":"mem.write"`) || strings.Contains(string(cmds), `"data"`) {
		t.Errorf("commands.jsonl:\n%s", cmds)
	}

	// GUI と同じく、エージェントの入力の経路を使わない通常のエミュレータで再生する。
	cfg := config.Default()
	cfg.Emulation.RAMInitPattern = "random" // 埋め込んだ状態から始まるため、初期化の設定に依らない
	e := emu.New(emu.Config{
		Emulation: cfg.Emulation, Input: cfg.Input, State: cfg.State, Movie: cfg.Movie, Debug: cfg.Debug,
		Dirs:     config.Paths{Data: t.TempDir()},
		NewPacer: func(*region.Region) emu.Pacer { return emu.NewNoPacer() },
	})
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(rom); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayMovieFile(dir); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "再生し終えて一時停止する", func() bool { st := e.Status(); return st.Paused && !st.Movie.Playing })
	var got [8]uint8
	var gotFrame uint64
	e.WithMachine(func(n *nes.NES) { got, gotFrame = n.StateHash(), n.Frames() })
	if got != want || gotFrame != wantFrame {
		t.Errorf("再生した状態 = %x（フレーム %d）、期待 %x（フレーム %d）", got, gotFrame, want, wantFrame)
	}
	if err := e.DesyncError(); err != nil {
		t.Errorf("desync: %v", err)
	}
}

// TestReReachFrame は同じ ROM で rom.reload と Re-Reach（frame）を行うと、読み直す
// 前と同じ状態ハッシュになることを確かめる（フェーズ 18 の完了判定）。続けて
// 3 回行っても同じ場面に着く。
func TestReReachFrame(t *testing.T) {
	s, _ := newGameSession(t, true)
	inst := s.h.Instances()[0]
	s.must("exec.input_sequence", map[string]any{"steps": []map[string]any{
		{"frames": 20, "input": "Right"}, {"frames": 1, "input": "A"}, {"frames": 15}}}, nil)
	s.must("exec.step_unit", map[string]any{"unit": "scanline", "count": 50}, nil)
	s.must("mem.write", map[string]any{"loc": "$0315", "value": 0x42}, nil)
	s.must("exec.step", map[string]any{"frames": 10, "input": "B"}, nil)
	want := stateHash(t, inst)
	wantFrame := inst.Emu.Status().Frames
	for i := range 3 {
		var ob Observation
		s.must("rom.reload", map[string]any{"re_reach": "frame"}, &ob)
		if ob.Frame != wantFrame || ob.StopReason != StopFramesDone {
			t.Fatalf("%d 回目: %+v（期待 フレーム %d）", i+1, ob, wantFrame)
		}
		if got := stateHash(t, inst); got != want {
			t.Fatalf("%d 回目の状態 = %x、期待 %x", i+1, got, want)
		}
	}
	// Re-Reach の後も記録は電源投入から始まり、続けて進められる。
	var st struct {
		Start  string `json:"start"`
		Frames uint64 `json:"frames"`
	}
	s.must("record.status", nil, &st)
	if st.Start != "power-on" || st.Frames != wantFrame {
		t.Errorf("Re-Reach の後の記録 = %+v", st)
	}
	s.must("exec.step", nil, nil)
}

// TestReReachCondition は Re-Reach（condition）が条件の成り立つ同じフレームで
// 止まることを確かめる。
func TestReReachCondition(t *testing.T) {
	s, _ := newGameSession(t, true)
	var ob Observation
	s.must("exec.run_until", map[string]any{"condition": "game.mode == 'play'"}, &ob)
	frame := ob.Frame
	s.must("exec.step", map[string]any{"frames": 30}, nil)
	ob = Observation{}
	s.must("rom.reload", map[string]any{"re_reach": "condition", "condition": "game.mode == 'play'"}, &ob)
	if ob.StopReason != StopCondition || ob.Frame != frame {
		t.Errorf("Re-Reach（condition） = %+v、期待 フレーム %d", ob, frame)
	}
	ob = Observation{}
	s.must("rom.reload", map[string]any{"re_reach": "condition", "condition": "game.mode == 'clear'", "max_frames": 10}, &ob)
	if ob.StopReason != StopMaxFrames {
		t.Errorf("上限 = %+v", ob)
	}
}

// TestReReachUnavailable はセーブステートから始まる記録で Re-Reach を断る
// ことを確かめる。
func TestReReachUnavailable(t *testing.T) {
	s, _ := newGameSession(t, false)
	s.must("exec.step", map[string]any{"frames": 10}, nil)
	s.must("state.save", map[string]any{"name": "a"}, nil)
	s.must("state.load", map[string]any{"name": "a"}, nil)
	var ob Observation
	s.must("rom.reload", map[string]any{"re_reach": "frame"}, &ob)
	if ob.Frame != 0 || len(ob.Notes) == 0 || !strings.Contains(strings.Join(ob.Notes, " "), "re_reach_unavailable") {
		t.Errorf("Re-Reach を断らない: %+v", ob)
	}
}

// TestReloadRereadsProject は rom.reload が Game State Definition を読み直すことを
// 確かめる。
func TestReloadRereadsProject(t *testing.T) {
	s, rom := newGameSession(t, true)
	def := `{"version":1,"items":[{"name":"only","loc":"player_x","type":"u8"}]}`
	if err := os.WriteFile(strings.TrimSuffix(rom, ".nes")+".gamestate.json", []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	s.must("rom.reload", nil, nil)
	var gs map[string]any
	s.must("gamestate.get", nil, &gs)
	if _, ok := gs["only"]; !ok || len(gs) != 1 {
		t.Errorf("読み直した定義 = %v", gs)
	}
}

// TestROMWatch は ROM ファイルを書き換えると rom_changed が 1 回だけ積まれる
// ことを確かめる（書きかけを読まない）。
func TestROMWatch(t *testing.T) {
	s, rom := newGameSession(t, false)
	s.must("rom.watch", nil, nil)
	data, _ := os.ReadFile(rom)
	// 書きかけ: 半分だけ書き、少しして残りを書く。
	time.Sleep(600 * time.Millisecond)
	os.WriteFile(rom, data[:len(data)/2], 0o644)
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(rom, data, 0o644)
	future := time.Now().Add(time.Second)
	os.Chtimes(rom, future, future)
	deadline := time.Now().Add(5 * time.Second)
	var r PollResult
	for time.Now().Before(deadline) {
		s.must("events.poll", map[string]any{"kinds": []string{EventROMChanged}}, &r)
		if len(r.Events) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	var all PollResult
	s.must("events.poll", map[string]any{"kinds": []string{EventROMChanged}}, &all)
	if len(all.Events) != 1 {
		t.Errorf("rom_changed = %d 回、期待 1 回", len(all.Events))
	}
	s.must("rom.watch", map[string]any{"enabled": false}, nil)
}

// TestJournalRewindAndFork は巻き戻しで記録を切り詰めることと、Fork した 2 つの
// Instance の記録が別々になることを確かめる。
func TestJournalRewindAndFork(t *testing.T) {
	s, _ := newGameSession(t, false)
	inst := s.h.Instances()[0]
	s.must("exec.step", map[string]any{"frames": 40}, nil)
	if err := inst.Emu.Rewind(10); err != nil {
		t.Fatal(err)
	}
	st, _ := inst.Emu.JournalStatus()
	if st.Frames != 30 || st.Rerecords != 1 {
		t.Errorf("巻き戻しの後の記録 = %+v", st)
	}
	var fr ForkResult
	s.must("instance.fork", nil, &fr)
	other, _ := s.h.Instance(fr.ID)
	s.must("exec.step", map[string]any{"instance": string(fr.ID), "frames": 5}, nil)
	a, _ := inst.Emu.JournalStatus()
	b, _ := other.Emu.JournalStatus()
	if a.Frames != 30 || b.Frames != 35 {
		t.Errorf("Fork した記録 = %d と %d（期待 30 と 35）", a.Frames, b.Frames)
	}
}

// TestReproFromFrame は from_frame で巻き戻しの状態から始まる短い記録を書き出す
// ことを確かめる。
func TestReproFromFrame(t *testing.T) {
	s, _ := newGameSession(t, false)
	s.must("exec.step", map[string]any{"frames": 50}, nil)
	var res ReproResult
	s.must("repro.export", map[string]any{"path": filepath.Join(t.TempDir(), "x.repro"), "from_frame": 25}, &res)
	if res.StartedAt > 25 || res.StartedAt < 15 || res.Frames != 50-res.StartedAt {
		t.Errorf("from_frame の結果 = %+v", res)
	}
	if err := s.call("repro.export", map[string]any{"path": t.TempDir(), "from_frame": 100000}, nil); err == nil {
		t.Error("範囲外の from_frame を受け付けた")
	}
	_ = context.Background
}

// TestReReachCancel は Re-Reach の再生を取り消せることを確かめる。
func TestReReachCancel(t *testing.T) {
	s, _ := newGameSession(t, false)
	s.must("exec.step", map[string]any{"frames": 5}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *Observation, 1)
	go func() {
		p := []byte(`{"re_reach":"condition","condition":"[$0011] == $FF","max_frames":216000}`)
		res, err := s.h.Dispatch(ctx, s.conn, "rom.reload", p)
		if err != nil {
			t.Error(err)
			done <- nil
			return
		}
		done <- res.(*Observation)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case ob := <-done:
		if ob == nil || ob.StopReason != StopCancelled {
			t.Errorf("結果 = %+v", ob)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取り消しても Re-Reach が終わらない")
	}
	if st := s.h.Instances()[0].Emu.Status(); st.Movie.Playing {
		t.Error("取り消した後も再生が続いている")
	}
}
