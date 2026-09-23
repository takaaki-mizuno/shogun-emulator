package debug

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Category はログのカテゴリ。ビットの集合として持つ。
type Category uint32

// カテゴリの一覧（設計書 09 編 §9.8）。
const (
	CatTraceCPU Category = 1 << iota
	CatTraceCPUBus
	CatPPURegister
	CatPPUTiming
	CatAPURegister
	CatAPUFrame
	CatMapper
	CatDMA
	CatInput
	CatWarnCompat
	CatError
)

// categoryNames はカテゴリの名前。ビットの順に並べる。
var categoryNames = []string{
	"trace.cpu", "trace.cpu.bus", "ppu.register", "ppu.timing",
	"apu.register", "apu.frame", "mapper", "dma", "input",
	"warn.compat", "error",
}

// CategoryNames はカテゴリの名前を並び順で返す。
func CategoryNames() []string { return append([]string(nil), categoryNames...) }

// CategoryByName は名前からカテゴリを返す。
func CategoryByName(name string) (Category, bool) {
	for i, n := range categoryNames {
		if n == name {
			return Category(1) << i, true
		}
	}
	return 0, false
}

// ParseCategories は名前の並びをカテゴリの集合にする。
func ParseCategories(names []string) (Category, error) {
	var c Category
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		cat, ok := CategoryByName(n)
		if !ok {
			return 0, fmt.Errorf("debug: 知らないログカテゴリ %q", n)
		}
		c |= cat
	}
	return c, nil
}

// String はカテゴリの名前を返す。複数のときは最初の 1 つ。
func (c Category) String() string {
	for i, n := range categoryNames {
		if c&(Category(1)<<i) != 0 {
			return n
		}
	}
	return "none"
}

// DefaultCategories は既定で有効なカテゴリ。
//
// 量の多いカテゴリは既定で無効にする。warn.compat と error は稀であり、
// 常に記録しても実行に影響しない。
const DefaultCategories = CatWarnCompat | CatError

// Entry はログビューアへ渡す 1 行。
type Entry struct {
	Time     time.Time
	Category Category
	Message  string
}

// maxEntries はログビューア向けに保持する行数の上限。
//
// 古い行から捨てる。量の多いカテゴリを有効にしてもメモリを使い切らない
// ようにする。
const maxEntries = 10000

// Logger はカテゴリ付きのログ。
//
// 出力するかの判定は Categories を呼び出し側が直接見て行う。関数呼び出しを
// 介さないのは、無効なカテゴリの費用を比較 1 回に抑えるためである。
//
//	if l.Categories&CatPPURegister != 0 {
//	    l.Log(CatPPURegister, "...")
//	}
//
// trace.cpu は Tracer の専用の整形で出力し、このログを通さない。毎秒
// 30 万行の出力に log/slog は重い。
type Logger struct {
	// Categories は有効なカテゴリの集合。
	Categories Category

	slog *slog.Logger

	mu      sync.Mutex
	entries []Entry
	start   int
	// repeats は warn.compat と error の内容ごとの記録の回数。
	repeats map[string]int
}

