package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// 以下のテストは、検査そのものが違反を検出できることを確かめる。
// 実際のパッケージを壊さずに検査の論理を確かめるため、合成したグラフと
// 合成したソースを入力にする。

const testMod = "example.com/m"

// synthetic は検出のテスト用のパッケージグラフを組み立てる。
func synthetic(imports map[string][]string) map[string]pkg {
	out := map[string]pkg{}
	for path, imps := range imports {
		out[path] = pkg{ImportPath: path, Imports: imps}
	}
	return out
}

// TestDetectsDirectForbiddenImport は直接のインポートを検出することを確かめる。
func TestDetectsDirectForbiddenImport(t *testing.T) {
	pkgs := synthetic(map[string][]string{
		testMod + "/internal/nes": {"encoding/binary", "time"},
	})
	chain, hit := findForbiddenImport(pkgs, testMod, []string{testMod + "/internal/nes"}, []string{"time"})
	if chain == nil {
		t.Fatal("time の直接インポートを検出できなかった")
	}
	if hit != "time" {
		t.Errorf("該当した項目 = %q, 期待 \"time\"", hit)
	}
	if got := strings.Join(chain, " -> "); got != testMod+"/internal/nes -> time" {
		t.Errorf("経路 = %q", got)
	}
}

// TestDetectsIndirectForbiddenImport はモジュール内を経由した間接のインポートを
// 検出することを確かめる。
func TestDetectsIndirectForbiddenImport(t *testing.T) {
	pkgs := synthetic(map[string][]string{
		testMod + "/internal/nes":       {testMod + "/internal/nes/state"},
		testMod + "/internal/nes/state": {"fyne.io/fyne/v2"},
	})
	chain, hit := findForbiddenImport(pkgs, testMod, []string{testMod + "/internal/nes"}, uiPackages)
	if chain == nil {
		t.Fatal("間接的な fyne への依存を検出できなかった")
	}
	if hit != "fyne.io/" {
		t.Errorf("該当した項目 = %q", hit)
	}
	want := testMod + "/internal/nes -> " + testMod + "/internal/nes/state -> fyne.io/fyne/v2"
	if got := strings.Join(chain, " -> "); got != want {
		t.Errorf("経路 = %q, 期待 %q", got, want)
	}
}

// TestDoesNotTraverseStdlib は標準ライブラリの内部をたどらないことを確かめる。
// fmt が os を経由して time に至るような経路を違反としないためである。
func TestDoesNotTraverseStdlib(t *testing.T) {
	pkgs := synthetic(map[string][]string{
		testMod + "/internal/nes": {"fmt"},
		"fmt":                     {"os"},
		"os":                      {"time"},
	})
	if chain, _ := findForbiddenImport(pkgs, testMod, []string{testMod + "/internal/nes"}, []string{"time"}); chain != nil {
		t.Errorf("標準ライブラリの内部をたどって誤検出した: %v", chain)
	}
}

// TestHandlesImportCycleSafely は循環があっても停止することを確かめる。
// go では実際には循環は作れないが、検査が無限ループしないことを保証する。
func TestHandlesImportCycleSafely(t *testing.T) {
	pkgs := synthetic(map[string][]string{
		testMod + "/a": {testMod + "/b"},
		testMod + "/b": {testMod + "/a"},
	})
	if chain, _ := findForbiddenImport(pkgs, testMod, []string{testMod + "/a"}, []string{"time"}); chain != nil {
		t.Errorf("誤検出した: %v", chain)
	}
}

// TestMatchForbidden は禁止パターンの一致の規則を確かめる。
func TestMatchForbidden(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"time", true},
		{"time/tzdata", true},
		{"timex", false},          // 前方一致で誤って一致してはならない
		{"internal/timer", false}, // 末尾の一致で拾ってはならない
	}
	for _, tt := range tests {
		if _, got := matchForbidden(tt.path, []string{"time"}); got != tt.want {
			t.Errorf("matchForbidden(%q, [time]) = %v, 期待 %v", tt.path, got, tt.want)
		}
	}

	if _, got := matchForbidden("fyne.io/fyne/v2/widget", []string{"fyne.io/"}); !got {
		t.Error("fyne.io/ の前方一致が働いていない")
	}
	if _, got := matchForbidden("fyne.iox/pkg", []string{"fyne.io/"}); got {
		t.Error("fyne.io/ が別のモジュールに一致した")
	}
}

