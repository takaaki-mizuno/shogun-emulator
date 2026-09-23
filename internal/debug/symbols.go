package debug

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// symbolsVersion はシンボルファイルの形式のバージョン。
const symbolsVersion = 1

// RegionKind は利用者が指定した領域の種類。
type RegionKind string

// 領域の種類。
const (
	// RegionCode はコードとして逆アセンブルする。
	RegionCode RegionKind = "code"
	// RegionData はデータとして 1 バイトずつ表示する。
	RegionData RegionKind = "data"
)

// MarkedRegion は利用者がコードまたはデータと指定した領域。
//
// PRG-ROM のオフセットで持つ。バンク切り替えにより同じ CPU アドレスが
// 複数のバンクを指すためである。
type MarkedRegion struct {
	Start int        `json:"start"`
	End   int        `json:"end"`
	Kind  RegionKind `json:"kind"`
}

// SavedBreakpoint はファイルに保存するブレークポイント。
type SavedBreakpoint struct {
	Kind      string `json:"kind"`
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	Scanline  int    `json:"scanline,omitempty"`
	Dot       int    `json:"dot,omitempty"`
	Event     string `json:"event,omitempty"`
	Condition string `json:"condition,omitempty"`
	Enabled   bool   `json:"enabled"`
}

// Symbols はアドレスの名前・コードとデータの指定・ウォッチリスト・
// ブレークポイントを持つ。ROM ハッシュごとのファイルへ保存する
// （設計書 09 編 §9.9）。
//
// UI スレッドとエミュレーションゴルーチンの両方から触るため mu で守る。
type Symbols struct {
	mu          sync.Mutex
	labels      map[uint16]string
	regions     []MarkedRegion
	watch       []uint16
	breakpoints []SavedBreakpoint
}

// NewSymbols は空の Symbols を作る。
func NewSymbols() *Symbols { return &Symbols{labels: map[uint16]string{}} }

// Label は addr に付けた名前を返す。無ければ空文字列。
func (s *Symbols) Label(addr uint16) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.labels[addr]
}

// SetLabel は addr に名前を付ける。空文字列で名前を外す。
func (s *Symbols) SetLabel(addr uint16, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		delete(s.labels, addr)
		return
	}
	s.labels[addr] = name
}

// Labels は名前の一覧をアドレス順に返す。
func (s *Symbols) Labels() []LabeledAddr {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LabeledAddr, 0, len(s.labels))
	for a, n := range s.labels {
		out = append(out, LabeledAddr{Addr: a, Name: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out
}

// LabeledAddr は名前の付いたアドレス。
type LabeledAddr struct {
	Addr uint16
	Name string
}

// MarkRegion は PRG-ROM のオフセットの範囲をコードまたはデータと指定する。
//
// 後から指定したものが優先する。
func (s *Symbols) MarkRegion(start, end int, kind RegionKind) {
	if end < start {
		start, end = end, start
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.regions = append(s.regions, MarkedRegion{Start: start, End: end, Kind: kind})
}

// IsData は PRG-ROM のオフセットがデータと指定されているかを返す。
func (s *Symbols) IsData(off int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 後から指定したものが優先する。
	for i := len(s.regions) - 1; i >= 0; i-- {
		r := s.regions[i]
		if off >= r.Start && off <= r.End {
			return r.Kind == RegionData
		}
	}
	return false
}

// Regions は指定した領域の写しを返す。
func (s *Symbols) Regions() []MarkedRegion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MarkedRegion(nil), s.regions...)
}

// Watch はウォッチリストの写しを返す。
func (s *Symbols) Watch() []uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint16(nil), s.watch...)
}

// AddWatch はウォッチリストへ加える。すでにあれば何もしない。
func (s *Symbols) AddWatch(addr uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.watch {
		if a == addr {
			return
		}
	}
	s.watch = append(s.watch, addr)
}

// RemoveWatch はウォッチリストから取り除く。
func (s *Symbols) RemoveWatch(addr uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, a := range s.watch {
		if a == addr {
			s.watch = append(s.watch[:i], s.watch[i+1:]...)
			return
		}
	}
}

