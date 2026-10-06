package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// Symbol の Agent Command（設計書 14 編 §14.7.2、§14.11）。

const maxSymbolList = 500

func registerSymbol(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("symbol.load", ClassConfig, symLoadParams{}, []string{"path"}, "Symbol を読み込む（ca65/ld65 の .dbg、Shogun のシンボルファイル）",
		"Load symbols from a ca65/ld65 .dbg file or a Shogun symbols JSON file.", handleSymbolLoad)
	reg("symbol.lookup", ClassObserve, symLookupParams{}, []string{"name"}, "名前から位置を、位置から名前とソース行を引く",
		"Look up a symbol by name (location, size, source) or by location (name and source line).", handleSymbolLookup)
	reg("symbol.list", ClassObserve, symListParams{}, []string{"filter"}, "Symbol の一覧。名前で絞り込む",
		"List symbols, optionally filtered by name (substring or prefix). Up to 500.", handleSymbolList)
	reg("symbol.set", ClassConfig, symSetParams{}, []string{"name", "loc"}, "利用者の Symbol を付ける",
		"Name a location (saved per ROM).", handleSymbolSet)
	reg("symbol.remove", ClassConfig, symNameParams{}, []string{"name"}, "利用者の Symbol を外す",
		"Remove a user-defined symbol.", handleSymbolRemove)
}

// SymbolInfo は Symbol 1 件。
type SymbolInfo struct {
	Name   string `json:"name"`
	Space  string `json:"space"`
	Offset string `json:"offset"`
	CPU    string `json:"cpu,omitempty"`
	Size   int    `json:"size,omitempty"`
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
	Source string `json:"source,omitempty"`
	Value  *int64 `json:"value,omitempty"`
}

func symbolInfo(s debug.Symbol) SymbolInfo {
	si := SymbolInfo{Name: s.Name, Space: s.Loc.Space.String(), Offset: hexAddr(int(s.Loc.Offset)),
		Size: int(s.Size), Kind: s.Kind.String(), Origin: s.Origin.String(), Source: s.Source.String()}
	if s.Loc.Space == debug.SymSpacePRG && s.CPU != 0 {
		si.CPU = hex16(s.CPU)
	}
	if s.Kind == debug.SymConstant {
		v := s.Value
		si.Value = &v
		si.Space, si.Offset = "", ""
	}
	return si
}

type symLoadParams struct {
	InstanceParam
	Path   string `json:"path" desc:"ファイルのパス"`
	Format string `json:"format,omitempty" desc:"dbg・json。省くと拡張子で決める"`
}

func handleSymbolLoad(c *Context, raw json.RawMessage) (any, error) {
	var p symLoadParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	format := p.Format
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(p.Path)), ".")
	}
	syms := c.Instance.symbols()
	if syms == nil {
		return nil, Errorf(KindNotLoaded, "ROM が読み込まれていない")
	}
	count := 0
	switch format {
	case "dbg":
		info, err := debug.LoadDbgFile(p.Path)
		if err != nil {
			return nil, Errorf(KindIOError, "読めない: %v", err)
		}
		syms.SetDbg(info, p.Path)
		count = len(info.Symbols)
	case "json":
		loaded, err := debug.LoadSymbols(p.Path)
		if err != nil {
			return nil, Errorf(KindIOError, "読めない: %v", err)
		}
		if _, err := os.Stat(p.Path); errors.Is(err, os.ErrNotExist) {
			return nil, Errorf(KindIOError, "%s が無い", p.Path)
		}
		for _, s := range loaded.All() {
			syms.SetSymbol(s)
			count++
		}
		c.Instance.Emu.SaveSymbols()
	default:
		return nil, Errorf(KindInvalidParams, "format %q を知らない（dbg・json）", format)
	}
	return struct {
		Loaded int `json:"loaded"`
	}{count}, nil
}

type symLookupParams struct {
	InstanceParam
	Name string `json:"name,omitempty" desc:"Symbol の名前"`
	Loc  string `json:"loc,omitempty" desc:"位置（名前の代わり）"`
}

// LookupResult は位置から引いた結果。
type LookupResult struct {
	Loc     string `json:"loc"`
	Name    string `json:"name,omitempty"`
	Nearest string `json:"nearest,omitempty"`
	Source  string `json:"source,omitempty"`
}

func handleSymbolLookup(c *Context, raw json.RawMessage) (any, error) {
	var p symLookupParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if (p.Name == "") == (p.Loc == "") {
		return nil, Errorf(KindInvalidParams, "name か loc のどちらか一方を指定する")
	}
	syms := c.Instance.symbols()
	if syms == nil {
		return nil, Errorf(KindNotLoaded, "ROM が読み込まれていない")
	}
	if p.Name != "" {
		s, ok := syms.Lookup(p.Name)
		if !ok {
			return nil, locationError(p.Name, &debug.ParseError{Pos: 1, Msg: "知らない名前 " + quote(p.Name)}, syms)
		}
		return symbolInfo(s), nil
	}
	loc, err := c.Instance.ResolveLocation(p.Loc)
	if err != nil {
		return nil, err
	}
	r := LookupResult{Loc: loc.String()}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		switch {
		case loc.Space == debug.SpaceCPU && loc.Addr <= 0xFFFF:
			a := uint16(loc.Addr)
			r.Name, r.Nearest = d.Label(a), d.NearestLabel(a)
			if src, ok := d.Source(a); ok {
				r.Source = src.String()
			}
		case loc.Space == debug.SpacePRGROM:
			if src, ok := d.Symbols().SourceAt(loc.Addr); ok {
				r.Source = src.String()
			}
			for _, s := range d.Symbols().All() {
				if s.Loc.Space == debug.SymSpacePRG && int(s.Loc.Offset) == loc.Addr {
					r.Name = s.Name
					break
				}
			}
		}
	})
	return r, nil
}

