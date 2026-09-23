package debug

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseCategories は名前の並びを集合にできることを確かめる。
func TestParseCategories(t *testing.T) {
	c, err := ParseCategories([]string{"ppu.register", "warn.compat", ""})
	if err != nil {
		t.Fatal(err)
	}
	if c != CatPPURegister|CatWarnCompat {
		t.Errorf("集合 = %b", c)
	}
	if _, err := ParseCategories([]string{"bogus"}); err == nil {
		t.Error("知らない名前を受け入れた")
	}
	if len(CategoryNames()) != 11 {
		t.Errorf("カテゴリの数 = %d, 期待 11", len(CategoryNames()))
	}
}

// TestLoggerFiltersByCategory は有効なカテゴリだけを記録することを
// 確かめる。
func TestLoggerFiltersByCategory(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(CatWarnCompat, &buf)
	l.Warnf("記録される %d", 1)
	l.Errorf("記録されない")
	if !strings.Contains(buf.String(), "記録される 1") {
		t.Errorf("warn.compat が出ていない: %s", buf.String())
	}
	if strings.Contains(buf.String(), "記録されない") {
		t.Errorf("無効な error が出た: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "category=warn.compat") {
		t.Errorf("カテゴリが付いていない: %s", buf.String())
	}
}

// TestLoggerEntriesSearch はログビューア向けの絞り込みを確かめる。
func TestLoggerEntriesSearch(t *testing.T) {
	l := NewLogger(CatWarnCompat|CatMapper, nil)
	l.Log(CatMapper, "バンクを 3 へ切り替えた")
	l.Log(CatWarnCompat, "色 $0D を使った")
	l.Log(CatMapper, "IRQ をアサートした")

	if got := l.Entries(CatMapper, "", 0); len(got) != 2 {
		t.Errorf("mapper の行 = %d, 期待 2", len(got))
	}
	if got := l.Entries(0, "$0D", 0); len(got) != 1 {
		t.Errorf("検索の結果 = %d, 期待 1", len(got))
	}
	if got := l.Entries(0, "", 1); len(got) != 1 || !strings.Contains(got[0].Message, "IRQ") {
		t.Errorf("最新 1 行 = %+v", got)
	}
}

// TestLoggerKeepsBoundedEntries は保持する行数に上限があることを
// 確かめる。
func TestLoggerKeepsBoundedEntries(t *testing.T) {
	l := NewLogger(CatMapper, nil)
	for i := range maxEntries + 10 {
		l.Log(CatMapper, "行 %d", i)
	}
	got := l.Entries(0, "", 0)
	if len(got) != maxEntries {
		t.Fatalf("保持した行 = %d, 期待 %d", len(got), maxEntries)
	}
	if !strings.Contains(got[0].Message, "行 10") {
		t.Errorf("最も古い行 = %q, 期待 \"行 10\"", got[0].Message)
	}
}

// TestRotatingFile は上限を超えたとき世代をずらすことを確かめる。
func TestRotatingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shogun.log")
	r, err := OpenRotatingFile(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"aaaaaaaa\n", "bbbbbbbb\n", "cccccccc\n", "dddddddd\n"} {
		if _, err := r.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	r.Close()

	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	if got := read("shogun.log"); got != "dddddddd\n" {
		t.Errorf("現在の世代 = %q", got)
	}
	if got := read("shogun.log.1"); got != "cccccccc\n" {
		t.Errorf("1 世代前 = %q", got)
	}
	if got := read("shogun.log.2"); got != "bbbbbbbb\n" {
		t.Errorf("2 世代前 = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "shogun.log.3")); err == nil {
		t.Error("世代数を超えたファイルが残っている")
	}
}

// BenchmarkDisabledCategoryCheck は無効なカテゴリの判定の費用を測る。
//
// 呼び出し側は Categories を直接見る。関数呼び出しを介さないため、
// 無効なときの費用は比較 1 回になる。
func BenchmarkDisabledCategoryCheck(b *testing.B) {
	l := NewLogger(CatWarnCompat, nil)
	n := 0
	for i := 0; i < b.N; i++ {
		if l.Categories&CatPPURegister != 0 {
			l.Log(CatPPURegister, "%d", i)
			n++
		}
	}
	if n != 0 {
		b.Fatal("無効なカテゴリが記録された")
	}
}

// BenchmarkEnabledCategoryLog は有効なカテゴリを記録する費用を測る。
func BenchmarkEnabledCategoryLog(b *testing.B) {
	l := NewLogger(CatPPURegister, nil)
	for i := 0; i < b.N; i++ {
		if l.Categories&CatPPURegister != 0 {
			l.Log(CatPPURegister, "$2000 へ %02X を書いた", i&0xFF)
		}
	}
}
