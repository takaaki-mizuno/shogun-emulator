package arch

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// pkg は go list から得たパッケージ 1 つ分の情報。
type pkg struct {
	ImportPath   string
	Dir          string
	GoFiles      []string
	TestGoFiles  []string
	XTestGoFiles []string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// module はモジュール全体のパッケージ情報。
type module struct {
	path string         // モジュールパス
	dir  string         // モジュールのルートディレクトリ
	pkgs map[string]pkg // インポートパス -> パッケージ
}

var (
	loadOnce sync.Once
	loaded   *module
	loadErr  error
)

// loadModule はモジュール内の全パッケージを読み込む。
// go list の実行は遅いため、テスト全体で 1 回だけ行う。
func loadModule(t *testing.T) *module {
	t.Helper()
	loadOnce.Do(func() { loaded, loadErr = doLoadModule() })
	if loadErr != nil {
		t.Skipf("パッケージ情報を取得できないため検査を飛ばす: %v", loadErr)
	}
	return loaded
}

func doLoadModule() (*module, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Path}}\t{{.Dir}}").Output()
	if err != nil {
		return nil, fmt.Errorf("go list -m: %w", err)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(fields) != 2 {
		return nil, fmt.Errorf("go list -m の出力を解釈できない: %q", out)
	}
	m := &module{path: fields[0], dir: fields[1], pkgs: map[string]pkg{}}

	// -e を付けるのは、ビルドできないパッケージがあっても他の検査を続けるため。
	cmd := exec.Command("go", "list", "-e", "-json", "./...")
	cmd.Dir = m.dir
	listOut, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -json ./...: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(listOut)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("go list の出力の解析: %w", err)
		}
		m.pkgs[p.ImportPath] = p
	}
	if len(m.pkgs) == 0 {
		return nil, fmt.Errorf("パッケージが 1 つも見つからない")
	}
	return m, nil
}

// full はモジュール相対のパスを完全なインポートパスにする。
func (m *module) full(rel string) string {
	rel = strings.TrimSuffix(rel, "/...")
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" || rel == "." {
		return m.path
	}
	return m.path + "/" + rel
}

// matching は pattern に一致するパッケージを返す。
// pattern は "internal/nes/..." のようなモジュール相対の形で与える。
func (m *module) matching(pattern string) []pkg {
	prefix := m.full(pattern)
	recursive := strings.HasSuffix(pattern, "/...")
	var out []pkg
	for path, p := range m.pkgs {
		if path == prefix || (recursive && strings.HasPrefix(path, prefix+"/")) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImportPath < out[j].ImportPath })
	return out
}

// imports はテストファイルの分も含めたインポートを返す。
// テストファイルを含めるのは、テストが GUI を参照すると
// コアだけをテストできなくなるためである。
func (p pkg) imports() []string {
	out := make([]string, 0, len(p.Imports)+len(p.TestImports)+len(p.XTestImports))
	out = append(out, p.Imports...)
	out = append(out, p.TestImports...)
	out = append(out, p.XTestImports...)
	return out
}

// isStdlib はインポートパスが標準ライブラリのものかを返す。
//
// 最初の要素にドットが含まれないことで判定する。モジュールパスは
// ホスト名から始まるため、標準ライブラリと確実に区別できる。
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// matchForbidden は path が forbidden のいずれかに該当するかを返す。
//
// forbidden の要素が "/" で終わるときは前方一致、そうでないときは
// 完全一致またはサブパッケージとの一致で判定する。"time" が "timex" に
// 誤って一致しないようにするためである。
func matchForbidden(path string, forbidden []string) (string, bool) {
	for _, f := range forbidden {
		if strings.HasSuffix(f, "/") {
			if strings.HasPrefix(path, f) {
				return f, true
			}
			continue
		}
		if path == f || strings.HasPrefix(path, f+"/") {
			return f, true
		}
	}
	return "", false
}