// NewLogger はログを作る。w が nil のとき出力先を持たず、ログビューア
// 向けの保持だけを行う。
func NewLogger(cats Category, w io.Writer) *Logger {
	l := &Logger{Categories: cats}
	if w != nil {
		l.slog = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return l
}

// 同じ内容の warn.compat と error を記録する回数の上限と、覚えておく内容の
// 種類の上限（設計書 09 編 §9.8）。
const (
	maxRepeats      = 3
	maxRepeatTracks = 1000
)

// Log はカテゴリ c の 1 行を記録する。有効かどうかは呼び出し側が確かめる。
//
// warn.compat と error は同じ内容を maxRepeats 回まで記録する。毎フレーム
// 同じ事象を起こすプログラムで出力が埋まらないようにするためである。
func (l *Logger) Log(c Category, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if c&(CatWarnCompat|CatError) != 0 {
		switch n := l.countRepeat(msg); {
		case n == maxRepeats+1:
			msg += "（同じ内容が続くため、以降は記録しない）"
		case n > maxRepeats+1:
			return
		}
	}
	if l.slog != nil {
		level := slog.LevelDebug
		switch c {
		case CatWarnCompat:
			level = slog.LevelWarn
		case CatError:
			level = slog.LevelError
		}
		l.slog.Log(context.Background(), level, msg, "category", c.String())
	}
	l.keep(Entry{Time: time.Now(), Category: c, Message: msg})
}

// countRepeat は msg を記録した回数を数え、その回数を返す。
func (l *Logger) countRepeat(msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.repeats == nil || len(l.repeats) >= maxRepeatTracks {
		l.repeats = map[string]int{}
	}
	l.repeats[msg]++
	return l.repeats[msg]
}

// ResetRepeats は同じ内容の記録の数を数え直す。ROM を読み込んだときに呼ぶ。
func (l *Logger) ResetRepeats() {
	l.mu.Lock()
	l.repeats = nil
	l.mu.Unlock()
}

// Warnf は warn.compat を記録する。有効でなければ何もしない。
//
// エミュレーションコアの Warn フィールドへ渡す形に合わせる。
func (l *Logger) Warnf(format string, args ...any) {
	if l.Categories&CatWarnCompat == 0 {
		return
	}
	l.Log(CatWarnCompat, format, args...)
}

// Errorf は error を記録する。有効でなければ何もしない。
func (l *Logger) Errorf(format string, args ...any) {
	if l.Categories&CatError == 0 {
		return
	}
	l.Log(CatError, format, args...)
}

// keep はログビューア向けに保持する。
func (l *Logger) keep(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) < maxEntries {
		l.entries = append(l.entries, e)
		return
	}
	l.entries[l.start] = e
	l.start = (l.start + 1) % maxEntries
}

// Entries は保持している行を古い順に返す。
//
// filter が 0 のとき全カテゴリ、search が空でないときその文字列を
// 含む行だけを返す。最大 limit 行（新しいものを優先）。
func (l *Logger) Entries(filter Category, search string, limit int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Entry
	n := len(l.entries)
	for i := range n {
		e := l.entries[(l.start+i)%n]
		if filter != 0 && e.Category&filter == 0 {
			continue
		}
		if search != "" && !strings.Contains(e.Message, search) {
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Clear は保持している行を捨てる。
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
	l.start = 0
}

// OpenLogOutput は出力先を開く。kind は file・stdout・stderr のいずれか。
//
// file のときは path へ書き、maxBytes を超えたら世代をずらす。
func OpenLogOutput(kind, path string, maxBytes int64, generations int) (io.WriteCloser, error) {
	switch kind {
	case "stdout":
		return nopCloser{os.Stdout}, nil
	case "stderr", "":
		return nopCloser{os.Stderr}, nil
	case "file":
		return OpenRotatingFile(path, maxBytes, generations)
	}
	return nil, fmt.Errorf("debug: 知らないログの出力先 %q", kind)
}

// nopCloser は閉じない WriteCloser。標準出力を閉じないために使う。
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// RotatingFile は大きさで世代をずらすログファイル。
//
// path が上限を超えたら path.1 へ、path.1 は path.2 へとずらし、
// generations を超えた世代を消す。
type RotatingFile struct {
	mu          sync.Mutex
	path        string
	maxBytes    int64
	generations int
	f           *os.File
	size        int64
}

// OpenRotatingFile はログファイルを開く。
func OpenRotatingFile(path string, maxBytes int64, generations int) (*RotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if generations < 1 {
		generations = 1
	}
	r := &RotatingFile{path: path, maxBytes: maxBytes, generations: generations}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

// open は path を追記で開く。
func (r *RotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f = f
	r.size = st.Size()
	return nil
}

// Write は書き込み、上限を超えるときは先に世代をずらす。
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > r.maxBytes && r.size > 0 {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate は世代をずらす。
func (r *RotatingFile) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	os.Remove(fmt.Sprintf("%s.%d", r.path, r.generations))
	for i := r.generations - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		return err
	}
	return r.open()
}

// Close はファイルを閉じる。
func (r *RotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
