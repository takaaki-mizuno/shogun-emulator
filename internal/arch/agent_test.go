package arch

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// mcpSDK は MCP の SDK のモジュールパス。
const mcpSDK = "github.com/modelcontextprotocol/go-sdk"

// yamlLib は YAML のライブラリのモジュールパス。
const yamlLib = "github.com/goccy/go-yaml"

// TestAgentDependencyRules は Agent Interface の依存の規則を検証する
// （設計書 14 編 §14.2.2、設計書 12 編 §12.7）。
func TestAgentDependencyRules(t *testing.T) {
	m := loadModule(t)
	// internal/agent 以下は GUI を参照しない。headless と GUI 版で同じ
	// コードを使うためである。
	assertNoImport(t, "internal/agent/...", append([]string{m.full("internal/ui")}, uiPackages...))
	// MCP ブリッジは internal/agent を直接参照せず、rpc のクライアントだけを使う。
	assertNoDirectImport(t, "internal/agent/mcpbridge", []string{"internal/agent"})
	// MCP SDK を参照してよいのは internal/agent/mcpbridge だけである。
	assertOnlyImporter(t, mcpSDK, "internal/agent/mcpbridge")
	// YAML のライブラリを参照してよいのは Scenario の読み込みだけである
	// （設計書 14 編 §14.17.1）。
	assertOnlyImporter(t, yamlLib, "internal/agent/scenario")
	// コアは Agent Interface を参照しない。
	for _, pattern := range corePackages {
		assertNoImport(t, pattern, []string{m.full("internal/agent")})
	}
}

// assertNoDirectImport は pattern に一致するパッケージが forbidden（モジュール
// 相対）を直接インポートしないことを検証する。
//
// 推移的にはたどらない。internal/agent/mcpbridge は internal/agent/rpc を介して
// internal/agent に依存するため、推移的にたどると規則を表せない。サブ
// パッケージは一致としない。
func assertNoDirectImport(t *testing.T, pattern string, forbidden []string) {
	t.Helper()
	m := loadModule(t)
	pkgs := m.matching(pattern)
	if len(pkgs) == 0 {
		t.Logf("%s に一致するパッケージがまだない", pattern)
		return
	}
	full := make([]string, len(forbidden))
	for i, f := range forbidden {
		full[i] = m.full(f)
	}
	for _, v := range directImportViolations(pkgs, full) {
		t.Error(v)
	}
}

// directImportViolations は forbidden を直接インポートしているものを返す。
func directImportViolations(pkgs []pkg, forbidden []string) []string {
	var out []string
	for _, p := range pkgs {
		for _, imp := range p.imports() {
			if slices.Contains(forbidden, imp) {
				out = append(out, fmt.Sprintf("%s が %s を直接参照している", p.ImportPath, imp))
			}
		}
	}
	return out
}

// assertOnlyImporter はモジュール内で target（外部モジュール）を直接
// インポートするのが allowed（モジュール相対）とその配下だけであることを
// 検証する。
func assertOnlyImporter(t *testing.T, target, allowed string) {
	t.Helper()
	m := loadModule(t)
	all := make([]pkg, 0, len(m.pkgs))
	for _, p := range m.pkgs {
		all = append(all, p)
	}
	slices.SortFunc(all, func(a, b pkg) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	for _, v := range foreignImporters(all, target, m.full(allowed)) {
		t.Error(v)
	}
}

// foreignImporters は allowed の外で target を参照しているものを返す。
func foreignImporters(pkgs []pkg, target, allowed string) []string {
	var out []string
	for _, p := range pkgs {
		if p.ImportPath == allowed || strings.HasPrefix(p.ImportPath, allowed+"/") {
			continue
		}
		for _, imp := range p.imports() {
			if imp == target || strings.HasPrefix(imp, target+"/") {
				out = append(out, fmt.Sprintf("%s が %s を参照している（%s だけが参照してよい）", p.ImportPath, imp, allowed))
			}
		}
	}
	return out
}

// TestDetectsDirectAgentImport は直接のインポートの検査が違反を検出し、
// サブパッケージを違反としないことを確かめる。
func TestDetectsDirectAgentImport(t *testing.T) {
	pkgs := []pkg{
		{ImportPath: testMod + "/internal/agent/mcpbridge", Imports: []string{testMod + "/internal/agent/rpc"}},
	}
	if v := directImportViolations(pkgs, []string{testMod + "/internal/agent"}); len(v) != 0 {
		t.Errorf("サブパッケージの参照を違反とした: %v", v)
	}
	pkgs[0].Imports = append(pkgs[0].Imports, testMod+"/internal/agent")
	if v := directImportViolations(pkgs, []string{testMod + "/internal/agent"}); len(v) != 1 {
		t.Errorf("直接の参照を検出できない: %v", v)
	}
}

// TestDetectsForeignSDKImporter は MCP SDK を許可の外で参照したことを
// 検出することを確かめる。
func TestDetectsForeignSDKImporter(t *testing.T) {
	pkgs := []pkg{
		{ImportPath: testMod + "/internal/agent/mcpbridge", Imports: []string{mcpSDK + "/mcp"}},
		{ImportPath: testMod + "/internal/agent", Imports: []string{"encoding/json"}},
	}
	allowed := testMod + "/internal/agent/mcpbridge"
	if v := foreignImporters(pkgs, mcpSDK, allowed); len(v) != 0 {
		t.Errorf("許可した場所の参照を違反とした: %v", v)
	}
	pkgs[1].Imports = append(pkgs[1].Imports, mcpSDK+"/mcp")
	if v := foreignImporters(pkgs, mcpSDK, allowed); len(v) != 1 {
		t.Errorf("許可の外の参照を検出できない: %v", v)
	}
}
