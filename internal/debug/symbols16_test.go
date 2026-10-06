package debug

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// agentTestdata はテスト用 ca65 プロジェクトの場所（設計書 14 編 §14.24）。
const agentTestdata = "../../testdata/agent"

// loadGameDbg はテスト用 ROM の .dbg を読む。
func loadGameDbg(t *testing.T) *DbgInfo {
	t.Helper()
	info, err := LoadDbgFile(filepath.Join(agentTestdata, "game.dbg"))
	if err != nil {
		t.Fatalf("テスト用 ROM の .dbg を読めない（testdata/agent/build.sh でビルドする）: %v", err)
	}
	return info
}

func findSym(t *testing.T, info *DbgInfo, name string) Symbol {
	t.Helper()
	for _, s := range info.Symbols {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("Symbol %q が無い", name)
	return Symbol{}
}

// sourceLineOf は game.s の中で text を含む最初の行の番号を返す。
func sourceLineOf(t *testing.T, text string) int {
	t.Helper()
	f, err := os.Open(filepath.Join(agentTestdata, "src", "game.s"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		if strings.Contains(sc.Text(), text) {
			return n
		}
	}
	t.Fatalf("game.s に %q が無い", text)
	return 0
}

// TestDbgSymbols は .dbg からバンクを区別した Symbol を得ることを確かめる
// （フェーズ 16 の完了判定）。
func TestDbgSymbols(t *testing.T) {
	info := loadGameDbg(t)
	b0, b1 := findSym(t, info, "bank0_entry"), findSym(t, info, "bank1_entry")
	if b0.Loc != (SymbolLoc{SymSpacePRG, 0}) || b1.Loc != (SymbolLoc{SymSpacePRG, 0x4000}) {
		t.Errorf("同じ $8000 の 2 つのバンクの位置 = %+v, %+v", b0.Loc, b1.Loc)
	}
	if b0.CPU != 0x8000 || b1.CPU != 0x8000 {
		t.Errorf("CPU アドレス = $%04X, $%04X", b0.CPU, b1.CPU)
	}
	reset := findSym(t, info, "reset")
	if reset.Loc != (SymbolLoc{SymSpacePRG, 7 * 0x4000}) || reset.Kind != SymLabel {
		t.Errorf("reset = %+v（固定バンクの先頭）", reset)
	}
	for name, want := range map[string]SymbolLoc{
		"mode": {SymSpaceCPU, 0}, "player_x": {SymSpaceCPU, 1}, "frame_count": {SymSpaceCPU, 3},
		"score": {SymSpaceCPU, 0x300},
	} {
		s := findSym(t, info, name)
		if s.Loc != want || s.Kind != SymVariable {
			t.Errorf("%s = %+v, 期待 %+v の変数", name, s, want)
		}
	}
	if s := findSym(t, info, "score"); s.Size != 3 {
		t.Errorf("score のサイズ = %d", s.Size)
	}
	if s := findSym(t, info, "enemy_x"); s.Size != 8 {
		t.Errorf("enemy_x のサイズ = %d", s.Size)
	}
	// .proc の中のラベルは修飾名にする。
	findSym(t, info, "reset::loop")
	findSym(t, info, "update_score::loop")
	findSym(t, info, "update_flags::done")
	c := findSym(t, info, "PLAYER_Y_START")
	if c.Kind != SymConstant || c.Value != 0x80 {
		t.Errorf("定数 = %+v", c)
	}
	for _, s := range info.Symbols {
		if strings.HasPrefix(s.Name, "LOCAL-MACRO") {
			t.Errorf("マクロの内部の名前を読んだ: %s", s.Name)
		}
	}
	if reset.Source.File != "game.s" || reset.Source.Line != sourceLineOf(t, ".proc reset") {
		t.Errorf("reset の定義の位置 = %+v", reset.Source)
	}
}

// TestDbgSourceLines は PRG-ROM の位置からソースの行を引けることと、マクロの
// 展開ではマクロの定義ではなく呼び出した行を返すことを確かめる。
func TestDbgSourceLines(t *testing.T) {
	info := loadGameDbg(t)
	s := NewSymbols()
	s.SetDbg(info, "game.dbg")
	nmi := findSym(t, info, "nmi")
	inc16 := sourceLineOf(t, "inc16 frame_count")
	for off := int(nmi.Loc.Offset); off < int(nmi.Loc.Offset)+6; off++ {
		src, ok := s.SourceAt(off)
		if !ok || src.File != "game.s" || src.Line != inc16 {
			t.Errorf("オフセット $%X のソース = %+v, 期待 game.s:%d（マクロを呼んだ行）", off, src, inc16)
		}
	}
	b0 := findSym(t, info, "bank0_entry")
	if src, ok := s.SourceAt(int(b0.Loc.Offset)); !ok || src.Line != sourceLineOf(t, "lda #$A0") {
		t.Errorf("bank0_entry のソース = %+v", src)
	}
	if _, ok := s.SourceAt(0x3000); ok {
		t.Error("コードの無い位置にソースがある")
	}
}

// TestLabelAtIsBankAware は現在のバンク構成で名前を引くことを確かめる。
func TestLabelAtIsBankAware(t *testing.T) {
	s := NewSymbols()
	s.SetDbg(loadGameDbg(t), "game.dbg")
	bank := 0
	off := func(addr uint16) (int, bool) {
		if addr >= 0xC000 {
			return 7*0x4000 + int(addr-0xC000), true
		}
		return bank*0x4000 + int(addr-0x8000), true
	}
	if got := s.LabelAt(0x8000, off); got != "bank0_entry" {
		t.Errorf("バンク 0 の $8000 = %q", got)
	}
	bank = 1
	if got := s.LabelAt(0x8000, off); got != "bank1_entry" {
		t.Errorf("バンク 1 の $8000 = %q", got)
	}
	bank = 2
	if got := s.LabelAt(0x8000, off); got != "" {
		t.Errorf("名前の無いバンクの $8000 = %q", got)
	}
	if got := s.LabelAt(0x0003, off); got != "frame_count" {
		t.Errorf("$0003 = %q", got)
	}
	nmi := findSym(t, loadGameDbg(t), "nmi")
	if name, d, ok := s.NearestAt(nmi.CPU+3, off); !ok || name != "nmi" || d != 3 {
		t.Errorf("nmi+3 = %q %d %v", name, d, ok)
	}
	// 利用者の名前を .dbg より優先する。
	s.SetLabel(0x0003, "frames", off)
	if got := s.LabelAt(0x0003, off); got != "frames" {
		t.Errorf("利用者の名前を優先しない: %q", got)
	}
}

// TestSymbolsMigration はバージョン 1 のファイルを読み、保存するとバージョン 2 に
// なり、$8000 以上のラベルがバンクを問わない位置になることを確かめる
// （フェーズ 16 の完了判定）。
func TestSymbolsMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.json")
	v1 := `{"version":1,"labels":{"$0300":"score","$C000":"main"},"regions":[{"start":16,"end":31,"kind":"data"}],` +
		`"watch":["$0300","$C010"],"breakpoints":[{"kind":"exec","start":"$C000","end":"$C000","enabled":true}]}`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSymbols(path)
	if err != nil {
		t.Fatal(err)
	}
	main, ok := s.Lookup("main")
	if !ok || main.Loc != (SymbolLoc{SymSpacePRGAny, 0xC000}) || main.Origin != OriginLegacy {
		t.Errorf("main = %+v", main)
	}
	if sc, _ := s.Lookup("score"); sc.Loc != (SymbolLoc{SymSpaceCPU, 0x300}) || sc.Origin != OriginUser {
		t.Errorf("score = %+v", sc)
	}
	if got := s.LabelAt(0xC000, func(uint16) (int, bool) { return 0x1C000, true }); got != "main" {
		t.Errorf("バンクを問わない名前を引けない: %q", got)
	}
	if len(s.Breakpoints(nil)) != 1 || !s.IsData(20) || len(s.Watch()) != 2 {
		t.Error("ブレークポイント・領域・ウォッチを移していない")
	}
	// .dbg の Symbol は保存しない。
	s.SetDbg(loadGameDbg(t), "game.dbg")
	out := filepath.Join(dir, "v2.json")
	if err := s.Save(out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	var f map[string]any
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f["version"].(float64) != 2 || len(f["symbols"].([]any)) != 2 {
		t.Errorf("保存した内容:\n%s", data)
	}
	if !strings.Contains(string(data), `"space": "prg_any"`) || strings.Contains(string(data), "bank0_entry") {
		t.Errorf("保存した内容:\n%s", data)
	}
	again, err := LoadSymbols(out)
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := again.Lookup("main"); m.Loc != main.Loc {
		t.Errorf("読み直した main = %+v", m)
	}
}

// TestLoadProject は ROM より古い .dbg を読み込まず知らせることと、Game State
// Definition の探す順を確かめる。
func TestLoadProject(t *testing.T) {
	dir := t.TempDir()
	rom := filepath.Join(dir, "game.nes")
	copyFile(t, filepath.Join(agentTestdata, "game.nes"), rom)
	copyFile(t, filepath.Join(agentTestdata, "game.dbg"), filepath.Join(dir, "game.dbg"))
	now := time.Now()
	os.Chtimes(rom, now, now)
	os.Chtimes(filepath.Join(dir, "game.dbg"), now.Add(-time.Hour), now.Add(-time.Hour))

	data := filepath.Join(dir, "data")
	p := ProjectPathsFor(rom, data, "abcd")
	s := NewSymbols()
	notes := LoadProject(s, rom, p)
	if len(notes) != 1 || !strings.Contains(notes[0], "古い") || s.HasSymbol("reset") {
		t.Errorf("古い .dbg: notes %v、読み込んだ %v", notes, s.HasSymbol("reset"))
	}
	os.Chtimes(filepath.Join(dir, "game.dbg"), now.Add(time.Hour), now.Add(time.Hour))
	if notes := LoadProject(s, rom, p); len(notes) != 0 || !s.HasSymbol("reset") || !s.ProjectLoaded() {
		t.Errorf("新しい .dbg: notes %v", notes)
	}
	// データディレクトリのものより ROM のディレクトリのものを優先する。
	os.MkdirAll(filepath.Join(data, GameStateDirName), 0o755)
	os.WriteFile(p.GameStateData, []byte(`{"version":1,"items":[{"name":"a","loc":"$00","type":"u8"}]}`), 0o644)
	LoadProject(s, rom, p)
	if _, ok := s.GameState().Item("a"); !ok {
		t.Error("データディレクトリの定義を読まない")
	}
	os.WriteFile(p.GameStateLocal, []byte(`{"version":1,"items":[{"name":"b","loc":"$00","type":"u8"},{"name":"bad","loc":"$00","type":"u9"}]}`), 0o644)
	LoadProject(s, rom, p)
	def := s.GameState()
	if _, ok := def.Item("b"); !ok || def.Path() != p.GameStateLocal {
		t.Error("ROM のディレクトリの定義を優先しない")
	}
	if reason, bad := def.Invalid("bad"); !bad || !strings.Contains(reason, "u9") {
		t.Errorf("誤りのある項目 = %q %v", reason, bad)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
