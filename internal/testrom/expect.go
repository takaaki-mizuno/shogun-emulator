package testrom

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Expectation は 1 つのテスト ROM に対する期待値。
type Expectation struct {
	// Expect は合格とみなす $6000 の値。
	Expect uint8 `json:"expect"`
	// Timeout は結果を待つ上限のフレーム数。
	Timeout int `json:"timeout"`
}

// expectationsFile は期待値を置くファイルの、リポジトリルートからのパス。
const expectationsFile = "testdata/golden/testroms.json"

// romDirRel は ROM を置くディレクトリの、リポジトリルートからのパス。
const romDirRel = "testdata/roms"

// LoadExpectations は期待値の表を読む。
func LoadExpectations(path string) (map[string]Expectation, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]Expectation
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for name, e := range m {
		if e.Timeout <= 0 {
			return nil, fmt.Errorf("%s: %s の timeout が 0 以下である", path, name)
		}
	}
	return m, nil
}

// Expectations は期待値の表を返す。読めないときテストを失敗させる。
func Expectations(t testing.TB) map[string]Expectation {
	t.Helper()
	m, err := LoadExpectations(filepath.Join(RepoRoot(t), expectationsFile))
	if err != nil {
		t.Fatalf("期待値を読めない: %v", err)
	}
	return m
}

// Expect は 1 つの ROM の期待値を返す。登録が無いときテストを失敗させる。
//
// 登録の無い ROM を飛ばさずに失敗させるのは、期待値の登録漏れが黙って
// 見逃されることを防ぐためである。
func Expect(t testing.TB, rom string) Expectation {
	t.Helper()
	m := Expectations(t)
	e, ok := m[rom]
	if !ok {
		t.Fatalf("%s の期待値が %s に登録されていない", rom, expectationsFile)
	}
	return e
}

var (
	rootOnce sync.Once
	rootDir  string
	rootErr  error
)

// RepoRoot はリポジトリのルートディレクトリを返す。
//
// go.mod を見つけるまで親へさかのぼる。テストは各パッケージのディレクトリで
// 動くため、testdata への相対パスを固定で書けない。
func RepoRoot(t testing.TB) string {
	t.Helper()
	rootOnce.Do(func() { rootDir, rootErr = findRepoRoot() })
	if rootErr != nil {
		t.Fatalf("リポジトリのルートが分からない: %v", rootErr)
	}
	return rootDir
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && st.Mode().IsRegular() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod が見つからない")
		}
		dir = parent
	}
}

// ROMPath は ROM の置き場所からの相対パスを絶対パスにする。
func ROMPath(t testing.TB, rel string) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), romDirRel, filepath.FromSlash(rel))
}

// RequireROM は ROM のパスを返す。ROM が無いときテストを飛ばす。
//
// 失敗ではなく飛ばすのは、ROM をリポジトリに含めないためである。ROM を
// 取得していない環境でも他のテストが動くようにする。
func RequireROM(t testing.TB, rel string) string {
	t.Helper()
	path := ROMPath(t, rel)
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		t.Skipf("%s が無い。go run ./tools/fetch-test-roms で取得する", rel)
		return ""
	}
	return path
}
