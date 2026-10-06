package debug

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ca65/ld65 のデバッグ情報（.dbg）の読み込み（設計書 14 編 §14.11.2）。
//
// ld65 --dbgfile が出力するテキスト形式を読む。1 行が「種類 タブ key=value の
// カンマ区切り」である。file・seg・span・line・sym・scope の行を読み、
// それ以外の種類の行と知らない key は読み飛ばす。ld65 の版による違いに
// 備えるためである。

// inesHeaderSize は iNES ヘッダの大きさ。ooffs から引いて PRG-ROM のオフセットにする。
const inesHeaderSize = 16

// DbgInfo は .dbg から読んだ内容。
type DbgInfo struct {
	Symbols []Symbol
	// Lines は PRG-ROM のオフセットとソースの位置の対応。オフセット順で重ならない。
	Lines []SourceLine
	// DataRanges はコードでないセグメント（RODATA など）の PRG-ROM の範囲。
	// Diagnostic の execute_data に使う（設計書 14 編 §14.20.1）。
	DataRanges []OffsetRange
}

// OffsetRange は PRG-ROM のオフセットの範囲（End を含む）。
type OffsetRange struct {
	Start, End int
}

// dataSegmentNames はコードでないセグメントの名前（大文字）。ld65 の .dbg は
// セグメントがコードかデータかを持たないため、cc65 の慣習の名前で決める。
var dataSegmentNames = []string{"RODATA", "DATA", "CHARS", "VECTORS"}

// isDataSegment はセグメントの名前がデータのものかを返す。
func isDataSegment(name string) bool {
	up := strings.ToUpper(name)
	for _, n := range dataSegmentNames {
		if up == n {
			return true
		}
	}
	return strings.HasPrefix(up, "RODATA")
}

// dbgRecord は 1 行の key=value の並び。
type dbgRecord map[string]string

func (r dbgRecord) int(key string) (int64, bool) {
	v, ok := r[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 0, 64)
	return n, err == nil
}

// ids は "1+2+3" の形の並びを読む。
func (r dbgRecord) ids(key string) []int {
	v, ok := r[key]
	if !ok || v == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(v, "+") {
		if n, err := strconv.Atoi(p); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// parseDbgLine は 1 行を種類と key=value に分ける。引用符の中のカンマと
// 等号を区切りとしない。
func parseDbgLine(line string) (string, dbgRecord, error) {
	kind, rest, ok := strings.Cut(line, "\t")
	if !ok {
		return strings.TrimSpace(line), dbgRecord{}, nil
	}
	rec := dbgRecord{}
	for len(rest) > 0 {
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return "", nil, fmt.Errorf("= が無い: %q", rest)
		}
		key := rest[:eq]
		rest = rest[eq+1:]
		var val string
		if strings.HasPrefix(rest, "\"") {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return "", nil, fmt.Errorf("引用符が閉じていない: %q", rest)
			}
			val = rest[1 : end+1]
			rest = rest[end+2:]
		} else {
			end := strings.IndexByte(rest, ',')
			if end < 0 {
				end = len(rest)
			}
			val = rest[:end]
			rest = rest[end:]
		}
		rec[key] = val
		rest = strings.TrimPrefix(rest, ",")
	}
	return kind, rec, nil
}

type dbgSeg struct {
	start   int64
	ooffs   int64
	hasOffs bool
	rw      bool
	name    string
	size    int64
}

type dbgSpan struct {
	seg, start, size int
}

type dbgLine struct {
	file, line int
	macro      bool
	spans      []int
}

type dbgScope struct {
	name   string
	parent int
	hasPar bool
}

// LoadDbgFile は .dbg のファイルを読む。
func LoadDbgFile(path string) (*DbgInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseDbg(f)
}

