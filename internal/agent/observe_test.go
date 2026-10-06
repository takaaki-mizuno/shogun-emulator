package agent

import (
	"bytes"
	"encoding/json"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// TestObservationDiffPerConnection は差分を接続ごとに持ち、他の接続の観測で
// 変わらないことを確かめる（設計書 14 編 §14.9.2）。
func TestObservationDiffPerConnection(t *testing.T) {
	s := newSession(t)
	s.warm()
	s.must("debug.watch.add", map[string]any{"loc": "$0010"}, nil)
	other := s.h.Connect()

	var a Observation
	s.must("obs.get", nil, &a)
	if c, ok := a.WatchChanged["$0010"]; !ok || c.From != nil {
		t.Fatalf("最初の観測は null からの変化にする: %+v", a.WatchChanged)
	}
	a = Observation{}
	s.must("obs.get", nil, &a)
	if a.WatchChanged != nil {
		t.Errorf("変わっていないのに差分がある: %+v", a.WatchChanged)
	}
	a = Observation{}
	s.must("exec.step", map[string]any{"frames": 2}, &a)
	c, ok := a.WatchChanged["$0010"]
	if !ok || c.From == nil || c.To.(float64)-c.From.(float64) != 2 {
		t.Errorf("2 フレーム後の差分 = %+v", a.WatchChanged)
	}
	// 他の接続が観測しても、この接続の差分は変わらない。
	var b Observation
	mustCall(t, s.h, other, "obs.get", nil, &b)
	if c := b.WatchChanged["$0010"]; c.From != nil {
		t.Errorf("他の接続の最初の観測 = %+v", b.WatchChanged)
	}
	s.must("exec.step", nil, &a)
	mustCall(t, s.h, other, "obs.get", nil, nil)
	a = Observation{}
	s.must("obs.get", nil, &a)
	if a.WatchChanged != nil {
		t.Errorf("他の接続の観測で差分が変わった: %+v", a.WatchChanged)
	}
	// 切断で前回値を捨てる。
	s.h.Disconnect(other)
	inst := s.h.Instances()[0]
	inst.mu.Lock()
	_, kept := inst.observed[other.ID]
	inst.mu.Unlock()
	if kept {
		t.Error("切断した接続の前回値が残る")
	}
}

// TestObserveInclude は付ける内容の選択を確かめる。
func TestObserveInclude(t *testing.T) {
	s := newSession(t)
	s.warm()
	var ob Observation
	s.must("exec.step", map[string]any{"observe": map[string]any{
		"include": []string{"image", "cpu_full", "sprites", "nametable", "ppu_writes", "gamestate"}, "scale": 1,
	}}, &ob)
	if ob.Image == nil || ob.CPUFull == nil || ob.Sprites == nil || ob.Nametable == nil || ob.PPUWrites == nil {
		t.Fatalf("付けた内容が足りない: %+v", ob)
	}
	img, err := png.Decode(bytes.NewReader(ob.Image.Data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 256 || b.Dy() != 240 {
		t.Errorf("scale 1 の画像の大きさ = %v", b)
	}
	if err := s.call("obs.get", map[string]any{"include": []string{"bogus"}}, nil); err == nil {
		t.Error("知らない include を受け付けた")
	}
	if err := s.call("obs.get", map[string]any{"scale": 9}, nil); err == nil {
		t.Error("範囲外の scale を受け付けた")
	}
}

// TestStructuredObservations は構造化された観測の形を確かめる
// （設計書 14 編 §14.10）。
func TestStructuredObservations(t *testing.T) {
	s := newSession(t)
	s.warm()
	s.must("exec.step", map[string]any{"frames": 2}, nil)

	var sp SpritesResult
	s.must("obs.sprites", nil, &sp)
	if sp.Size != "8x8" || len(sp.Sprites) != 1 || sp.HiddenCount != 63 {
		t.Fatalf("スプライト = %+v", sp)
	}
	s0 := sp.Sprites[0]
	if !s0.Sprite0 || s0.X != 0x40 || s0.Y != 0x50 || s0.ScreenY != 0x51 || s0.Tile != "$21" || s0.Palette != 1 || s0.Priority != "front" {
		t.Errorf("スプライト 0 = %+v", s0)
	}
	s.must("obs.sprites", map[string]any{"all": true}, &sp)
	if len(sp.Sprites) != 64 {
		t.Errorf("all の数 = %d", len(sp.Sprites))
	}

	var nt NametableResult
	s.must("obs.nametable", map[string]any{"table": 1}, &nt)
	if nt.Base != "$2400" || len(nt.Tiles) != 30 || len(strings.Fields(nt.Tiles[0])) != 32 || len(nt.Attributes) != 15 ||
		len(strings.Fields(nt.Attributes[0])) != 16 || nt.Mirroring != "horizontal" {
		t.Errorf("面 1 = %+v", nt)
	}
	s.must("obs.nametable", nil, &nt)
	if nt.Table != -1 || len(nt.Tiles) != 30 {
		t.Errorf("表示中の範囲 = %+v", nt)
	}
	if err := s.call("obs.nametable", map[string]any{"table": 4}, nil); err == nil {
		t.Error("table 4 を受け付けた")
	}

	var ph PatternsHashResult
	s.must("obs.patterns", nil, &ph)
	if len(ph.Tables[0]) != 256 || ph.Tables[0][0] != nil {
		t.Errorf("CHR が空のときのハッシュ = %v", ph.Tables[0][:2])
	}
	// CHR-RAM のタイル $21 に模様を書いて、ハッシュと色番号を確かめる。
	s.must("mem.write", map[string]any{"loc": "ppu:$0210", "value": []int{0x80, 0, 0, 0, 0, 0, 0, 0, 0x80}}, nil)
	s.must("obs.patterns", nil, &ph)
	if ph.Tables[0][0x21] == nil {
		t.Error("書いたタイルのハッシュが null")
	}
	var px struct {
		Tiles []TilePixels `json:"tiles"`
	}
	s.must("obs.patterns", map[string]any{"format": "pixels", "tiles": []int{0x21}}, &px)
	if len(px.Tiles) != 1 || px.Tiles[0].Rows[0] != "30000000" || px.Tiles[0].Rows[1] != "00000000" {
		t.Errorf("色番号 = %+v", px.Tiles)
	}
	var pimg struct {
		Image *Image `json:"image"`
	}
	s.must("obs.patterns", map[string]any{"format": "image", "palette": "sp1"}, &pimg)
	if img, err := png.Decode(bytes.NewReader(pimg.Image.Data)); err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 128 {
		t.Errorf("パターンの画像 = %v, %v", img, err)
	}

	var pal PaletteResult
	s.must("obs.palette", nil, &pal)
	if pal.Backdrop.Addr != "$3F00" || pal.Sprite[3][3].Addr != "$3F1F" || !strings.HasPrefix(pal.Backdrop.RGB, "#") {
		t.Errorf("パレット = %+v", pal)
	}

	var pw PPUWritesResult
	s.must("obs.ppu_writes", nil, &pw)
	if len(pw.Notes) == 0 || len(pw.Writes) != 0 {
		t.Errorf("記録を始めた直後 = %+v", pw)
	}
	s.must("exec.step", map[string]any{"frames": 2}, nil)
	s.must("obs.ppu_writes", nil, &pw)
	if !pw.Complete || len(pw.Writes) != 3 {
		t.Fatalf("記録 = %+v", pw)
	}
	if pw.Writes[0].Reg != "$2005" || pw.Writes[2].Reg != "$4014" || pw.Writes[2].Value != "$02" || pw.Writes[0].Scanline < 241 {
		t.Errorf("記録の中身 = %+v", pw.Writes)
	}

	var apu map[string]any
	s.must("obs.apu", nil, &apu)
	if _, ok := apu["Pulse"]; !ok {
		t.Errorf("APU = %v", apu)
	}

	var shot ScreenshotResult
	s.must("obs.screenshot", map[string]any{"scale": 2}, &shot)
	if img, err := png.Decode(bytes.NewReader(shot.Image.Data)); err != nil || img.Bounds().Dx() != 512 || shot.Width != 512 {
		t.Errorf("スクリーンショット = %+v, %v", shot.Width, err)
	}
}

// TestPPUWriteLogHookLifetime は PPU 書き込みの記録が要求の無いまま 600
// フレームたつと外れ、フックが nil に戻ることを確かめる。
func TestPPUWriteLogHookLifetime(t *testing.T) {
	s := newSession(t)
	s.quiet()
	inst := s.h.Instances()[0]
	hooked := func() bool {
		var on bool
		inst.Emu.WithDebugger(func(d *debug.Debugger) { on = d.Machine().Hooks().OnCPUWrite != nil })
		return on
	}
	if hooked() {
		t.Fatal("最初から書き込みのフックがある")
	}
	s.must("obs.ppu_writes", nil, nil)
	if !hooked() {
		t.Fatal("記録を始めてもフックが無い")
	}
	s.must("exec.step", map[string]any{"frames": 601}, nil)
	if hooked() {
		t.Error("600 フレームたってもフックが外れない")
	}
}

// TestMemAndCPU は mem と cpu の Agent Command を確かめる。
func TestMemAndCPU(t *testing.T) {
	s := newSession(t)
	s.warm()
	s.must("mem.write", map[string]any{"loc": "$0300", "value": "A9 00 FF"}, nil)
	var r struct {
		Loc   string   `json:"loc"`
		Hex   []string `json:"hex"`
		Bytes []int    `json:"bytes"`
	}
	s.must("mem.read", map[string]any{"loc": "$0300", "length": 3}, &r)
	if r.Hex[0] != "A9 00 FF" || r.Bytes[2] != 255 {
		t.Errorf("mem.read = %+v", r)
	}
	s.must("mem.read", map[string]any{"loc": "oam:$00", "length": 4}, &r)
	if r.Hex[0] != "50 21 01 40" || r.Loc != "oam:$0000" {
		t.Errorf("OAM = %+v", r)
	}
	var f struct {
		Matches []string `json:"matches"`
	}
	s.must("mem.find", map[string]any{"bytes": "A9 00 FF"}, &f)
	if len(f.Matches) == 0 || f.Matches[0] != "$0300" {
		t.Errorf("mem.find = %+v", f)
	}
	for _, bad := range []map[string]any{{"loc": "nope"}, {"loc": "xyz:$00"}, {"loc": "$FFFF", "length": 2}, {"loc": "$0000", "length": 5000}} {
		if err := s.call("mem.read", bad, nil); err == nil {
			t.Errorf("%v を受け付けた", bad)
		}
	}
	if err := s.call("mem.write", map[string]any{"loc": "$0300", "value": 300}, nil); err == nil {
		t.Error("256 以上の値を受け付けた")
	}

	var c CPUInfo
	s.must("cpu.set", map[string]any{"a": "$42", "x": 7}, &c)
	if c.A != "$42" || c.X != "$07" {
		t.Errorf("cpu.set = %+v", c)
	}
	s.must("cpu.get", nil, &c)
	if c.A != "$42" || len(c.Flags) != 8 {
		t.Errorf("cpu.get = %+v", c)
	}
	if err := s.call("cpu.set", map[string]any{"pc": 70000}, nil); err == nil {
		t.Error("範囲外の PC を受け付けた")
	}
	var cs struct {
		Frames []CallInfo `json:"frames"`
		Notes  []string   `json:"notes"`
	}
	s.must("cpu.callstack", nil, &cs)
	if len(cs.Notes) < 2 {
		t.Errorf("初回は記録を有効にした旨を知らせる: %+v", cs)
	}
	s.must("exec.run_until", map[string]any{"condition": "PC == " + romAddr("after_inc"), "check": "instruction"}, nil)
	s.must("cpu.callstack", nil, &cs)
	if len(cs.Frames) != 1 || cs.Frames[0].Kind != "nmi" {
		t.Errorf("NMI の中のコールスタック = %+v", cs.Frames)
	}
}

// TestFreeze は Freeze した値がフレームの途中の書き込みの後も固定されることと、
// 0 件のときフックが nil であること、セーブステートに含めないことを確かめる。
func TestFreeze(t *testing.T) {
	s := newSession(t)
	s.quiet()
	inst := s.h.Instances()[0]
	hooked := func() bool {
		var on bool
		inst.Emu.WithDebugger(func(d *debug.Debugger) { on = d.Machine().Hooks().OnCPUWrite != nil })
		return on
	}
	s.warm()
	var list []FreezeInfo
	s.must("mem.freeze", map[string]any{"loc": "$0810", "value": 7}, &list)
	if len(list) != 1 || list[0].Loc != "$0010" {
		t.Fatalf("ミラーを畳まない: %+v", list)
	}
	s.must("exec.step", map[string]any{"frames": 5}, nil)
	if v := s.readByte("$0010"); v != 7 {
		t.Errorf("NMI の INC の後の $10 = %d, 期待 7", v)
	}
	s.must("mem.freeze", map[string]any{"loc": "$6000", "value": 0x1234, "size": 2}, &list)
	if len(list) != 2 || list[1].Value[0] != "$34" {
		t.Errorf("2 バイトの Freeze = %+v", list)
	}
	for _, bad := range []map[string]any{{"loc": "$2000", "value": 1}, {"loc": "$07FF", "value": []int{1, 2}}, {"loc": "$0000", "value": 1, "size": 5}} {
		if err := s.call("mem.freeze", bad, nil); err == nil {
			t.Errorf("%v を受け付けた", bad)
		}
	}
	s.must("state.save", map[string]any{"name": "frozen"}, nil)
	s.must("mem.unfreeze", map[string]any{"all": true}, &list)
	if len(list) != 0 || hooked() {
		t.Errorf("すべて外した後 = %+v、フック %v", list, hooked())
	}
	s.must("state.load", map[string]any{"name": "frozen"}, nil)
	s.must("mem.freezes", nil, &list)
	if len(list) != 0 {
		t.Errorf("セーブステートに Freeze が含まれた: %+v", list)
	}
	// Fork で複製する。
	s.must("mem.freeze", map[string]any{"loc": "$0010", "value": 3}, nil)
	var fr ForkResult
	s.must("instance.fork", nil, &fr)
	s.must("mem.freezes", map[string]any{"instance": string(fr.ID)}, &list)
	if len(list) != 1 || list[0].Value[0] != "$03" {
		t.Errorf("Fork した Freeze = %+v", list)
	}
}

// TestStateAndROM は state と rom.load を確かめる。
func TestStateAndROM(t *testing.T) {
	s := newSession(t)
	s.warm()
	var saved struct {
		Name  string `json:"name"`
		Frame uint64 `json:"frame"`
	}
	s.must("state.save", nil, &saved)
	if !strings.HasPrefix(saved.Name, "auto-") {
		t.Errorf("名前の既定 = %q", saved.Name)
	}
	before := s.readByte("$0010")
	s.must("exec.step", map[string]any{"frames": 10}, nil)
	var ob Observation
	s.must("state.load", map[string]any{"name": saved.Name}, &ob)
	if ob.Frame != saved.Frame || s.readByte("$0010") != before {
		t.Errorf("読み込んだ状態 = フレーム %d、$10 = %d（期待 %d、%d）", ob.Frame, s.readByte("$0010"), saved.Frame, before)
	}
	path := filepath.Join(t.TempDir(), "a.state")
	s.must("state.save", map[string]any{"path": path}, nil)
	s.must("exec.step", map[string]any{"frames": 3}, nil)
	s.must("state.load", map[string]any{"path": path}, &ob)
	if ob.Frame != saved.Frame {
		t.Errorf("ファイルから読んだフレーム = %d", ob.Frame)
	}
	var list []StateInfo
	s.must("state.list", nil, &list)
	if len(list) != 1 {
		t.Errorf("state.list = %+v", list)
	}
	if err := s.call("state.load", map[string]any{"name": "nope"}, nil); err == nil {
		t.Error("無い名前を読み込めた")
	}
	if err := s.call("rom.load", map[string]any{"path": "/nonexistent.nes"}, nil); err == nil || err.Kind != KindIOError {
		t.Errorf("無い ROM: %v", err)
	}
	s.must("rom.load", map[string]any{"path": writeTestROM(t)}, &ob)
	var info []InstanceInfo
	s.must("instance.list", nil, &info)
	if info[0].ROM != "agent" || !info[0].Paused || info[0].Frame != 0 {
		t.Errorf("rom.load の後 = %+v", info[0])
	}
	s.must("state.list", nil, &list)
	if len(list) != 0 {
		t.Error("別の ROM の保管場所が残る")
	}
}

// TestMovieConflict はムービーの再生中に入力の指定と書き換えを断ることを
// 確かめる（設計書 14 編 §14.7.3）。
func TestMovieConflict(t *testing.T) {
	s := newSession(t)
	inst := s.h.Instances()[0]
	path := filepath.Join(t.TempDir(), "m.movie")
	if err := inst.Emu.StartRecordingMovie(path); err != nil {
		t.Fatal(err)
	}
	s.must("exec.step", map[string]any{"frames": 20, "input": "A"}, nil)
	if err := inst.Emu.StopRecordingMovie(); err != nil {
		t.Fatal(err)
	}
	if err := inst.Emu.PlayMovieFile(path); err != nil {
		t.Fatal(err)
	}
	if !inst.Emu.Status().Paused {
		t.Error("Agent-Paced の Instance がムービーの再生で走り出した")
	}
	if err := s.call("exec.step", map[string]any{"input": "B"}, nil); err == nil || err.Kind != KindMovieConflict {
		t.Errorf("再生中の入力: %v", err)
	}
	if err := s.call("mem.write", map[string]any{"loc": "$0300", "value": 1}, nil); err == nil || err.Kind != KindMovieConflict {
		t.Errorf("再生中の書き込み: %v", err)
	}
	var ob Observation
	s.must("exec.step", map[string]any{"frames": 2}, &ob)
	if ob.StopReason != StopFramesDone {
		t.Errorf("再生中の入力なしの進行 = %+v", ob)
	}
	s.must("obs.get", nil, nil)
}

// determinismScript は決定論テストの Agent Command の列。
var determinismScript = []struct {
	method string
	params string
}{
	{"exec.run_until", `{"condition":"[$0010] >= 1"}`},
	{"exec.step", `{"frames":3,"input":"A"}`},
	{"exec.input_sequence", `{"steps":[{"frames":2,"input":"Up+B"},{"frames":1,"input":"$81"}],"observe_each":true}`},
	{"mem.write", `{"loc":"$0300","value":[1,2,3]}`},
	{"cpu.set", `{"y":"$33"}`},
	{"mem.freeze", `{"loc":"$0400","value":9}`},
	{"state.save", `{"name":"s1"}`},
	{"exec.run_until", `{"condition":"[$0010] == 20","observe":{"include":["image","sprites","nametable","cpu_full"]}}`},
	{"exec.step_unit", `{"unit":"instruction","count":5}`},
	{"state.load", `{"name":"s1"}`},
	{"exec.step", `{"frames":4,"input":"Start","observe":{"include":["image","ppu_writes"]}}`},
	{"exec.step", `{"frames":2,"observe":{"include":["ppu_writes","image"]}}`},
	{"obs.get", `{"full":true,"include":["image","cpu_full"]}`},
}

// runScript は列を実行して各結果の JSON を返す。instance 引数で対象を選ぶ。
func runScript(t *testing.T, s *agentSession, instance string) []string {
	t.Helper()
	var out []string
	for _, st := range determinismScript {
		var p map[string]any
		if err := json.Unmarshal([]byte(st.params), &p); err != nil {
			t.Fatal(err)
		}
		p["instance"] = instance
		raw := string(s.raw(st.method, p))
		// Instance の ID は Instance ごとに違う。比べない。
		raw = strings.ReplaceAll(raw, `"instance":"`+instance+`"`, `"instance":"*"`)
		out = append(out, raw)
	}
	return out
}

// TestDeterministicObservations は同じ Agent Command の列を 2 回実行したときと、
// Fork した 2 つの Instance に同じ列を与えたときに、すべての Observation
// （画像を含む）が一致することを確かめる（フェーズ 15 の完了判定）。
func TestDeterministicObservations(t *testing.T) {
	s := newSession(t)
	rom := writeNMIROM(t)
	s.must("instance.create", map[string]any{"rom": rom, "deterministic": true}, nil)
	a := runScript(t, s, "i1")
	b := runScript(t, s, "i2")
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("%d 番目（%s）が一致しない:\n%s\n%s", i, determinismScript[i].method, a[i], b[i])
		}
	}
	// Fork した 2 つの Instance に同じ列を与える。
	s.must("instance.fork", map[string]any{"instance": "i1"}, nil)
	s.must("instance.fork", map[string]any{"instance": "i1"}, nil)
	c := runScript(t, s, "i3")
	d := runScript(t, s, "i4")
	for i := range c {
		if c[i] != d[i] {
			t.Errorf("Fork: %d 番目（%s）が一致しない:\n%s\n%s", i, determinismScript[i].method, c[i], d[i])
		}
	}
}