// TestIsStdlib は標準ライブラリの判定を確かめる。
func TestIsStdlib(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"time", true},
		{"encoding/binary", true},
		{"go/ast", true},
		{"fyne.io/fyne/v2", false},
		{"github.com/takaakimizuno/shogun-emulator/internal/nes", false},
	}
	for _, tt := range tests {
		if got := isStdlib(tt.path); got != tt.want {
			t.Errorf("isStdlib(%q) = %v, 期待 %v", tt.path, got, tt.want)
		}
	}
}

// TestSelectorStringsDetectsCall は構文木から呼び出しを拾えることを確かめる。
func TestSelectorStringsDetectsCall(t *testing.T) {
	src := `package p

import (
	"math/rand"
	fy "fyne.io/fyne/v2"
)

func f() {
	_ = rand.Intn(10)
	fy.Do(func() {})
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	got := selectorStrings(f)
	for _, want := range []string{"rand.Intn", "fy.Do"} {
		if !slices.Contains(got, want) {
			t.Errorf("%q を検出できなかった: %v", want, got)
		}
	}
}

// TestSelectorStringsIgnoresCommentsAndStrings はコメントと文字列リテラルの中の
// 記述を拾わないことを確かめる。文字列の検索ではなく構文木を見る理由である。
func TestSelectorStringsIgnoresCommentsAndStrings(t *testing.T) {
	src := `package p

// ここでは rand.Intn を使わない。
func f() string {
	return "fyne.Do は dispatch.go だけで呼ぶ"
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	for _, sel := range selectorStrings(f) {
		if sel == "rand.Intn" || sel == "fyne.Do" {
			t.Errorf("コメントまたは文字列の中の %q を検出してしまった", sel)
		}
	}
}

// TestStatementKindDetectsGoAndSelect は go 文と select 文を構文木から
// 見分けられることを確かめる。
func TestStatementKindDetectsGoAndSelect(t *testing.T) {
	src := `package p

func f(ch chan int) {
	go func() {}()
	select {
	case <-ch:
	}
	defer f(ch)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("解析に失敗した: %v", err)
	}
	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if k := statementKind(n); k != "" {
			found[k] = true
		}
		return true
	})
	for _, want := range []string{"go", "select", "defer"} {
		if !found[want] {
			t.Errorf("%s 文を検出できなかった", want)
		}
	}
}

// TestMapNamesFindsDeclarations は map として宣言された名前を集められる
// ことを確かめる。
func TestMapNamesFindsDeclarations(t *testing.T) {
	src := `package p

var table = map[string]int{}

type s struct {
	byName map[string]int
	count  int
}

func f() {
	local := make(map[int]bool)
	other := []int{1}
	_ = local
	_ = other
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := mapNames(f)
	for _, want := range []string{"table", "byName", "local"} {
		if !got[want] {
			t.Errorf("%q を map と判定していない", want)
		}
	}
	for _, notWant := range []string{"count", "other"} {
		if got[notWant] {
			t.Errorf("%q を map と判定した", notWant)
		}
	}
}

// TestRootIdentTakesTheName は式の根の名前を取り出せることを確かめる。
func TestRootIdentTakesTheName(t *testing.T) {
	for src, want := range map[string]string{
		"a":     "a",
		"b.c":   "c",
		"f()":   "",
		"a[0]":  "",
		"x.y.z": "z",
	} {
		e, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got := rootIdent(e); got != want {
			t.Errorf("rootIdent(%s) = %q, 期待 %q", src, got, want)
		}
	}
}
