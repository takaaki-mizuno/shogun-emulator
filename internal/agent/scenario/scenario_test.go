package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/region"
	"github.com/takaakimizuno/shogun-emulator/internal/video"
)

// gameStateDef はテスト用 ROM（testdata/agent）の Game State Definition。
const gameStateDef = `{"version":1,"items":[
  {"name":"mode","loc":"mode","type":"u8","enum":{"0":"title","1":"play","2":"dead","3":"clear"}},
  {"name":"player_x","loc":"player_x","type":"u8"},
  {"name":"score","loc":"score","type":"bcd","size":3},
  {"name":"frames","loc":"frame_count","type":"u16","hidden":true}
]}`

// project はテスト用 ROM をディレクトリに写す。ROM は rom/ の下に置き、
// Scenario からの相対パスを確かめる。
func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	romDir := filepath.Join(dir, "rom")
	if err := os.MkdirAll(romDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"game.nes", "game.dbg"} {
		data, err := os.ReadFile(filepath.Join("../../../testdata/agent", f))
		if err != nil {
			t.Fatalf("テスト用 ROM が無い: %v", err)
		}
		if err := os.WriteFile(filepath.Join(romDir, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	os.Chtimes(filepath.Join(romDir, "game.nes"), now, now)
	os.Chtimes(filepath.Join(romDir, "game.dbg"), now.Add(time.Second), now.Add(time.Second))
	if err := os.WriteFile(filepath.Join(romDir, "defs.json"), []byte(gameStateDef), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// testHost は headless の Host とプロセス内のクライアントを作る。
func testHost(t *testing.T) (*agent.Host, *rpc.Client) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	h := agent.NewHost(agent.Options{
		Kind: agent.KindServe, Server: "shogun test", MaxInstances: 4,
		EmuConfig: emu.Config{
			Emulation: cfg.Emulation, Input: cfg.Input, Audio: cfg.Audio, Paths: cfg.Paths,
			State: cfg.State, Movie: cfg.Movie, Debug: cfg.Debug,
			Dirs: config.Paths{Config: dir, Data: dir, Cache: dir, Logs: dir, Screenshots: dir},
		},
	})
	t.Cleanup(h.Close)
	c, closeFn, err := rpc.InProcess(context.Background(), h, "scenario test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	return h, c
}

func writeScenario(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, "tests", name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runOne(t *testing.T, path string, opts Options) FileResult {
	t.Helper()
	_, c := testHost(t)
	return NewRunner(c, opts).RunFile(context.Background(), path)
}

const header = `rom: ../rom/game.nes
gamestate: ../rom/defs.json
`

// TestPassingScenario は合格する Scenario を確かめる。モードは 64 フレーム
// ごとに title・play・dead・clear と変わる。
func TestPassingScenario(t *testing.T) {
	dir := project(t)
	p := writeScenario(t, dir, "pass.yaml", "name: タイトルからプレイ\n"+header+`init: {ram_init: zero}
steps:
  - exec.step: {frames: 10}
  - assert_stop: frames_done
  - assert: "game.mode == 'title'"
  - exec.run_until: {condition: "game.mode == 'play'", max_frames: 300}
  - assert_stop: condition
  - assert: "game.mode == 'play' && player_x > 0"
  - mem.write: {loc: player_x, value: 0x10}
  - assert_mem: {loc: player_x, equals: [0x10]}
  - exec.input_sequence:
      steps: [{frames: 2, input: Start}, {frames: 3, input: ""}]
  - assert_stop: sequence_done
  - assert_no_diagnostics: [stack_overflow, stack_underflow, nmi_reentry]
`)
	res := runOne(t, p, Options{})
	if res.Err != nil {
		t.Fatalf("読み込みの誤り: %v", res.Err)
	}
	if len(res.Cases) != 1 || res.Cases[0].Status != Passed {
		var buf bytes.Buffer
		WriteFileResult(&buf, res)
		t.Fatalf("合格しない:\n%s", buf.String())
	}
	c := res.Cases[0]
	if c.Name != "タイトルからプレイ" || c.Final["mode"] != "play" {
		t.Errorf("結果 = %+v", c)
	}
	if code := ExitCode([]FileResult{res}); code != ExitOK {
		t.Errorf("終了コード = %d", code)
	}
}

// TestFailingAssert は式のアサーションの失敗で、失敗のステップ・式・実際の
// 値・要約が出て、Repro が書かれることを確かめる。
func TestFailingAssert(t *testing.T) {
	dir := project(t)
	p := writeScenario(t, dir, "fail.yaml", header+`steps:
  - exec.step: {frames: 5}
  - assert: "game.mode == 'play'"
  - exec.step: {frames: 1}
`)
	reproDir := filepath.Join(dir, "out")
	res := runOne(t, p, Options{ReproDir: reproDir})
	c := res.Cases[0]
	if c.Status != Failed || c.Failure == nil {
		t.Fatalf("失敗しない: %+v", c)
	}
	f := c.Failure
	if f.Step != 2 || f.Line != 5 || f.Expr != "game.mode == 'play'" || !strings.Contains(f.Actual, `game.mode = "title"`) {
		t.Errorf("失敗の内容 = %+v", f)
	}
	if !strings.Contains(f.Summary, "frame 5") || !strings.Contains(f.Summary, "frames_done") {
		t.Errorf("要約 = %q", f.Summary)
	}
	if c.Frame != 5 {
		t.Errorf("失敗の後も進んだ: frame %d", c.Frame)
	}
	if code := ExitCode([]FileResult{res}); code != ExitFailed {
		t.Errorf("終了コード = %d", code)
	}
	want := filepath.Join(reproDir, "fail-1.repro")
	if c.Repro != want {
		t.Fatalf("Repro = %q（%s）", c.Repro, c.ReproErr)
	}
	data, err := os.ReadFile(filepath.Join(want, "repro.shgm"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := movie.Decode(data)
	if err != nil || m.Header.Author != emu.ReproAuthor || m.Header.TotalFrames != 5 {
		t.Fatalf("Repro のムービー = %+v, %v", m, err)
	}
	if readme, _ := os.ReadFile(filepath.Join(want, "README.md")); !strings.Contains(string(readme), "ステップ 2") {
		t.Errorf("README に失敗の内容が無い:\n%s", readme)
	}
	// GUI と同じ通常のエミュレータで再生できる。
	cfg := config.Default()
	e := emu.New(emu.Config{
		Emulation: cfg.Emulation, Input: cfg.Input, State: cfg.State, Movie: cfg.Movie, Debug: cfg.Debug,
		Dirs:     config.Paths{Data: t.TempDir()},
		NewPacer: func(*region.Region) emu.Pacer { return emu.NewNoPacer() },
	})
	e.Start()
	defer e.Stop()
	if err := e.LoadROM(filepath.Join(dir, "rom", "game.nes")); err != nil {
		t.Fatal(err)
	}
	if err := e.PlayMovieFile(want); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := e.Status()
		if st.Paused && !st.Movie.Playing {
			if st.Frames != 5 {
				t.Errorf("再生の終わりのフレーム = %d", st.Frames)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("再生が終わらない")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.DesyncError(); err != nil {
		t.Errorf("desync: %v", err)
	}
}

// TestAssertStopAndMemFailures は assert_stop と assert_mem の失敗を確かめる。
func TestAssertStopAndMemFailures(t *testing.T) {
	dir := project(t)
	cases := map[string]string{
		"進行が無い":       "  - assert_stop: frames_done\n",
		"Stop Reason": "  - exec.step: {frames: 1}\n  - assert_stop: condition\n",
		"メモリ":         "  - exec.step: {frames: 3}\n  - assert_mem: {loc: player_x, equals: [9, 9]}\n",
		"コマンドの誤り":     "  - exec.run_until: {condition: \"no_such_name == 1\"}\n",
	}
	for name, steps := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeScenario(t, dir, "x.yaml", header+"steps:\n"+steps)
			res := runOne(t, p, Options{ReproDir: filepath.Join(dir, "out")})
			if res.Err != nil || res.Cases[0].Status != Failed {
				t.Fatalf("失敗しない: %+v %v", res.Cases, res.Err)
			}
			t.Logf("%+v", res.Cases[0].Failure)
		})
	}
}

// TestScreenAssert は --update-golden でお手本を作り、2 回目に合格すること、
// 一致しないとき .actual.png を書くこと、既定のパレットに無い色を断ることを
// 確かめる。
func TestScreenAssert(t *testing.T) {
	dir := project(t)
	body := header + `steps:
  - exec.step: {frames: 30}
  - assert_screen: {golden: golden/title.png}
`
	p := writeScenario(t, dir, "screen.yaml", body)
	golden := filepath.Join(dir, "tests", "golden", "title.png")

	res := runOne(t, p, Options{})
	if res.Cases[0].Status != Failed || !strings.Contains(res.Cases[0].Failure.Message, "--update-golden") {
		t.Fatalf("お手本が無いのに合格した: %+v", res.Cases[0])
	}
	res = runOne(t, p, Options{UpdateGolden: true})
	if c := res.Cases[0]; c.Status != Passed || len(c.Updated) != 1 || c.Updated[0] != golden {
		t.Fatalf("お手本を書かない: %+v", c)
	}
	res = runOne(t, p, Options{})
	if res.Cases[0].Status != Passed {
		t.Fatalf("2 回目が合格しない: %+v", res.Cases[0].Failure)
	}

	// 1 画素を既定のパレットの別の色にする。
	img := decodePNG(t, golden)
	w := img.Bounds().Dx()
	other := img.At(0, 0)
	for _, v := range []uint16{0x30, 0x16, 0x0F} {
		c := defaultPaletteColor(v)
		if rgbOf(c) != rgbOf(other) {
			other = c
			break
		}
	}
	mod := image.NewRGBA(img.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < w; x++ {
			mod.Set(x, y, img.At(x, y))
		}
	}
	mod.Set(5, 7, other)
	encodePNG(t, golden, mod)
	res = runOne(t, p, Options{})
	c := res.Cases[0]
	if c.Status != Failed || !strings.Contains(c.Failure.Message, "1 画素") || !strings.Contains(c.Failure.Message, "(5, 7)") {
		t.Fatalf("1 画素の違い: %+v", c.Failure)
	}
	if _, err := os.Stat(strings.TrimSuffix(golden, ".png") + ".actual.png"); err != nil {
		t.Errorf(".actual.png が無い: %v", err)
	}
	// max_diff_pixels で許す。
	p2 := writeScenario(t, dir, "screen2.yaml", strings.Replace(body, "golden/title.png}", "golden/title.png, max_diff_pixels: 1}", 1))
	if res := runOne(t, p2, Options{}); res.Cases[0].Status != Passed {
		t.Errorf("max_diff_pixels: 1 で合格しない: %+v", res.Cases[0].Failure)
	}

	// 既定のパレットに無い色。
	mod.Set(5, 7, colorRGB(1, 2, 3))
	encodePNG(t, golden, mod)
	res = runOne(t, p, Options{})
	if c := res.Cases[0]; c.Status != Failed || !strings.Contains(c.Failure.Message, "既定のパレットに無い") {
		t.Fatalf("パレットに無い色: %+v", c.Failure)
	}
	// --update-golden で直る。
	res = runOne(t, p, Options{UpdateGolden: true})
	if res.Cases[0].Status != Passed || len(res.Cases[0].Updated) != 1 {
		t.Fatalf("上書きしない: %+v", res.Cases[0])
	}
}

// TestLoadErrors は不正な Scenario を行番号とステップの番号つきで断ることを
// 確かめる。
func TestLoadErrors(t *testing.T) {
	dir := project(t)
	cases := []struct {
		name, body, want string
		line, step       int
	}{
		{"無い Agent Command", header + "steps:\n  - exec.step: {frames: 1}\n  - exec.jump: {frames: 1}\n", "Agent Command でもアサーションでもない", 5, 2},
		{"引数の型", header + "steps:\n  - exec.step: {frames: \"many\"}\n", "exec.step", 4, 1},
		{"知らない引数", header + "steps:\n  - exec.step: {frams: 1}\n", "frams", 4, 1},
		{"Session", header + "steps:\n  - instance.create: {rom: x.nes}\n", "Scenario では使えない", 4, 1},
		{"GUI のみ", header + "steps:\n  - exec.run:\n", "headless では使えない", 4, 1},
		{"instance", header + "steps:\n  - exec.step: {frames: 1, instance: i1}\n", "instance", 4, 1},
		{"Stop Reason", header + "steps:\n  - assert_stop: done\n", "Stop Reason ではない", 4, 1},
		{"キーが 2 つ", header + "steps:\n  - {exec.step: {frames: 1}, assert: \"1\"}\n", "キーを 1 つだけ", 4, 1},
		{"rom が無い", "steps:\n  - exec.step: {frames: 1}\n", "rom を指定する", 1, 0},
		{"知らない項目", header + "stepz: []\n", "知らない項目", 3, 0},
		{"構文", header + "steps:\n  - exec.step: {frames: 1\n", "", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeScenario(t, dir, "bad.yaml", tc.body)
			_, err := Load(p, nil)
			le, ok := err.(*LoadError)
			if !ok {
				t.Fatalf("誤りにならない: %v", err)
			}
			if !strings.Contains(le.Msg, tc.want) || (tc.line > 0 && le.Line != tc.line) || le.Step != tc.step {
				t.Errorf("誤り = %+v（期待 %q 行 %d ステップ %d）", le, tc.want, tc.line, tc.step)
			}
			if !strings.HasPrefix(le.Error(), p) {
				t.Errorf("ファイル名が無い: %s", le.Error())
			}
		})
	}
	res := runOne(t, writeScenario(t, dir, "bad.yaml", cases[0].body), Options{})
	if res.Err == nil || ExitCode([]FileResult{res}) != ExitInvalidFile {
		t.Errorf("不正なファイルの終了コード = %d", ExitCode([]FileResult{res}))
	}
}

// TestLoadForms は JSON・Scenario の列・パスの解決・既定値・timeout_ms の警告を
// 確かめる。
func TestLoadForms(t *testing.T) {
	dir := project(t)
	p := writeScenario(t, dir, "list.json", `[
  {"name": "a", "rom": "../rom/game.nes", "steps": [{"exec.step": {"frames": 1}}]},
  {"rom": "../rom/game.nes", "start": {"state": "s.state"}, "init": {"deterministic": false, "ram_init": "ff"},
   "diagnostics": {"enable": "all", "fail_on": ["stack_overflow"]},
   "steps": [{"exec.run_until": {"condition": "1", "timeout_ms": 10}}, {"assert_no_diagnostics": ["stack_overflow"]}]}
]`)
	f, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Scenarios) != 2 {
		t.Fatalf("Scenario の数 = %d", len(f.Scenarios))
	}
	a, b := f.Scenarios[0], f.Scenarios[1]
	if a.Name != "a" || !a.Init.Deterministic || a.ROM != filepath.Join(dir, "rom", "game.nes") {
		t.Errorf("1 つ目 = %+v", a)
	}
	if b.Name != "list.json#2" || b.Init.Deterministic || b.Init.RAMInit != "ff" || b.State != filepath.Join(dir, "tests", "s.state") {
		t.Errorf("2 つ目 = %+v", b)
	}
	if !b.Diagnostics.All || len(b.Diagnostics.FailOn) != 1 || len(b.Steps[1].Kinds) != 1 {
		t.Errorf("diagnostics = %+v %+v", b.Diagnostics, b.Steps[1])
	}
	if len(f.Warnings) != 1 || !strings.Contains(f.Warnings[0], "timeout_ms") {
		t.Errorf("警告 = %v", f.Warnings)
	}
	if _, err := Load(writeScenario(t, dir, "x.txt", "{}"), nil); err == nil {
		t.Error("拡張子を確かめない")
	}
}

// TestSetupError は ROM が無いとき読み込みの失敗（終了コード 1）にすることを
// 確かめる。
func TestSetupError(t *testing.T) {
	dir := project(t)
	p := writeScenario(t, dir, "norom.yaml", "rom: ../rom/missing.nes\nsteps:\n  - exec.step: {frames: 1}\n")
	res := runOne(t, p, Options{})
	if res.Cases[0].Status != SetupError || ExitCode([]FileResult{res}) != ExitSetupError {
		t.Fatalf("結果 = %+v", res.Cases[0])
	}
}

// TestFailOn は diagnostics.fail_on の種類の Diagnostic を検知したステップで
// 失敗し、assert_no_diagnostics が開始からの Diagnostic を数えることを
// 確かめる。Diagnostic の検知はフェーズ 20 で作るため、イベントを直接積む。
func TestFailOn(t *testing.T) {
	dir := project(t)
	h, c := testHost(t)
	r := NewRunner(c, Options{})
	sc := &Scenario{Name: "d", Diagnostics: Diagnostics{FailOn: []string{"stack_overflow"}}}
	x := &run{r: r, ctx: context.Background(), file: &File{Path: "d.yaml"}, sc: sc, res: &CaseResult{}}
	var info agent.InstanceInfo
	if err := x.call("instance.create", map[string]any{"rom": filepath.Join(dir, "rom", "game.nes")}, &info); err != nil {
		t.Fatal(err)
	}
	x.inst = string(info.ID)
	inst, _ := h.Instance(info.ID)
	h.PushEvent(inst, agent.EventDiagnostic, 3, map[string]any{"kind": "vram_access_during_render"})
	if f := x.collectDiagnostics(Step{Index: 1}); f != nil {
		t.Fatalf("fail_on に無い種類で失敗した: %+v", f)
	}
	if f := x.step(Step{Index: 2, Name: AssertNoDiagnostics, Kinds: []string{"stack_overflow"}}); f != nil {
		t.Errorf("種類を絞った assert_no_diagnostics: %+v", f)
	}
	if f := x.step(Step{Index: 2, Name: AssertNoDiagnostics}); f == nil || !strings.Contains(f.Actual, "vram_access_during_render") {
		t.Errorf("assert_no_diagnostics: %+v", f)
	}
	h.PushEvent(inst, agent.EventDiagnostic, 4, map[string]any{"kind": "stack_overflow"})
	if f := x.collectDiagnostics(Step{Index: 3}); f == nil || !strings.Contains(f.Message, "stack_overflow") {
		t.Errorf("fail_on で失敗しない: %+v", f)
	}
}

// TestFailOnDetected は実際に検知した Diagnostic で diagnostics.fail_on が
// 失敗させることを確かめる。テスト用 ROM（testdata/agent）は RAM を初期化
// せずに読むため uninitialized_ram_read を起こす。
func TestFailOnDetected(t *testing.T) {
	dir := project(t)
	p := writeScenario(t, dir, "failon.yaml", header+`diagnostics: {enable: all, fail_on: [uninitialized_ram_read]}
steps:
  - exec.step: {frames: 5}
  - exec.step: {frames: 5}
`)
	res := runOne(t, p, Options{ReproDir: filepath.Join(dir, "out")})
	c := res.Cases[0]
	if c.Status != Failed || c.Failure.Step != 1 || !strings.Contains(c.Failure.Message, "uninitialized_ram_read") {
		t.Fatalf("fail_on で失敗しない: %+v", c.Failure)
	}
	for _, n := range c.Notes {
		if strings.Contains(n, "diag.configure が無い") {
			t.Errorf("diag.configure があるのに知らせた: %s", n)
		}
	}
}

// TestDeterministic は同じ Scenario を 2 回実行して結果が一致することを
// 確かめる。
func TestDeterministic(t *testing.T) {
	dir := project(t)
	p := writeScenario(t, dir, "det.yaml", header+`init: {ram_init: random}
steps:
  - exec.input_sequence: {steps: [{frames: 20, input: Right}, {frames: 1, input: A}, {frames: 15}]}
  - exec.step_unit: {unit: scanline, count: 50}
  - exec.step: {frames: 7}
`)
	a, b := runOne(t, p, Options{}), runOne(t, p, Options{})
	ja, _ := json.Marshal(a.Cases[0].Final)
	jb, _ := json.Marshal(b.Cases[0].Final)
	if a.Cases[0].Frame != b.Cases[0].Frame || string(ja) != string(jb) || a.Cases[0].Status != Passed {
		t.Errorf("結果が違う: %d %s / %d %s", a.Cases[0].Frame, ja, b.Cases[0].Frame, jb)
	}
}

// TestJUnit は JUnit XML の構成を確かめる。
func TestJUnit(t *testing.T) {
	dir := project(t)
	pass := writeScenario(t, dir, "p.yaml", header+"steps:\n  - exec.step: {frames: 1}\n")
	fail := writeScenario(t, dir, "f.yaml", header+"steps:\n  - exec.step: {frames: 1}\n  - assert: \"frame == 99\"\n")
	bad := writeScenario(t, dir, "b.yaml", header+"steps:\n  - nope: {}\n")
	_, c := testHost(t)
	r := NewRunner(c, Options{ReproDir: filepath.Join(dir, "out")})
	var results []FileResult
	for _, p := range []string{pass, fail, bad} {
		results = append(results, r.RunFile(context.Background(), p))
	}
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, results); err != nil {
		t.Fatal(err)
	}
	var doc junitSuites
	if err := xml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("XML として読めない: %v\n%s", err, buf.String())
	}
	if doc.Tests != 3 || doc.Failures != 1 || doc.Errors != 1 || len(doc.Suites) != 3 {
		t.Fatalf("集計 = %+v", doc)
	}
	f := doc.Suites[1].Cases[0]
	if f.Failure == nil || !strings.Contains(f.Failure.Message, "ステップ 2") || !strings.Contains(f.SystemOut, "Repro:") {
		t.Errorf("failure = %+v", f)
	}
	if e := doc.Suites[2].Cases[0].Error; e == nil || e.Type != "invalid_scenario" {
		t.Errorf("error = %+v", doc.Suites[2].Cases[0])
	}
	if ExitCode(results) != ExitInvalidFile {
		t.Errorf("終了コード = %d", ExitCode(results))
	}
}

// TestExportRoundTrip は Instance で操作した後に scenario.export で書き出し、
// アサーションを足して shogun run と同じ実行器で再実行すると、同じ場面に
// 着くことを確かめる（フェーズ 19 の完了判定）。from: mark も確かめる。
func TestExportRoundTrip(t *testing.T) {
	dir := project(t)
	_, c := testHost(t)
	ctx := context.Background()
	rom := filepath.Join(dir, "rom", "game.nes")
	var info agent.InstanceInfo
	if err := c.Call(ctx, "instance.create", map[string]any{"rom": rom, "deterministic": true, "ram_init": "pattern"}, &info); err != nil {
		t.Fatal(err)
	}
	do := func(method string, params map[string]any, out any) {
		t.Helper()
		params["instance"] = info.ID
		if err := c.Call(ctx, method, params, out); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	do("gamestate.load", map[string]any{"path": filepath.Join(dir, "rom", "defs.json")}, nil)
	do("exec.input_sequence", map[string]any{"steps": []map[string]any{{"frames": 12, "input": "Right"}, {"frames": 3}}}, nil)
	do("mem.read", map[string]any{"loc": "$0300", "length": 4}, nil) // 観測は書き出さない
	do("scenario.mark", map[string]any{}, nil)
	do("mem.write", map[string]any{"loc": "player_x", "value": 0x40}, nil)
	do("exec.run_until", map[string]any{"condition": "game.mode == 'play'", "max_frames": 200}, nil)
	do("debug.bp.add", map[string]any{"kind": "exec", "loc": "bank0_entry"}, nil)
	do("exec.step_unit", map[string]any{"unit": "scanline", "count": 30}, nil)
	_ = c.Call(ctx, "exec.step", map[string]any{"instance": info.ID, "frames": -1}, nil) // 誤りは書き出さない

	var want agent.Observation
	do("obs.get", map[string]any{"include": []string{"gamestate"}}, &want)
	var ram struct {
		Bytes []int `json:"bytes"`
	}
	do("mem.read", map[string]any{"loc": "$0000", "length": 2048}, &ram)
	ramYAML, _ := json.Marshal(ram.Bytes)

	for _, from := range []string{"start", "mark"} {
		t.Run(from, func(t *testing.T) {
			path := filepath.Join(dir, "tests", "exported-"+from+".yaml")
			var res agent.ScenarioExportResult
			do("scenario.export", map[string]any{"path": path, "from": from}, &res)
			data, _ := os.ReadFile(path)
			text := string(data)
			if strings.Contains(text, "mem.read") || strings.Contains(text, "instance") || strings.Contains(text, "frames\":-1") {
				t.Errorf("余計なステップがある:\n%s", text)
			}
			if !strings.Contains(text, `rom: "../rom/game.nes"`) || !strings.Contains(text, `gamestate: "../rom/defs.json"`) || !strings.Contains(text, `"ram_init":"pattern"`) {
				t.Errorf("設定が無い:\n%s", text)
			}
			wantSteps := map[string]int{"start": 5, "mark": 4}[from]
			if res.Steps != wantSteps {
				t.Errorf("ステップの数 = %d、期待 %d:\n%s", res.Steps, wantSteps, text)
			}
			if from == "mark" {
				if !strings.Contains(text, `start: {state: "exported-mark.state"}`) {
					t.Errorf("印のセーブステートから始まらない:\n%s", text)
				}
			} else if !strings.Contains(text, "start: power-on") {
				t.Errorf("電源投入から始まらない:\n%s", text)
			}
			// エージェントがアサーションを足す。
			text += "  - assert: \"game.mode == '" + want.GameState["mode"].(string) + "'\"\n"
			text += "  - assert_mem: {loc: \"$0000\", equals: " + string(ramYAML) + "}\n"
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			got := runOne(t, path, Options{})
			if got.Err != nil {
				t.Fatalf("書き出した Scenario を読めない: %v", got.Err)
			}
			cr := got.Cases[0]
			if cr.Status != Passed {
				var buf bytes.Buffer
				WriteFileResult(&buf, got)
				t.Fatalf("再実行が合格しない:\n%s\n%s", buf.String(), text)
			}
			wj, _ := json.Marshal(want.GameState)
			gj, _ := json.Marshal(cr.Final)
			if cr.Frame != want.Frame || string(wj) != string(gj) {
				t.Errorf("再実行の場面 = %d %s、期待 %d %s", cr.Frame, gj, want.Frame, wj)
			}
		})
	}

	// JSON でも書ける。
	jpath := filepath.Join(dir, "tests", "exported.json")
	do("scenario.export", map[string]any{"path": jpath, "name": "json"}, nil)
	f, err := Load(jpath, nil)
	if err != nil || f.Scenarios[0].Name != "json" || len(f.Scenarios[0].Steps) != 5 {
		t.Fatalf("JSON の書き出し: %+v %v", f, err)
	}
}

// TestExportAfterStateLoad は state.load の後を開始とすることと、印の後の
// state.load を断ることを確かめる。
func TestExportAfterStateLoad(t *testing.T) {
	dir := project(t)
	_, c := testHost(t)
	ctx := context.Background()
	var info agent.InstanceInfo
	if err := c.Call(ctx, "instance.create", map[string]any{"rom": filepath.Join(dir, "rom", "game.nes"), "deterministic": true}, &info); err != nil {
		t.Fatal(err)
	}
	do := func(method string, params map[string]any) error {
		params["instance"] = info.ID
		return c.Call(ctx, method, params, nil)
	}
	must := func(method string, params map[string]any) {
		t.Helper()
		if err := do(method, params); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	must("exec.step", map[string]any{"frames": 10})
	must("state.save", map[string]any{"name": "a"})
	must("scenario.mark", map[string]any{})
	must("exec.step", map[string]any{"frames": 10})
	must("state.load", map[string]any{"name": "a"})
	must("exec.step", map[string]any{"frames": 3})
	path := filepath.Join(dir, "tests", "after.yaml")
	var res agent.ScenarioExportResult
	params := map[string]any{"path": path, "instance": info.ID}
	if err := c.Call(ctx, "scenario.export", params, &res); err != nil {
		t.Fatal(err)
	}
	if res.Steps != 1 || !strings.HasSuffix(res.Start, "after.state") {
		t.Errorf("結果 = %+v", res)
	}
	got := runOne(t, path, Options{})
	if got.Err != nil || got.Cases[0].Status != Passed || got.Cases[0].Frame != 13 {
		t.Errorf("再実行 = %+v %v", got.Cases, got.Err)
	}
	err := do("scenario.export", map[string]any{"path": path, "from": "mark"})
	if kind, _, _ := rpc.ErrorKind(err); kind != agent.KindInvalidParams {
		t.Errorf("印の後の state.load を断らない: %v", err)
	}
}

func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func encodePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func defaultPaletteColor(v uint16) color.Color { return video.DefaultPalette().Color(v) }

func rgbOf(c color.Color) rgb {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return rgb{n.R, n.G, n.B}
}

func colorRGB(r, g, b uint8) color.Color { return color.RGBA{r, g, b, 255} }
