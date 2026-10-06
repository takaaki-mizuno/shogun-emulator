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

// symbolsVersion はシンボルファイルの形式のバージョン（設計書 09 編 §9.9）。
const symbolsVersion = 2

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

// SymbolSpace は Symbol の位置の空間（設計書 14 編 §14.11.1）。
type SymbolSpace uint8

// Symbol の位置の空間。
const (
	// SymSpaceCPU は $0000–$7FFF の CPU アドレス（内蔵 RAM、レジスタ、PRG-RAM）。
	SymSpaceCPU SymbolSpace = iota
	// SymSpacePRG は PRG-ROM のファイルオフセット（ヘッダを除く）。
	SymSpacePRG
	// SymSpacePRGAny は $8000–$FFFF の CPU アドレスで、バンクを問わないもの。
	SymSpacePRGAny
)

var symSpaceNames = []string{"cpu", "prg", "prg_any"}

// String は空間の名前（ファイルに書く形）を返す。
func (s SymbolSpace) String() string { return symSpaceNames[s] }

// SymbolLoc は Symbol の位置。
type SymbolLoc struct {
	Space  SymbolSpace
	Offset uint32
}

// SymbolKind は Symbol の種類。
type SymbolKind uint8

// Symbol の種類。
const (
	SymLabel SymbolKind = iota
	SymVariable
	SymConstant
)

var symKindNames = []string{"label", "variable", "constant"}

// String は種類の名前を返す。
func (k SymbolKind) String() string { return symKindNames[k] }

// SymbolOrigin は Symbol の由来。
type SymbolOrigin uint8

// Symbol の由来。
const (
	// OriginDbg は ca65/ld65 のデバッグ情報から読んだもの。保存しない。
	OriginDbg SymbolOrigin = iota
	// OriginUser は利用者やエージェントが付けたもの。
	OriginUser
	// OriginLegacy はバージョン 1 のファイルから移したもの。
	OriginLegacy
)

var originNames = []string{"dbg", "user", "legacy"}

// String は由来の名前を返す。
func (o SymbolOrigin) String() string { return originNames[o] }

// SourceRef はソースの位置。
type SourceRef struct {
	File string
	Line int
}

// String は "src/main.s:120" の形にする。位置が無いとき空。
func (r SourceRef) String() string {
	if r.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", r.File, r.Line)
}

// Symbol は位置に付けた名前（設計書 14 編 §14.11.1）。
type Symbol struct {
	Name string
	Loc  SymbolLoc
	// Size はバイト数。不明なら 0。
	Size   uint16
	Kind   SymbolKind
	Source SourceRef
	Origin SymbolOrigin
	// Value は定数（SymConstant）の値。
	Value int64
	// CPU は PRG-ROM の Symbol の CPU アドレス（リンクしたときの位置）。
	// 実行ブレークポイントなど CPU アドレスが要る用途に使う。0 は不明。
	CPU uint16
}

// LocOf は CPU アドレスを Symbol の位置にする。$8000 以上は現在のバンク構成で
// PRG-ROM のオフセットに直す。直せないときは SymSpacePRGAny とする。
func LocOf(addr uint16, off Offsetter) SymbolLoc {
	if addr < 0x8000 {
		return SymbolLoc{Space: SymSpaceCPU, Offset: uint32(addr)}
	}
	if off != nil {
		if o, ok := off(addr); ok {
			return SymbolLoc{Space: SymSpacePRG, Offset: uint32(o)}
		}
	}
	return SymbolLoc{Space: SymSpacePRGAny, Offset: uint32(addr)}
}

// SourceLine は PRG-ROM のオフセットの範囲とソースの位置の対応。
type SourceLine struct {
	Off, Len int
	Source   SourceRef
}

// Symbols は Symbol・コードとデータの指定・ウォッチリスト・ブレークポイント・
// ソース行・Game State Definition を持つ。ROM ごとに 1 つで、同じ ROM の
// Instance が共有する（設計書 14 編 §14.3.1）。利用者が付けたものを ROM
// ハッシュごとのファイルへ保存する（設計書 09 編 §9.9）。
//
// UI スレッドとエミュレーションゴルーチンの両方から触るため mu で守る。
type Symbols struct {
	mu          sync.Mutex
	syms        []Symbol
	regions     []MarkedRegion
	watch       []SymbolLoc
	breakpoints []SavedBreakpoint
	// lines はソース行の対応。オフセット順で重ならない。
	lines []SourceLine
	// dataRanges は .dbg のコードでないセグメントの範囲。
	dataRanges []OffsetRange
	// dbgPath は読み込んだ .dbg のパス。
	dbgPath string
	// gamestate は Game State Definition。
	gamestate *GameStateDef
	// projectLoaded は .dbg と Game State Definition を読み込んだことを表す。
	projectLoaded bool

	// gen は Symbol を変えるたびに増える版。解析結果を覚える側が使う。
	gen uint64

	// 索引。syms を変えたら dirty にし、次に引くときに作り直す。
	dirty   bool
	byName  map[string]int
	byLoc   map[SymbolLoc]int
	ordered []int // 位置のある Symbol を（空間、オフセット）の順に並べたもの
}

