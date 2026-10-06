package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// Location は解決したメモリの位置。
type Location struct {
	Space debug.Space
	Addr  int
	// CPU は PRG-ROM の位置に対応する CPU アドレス。不明なとき -1。
	CPU int
}

// String は位置を "ppu:$2000" や "$0300" の形にする。
func (l Location) String() string {
	for _, p := range spacePrefixes {
		if p.space == l.Space && p.name != "pal" {
			return p.name + ":" + hexAddr(l.Addr)
		}
	}
	return hexAddr(l.Addr)
}

func hexAddr(a int) string {
	if a > 0xFFFF {
		return "$" + strings.ToUpper(strconv.FormatInt(int64(a), 16))
	}
	return hex16(uint16(a))
}

// spacePrefixes は位置の空間の接頭辞（設計書 14 編 §14.6）。
var spacePrefixes = []struct {
	name  string
	space debug.Space
	base  int
}{
	{"ppu", debug.SpacePPU, 0},
	{"oam", debug.SpaceOAM, 0},
	{"pal", debug.SpacePPU, 0x3F00},
	{"chr", debug.SpaceCHR, 0},
	{"prg", debug.SpacePRGROM, 0},
}

// parseNumber は "$0300"・"0x0300"・"768"・"%0100" を数にする。
func parseNumber(s string) (int64, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	var v uint64
	var err error
	switch {
	case strings.HasPrefix(s, "$"):
		v, err = strconv.ParseUint(s[1:], 16, 32)
	case strings.HasPrefix(s, "0x"), strings.HasPrefix(s, "0X"):
		v, err = strconv.ParseUint(s[2:], 16, 32)
	case strings.HasPrefix(s, "%"):
		v, err = strconv.ParseUint(s[1:], 2, 32)
	default:
		v, err = strconv.ParseUint(s, 10, 32)
	}
	return int64(v), err == nil && s != ""
}

// ResolveLocation は位置の書き方を解決する（設計書 14 編 §14.6）。
//
// CPU アドレス（$0300・0x0300・768）、Symbol の名前、式（enemies+2*4）、
// 空間の接頭辞（ppu:・oam:・pal:・chr:・prg:・bankN:）、間接参照（[ptr]、
// 2 バイトのリトルエンディアン）を受け付ける。間接参照を読むため、
// エミュレーションゴルーチンの命令境界で評価する。
func (inst *Instance) ResolveLocation(s string) (Location, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return Location{}, Errorf(KindInvalidLocation, "位置が空である")
	}
	var loc Location
	var rerr error
	ok := inst.Emu.WithDebugger(func(d *debug.Debugger) {
		e, err := debug.ParseAddrExpr(t, d)
		if err != nil {
			rerr = locationError(s, err, d.Symbols())
			return
		}
		space, addr, cpu := e.Eval(d)
		loc = Location{Space: space, Addr: addr, CPU: cpu}
	})
	if !ok {
		return Location{}, Errorf(KindInternalError, "エミュレーションが停止している")
	}
	if rerr != nil {
		return Location{}, rerr
	}
	if loc.Addr < 0 {
		return Location{}, Errorf(KindInvalidLocation, "位置 %q が負になる", s)
	}
	return loc, nil
}

// locationError は位置の解析の誤りを invalid_location にし、知らない名前には
// 似た名前の候補を最大 5 件添える。
func locationError(s string, err error, syms *debug.Symbols) error {
	var pe *debug.ParseError
	if !errors.As(err, &pe) {
		return Errorf(KindInvalidLocation, "位置 %q を解決できない: %v", s, err)
	}
	msg := fmt.Sprintf("位置 %q を解決できない（%d 文字目: %s）", s, pe.Pos, pe.Msg)
	if name, ok := unknownName(pe.Msg); ok && syms != nil {
		if c := similarNames(name, syms, 5); len(c) > 0 {
			msg += "。候補: " + strings.Join(c, ", ")
		}
	}
	e := Errorf(KindInvalidLocation, "%s", msg)
	e.Position = pe.Pos
	return e
}

// unknownName は「知らない名前 "x"」の誤りから名前を取り出す。
func unknownName(msg string) (string, bool) {
	_, rest, ok := strings.Cut(msg, "知らない名前 ")
	if !ok {
		return "", false
	}
	n, err := strconv.Unquote(rest)
	return n, err == nil
}

// similarNames は name に似た Symbol の名前を返す。部分一致を先に、次に
// 編集距離の小さいものを並べる。
func similarNames(name string, syms *debug.Symbols, limit int) []string {
	type cand struct {
		name string
		dist int
	}
	low := strings.ToLower(name)
	var cs []cand
	for _, sym := range syms.All() {
		l := strings.ToLower(sym.Name)
		d := editDistance(low, l)
		if strings.Contains(l, low) || strings.Contains(low, l) {
			d = 0
		}
		if d <= max(2, len(name)/3) {
			cs = append(cs, cand{sym.Name, d})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].dist != cs[j].dist {
			return cs[i].dist < cs[j].dist
		}
		return cs[i].name < cs[j].name
	})
	var out []string
	for _, c := range cs {
		if !slices.Contains(out, c.name) {
			out = append(out, c.name)
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

// editDistance は 2 つの文字列の編集距離を返す。
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// cpuAddr は CPU アドレス空間の位置であることを確かめてアドレスを返す。
//
// PRG-ROM の位置（バンクの中の Symbol、bankN:$ADDR）は、対応する CPU アドレスが
// 分かればそれを返す。実行ブレークポイントなどはバンクを区別せず、その
// CPU アドレスで止まる。
func (l Location) cpuAddr() (uint16, error) {
	if l.Space == debug.SpacePRGROM && l.CPU >= 0x8000 && l.CPU <= 0xFFFF {
		return uint16(l.CPU), nil
	}
	if l.Space != debug.SpaceCPU || l.Addr > 0xFFFF {
		return 0, Errorf(KindInvalidLocation, "CPU アドレス空間の位置を指定する（%s）", l)
	}
	return uint16(l.Addr), nil
}

// ParseBytes は値の書き方（数、数の配列、"$42"、"A9 00" の 16 進の並び）を
// バイト列にする（設計書 14 編 §14.6）。
func ParseBytes(raw json.RawMessage) ([]uint8, error) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		v, err := n.Int64()
		if err != nil || v < 0 || v > 255 {
			return nil, Errorf(KindInvalidParams, "値 %s は 0–255 の整数とする", n)
		}
		return []uint8{uint8(v)}, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		var out []uint8
		for _, e := range list {
			b, err := ParseBytes(e)
			if err != nil {
				return nil, err
			}
			out = append(out, b...)
		}
		if len(out) == 0 {
			return nil, Errorf(KindInvalidParams, "値が空である")
		}
		return out, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		fields := strings.Fields(s)
		if len(fields) == 1 {
			if v, ok := parseNumber(fields[0]); ok && v <= 255 {
				return []uint8{uint8(v)}, nil
			}
		}
		var out []uint8
		for _, f := range fields {
			v, err := strconv.ParseUint(strings.TrimPrefix(f, "$"), 16, 8)
			if err != nil {
				return nil, Errorf(KindInvalidParams, "値 %q を読めない（数、数の配列、\"A9 00\" の形）", s)
			}
			out = append(out, uint8(v))
		}
		if len(out) == 0 {
			return nil, Errorf(KindInvalidParams, "値が空である")
		}
		return out, nil
	}
	return nil, Errorf(KindInvalidParams, "値 %s を読めない（数、数の配列、\"A9 00\" の形）", raw)
}