// ParseDbg は .dbg を読む。
func ParseDbg(r io.Reader) (*DbgInfo, error) {
	files := map[int]string{}
	segs := map[int]dbgSeg{}
	spans := map[int]dbgSpan{}
	lines := map[int]dbgLine{}
	scopes := map[int]dbgScope{}
	var syms []dbgRecord

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	n := 0
	for sc.Scan() {
		n++
		text := sc.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		kind, rec, err := parseDbgLine(text)
		if err != nil {
			return nil, fmt.Errorf("debug: .dbg の %d 行目: %v", n, err)
		}
		id64, _ := rec.int("id")
		id := int(id64)
		switch kind {
		case "file":
			files[id] = rec["name"]
		case "seg":
			start, _ := rec.int("start")
			ooffs, has := rec.int("ooffs")
			size, _ := rec.int("size")
			segs[id] = dbgSeg{start: start, ooffs: ooffs, hasOffs: has, rw: rec["type"] == "rw",
				name: strings.Trim(rec["name"], `"`), size: size}
		case "span":
			seg, _ := rec.int("seg")
			start, _ := rec.int("start")
			size, _ := rec.int("size")
			spans[id] = dbgSpan{seg: int(seg), start: int(start), size: int(size)}
		case "line":
			file, _ := rec.int("file")
			line, _ := rec.int("line")
			typ, _ := rec.int("type")
			lines[id] = dbgLine{file: int(file), line: int(line), macro: typ == 2, spans: rec.ids("span")}
		case "scope":
			parent, has := rec.int("parent")
			scopes[id] = dbgScope{name: rec["name"], parent: int(parent), hasPar: has}
		case "sym":
			syms = append(syms, rec)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("debug: .dbg の %d 行目: %v", n+1, err)
	}

	info := &DbgInfo{}
	// scopeName は修飾名の接頭辞（"outer::inner"）を返す。根は空。
	var scopeName func(id, depth int) string
	scopeName = func(id, depth int) string {
		s, ok := scopes[id]
		if !ok || s.name == "" || depth > 64 {
			return ""
		}
		if s.hasPar {
			if p := scopeName(s.parent, depth+1); p != "" {
				return p + "::" + s.name
			}
		}
		return s.name
	}
	source := func(rec dbgRecord) SourceRef {
		for _, lid := range rec.ids("def") {
			if l, ok := lines[lid]; ok && !l.macro {
				return SourceRef{File: files[l.file], Line: l.line}
			}
		}
		return SourceRef{}
	}
	for _, rec := range syms {
		name := rec["name"]
		if name == "" || strings.HasPrefix(name, "LOCAL-MACRO") {
			continue
		}
		if _, cheap := rec["parent"]; cheap {
			// @ で始まるローカルラベル。名前が一意にならないため扱わない。
			continue
		}
		scope, _ := rec.int("scope")
		if p := scopeName(int(scope), 0); p != "" {
			name = p + "::" + name
		}
		val, _ := rec.int("val")
		size, _ := rec.int("size")
		sym := Symbol{Name: name, Size: uint16(size), Origin: OriginDbg, Source: source(rec), CPU: uint16(val)}
		switch rec["type"] {
		case "equ":
			sym.Kind, sym.Value = SymConstant, val
			info.Symbols = append(info.Symbols, sym)
			continue
		case "imp":
			// 同じ名前の lab を参照する。重複させない。
			continue
		}
		segID, hasSeg := rec.int("seg")
		seg, ok := segs[int(segID)]
		switch {
		case hasSeg && ok && seg.hasOffs:
			off := seg.ooffs - inesHeaderSize + (val - seg.start)
			if off < 0 {
				// ヘッダの中の名前。
				continue
			}
			sym.Loc = SymbolLoc{Space: SymSpacePRG, Offset: uint32(off)}
		case val < 0x8000:
			sym.Loc = SymbolLoc{Space: SymSpaceCPU, Offset: uint32(val)}
		default:
			sym.Loc = SymbolLoc{Space: SymSpacePRGAny, Offset: uint32(val)}
		}
		sym.Kind = SymLabel
		if hasSeg && ok && seg.rw {
			sym.Kind = SymVariable
		}
		info.Symbols = append(info.Symbols, sym)
	}
	// 名前の順に並べる。map の順に依らない並びにするためである。
	sort.SliceStable(info.Symbols, func(i, j int) bool { return info.Symbols[i].Name < info.Symbols[j].Name })
	info.Lines = buildSourceLines(files, segs, spans, lines)
	for _, seg := range segs {
		if !seg.hasOffs || seg.rw || seg.size <= 0 || !isDataSegment(seg.name) {
			continue
		}
		start := int(seg.ooffs - inesHeaderSize)
		if start < 0 {
			continue
		}
		info.DataRanges = append(info.DataRanges, OffsetRange{Start: start, End: start + int(seg.size) - 1})
	}
	sort.Slice(info.DataRanges, func(i, j int) bool { return info.DataRanges[i].Start < info.DataRanges[j].Start })
	return info, nil
}

// buildSourceLines は line と span から、PRG-ROM のオフセットとソースの
// 位置の対応を作る（設計書 14 編 §14.11.3）。
//
// 同じオフセットを複数の行が覆うときは、マクロでない行（ソースそのもの）を
// 優先し、その中では範囲の狭いものを優先する。マクロの呼び出しの行は展開
// 全体を覆い、展開の各行はマクロの定義の行を指すためである。
func buildSourceLines(files map[int]string, segs map[int]dbgSeg, spans map[int]dbgSpan, lines map[int]dbgLine) []SourceLine {
	type cand struct {
		off, size int
		macro     bool
		src       SourceRef
	}
	var cands []cand
	maxOff := 0
	// line の id の順にたどる。map の順に依らないためである。
	ids := make([]int, 0, len(lines))
	for id := range lines {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		l := lines[id]
		for _, sid := range l.spans {
			sp, ok := spans[sid]
			if !ok || sp.size <= 0 {
				continue
			}
			seg, ok := segs[sp.seg]
			if !ok || !seg.hasOffs {
				continue
			}
			off := int(seg.ooffs) - inesHeaderSize + sp.start
			if off < 0 {
				continue
			}
			cands = append(cands, cand{off: off, size: sp.size, macro: l.macro, src: SourceRef{File: files[l.file], Line: l.line}})
			maxOff = max(maxOff, off+sp.size)
		}
	}
	if len(cands) == 0 {
		return nil
	}
	// 優先度の低いものから塗り、優先するもので上書きする。
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].macro != cands[j].macro {
			return cands[i].macro
		}
		return cands[i].size > cands[j].size
	})
	owner := make([]int32, maxOff)
	for i := range owner {
		owner[i] = -1
	}
	for i, c := range cands {
		for o := c.off; o < c.off+c.size; o++ {
			owner[o] = int32(i)
		}
	}
	var out []SourceLine
	for o := 0; o < maxOff; {
		if owner[o] < 0 {
			o++
			continue
		}
		start, who := o, owner[o]
		for o < maxOff && owner[o] == who {
			o++
		}
		out = append(out, SourceLine{Off: start, Len: o - start, Source: cands[who].src})
	}
	return out
}