// NewSymbols は空の Symbols を作る。
func NewSymbols() *Symbols { return &Symbols{gamestate: NewGameStateDef()} }

// originRank は同じ名前や位置に複数の Symbol があるときの優先順位を返す。
// 小さいほど優先する。利用者が付けたものを .dbg より優先する。
func originRank(o SymbolOrigin) int {
	switch o {
	case OriginUser:
		return 0
	case OriginLegacy:
		return 1
	}
	return 2
}

// reindex は索引を作り直す。mu を持って呼ぶ。
func (s *Symbols) reindex() {
	if !s.dirty && s.byName != nil {
		return
	}
	s.byName = make(map[string]int, len(s.syms))
	s.byLoc = make(map[SymbolLoc]int, len(s.syms))
	s.ordered = s.ordered[:0]
	for i, sym := range s.syms {
		if j, ok := s.byName[sym.Name]; !ok || originRank(sym.Origin) < originRank(s.syms[j].Origin) {
			s.byName[sym.Name] = i
		}
		if sym.Kind == SymConstant {
			continue
		}
		if j, ok := s.byLoc[sym.Loc]; !ok || originRank(sym.Origin) < originRank(s.syms[j].Origin) {
			s.byLoc[sym.Loc] = i
		}
		s.ordered = append(s.ordered, i)
	}
	sort.SliceStable(s.ordered, func(a, b int) bool {
		la, lb := s.syms[s.ordered[a]].Loc, s.syms[s.ordered[b]].Loc
		if la.Space != lb.Space {
			return la.Space < lb.Space
		}
		return la.Offset < lb.Offset
	})
	s.dirty = false
}

// LocSpan は位置の範囲（Lo と Hi を含む）。PRG-ROM の範囲はバンクを区別する。
type LocSpan struct {
	Space  SymbolSpace
	Lo, Hi uint32
	// CPULo と CPUHi は範囲に当たる CPU アドレス（トレースの絞り込みに使う）。
	CPULo, CPUHi uint16
	// Name は名前から作った範囲の名前。
	Name string
}

// Contains は位置が範囲に入るかを返す。
func (r LocSpan) Contains(loc SymbolLoc) bool {
	return loc.Space == r.Space && loc.Offset >= r.Lo && loc.Offset <= r.Hi
}

// spanOf は ordered の k 番目の Symbol の範囲を求める。サイズが分かれば
// その範囲、分からなければ同じ空間の次の Symbol の直前まで（最大 256 バイト）。
// mu を持って呼ぶ。
func (s *Symbols) spanOf(k int) LocSpan {
	sym := s.syms[s.ordered[k]]
	lo := sym.Loc.Offset
	hi := lo + 255
	if sym.Size > 0 {
		hi = lo + uint32(sym.Size) - 1
	} else {
		for j := k + 1; j < len(s.ordered); j++ {
			next := s.syms[s.ordered[j]].Loc
			if next.Space != sym.Loc.Space {
				break
			}
			if next.Offset > lo {
				hi = min(hi, next.Offset-1)
				break
			}
		}
	}
	cpu := uint32(sym.CPU)
	if sym.Loc.Space != SymSpacePRG {
		cpu = lo
	}
	return LocSpan{Space: sym.Loc.Space, Lo: lo, Hi: hi, CPULo: uint16(cpu), CPUHi: uint16(min(cpu+hi-lo, 0xFFFF)), Name: sym.Name}
}

// SpanOfName は名前の Symbol から次の Symbol までの範囲を返す（trace.query の
// pc と profile.start の idle に名前を渡したとき）。
func (s *Symbols) SpanOfName(name string) (LocSpan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindex()
	for k, i := range s.ordered {
		if s.syms[i].Name == name {
			return s.spanOf(k), true
		}
	}
	return LocSpan{}, false
}