type symListParams struct {
	InstanceParam
	Filter string `json:"filter,omitempty" desc:"名前で絞り込む文字列"`
	Match  string `json:"match,omitempty" desc:"contains（部分一致）・prefix（前方一致）" default:"\"contains\""`
	Kind   string `json:"kind,omitempty" desc:"label・variable・constant で絞り込む"`
	Limit  int    `json:"limit,omitempty" desc:"最大の件数（1–500）" default:"500"`
}

func handleSymbolList(c *Context, raw json.RawMessage) (any, error) {
	var p symListParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Limit == 0 {
		p.Limit = maxSymbolList
	}
	if p.Limit < 1 || p.Limit > maxSymbolList {
		return nil, Errorf(KindInvalidParams, "limit は 1–%d とする", maxSymbolList)
	}
	if p.Match != "" && p.Match != "contains" && p.Match != "prefix" {
		return nil, Errorf(KindInvalidParams, "match は contains か prefix とする")
	}
	syms := c.Instance.symbols()
	if syms == nil {
		return nil, Errorf(KindNotLoaded, "ROM が読み込まれていない")
	}
	out := []SymbolInfo{}
	total := 0
	for _, s := range syms.All() {
		if p.Kind != "" && s.Kind.String() != p.Kind {
			continue
		}
		if p.Filter != "" {
			if p.Match == "prefix" && !strings.HasPrefix(s.Name, p.Filter) ||
				p.Match != "prefix" && !strings.Contains(strings.ToLower(s.Name), strings.ToLower(p.Filter)) {
				continue
			}
		}
		total++
		if len(out) < p.Limit {
			out = append(out, symbolInfo(s))
		}
	}
	return struct {
		Symbols   []SymbolInfo `json:"symbols"`
		Total     int          `json:"total"`
		Truncated bool         `json:"truncated"`
		Dbg       string       `json:"dbg,omitempty"`
	}{out, total, total > len(out), syms.DbgPath()}, nil
}

type symSetParams struct {
	InstanceParam
	Name string `json:"name" desc:"名前"`
	Loc  string `json:"loc" desc:"位置"`
	Size int    `json:"size,omitempty" desc:"バイト数"`
}

func handleSymbolSet(c *Context, raw json.RawMessage) (any, error) {
	var p symSetParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if !validSymbolName(p.Name) {
		return nil, Errorf(KindInvalidParams, "名前 %q は英字かアンダースコアで始め、英字・数字・アンダースコア・:: だけを使う", p.Name)
	}
	if p.Size < 0 || p.Size > 0xFFFF {
		return nil, Errorf(KindInvalidParams, "size は 0–65535 とする")
	}
	loc, err := c.Instance.ResolveLocation(p.Loc)
	if err != nil {
		return nil, err
	}
	var sym debug.Symbol
	var serr error
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		switch {
		case loc.Space == debug.SpaceCPU && loc.Addr <= 0xFFFF:
			var off debug.Offsetter
			if n != nil {
				off = n.Cart.PRGOffset
			}
			sym = debug.Symbol{Name: p.Name, Loc: debug.LocOf(uint16(loc.Addr), off), CPU: uint16(loc.Addr)}
		case loc.Space == debug.SpacePRGROM:
			sym = debug.Symbol{Name: p.Name, Loc: debug.SymbolLoc{Space: debug.SymSpacePRG, Offset: uint32(loc.Addr)}}
			if loc.CPU >= 0 {
				sym.CPU = uint16(loc.CPU)
			}
		default:
			serr = Errorf(KindInvalidLocation, "Symbol は CPU アドレス空間か PRG-ROM の位置に付ける（%s）", loc)
			return
		}
		sym.Size, sym.Kind, sym.Origin = uint16(p.Size), debug.SymLabel, debug.OriginUser
		d.Symbols().SetSymbol(sym)
	})
	if serr != nil {
		return nil, serr
	}
	c.Instance.Emu.SaveSymbols()
	return symbolInfo(sym), nil
}

type symNameParams struct {
	InstanceParam
	Name string `json:"name" desc:"名前"`
}

func handleSymbolRemove(c *Context, raw json.RawMessage) (any, error) {
	var p symNameParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	syms := c.Instance.symbols()
	if syms == nil || !syms.RemoveSymbol(p.Name) {
		return nil, Errorf(KindInvalidParams, "利用者の Symbol %q は無い（.dbg の Symbol は外せない）", p.Name)
	}
	c.Instance.Emu.SaveSymbols()
	return struct {
		Removed string `json:"removed"`
	}{p.Name}, nil
}

// validSymbolName は Symbol の名前に使える文字だけかを返す。
func validSymbolName(s string) bool {
	if s == "" {
		return false
	}
	for i, part := range strings.Split(s, "::") {
		if part == "" {
			return false
		}
		for j, r := range part {
			switch {
			case r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z':
			case r >= '0' && r <= '9' && (j > 0 || i > 0 && j > 0):
			default:
				return false
			}
		}
	}
	return true
}

// quote は名前を引用符で囲む。
func quote(s string) string { return `"` + s + `"` }
