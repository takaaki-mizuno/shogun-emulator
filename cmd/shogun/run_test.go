package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scenarioProject はテスト用 ROM（testdata/agent）と Scenario を置いた
// ディレクトリを作る。
func scenarioProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"game.nes", "game.dbg"} {
		data, err := os.ReadFile(filepath.Join("../../testdata/agent", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	os.Chtimes(filepath.Join(dir, "game.nes"), now, now)
	os.Chtimes(filepath.Join(dir, "game.dbg"), now.Add(time.Second), now.Add(time.Second))
	def := `{"version":1,"items":[{"name":"mode","loc":"mode","type":"u8","enum":{"0":"title","1":"play"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "game.gamestate.json"), []byte(def), 0o644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"pass.yaml": `name: 合格
rom: game.nes
steps:
  - exec.run_until: {condition: "game.mode == 'play'", max_frames: 200}
  - assert_stop: condition
  - assert_screen: {golden: golden/play.png}
`,
		"fail.yaml": `name: 失敗
rom: game.nes
steps:
  - exec.step: {frames: 2}
  - assert: "game.mode == 'play'"
`,
		"bad.yaml": `rom: game.nes
steps:
  - exec.step: {frames: "x"}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestRunScenarios は shogun run の終了コード（0・5・6）、標準出力、JUnit XML、
// --update-golden、失敗時の Repro を確かめる（フェーズ 19 の完了判定）。
func TestRunScenarios(t *testing.T) {
	dir := scenarioProject(t)
	pass, fail, bad := filepath.Join(dir, "pass.yaml"), filepath.Join(dir, "fail.yaml"), filepath.Join(dir, "bad.yaml")

	// お手本が無いと失敗し、--update-golden で作ると合格する。
	if code, out, _ := runCLI(t, "run", pass); code != exitScenarioFailed || !strings.Contains(out, "--update-golden") {
		t.Fatalf("お手本が無いとき = %d\n%s", code, out)
	}
	code, out, errOut := runCLI(t, "run", "--update-golden", pass)
	if code != exitOK || !strings.Contains(out, "書き換えたお手本") || !strings.Contains(out, filepath.Join(dir, "golden", "play.png")) {
		t.Fatalf("--update-golden = %d\n%s%s", code, out, errOut)
	}
	if code, out, _ := runCLI(t, "run", pass); code != exitOK || !strings.Contains(out, "PASS  合格") {
		t.Fatalf("合格 = %d\n%s", code, out)
	}

	// 失敗: 終了コード 5、失敗のステップと実際の値、Repro、JUnit XML。
	junit := filepath.Join(dir, "report.xml")
	repros := filepath.Join(dir, "repros")
	code, out, _ = runCLI(t, "run", pass, fail, "--junit", junit, "--repro-dir", repros)
	if code != exitScenarioFailed {
		t.Fatalf("失敗の終了コード = %d\n%s", code, out)
	}
	for _, want := range []string{"FAIL  失敗", "ステップ 2（5 行）", `game.mode = "title"`, "Repro: " + filepath.Join(repros, "fail-1.repro")} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(repros, "fail-1.repro", "repro.shgm")); err != nil {
		t.Errorf("Repro が無い: %v", err)
	}
	data, err := os.ReadFile(junit)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Suites []struct {
			Name  string `xml:"name,attr"`
			Cases []struct {
				Name    string `xml:"name,attr"`
				Failure *struct {
					Message string `xml:"message,attr"`
				} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("JUnit XML を読めない: %v", err)
	}
	if len(doc.Suites) != 2 || doc.Suites[0].Cases[0].Failure != nil || doc.Suites[1].Cases[0].Failure == nil ||
		!strings.Contains(doc.Suites[1].Cases[0].Failure.Message, "ステップ 2") {
		t.Errorf("JUnit XML:\n%s", data)
	}

	// 不正なファイル: 終了コード 6。残りのファイルは実行する。
	code, out, _ = runCLI(t, "run", bad, fail, "--repro-dir", repros)
	if code != exitScenarioInvalid || !strings.Contains(out, "INVALID") || !strings.Contains(out, "bad.yaml:3: ステップ 1") || !strings.Contains(out, "FAIL  失敗") {
		t.Errorf("不正なファイル = %d\n%s", code, out)
	}
	if code, _, _ := runCLI(t, "run"); code != exitBadArgs {
		t.Errorf("ファイルが無いときの終了コード = %d", code)
	}
}

// TestRunIgnoresPalette はパレットの設定を変えても画面のアサーションが
// 合格することを確かめる。
func TestRunIgnoresPalette(t *testing.T) {
	dir := scenarioProject(t)
	pass := filepath.Join(dir, "pass.yaml")
	if code, out, _ := runCLI(t, "run", "--update-golden", pass); code != exitOK {
		t.Fatalf("お手本を作れない: %d\n%s", code, out)
	}
	pal := make([]byte, 64*3)
	for i := range pal {
		pal[i] = byte(255 - i)
	}
	palPath := filepath.Join(dir, "odd.pal")
	if err := os.WriteFile(palPath, pal, 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.json")
	cfg := `{"version": 1, "video": {"paletteFile": "` + filepath.ToSlash(palPath) + `"}}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "run", "--config", cfgPath, pass); code != exitOK {
		t.Errorf("パレットを変えると合格しない: %d\n%s%s", code, out, errOut)
	}
}