// LabelSpans は名前が match に合うラベルの範囲を返す（アイドルループの判定 2）。
func (s *Symbols) LabelSpans(match func(name string) bool) []LocSpan {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindex()
	var out []LocSpan
	for k, i := range s.ordered {
		sym := s.syms[i]
		if sym.Kind == SymLabel && match(sym.Name) {
			out = append(out, s.spanOf(k))
		}
	}
	return out
}

// NameAtLoc は位置の名前を "name" か "name+3" の形で返す。無いとき空。
// addr は位置に当たる CPU アドレス（バンクを問わない名前を引くのに使う）。
func (s *Symbols) NameAtLoc(loc SymbolLoc, addr uint16) string {
	off := func(uint16) (int, bool) { return int(loc.Offset), loc.Space == SymSpacePRG }
	name, d, ok := s.NearestAt(addr, off)
	if !ok {
		return ""
	}
	if d == 0 {
		return name
	}
	return name + "+" + itoa(d)
}

// LabelAt は CPU アドレス addr の位置に付けた名前を返す。無ければ空。
//
// $8000 以上は現在のバンク構成で PRG-ROM のオフセットに直して引き、無ければ
// バンクを問わない名前を引く（設計書 14 編 §14.11.1）。
func (s *Symbols) LabelAt(addr uint16, off Offsetter) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindex()
	loc := LocOf(addr, off)
	if i, ok := s.byLoc[loc]; ok {
		return s.syms[i].Name
	}
	if loc.Space == SymSpacePRG {
		if i, ok := s.byLoc[SymbolLoc{Space: SymSpacePRGAny, Offset: uint32(addr)}]; ok {
			return s.syms[i].Name
		}
	}
	return ""
}

// NearestAt は addr 以前で最も近い名前と差を返す（"main_loop+3" の表示に
// 使う）。サイズが分かる Symbol はその範囲の中だけ、分からないものは
// 256 バイト以内だけを対象にする。
func (s *Symbols) NearestAt(addr uint16, off Offsetter) (string, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindex()
	try := func(loc SymbolLoc) (string, int, bool) {
		// loc 以下で最後の Symbol を二分探索する。
		i := sort.Search(len(s.ordered), func(k int) bool {
			l := s.syms[s.ordered[k]].Loc
			return l.Space > loc.Space || l.Space == loc.Space && l.Offset > loc.Offset
		}) - 1
		if i < 0 {
			return "", 0, false
		}
		sym := s.syms[s.ordered[i]]
		if sym.Loc.Space != loc.Space {
			return "", 0, false
		}
		// 同じ位置に複数あれば優先するものを使う。
		if j, ok := s.byLoc[sym.Loc]; ok {
			sym = s.syms[j]
		}
		d := int(loc.Offset - sym.Loc.Offset)
		limit := 256
		if sym.Size > 0 {
			limit = int(sym.Size)
		}
		if d >= limit {
			return "", 0, false
		}
		return sym.Name, d, true
	}
	loc := LocOf(addr, off)
	if n, d, ok := try(loc); ok {
		return n, d, true
	}
	if loc.Space == SymSpacePRG {
		return try(SymbolLoc{Space: SymSpacePRGAny, Offset: uint32(addr)})
	}
	return "", 0, false
}

// Lookup は名前で Symbol を引く。
func (s *Symbols) Lookup(name string) (Symbol, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reindex()
	i, ok := s.byName[name]
	if !ok {
		return Symbol{}, false
	}
	return s.syms[i], true
}

// HasSymbol は位置を持つ Symbol か定数があるかを返す。
func (s *Symbols) HasSymbol(name string) bool {
	_, ok := s.Lookup(name)
	return ok
}

// All は Symbol を定義の順に返す。
func (s *Symbols) All() []Symbol {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Symbol(nil), s.syms...)
}

// SetLabel は CPU アドレス addr に利用者の名前を付ける。空文字列で名前を
// 外す。$8000 以上は現在のバンク構成の PRG-ROM の位置で持つ。
func (s *Symbols) SetLabel(addr uint16, name string, off Offsetter) {
	s.SetSymbol(Symbol{Name: strings.TrimSpace(name), Loc: LocOf(addr, off), Kind: SymLabel, Origin: OriginUser, CPU: addr})
}