// findForbiddenImport は roots から到達できるインポートのうち、forbidden に
// 該当する最初のものを探す。見つかったとき、根からそこまでの経路を返す。
//
// たどるのはモジュール内のパッケージだけである。標準ライブラリの内部の
// インポートまでたどると、たとえば fmt が os を経由して time を参照する
// ために、意味のない違反が報告されてしまう。
func findForbiddenImport(pkgs map[string]pkg, modPath string, roots []string, forbidden []string) (chain []string, hit string) {
	type node struct {
		path string
		via  []string
	}
	seen := map[string]bool{}
	queue := make([]node, 0, len(roots))
	sorted := append([]string(nil), roots...)
	sort.Strings(sorted)
	for _, r := range sorted {
		if !seen[r] {
			seen[r] = true
			queue = append(queue, node{path: r, via: []string{r}})
		}
	}

	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]

		p, ok := pkgs[n.path]
		if !ok {
			continue
		}
		imports := p.imports()
		sort.Strings(imports)
		for _, imp := range imports {
			if f, bad := matchForbidden(imp, forbidden); bad {
				return append(append([]string(nil), n.via...), imp), f
			}
			// モジュール内のパッケージだけ先へたどる
			if imp != modPath && !strings.HasPrefix(imp, modPath+"/") {
				continue
			}
			if seen[imp] {
				continue
			}
			seen[imp] = true
			queue = append(queue, node{path: imp, via: append(append([]string(nil), n.via...), imp)})
		}
	}
	return nil, ""
}

// assertNoImport は pattern に一致するパッケージが forbidden を参照しないことを検証する。
func assertNoImport(t *testing.T, pattern string, forbidden []string) {
	t.Helper()
	m := loadModule(t)
	pkgs := m.matching(pattern)
	if len(pkgs) == 0 {
		t.Logf("%s に一致するパッケージがまだない", pattern)
		return
	}
	roots := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		roots = append(roots, p.ImportPath)
	}
	if chain, hit := findForbiddenImport(m.pkgs, m.path, roots, forbidden); chain != nil {
		t.Errorf("%s が %s を参照している: %s", pattern, hit, strings.Join(chain, " -> "))
	}
}

// selectorStrings はファイル内の "x.Y" の形の式を文字列として列挙する。
//
// 文字列の検索ではなく構文木を見るのは、コメントと文字列リテラルの中の
// 記述を違反として誤検出しないためである。
func selectorStrings(f *ast.File) []string {
	set := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok {
			set[x.Name+"."+sel.Sel.Name] = true
		}
		return true
	})
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// goFilesOf は pattern に一致するパッケージの Go ファイルのパスを返す。
func goFilesOf(t *testing.T, pattern string) []string {
	t.Helper()
	m := loadModule(t)
	var out []string
	for _, p := range m.matching(pattern) {
		names := append([]string(nil), p.GoFiles...)
		names = append(names, p.TestGoFiles...)
		names = append(names, p.XTestGoFiles...)
		for _, name := range names {
			out = append(out, filepath.Join(p.Dir, name))
		}
	}
	sort.Strings(out)
	return out
}

// parseSelectors はファイルを解析して "x.Y" の一覧を返す。
func parseSelectors(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Errorf("%s の解析に失敗した: %v", path, err)
		return nil
	}
	return selectorStrings(f)
}

// assertNoSymbol は pattern に一致するパッケージが forbidden の呼び出しを
// 含まないことを検証する。forbidden は "rand.Intn" の形で与える。
func assertNoSymbol(t *testing.T, pattern string, forbidden []string) {
	t.Helper()
	files := goFilesOf(t, pattern)
	if len(files) == 0 {
		t.Logf("%s に一致するパッケージがまだない", pattern)
		return
	}
	want := map[string]bool{}
	for _, s := range forbidden {
		want[s] = true
	}
	for _, path := range files {
		for _, sel := range parseSelectors(t, path) {
			if want[sel] {
				t.Errorf("%s が %s を呼んでいる", path, sel)
			}
		}
	}
}

// statementKinds は文の種類の名前と構文木の節を対応づける。
func statementKind(n ast.Node) string {
	switch n.(type) {
	case *ast.GoStmt:
		return "go"
	case *ast.SelectStmt:
		return "select"
	case *ast.DeferStmt:
		return "defer"
	}
	return ""
}