// SetBreakpoints は保存するブレークポイントを差し替える。
func (s *Symbols) SetBreakpoints(list []Breakpoint) {
	saved := make([]SavedBreakpoint, 0, len(list))
	for _, b := range list {
		if b.Temporary {
			continue
		}
		saved = append(saved, saveBreakpoint(b))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.breakpoints = saved
}

// Breakpoints は保存したブレークポイントを戻す。読めないものは飛ばす。
func (s *Symbols) Breakpoints() []Breakpoint {
	s.mu.Lock()
	saved := append([]SavedBreakpoint(nil), s.breakpoints...)
	s.mu.Unlock()
	out := make([]Breakpoint, 0, len(saved))
	for _, sb := range saved {
		if b, err := loadBreakpoint(sb); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// symbolsFile はファイルの形。
type symbolsFile struct {
	Version     int               `json:"version"`
	Labels      map[string]string `json:"labels"`
	Regions     []MarkedRegion    `json:"regions"`
	Watch       []string          `json:"watch"`
	Breakpoints []SavedBreakpoint `json:"breakpoints"`
}

// Empty は何も持っていないかを返す。
func (s *Symbols) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.labels) == 0 && len(s.regions) == 0 && len(s.watch) == 0 && len(s.breakpoints) == 0
}

// Save はファイルへ書き出す。一時ファイルへ書いてから rename する。
//
// 何も持っていないときはファイルを消す。ROM を開いただけで空のファイルを
// 残さないためである。map のキーは encoding/json が並べ替えて書く。内容が
// 同じなら同じバイト列になる。
func (s *Symbols) Save(path string) error {
	if s.Empty() {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	f := symbolsFile{
		Version:     symbolsVersion,
		Labels:      map[string]string{},
		Regions:     append([]MarkedRegion{}, s.regions...),
		Watch:       []string{},
		Breakpoints: append([]SavedBreakpoint{}, s.breakpoints...),
	}
	for a, n := range s.labels {
		f.Labels[hexAddr(a)] = n
	}
	for _, a := range s.watch {
		f.Watch = append(f.Watch, hexAddr(a))
	}
	s.mu.Unlock()

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// LoadSymbols はファイルから読む。ファイルが無いときは空の Symbols を返す。
func LoadSymbols(path string) (*Symbols, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewSymbols(), nil
	}
	if err != nil {
		return nil, err
	}
	var f symbolsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("debug: %s: %w", filepath.Base(path), err)
	}
	if f.Version > symbolsVersion {
		return nil, fmt.Errorf("debug: %s は新しい形式である（%d）", filepath.Base(path), f.Version)
	}
	s := NewSymbols()
	for k, n := range f.Labels {
		if a, err := parseHexAddr(k); err == nil {
			s.labels[a] = n
		}
	}
	s.regions = f.Regions
	for _, k := range f.Watch {
		if a, err := parseHexAddr(k); err == nil {
			s.watch = append(s.watch, a)
		}
	}
	s.breakpoints = f.Breakpoints
	return s, nil
}

// hexAddr はアドレスを "$C000" の形にする。
func hexAddr(a uint16) string { return fmt.Sprintf("$%04X", a) }

// parseHexAddr は "$C000" または "C000" を読む。
func parseHexAddr(s string) (uint16, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "$")
	v, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("debug: アドレス %q を読めない", s)
	}
	return uint16(v), nil
}

// ParseAddr は利用者が入力したアドレスを読む。$ の有無を問わず 16 進とする。
func ParseAddr(s string) (uint16, error) { return parseHexAddr(s) }

// breakKindNames は保存時の種別名。
var breakKindNames = []string{"exec", "read", "write", "ppu", "event"}

// eventNames は保存時のイベント名。
var eventNames = []string{"nmi", "irq", "reset", "sprite0", "mapperIRQ", "uninitRead", "mmc3Reload"}

// saveBreakpoint はブレークポイントを保存の形にする。
func saveBreakpoint(b Breakpoint) SavedBreakpoint {
	sb := SavedBreakpoint{Kind: breakKindNames[b.Kind], Enabled: b.Enabled}
	switch b.Kind {
	case BreakExec, BreakRead, BreakWrite:
		sb.Start = hexAddr(b.AddrStart)
		sb.End = hexAddr(b.AddrEnd)
	case BreakPPUPosition:
		sb.Scanline = b.Scanline
		sb.Dot = b.Dot
	case BreakEvent:
		sb.Event = eventNames[b.Event]
	}
	if b.Condition != nil {
		sb.Condition = b.Condition.Expr
	}
	return sb
}

// loadBreakpoint は保存の形からブレークポイントを戻す。
func loadBreakpoint(sb SavedBreakpoint) (Breakpoint, error) {
	b := Breakpoint{Enabled: sb.Enabled, Scanline: sb.Scanline, Dot: sb.Dot}
	kind := -1
	for i, n := range breakKindNames {
		if n == sb.Kind {
			kind = i
		}
	}
	if kind < 0 {
		return b, fmt.Errorf("debug: 知らない種別 %q", sb.Kind)
	}
	b.Kind = BreakKind(kind)
	switch b.Kind {
	case BreakExec, BreakRead, BreakWrite:
		start, err := parseHexAddr(sb.Start)
		if err != nil {
			return b, err
		}
		end := start
		if sb.End != "" {
			if end, err = parseHexAddr(sb.End); err != nil {
				return b, err
			}
		}
		b.AddrStart, b.AddrEnd = start, end
	case BreakEvent:
		ev := -1
		for i, n := range eventNames {
			if n == sb.Event {
				ev = i
			}
		}
		if ev < 0 {
			return b, fmt.Errorf("debug: 知らないイベント %q", sb.Event)
		}
		b.Event = EventKind(ev)
	}
	if sb.Condition != "" {
		c, err := ParseCondition(sb.Condition)
		if err != nil {
			return b, err
		}
		b.Condition = c
	}
	return b, nil
}