// SetSymbol は利用者の Symbol を付ける。同じ位置の利用者の Symbol と、
// 同じ名前の利用者の Symbol を置き換える。名前が空なら、その位置の
// 利用者の Symbol を外す。
func (s *Symbols) SetSymbol(sym Symbol) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.syms[:0:0]
	for _, x := range s.syms {
		if x.Origin != OriginDbg && (x.Loc == sym.Loc && x.Kind != SymConstant || sym.Name != "" && x.Name == sym.Name) {
			continue
		}
		keep = append(keep, x)
	}
	if sym.Name != "" {
		if sym.Origin == OriginDbg {
			sym.Origin = OriginUser
		}
		keep = append(keep, sym)
	}
	s.syms = keep
	s.dirty = true
	s.gen++
}

// RemoveSymbol は利用者の Symbol を名前で外す。外したとき true。
func (s *Symbols) RemoveSymbol(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.syms[:0:0]
	removed := false
	for _, x := range s.syms {
		if x.Origin != OriginDbg && x.Name == name {
			removed = true
			continue
		}
		keep = append(keep, x)
	}
	s.syms = keep
	s.dirty = true
	s.gen++
	return removed
}

// SetDbg は .dbg から読んだ Symbol とソース行を差し替える（設計書 14 編
// §14.11.2）。info が nil のときは .dbg の内容を外す。
func (s *Symbols) SetDbg(info *DbgInfo, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.syms[:0:0]
	for _, x := range s.syms {
		if x.Origin != OriginDbg {
			keep = append(keep, x)
		}
	}
	s.lines = nil
	s.dbgPath = ""
	s.dataRanges = nil
	if info != nil {
		keep = append(keep, info.Symbols...)
		s.lines = info.Lines
		s.dbgPath = path
		s.dataRanges = info.DataRanges
	}
	s.syms = keep
	s.dirty = true
	s.gen++
}

// Generation は Symbol の版を返す。Symbol を変えるたびに増える。
func (s *Symbols) Generation() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// ProjectLoaded は LoadProject で読み込んだことがあるかを返す。
//
// 同じ ROM の Instance は Symbols を共有する。2 つ目以降の Instance の
// 読み込みで読み直すと、実行中に定義した Game State の項目が消えるため、
// 読み込みは 1 回だけにする。読み直しは ReloadProject で明示的に行う。
func (s *Symbols) ProjectLoaded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projectLoaded
}

// ResetProject は .dbg と Game State Definition を読み込んでいない状態に戻す。
// 次の ROM の読み込みで読み直させる（rom.reload）。
func (s *Symbols) ResetProject() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projectLoaded = false
}

// DbgPath は読み込んだ .dbg のパスを返す。読み込んでいないとき空。
func (s *Symbols) DbgPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dbgPath
}

// SourceAt は PRG-ROM のオフセットに当たるソースの位置を返す。
func (s *Symbols) SourceAt(off int) (SourceRef, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := sort.Search(len(s.lines), func(k int) bool { return s.lines[k].Off > off }) - 1
	if i < 0 {
		return SourceRef{}, false
	}
	l := s.lines[i]
	if off >= l.Off+l.Len {
		return SourceRef{}, false
	}
	return l.Source, true
}

// GameState は Game State Definition を返す。
func (s *Symbols) GameState() *GameStateDef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gamestate
}

// SetGameState は Game State Definition を差し替える。
func (s *Symbols) SetGameState(def *GameStateDef) {
	if def == nil {
		def = NewGameStateDef()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gamestate = def
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

// ExecDataMap は PRG-ROM のオフセットごとに、実行してはいけない（データと
// 指定された）位置かを表す表を作る。.dbg のコードでないセグメントに、利用者の
// 指定（後のものが優先）を重ねる。Diagnostic の execute_data が使う。
func (s *Symbols) ExecDataMap(prgSize int) []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := make([]bool, prgSize)
	set := func(start, end int, v bool) {
		for i := max(start, 0); i <= end && i < prgSize; i++ {
			m[i] = v
		}
	}
	for _, r := range s.dataRanges {
		set(r.Start, r.End, true)
	}
	for _, r := range s.regions {
		set(r.Start, r.End, r.Kind == RegionData)
	}
	return m
}

// Regions は指定した領域の写しを返す。
func (s *Symbols) Regions() []MarkedRegion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MarkedRegion(nil), s.regions...)
}

// watchLoc はウォッチの CPU アドレスを位置にする。ウォッチは RAM の値を
// 見るものであり、$8000 以上はバンクを問わない位置で持つ。
func watchLoc(addr uint16) SymbolLoc {
	if addr < 0x8000 {
		return SymbolLoc{Space: SymSpaceCPU, Offset: uint32(addr)}
	}
	return SymbolLoc{Space: SymSpacePRGAny, Offset: uint32(addr)}
}