// assertNoStatement は pattern に一致するパッケージが、指定した種類の文を
// 含まないことを検証する。kinds には "go" や "select" を渡す。
func assertNoStatement(t *testing.T, pattern string, kinds []string) {
	t.Helper()
	files := goFilesOf(t, pattern)
	if len(files) == 0 {
		t.Logf("%s に一致するパッケージがまだない", pattern)
		return
	}
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s の解析に失敗した: %v", path, err)
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if k := statementKind(n); k != "" && want[k] {
				t.Errorf("%s が %s 文を使っている", path, k)
			}
			return true
		})
	}
}

// grepFiles は pattern に一致するパッケージのうち、symbols のいずれかを
// 含むファイルのパスを返す。
func grepFiles(t *testing.T, pattern string, symbols []string) []string {
	t.Helper()
	want := map[string]bool{}
	for _, s := range symbols {
		want[s] = true
	}
	var out []string
	for _, path := range goFilesOf(t, pattern) {
		for _, sel := range parseSelectors(t, path) {
			if want[sel] {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

// uiPackages は GUI のパッケージ。
var uiPackages = []string{"fyne.io/"}

// audioDevicePackages はオーディオデバイスを直接触るパッケージ。
var audioDevicePackages = []string{"github.com/ebitengine/oto", "github.com/hajimehoshi/oto"}

// corePackages は GUI から独立していなければならないパッケージ。
var corePackages = []string{"internal/nes/...", "internal/emu/...", "internal/debug/..."}

// TestCoreHasNoUIDependency はコアが GUI を参照しないことを検証する。
// 設計書 01 編 §1.4。
func TestCoreHasNoUIDependency(t *testing.T) {
	for _, pattern := range corePackages {
		assertNoImport(t, pattern, uiPackages)
	}
}

// TestOnlyAudioPackageTouchesDevice はオーディオデバイスを直接参照するのが
// internal/audio だけであることを検証する。設計書 01 編 §1.4。
//
// internal/emu は internal/audio を介してデバイスに触れる。進行の駆動が
// オーディオの消費で決まるためである（設計書 02 編 §2.6）。
func TestOnlyAudioPackageTouchesDevice(t *testing.T) {
	for _, pattern := range []string{"internal/nes/...", "internal/debug/..."} {
		assertNoImport(t, pattern, audioDevicePackages)
	}
}

// TestCoreImportsOnlyStdlibAndModule はエミュレーションコアが標準ライブラリと
// モジュール内のパッケージ以外を参照しないことを検証する。設計書 01 編 §1.4。
func TestCoreImportsOnlyStdlibAndModule(t *testing.T) {
	m := loadModule(t)
	pkgs := m.matching("internal/nes/...")
	if len(pkgs) == 0 {
		t.Log("internal/nes/... に一致するパッケージがまだない")
		return
	}
	for _, p := range pkgs {
		for _, imp := range p.imports() {
			if isStdlib(imp) {
				continue
			}
			if imp == m.path || strings.HasPrefix(imp, m.path+"/") {
				continue
			}
			t.Errorf("%s が外部のパッケージ %s を参照している", p.ImportPath, imp)
		}
	}
}

// TestNoTimeInEmulationCore はエミュレーションコアが time を参照しないことを
// 検証する。実時間に依存すると再現性が失われる。設計書 02 編。
func TestNoTimeInEmulationCore(t *testing.T) {
	assertNoImport(t, "internal/nes/...", []string{"time"})
}

// TestNoGlobalRandInEmulationCore はエミュレーションコアが math/rand の
// グローバル関数を呼ばないことを検証する。生成列が処理系に依存すると
// セーブステートとムービーの再現性が失われる。
func TestNoGlobalRandInEmulationCore(t *testing.T) {
	assertNoImport(t, "internal/nes/...", []string{"math/rand"})
	assertNoSymbol(t, "internal/nes/...", []string{
		"rand.Int", "rand.Intn", "rand.Int31", "rand.Int31n",
		"rand.Int63", "rand.Int63n", "rand.Uint32", "rand.Uint64",
		"rand.Float32", "rand.Float64", "rand.Perm", "rand.Shuffle",
		"rand.Read", "rand.Seed", "rand.N",
	})
}

// TestNoGoroutinesInEmulationCore はエミュレーションコアが並行処理の仕組みを
// 使わないことを検証する。実行順序が変わると再現性が失われる。
func TestNoGoroutinesInEmulationCore(t *testing.T) {
	assertNoImport(t, "internal/nes/...", []string{"sync"})
	assertNoStatement(t, "internal/nes/...", []string{"go", "select"})
}

// TestNoMapIterationInEmulationCore はエミュレーションコアが map を
// たどらないことを検証する。
//
// map のたどる順序は実行ごとに変わる。順序が結果に影響すると、同じ
// 入力から同じ結果を得られなくなる（設計書 08 編 §8.5）。
func TestNoMapIterationInEmulationCore(t *testing.T) {
	files := goFilesOf(t, "internal/nes/...")
	if len(files) == 0 {
		t.Log("internal/nes/... に一致するパッケージがまだない")
		return
	}
	fset := token.NewFileSet()
	for _, path := range files {
		// テストは対象にしない。エミュレーションの結果に影響しない。
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s の解析に失敗した: %v", path, err)
			continue
		}
		maps := mapNames(f)
		ast.Inspect(f, func(n ast.Node) bool {
			r, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			if name := rootIdent(r.X); name != "" && maps[name] {
				t.Errorf("%s が map %s をたどっている", path, name)
			}
			return true
		})
	}
}

// mapNames はファイルの中で map 型として宣言された名前を集める。
//
// 型情報を使わずに構文だけで判定する。宣言の形が限られているため、
// 見落としよりも過検出の側に倒れる。
func mapNames(f *ast.File) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.ValueSpec:
			if isMapType(v.Type) {
				addIdents(out, v.Names)
				return true
			}
			for i, name := range v.Names {
				if i < len(v.Values) && isMapExpr(v.Values[i]) {
					out[name.Name] = true
				}
			}
		case *ast.Field:
			if isMapType(v.Type) {
				addIdents(out, v.Names)
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(v.Rhs) {
					continue
				}
				if isMapExpr(v.Rhs[i]) {
					out[id.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// addIdents は名前を集合へ加える。
func addIdents(out map[string]bool, names []*ast.Ident) {
	for _, n := range names {
		out[n.Name] = true
	}
}

// isMapType は型が map かを返す。
func isMapType(e ast.Expr) bool {
	_, ok := e.(*ast.MapType)
	return ok
}

// isMapExpr は式が map を作るものかを返す。
func isMapExpr(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.CompositeLit:
		return isMapType(v.Type)
	case *ast.CallExpr:
		id, ok := v.Fun.(*ast.Ident)
		if !ok || id.Name != "make" || len(v.Args) == 0 {
			return false
		}
		return isMapType(v.Args[0])
	}
	return false
}

// rootIdent は式の根にある識別子の名前を返す。
func rootIdent(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// TestDispatchIsTheOnlyFyneDoCaller は fyne.Do と fyne.DoAndWait を呼ぶ
// ファイルが dispatch.go だけであることを検証する。設計書 01 編 §1.5。
func TestDispatchIsTheOnlyFyneDoCaller(t *testing.T) {
	m := loadModule(t)
	if len(m.matching("internal/ui/...")) == 0 {
		t.Log("internal/ui/... に一致するパッケージがまだない")
		return
	}
	files := grepFiles(t, "internal/ui/...", []string{"fyne.Do", "fyne.DoAndWait"})
	if len(files) == 0 {
		t.Log("fyne.Do を呼ぶファイルがまだない")
		return
	}
	for _, f := range files {
		if filepath.Base(f) != "dispatch.go" {
			t.Errorf("%s が fyne.Do を呼んでいる（dispatch.go だけに集める）", f)
		}
	}
	if len(files) > 1 {
		t.Errorf("fyne.Do を呼ぶファイルが複数ある: %v", files)
	}
}

// dependencyRules は設計書 01 編 §1.4 の依存グラフを、参照してはならない
// 方向として表したもの。下位から上位への参照を禁じる。
var dependencyRules = []struct {
	from      string
	forbidden []string
}{
	{"internal/nes/...", []string{"internal/ui", "internal/emu", "internal/debug", "internal/audio", "internal/config"}},
	{"internal/nes/state/...", []string{"internal/nes"}},
	// internal/video はフレームバッファとその表示への変換だけを持つ
	// 葉のパッケージである。PPU と表示のどちらからも参照されるため、
	// internal/ 以下のどのパッケージにも依存しない。
	{"internal/video/...", []string{"internal/ui", "internal/emu", "internal/debug", "internal/nes", "internal/audio", "internal/config"}},
	{"internal/audio/...", []string{"internal/ui", "internal/emu", "internal/debug"}},
	{"internal/config/...", []string{"internal/ui", "internal/emu", "internal/debug", "internal/nes"}},
	{"internal/debug/...", []string{"internal/ui", "internal/emu"}},
	{"internal/emu/...", []string{"internal/ui"}},
}

// TestAssetsHoldDataOnly は assets が embed 以外を参照しないことを検証する。
// 設計書 01 編 §1.4。データだけを持つ葉のパッケージであることを保つ。
func TestAssetsHoldDataOnly(t *testing.T) {
	m := loadModule(t)
	pkgs := m.matching("assets")
	if len(pkgs) == 0 {
		t.Log("assets に一致するパッケージがまだない")
		return
	}
	for _, p := range pkgs {
		for _, imp := range p.imports() {
			if imp != "embed" {
				t.Errorf("%s が %s を参照している（embed だけを参照する）", p.ImportPath, imp)
			}
		}
	}
}

// TestMP4FFOnlyInMP4Rec は mp4ff を参照するのが internal/video/mp4rec だけで
// あることを検証する(設計書 01 編 §1.4)。
//
// m.matching は "pkg/..." の形のパターンを前提にしており、全パッケージを表す
// パターンを持たないため、m.pkgs を直接たどる。p.Imports だけを見るのは、
// _test.go からの参照を対象外とするためである(TestImports・XTestImports を
// 含まない)。
func TestMP4FFOnlyInMP4Rec(t *testing.T) {
	m := loadModule(t)
	for _, p := range m.pkgs {
		if strings.HasSuffix(p.ImportPath, "internal/video/mp4rec") {
			continue
		}
		for _, imp := range p.Imports {
			if strings.HasPrefix(imp, "github.com/Eyevinn/mp4ff") {
				t.Errorf("%s が %s を参照している(internal/video/mp4rec に閉じる)", p.ImportPath, imp)
			}
		}
	}
}

// TestDependencyDirection は依存が上から下への一方向であることを検証する。
// 設計書 01 編 §1.4。
func TestDependencyDirection(t *testing.T) {
	m := loadModule(t)
	for _, rule := range dependencyRules {
		pkgs := m.matching(rule.from)
		if len(pkgs) == 0 {
			t.Logf("%s に一致するパッケージがまだない", rule.from)
			continue
		}
		forbidden := make([]string, 0, len(rule.forbidden))
		for _, f := range rule.forbidden {
			forbidden = append(forbidden, m.full(f)+"/")
			forbidden = append(forbidden, m.full(f))
		}
		for _, p := range pkgs {
			for _, imp := range p.imports() {
				// 自分自身とその配下は対象外
				if strings.HasPrefix(imp, m.full(rule.from)) {
					continue
				}
				if hit, bad := matchForbidden(imp, forbidden); bad {
					t.Errorf("%s が %s を参照している（%s から %s への参照は禁止）",
						p.ImportPath, imp, rule.from, hit)
				}
			}
		}
	}
}

// TestUITextLivesInCatalog は internal/ui のコードに日本語の文字列リテラルが
// 無いことを検証する。文言は internal/ui/i18n に集める（設計書 10 編 §10.9）。
// テストのファイルと internal/ui/i18n は対象にしない。
func TestUITextLivesInCatalog(t *testing.T) {
	files := goFilesOf(t, "internal/ui")
	if len(files) == 0 {
		t.Log("internal/ui がまだない")
		return
	}
	isJapanese := func(r rune) bool {
		return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("%s の解析に失敗した: %v", path, err)
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.ContainsFunc(lit.Value, isJapanese) {
				t.Errorf("%s: 文言 %s を直接書いている（internal/ui/i18n の表へ移す）", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}
