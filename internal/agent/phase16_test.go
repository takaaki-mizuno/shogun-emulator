package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gameProject はテスト用 ca65 プロジェクトの ROM と .dbg を一時ディレクトリに
// 写し、Game State Definition を置いて、ROM のパスを返す。
func gameProject(t *testing.T, withGameState bool) string {
	t.Helper()
	dir := t.TempDir()
	src := "../../testdata/agent"
	for _, f := range []string{"game.nes", "game.dbg"} {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatalf("テスト用 ROM が無い（testdata/agent/build.sh でビルドする）: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// .dbg を ROM より新しくする（古い .dbg は読み込まない）。
	now := time.Now()
	os.Chtimes(filepath.Join(dir, "game.nes"), now, now)
	os.Chtimes(filepath.Join(dir, "game.dbg"), now.Add(time.Second), now.Add(time.Second))
	if withGameState {
		def := `{"version":1,"items":[
  {"name":"mode","loc":"mode","type":"u8","enum":{"0":"title","1":"play","2":"dead","3":"clear"},"desc":"進行状態"},
  {"name":"player_x","loc":"player_x","type":"u8"},
  {"name":"score","loc":"score","type":"bcd","size":3},
  {"name":"flags","loc":"flags","type":"bits","bits":{"0":"jumping","7":"facing_left"}},
  {"name":"enemies_x","loc":"enemy_x","type":"u8","count":8},
  {"name":"frames","loc":"frame_count","type":"u16","hidden":true}
]}`
		if err := os.WriteFile(filepath.Join(dir, "game.gamestate.json"), []byte(def), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "game.nes")
}

// newGameSession はテスト用 ca65 プロジェクトの ROM で Instance を作る。
func newGameSession(t *testing.T, withGameState bool) (*agentSession, string) {
	t.Helper()
	h := newTestHost(t, KindServe)
	s := &agentSession{t: t, h: h, conn: h.Connect()}
	rom := gameProject(t, withGameState)
	var info InstanceInfo
	s.must("instance.create", map[string]any{"rom": rom, "deterministic": true}, &info)
	if len(info.Notes) != 0 {
		t.Fatalf("読み込みの知らせ = %v", info.Notes)
	}
	return s, rom
}

// frames は NMI の回数（frame_count）を返す。
func (s *agentSession) frames() int {
	s.t.Helper()
	var r struct {
		Bytes []int `json:"bytes"`
	}
	s.must("mem.read", map[string]any{"loc": "frame_count", "length": 2}, &r)
	return r.Bytes[0] | r.Bytes[1]<<8
}

// TestGameStateValues は Game State の各型と修飾が ROM の規則どおりに解釈
// されることを確かめる（フェーズ 16 の完了判定）。
func TestGameStateValues(t *testing.T) {
	s, _ := newGameSession(t, true)
	var ob Observation
	s.must("exec.run_until", map[string]any{"condition": "[frame_count].w == 73"}, &ob)
	if ob.StopReason != StopCondition {
		t.Fatalf("止まらない: %+v", ob)
	}
	var gs map[string]any
	s.must("gamestate.get", nil, &gs)
	n := 73
	want := map[string]any{
		"mode": "play", "player_x": float64(n), "score": float64(n), "frames": float64(n),
		"flags": []any{"jumping", "facing_left"},
	}
	for k, v := range want {
		if got := gs[k]; !equalJSON(got, v) {
			t.Errorf("%s = %#v, 期待 %#v", k, got, v)
		}
	}
	enemies := gs["enemies_x"].([]any)
	if len(enemies) != 8 || enemies[3].(float64) != float64(n+48) {
		t.Errorf("enemies_x = %v", enemies)
	}
	var one map[string]any
	s.must("gamestate.get", map[string]any{"names": []string{"sym:score", "sym:frame_count", "mode"}}, &one)
	if !equalJSON(one["sym:score"], []any{float64(0), float64(0), float64(0x73)}) || one["sym:frame_count"] != float64(n) {
		t.Errorf("sym: の値 = %v", one)
	}
	if err := s.call("gamestate.get", map[string]any{"names": []string{"nope"}}, nil); err == nil {
		t.Error("無い項目を読めた")
	}
}

func equalJSON(a, b any) bool {
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}

// TestGameStateDiff は Observation の gamestate_changed に規則どおりの差分が
// 出て、hidden の項目が入らないことを確かめる。
func TestGameStateDiff(t *testing.T) {
	s, _ := newGameSession(t, true)
	s.must("exec.run_until", map[string]any{"condition": "[frame_count].w == 63"}, nil)
	var ob Observation
	s.must("obs.get", nil, &ob)
	ob = Observation{}
	s.must("exec.step", nil, &ob)
	ch := ob.GameStateChanged
	if c := ch["mode"]; c.From != "title" || c.To != "play" {
		t.Errorf("mode の差分 = %+v", c)
	}
	if c := ch["player_x"]; c.From != float64(63) || c.To != float64(64) {
		t.Errorf("player_x の差分 = %+v", c)
	}
	if _, ok := ch["frames"]; ok {
		t.Error("hidden の項目が差分に入った")
	}
	ob = Observation{}
	s.must("obs.get", map[string]any{"full": true}, &ob)
	if ob.GameState["frames"] != float64(64) || ob.GameStateChanged != nil {
		t.Errorf("full の結果 = %+v %+v", ob.GameState, ob.GameStateChanged)
	}
}

// TestRunUntilGameState は Game State の列挙の名前で条件を書けることを確かめる。
func TestRunUntilGameState(t *testing.T) {
	s, _ := newGameSession(t, true)
	var ob Observation
	s.must("exec.run_until", map[string]any{"condition": "game.mode == 'dead'", "max_frames": 300}, &ob)
	if ob.StopReason != StopCondition || s.frames() != 128 {
		t.Errorf("mode が dead になるのは frame_count 128: %+v（%d）", ob, s.frames())
	}
	if err := s.call("exec.run_until", map[string]any{"condition": "game.nope == 1"}, nil); err == nil || err.Kind != KindInvalidExpression {
		t.Errorf("無い Game State: %v", err)
	}
	// .dbg の名前と定数も使える。
	s.must("exec.run_until", map[string]any{"condition": "player_y == PLAYER_Y_START && [score+2] == $40"}, &ob)
	if ob.StopReason != StopCondition {
		t.Errorf("名前の条件: %+v", ob)
	}
}

// TestLocationForms は §14.6 の表のすべての書き方で mem.read が正しい位置を
// 読むことを確かめる（フェーズ 16 の完了判定）。
func TestLocationForms(t *testing.T) {
	s, _ := newGameSession(t, false)
	s.must("exec.run_until", map[string]any{"condition": "[frame_count].w == 5"}, nil)
	cases := []struct {
		loc  string
		n    int
		want string
	}{
		{"$0001", 1, "05"}, // player_x
		{"0x0001", 1, "05"},
		{"1", 1, "05"},
		{"player_x", 1, "05"},
		{"score", 3, "00 00 05"},
		{"enemy_x+2", 1, "25"}, // 5 + 2 * 16
		{"enemy_x+2*1", 1, "25"},
		{"[ptr]", 5, "41 47 45 4E 54"}, // message の "AGENT"
		{"bank0:$8000", 2, "A9 A0"},    // bank0_entry: LDA #$A0
		{"bank2:$8000", 2, "A9 A1"},    // 8 KiB 単位。bank2 は 16 KiB のバンク 1 の先頭
		{"prg:$4001", 1, "A1"},
		{"bank1_entry+1", 1, "A1"},
		{"ppu:$3F00", 1, "00"},
		{"pal:$00", 1, "00"},
		{"oam:$00", 1, "00"},
		{"chr:$0000", 1, "00"},
	}
	for _, c := range cases {
		var r struct {
			Hex []string `json:"hex"`
		}
		if err := s.call("mem.read", map[string]any{"loc": c.loc, "length": c.n}, &r); err != nil {
			t.Errorf("%s: %v", c.loc, err)
			continue
		}
		if r.Hex[0] != c.want {
			t.Errorf("%s = %s, 期待 %s", c.loc, r.Hex[0], c.want)
		}
	}
	err := s.call("mem.read", map[string]any{"loc": "player_z"}, nil)
	if err == nil || err.Kind != KindInvalidLocation || !strings.Contains(err.Message, "player_x") {
		t.Errorf("似た名前の候補 = %v", err)
	}
	// フェーズ 15 の Agent Command も名前を受け付ける。
	s.must("debug.watch.add", map[string]any{"loc": "player_x"}, nil)
	s.must("mem.freeze", map[string]any{"loc": "player_y", "value": 7}, nil)
	var bp BreakpointInfo
	s.must("debug.bp.add", map[string]any{"kind": "exec", "loc": "update_score", "condition": "player_x == 9"}, &bp)
	var ob Observation
	s.must("exec.step", map[string]any{"frames": 10}, &ob)
	if ob.StopReason != StopBreakpoint || !strings.HasPrefix(ob.CPU.Symbol, "update_score") || s.readByte("player_x") != 9 {
		t.Errorf("名前のブレークポイント: %+v %+v", ob, ob.CPU)
	}
	if ob.Watch["player_x"] != 9 {
		t.Errorf("ウォッチの名前 = %v", ob.Watch)
	}
}

// TestBankAwareSymbols は 2 つのバンクの同じ CPU アドレスで、現在のバンクに
// 応じて別の名前が返ることを確かめる（フェーズ 16 の完了判定）。
func TestBankAwareSymbols(t *testing.T) {
	s, _ := newGameSession(t, false)
	for range 4 {
		s.must("exec.step", nil, nil)
		bank := s.readByte("cur_bank")
		var r LookupResult
		s.must("symbol.lookup", map[string]any{"loc": "$8000"}, &r)
		want := []string{"bank0_entry", "bank1_entry"}[bank]
		if r.Name != want {
			t.Errorf("バンク %d の $8000 = %q, 期待 %q", bank, r.Name, want)
		}
		if r.Source == "" {
			t.Error("ソースの位置が無い")
		}
	}
	var si SymbolInfo
	s.must("symbol.lookup", map[string]any{"name": "bank1_entry"}, &si)
	if si.Space != "prg" || si.Offset != "$4000" || si.CPU != "$8000" || si.Origin != "dbg" {
		t.Errorf("bank1_entry = %+v", si)
	}
}

// TestSourceLines は既知の命令の位置で、正しいファイルと行が返ることを確かめる
// （フェーズ 16 の完了判定）。
func TestSourceLines(t *testing.T) {
	s, _ := newGameSession(t, false)
	var r struct {
		Lines []DisasmLine `json:"lines"`
	}
	s.must("cpu.disasm", map[string]any{"loc": "nmi", "count": 1}, &r)
	if r.Lines[0].Source != "game.s:89" || r.Lines[0].Label != "nmi" {
		t.Errorf("nmi の行 = %+v", r.Lines[0])
	}
	var ob Observation
	s.must("exec.run_until", map[string]any{"condition": "PC == update_mode", "check": "instruction"}, &ob)
	if ob.CPU.Symbol != "update_mode" || !strings.HasPrefix(ob.CPU.Source, "game.s:") {
		t.Errorf("Observation の CPU = %+v", ob.CPU)
	}
	s.must("exec.step_unit", map[string]any{"unit": "instruction"}, &ob)
	if ob.CPU.Symbol != "update_mode+2" {
		t.Errorf("名前+オフセット = %q", ob.CPU.Symbol)
	}
}

// TestSymbolCommands は symbol.* を確かめる。
func TestSymbolCommands(t *testing.T) {
	s, _ := newGameSession(t, false)
	var list struct {
		Symbols []SymbolInfo `json:"symbols"`
		Total   int          `json:"total"`
		Dbg     string       `json:"dbg"`
	}
	s.must("symbol.list", map[string]any{"filter": "update_", "match": "prefix"}, &list)
	// 前方一致は .proc の中の修飾名（update_score::loop など）も含む。
	if list.Total != 9 || !strings.HasSuffix(list.Dbg, "game.dbg") {
		t.Errorf("update_ の一覧 = %+v", list)
	}
	s.must("symbol.list", map[string]any{"kind": "constant"}, &list)
	if list.Total != 1 || list.Symbols[0].Value == nil || *list.Symbols[0].Value != 0x80 {
		t.Errorf("定数 = %+v", list.Symbols)
	}
	var si SymbolInfo
	s.must("symbol.set", map[string]any{"name": "lives", "loc": "$0310", "size": 1}, &si)
	if si.Origin != "user" || si.Space != "cpu" {
		t.Errorf("symbol.set = %+v", si)
	}
	if v := s.readByte("lives"); v != 0 {
		t.Errorf("付けた名前で読めない: %d", v)
	}
	if err := s.call("symbol.set", map[string]any{"name": "1bad", "loc": "$00"}, nil); err == nil {
		t.Error("不正な名前を受け付けた")
	}
	s.must("symbol.remove", map[string]any{"name": "lives"}, nil)
	if err := s.call("symbol.remove", map[string]any{"name": "reset"}, nil); err == nil {
		t.Error(".dbg の Symbol を外せた")
	}
	var loaded struct {
		Loaded int `json:"loaded"`
	}
	s.must("symbol.load", map[string]any{"path": "../../testdata/agent/game.dbg"}, &loaded)
	if loaded.Loaded < 20 {
		t.Errorf("symbol.load = %d", loaded.Loaded)
	}
}

// TestGameStateCommands は gamestate.define・list・save・remove と、
// 同じ ROM の Instance で定義を共有することを確かめる。
func TestGameStateCommands(t *testing.T) {
	s, rom := newGameSession(t, false)
	err := s.call("gamestate.define", map[string]any{"name": "x", "loc": "playr_x", "type": "u8"}, nil)
	if err == nil || err.Kind != KindInvalidLocation {
		t.Errorf("解決できない位置: %v", err)
	}
	if err := s.call("gamestate.define", map[string]any{"name": "x", "loc": "player_x", "type": "u9"}, nil); err == nil {
		t.Error("知らない型を受け付けた")
	}
	s.must("gamestate.define", map[string]any{"name": "x", "loc": "player_x", "type": "u8", "desc": "自機の X"}, nil)
	var fr ForkResult
	s.must("exec.step", nil, nil)
	s.must("instance.fork", nil, &fr)
	var gs map[string]any
	s.must("gamestate.get", map[string]any{"instance": string(fr.ID)}, &gs)
	if _, ok := gs["x"]; !ok {
		t.Error("Fork した Instance で定義を共有していない")
	}
	var saved struct {
		Path string `json:"path"`
	}
	s.must("gamestate.save", map[string]any{"instance": "i1"}, &saved)
	if saved.Path != strings.TrimSuffix(rom, ".nes")+".gamestate.json" {
		t.Errorf("保存先 = %s", saved.Path)
	}
	if _, err := os.Stat(saved.Path); err != nil {
		t.Error(err)
	}
	var list struct {
		Items []GameStateItemInfo `json:"items"`
		Path  string              `json:"path"`
	}
	s.must("gamestate.list", map[string]any{"instance": "i1"}, &list)
	if len(list.Items) != 1 || list.Items[0].Desc != "自機の X" || list.Path != saved.Path {
		t.Errorf("gamestate.list = %+v", list)
	}
	s.must("gamestate.remove", map[string]any{"instance": "i1", "name": "x"}, nil)
	s.must("gamestate.list", map[string]any{"instance": "i1"}, &list)
	if len(list.Items) != 0 {
		t.Errorf("削除後 = %+v", list.Items)
	}
}

// TestStaleDbgNote は ROM より古い .dbg を読み込まず、instance.create の結果で
// 知らせることを確かめる。
func TestStaleDbgNote(t *testing.T) {
	h := newTestHost(t, KindServe)
	s := &agentSession{t: t, h: h, conn: h.Connect()}
	rom := gameProject(t, false)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(strings.TrimSuffix(rom, ".nes")+".dbg", old, old)
	var info InstanceInfo
	s.must("instance.create", map[string]any{"rom": rom}, &info)
	if len(info.Notes) != 1 || !strings.Contains(info.Notes[0], "古い") {
		t.Errorf("知らせ = %v", info.Notes)
	}
	if err := s.call("mem.read", map[string]any{"loc": "player_x"}, nil); err == nil {
		t.Error("古い .dbg の名前を使えた")
	}
}