// Watch はウォッチリストの CPU アドレスを返す。PRG-ROM の位置のものは含めない。
func (s *Symbols) Watch() []uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint16, 0, len(s.watch))
	for _, l := range s.watch {
		if l.Space != SymSpacePRG {
			out = append(out, uint16(l.Offset))
		}
	}
	return out
}

// AddWatch はウォッチリストへ加える。すでにあれば何もしない。
func (s *Symbols) AddWatch(addr uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := watchLoc(addr)
	for _, a := range s.watch {
		if a == l {
			return
		}
	}
	s.watch = append(s.watch, l)
}

// RemoveWatch はウォッチリストから取り除く。
func (s *Symbols) RemoveWatch(addr uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := watchLoc(addr)
	for i, a := range s.watch {
		if a == l {
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

// Breakpoints は保存したブレークポイントを戻す。条件式の名前は r で
// 確かめる。読めないものは飛ばす。
func (s *Symbols) Breakpoints(r Resolver) []Breakpoint {
	s.mu.Lock()
	saved := append([]SavedBreakpoint(nil), s.breakpoints...)
	s.mu.Unlock()
	out := make([]Breakpoint, 0, len(saved))
	for _, sb := range saved {
		if b, err := loadBreakpoint(sb, r); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// savedSymbol はファイルに書く Symbol。
type savedSymbol struct {
	Name   string `json:"name"`
	Space  string `json:"space"`
	Offset string `json:"offset"`
	Size   int    `json:"size,omitempty"`
	// CPU は PRG-ROM の Symbol の CPU アドレス。
	CPU string `json:"cpu,omitempty"`
}

// savedLoc はファイルに書くウォッチの位置。
type savedLoc struct {
	Space  string `json:"space"`
	Offset string `json:"offset"`
}

// symbolsFile はファイルの形（バージョン 2）。
type symbolsFile struct {
	Version     int               `json:"version"`
	Symbols     []savedSymbol     `json:"symbols"`
	Regions     []MarkedRegion    `json:"regions"`
	Watch       []savedLoc        `json:"watch"`
	Breakpoints []SavedBreakpoint `json:"breakpoints"`
}

// symbolsFileV1 はバージョン 1 のファイルの形。
type symbolsFileV1 struct {
	Version     int               `json:"version"`
	Labels      map[string]string `json:"labels"`
	Regions     []MarkedRegion    `json:"regions"`
	Watch       []string          `json:"watch"`
	Breakpoints []SavedBreakpoint `json:"breakpoints"`
}

// savable は保存する Symbol（.dbg から読んだもの以外）を返す。mu を持って呼ぶ。
func (s *Symbols) savable() []Symbol {
	var out []Symbol
	for _, x := range s.syms {
		if x.Origin != OriginDbg {
			out = append(out, x)
		}
	}
	return out
}

// Empty は保存するものを何も持っていないかを返す。
func (s *Symbols) Empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.savable()) == 0 && len(s.regions) == 0 && len(s.watch) == 0 && len(s.breakpoints) == 0
}

// locString は位置のオフセットを "$0300" の形にする。
func locString(l SymbolLoc) string {
	if l.Offset > 0xFFFF {
		return fmt.Sprintf("$%X", l.Offset)
	}
	return fmt.Sprintf("$%04X", l.Offset)
}

// parseLoc は空間の名前とオフセットの文字列を位置にする。
func parseLoc(space, offset string) (SymbolLoc, error) {
	sp := -1
	for i, n := range symSpaceNames {
		if n == space {
			sp = i
		}
	}
	if sp < 0 {
		return SymbolLoc{}, fmt.Errorf("debug: 知らない空間 %q", space)
	}
	v, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(offset), "$"), 16, 32)
	if err != nil {
		return SymbolLoc{}, fmt.Errorf("debug: オフセット %q を読めない", offset)
	}
	return SymbolLoc{Space: SymbolSpace(sp), Offset: uint32(v)}, nil
}

// Save はファイルへ書き出す。一時ファイルへ書いてから rename する。
//
// 何も持っていないときはファイルを消す。ROM を開いただけで空のファイルを
// 残さないためである。.dbg から読んだ Symbol は書かない（設計書 09 編 §9.9）。
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
		Symbols:     []savedSymbol{},
		Regions:     append([]MarkedRegion{}, s.regions...),
		Watch:       []savedLoc{},
		Breakpoints: append([]SavedBreakpoint{}, s.breakpoints...),
	}
	for _, x := range s.savable() {
		ss := savedSymbol{Name: x.Name, Space: x.Loc.Space.String(), Offset: locString(x.Loc), Size: int(x.Size)}
		if x.Loc.Space == SymSpacePRG && x.CPU != 0 {
			ss.CPU = hexAddr(x.CPU)
		}
		f.Symbols = append(f.Symbols, ss)
	}
	for _, l := range s.watch {
		f.Watch = append(f.Watch, savedLoc{Space: l.Space.String(), Offset: locString(l)})
	}
	s.mu.Unlock()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// writeFileAtomic は一時ファイルに書いてから名前を変える。
func writeFileAtomic(path string, data []byte) error {
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
// バージョン 1 のファイルはバージョン 2 へ移す（設計書 09 編 §9.9）。
func LoadSymbols(path string) (*Symbols, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewSymbols(), nil
	}
	if err != nil {
		return nil, err
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("debug: %s: %w", filepath.Base(path), err)
	}
	if head.Version > symbolsVersion {
		return nil, fmt.Errorf("debug: %s は新しい形式である（%d）", filepath.Base(path), head.Version)
	}
	s := NewSymbols()
	s.dirty = true
	s.gen++
	if head.Version <= 1 {
		var f symbolsFileV1
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("debug: %s: %w", filepath.Base(path), err)
		}
		// アドレス順に移す。map の順に依らない並びにするためである。
		type label struct {
			addr uint16
			name string
		}
		var labels []label
		for k, n := range f.Labels {
			if a, err := parseHexAddr(k); err == nil && n != "" {
				labels = append(labels, label{a, n})
			}
		}
		sort.Slice(labels, func(i, j int) bool { return labels[i].addr < labels[j].addr })
		for _, l := range labels {
			origin := OriginUser
			if l.addr >= 0x8000 {
				// どのバンクの位置か分からない。
				origin = OriginLegacy
			}
			s.syms = append(s.syms, Symbol{Name: l.name, Loc: watchLoc(l.addr), Kind: SymLabel, Origin: origin, CPU: l.addr})
		}
		s.regions = f.Regions
		for _, k := range f.Watch {
			if a, err := parseHexAddr(k); err == nil {
				s.watch = append(s.watch, watchLoc(a))
			}
		}
		s.breakpoints = f.Breakpoints
		return s, nil
	}
	var f symbolsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("debug: %s: %w", filepath.Base(path), err)
	}
	for _, x := range f.Symbols {
		loc, err := parseLoc(x.Space, x.Offset)
		if err != nil {
			continue
		}
		origin := OriginUser
		if loc.Space == SymSpacePRGAny {
			origin = OriginLegacy
		}
		sym := Symbol{Name: x.Name, Loc: loc, Size: uint16(x.Size), Kind: SymLabel, Origin: origin}
		if cpu, err := parseHexAddr(x.CPU); err == nil && x.CPU != "" {
			sym.CPU = cpu
		}
		if loc.Space != SymSpacePRG {
			sym.CPU = uint16(loc.Offset)
		}
		s.syms = append(s.syms, sym)
	}
	s.regions = f.Regions
	for _, w := range f.Watch {
		if loc, err := parseLoc(w.Space, w.Offset); err == nil {
			s.watch = append(s.watch, loc)
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
func loadBreakpoint(sb SavedBreakpoint, r Resolver) (Breakpoint, error) {
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
		c, err := ParseConditionWith(sb.Condition, r)
		if err != nil {
			return b, err
		}
		b.Condition = c
	}
	return b, nil
}

// ParseBreakKind は種別の名前（exec・read・write・ppu・event）を読む。
func ParseBreakKind(name string) (BreakKind, bool) {
	for i, n := range breakKindNames {
		if n == name {
			return BreakKind(i), true
		}
	}
	return 0, false
}

// BreakKindName は種別の名前を返す。
func BreakKindName(k BreakKind) string { return breakKindNames[k] }

// ParseEvent はイベントの名前（nmi・irq・reset・sprite0・mapperIRQ・
// uninitRead・mmc3Reload）を読む。
func ParseEvent(name string) (EventKind, bool) {
	for i, n := range eventNames {
		if n == name {
			return EventKind(i), true
		}
	}
	return 0, false
}

// EventName はイベントの名前を返す。
func EventName(e EventKind) string { return eventNames[e] }

// EventNames はイベントの名前の一覧を返す。
func EventNames() []string { return append([]string(nil), eventNames...) }
