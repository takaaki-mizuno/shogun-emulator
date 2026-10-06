package agent

import (
	"bytes"
	"encoding/json"

	"fmt"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// メモリと CPU の Agent Command（設計書 14 編 §14.7.2、§14.13）。

const (
	maxReadLength  = 4096
	maxFindResults = 100
	maxDisasmLines = 200
)

func registerMem(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("mem.read", ClassObserve, memReadParams{}, []string{"loc", "length"}, "メモリを副作用なしに読む",
		"Read memory without side effects. loc accepts $0300, labels and space prefixes (ppu:, oam:, pal:, chr:, prg:).", handleMemRead)
	reg("mem.write", ClassMutate, memWriteParams{}, []string{"loc", "value"}, "メモリを書き換える。side_effects: true でバスの書き込み",
		"Write memory. Uses a side-effect-free poke unless side_effects is true.", handleMemWrite)
	reg("mem.find", ClassObserve, memFindParams{}, []string{"bytes"}, "バイト列を探す",
		"Search memory for a byte sequence (up to 100 matches).", handleMemFind)
	reg("mem.freeze", ClassMutate, freezeParams{}, []string{"loc", "value"}, "値を固定する（デバッグ用）",
		"Freeze RAM ($0000-$07FF) or PRG-RAM ($6000-$7FFF) at a value for debugging; CPU writes are reverted immediately.", handleFreeze)
	reg("mem.unfreeze", ClassMutate, unfreezeParams{}, []string{"loc"}, "固定を外す。all: true ですべて",
		"Remove a freeze (or all freezes).", handleUnfreeze)
	reg("mem.freezes", ClassObserve, InstanceParam{}, nil, "固定している位置の一覧",
		"List frozen locations.", handleFreezes)
	reg("cpu.get", ClassObserve, InstanceParam{}, nil, "レジスタ、PPU の位置、保留中の割り込みを返す",
		"Return CPU registers, flags, PPU position, cycle count and pending interrupts.", handleCPUGet)
	reg("cpu.set", ClassMutate, cpuSetParams{}, nil, "レジスタを書き換える",
		"Set CPU registers (a, x, y, s, pc, p).", handleCPUSet)
	reg("cpu.disasm", ClassObserve, disasmParams{}, []string{"loc", "count"}, "逆アセンブルする。省くと PC から",
		"Disassemble from an address (default: around PC) with labels and effective-address comments.", handleDisasm)
	reg("cpu.callstack", ClassObserve, InstanceParam{}, nil, "コールスタック（推定）を返す",
		"Return the estimated call stack (JSR and interrupts).", handleCallStack)
}

// readMem は空間 space の addr から len(dst) バイトを副作用なしに読む。
func readMem(inst *Instance, loc Location, length int) ([]uint8, error) {
	if err := requireLoaded(inst); err != nil {
		return nil, err
	}
	out := make([]uint8, length)
	var rerr error
	inst.Emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		if n == nil {
			rerr = Errorf(KindNotLoaded, "ROM が読み込まれていない")
			return
		}
		if loc.Addr < 0 || loc.Addr+length > debug.Size(n, loc.Space) {
			rerr = Errorf(KindInvalidLocation, "%s から %d バイトは範囲外である（大きさ %d）", loc, length, debug.Size(n, loc.Space))
			return
		}
		debug.ReadMemory(n, loc.Space, loc.Addr, out)
	})
	return out, rerr
}

type memReadParams struct {
	InstanceParam
	Loc    string `json:"loc" desc:"位置（$0300、名前、ppu:$2000 など）"`
	Length int    `json:"length,omitempty" desc:"バイト数（1–4096）" default:"1"`
}

// MemReadResult は mem.read の結果。
type MemReadResult struct {
	Loc   string   `json:"loc"`
	Hex   []string `json:"hex"`
	Bytes []uint8  `json:"bytes"`
}

// MarshalJSON は Bytes を Base64 ではなく数の配列で書く。
func (r MemReadResult) MarshalJSON() ([]byte, error) {
	nums := make([]int, len(r.Bytes))
	for i, b := range r.Bytes {
		nums[i] = int(b)
	}
	return json.Marshal(struct {
		Loc   string   `json:"loc"`
		Hex   []string `json:"hex"`
		Bytes []int    `json:"bytes"`
	}{r.Loc, r.Hex, nums})
}

func handleMemRead(c *Context, raw json.RawMessage) (any, error) {
	var p memReadParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Length == 0 {
		p.Length = 1
	}
	if p.Length < 1 || p.Length > maxReadLength {
		return nil, Errorf(KindInvalidParams, "length は 1–%d とする（%d）", maxReadLength, p.Length)
	}
	loc, err := c.Instance.ResolveLocation(p.Loc)
	if err != nil {
		return nil, err
	}
	b, err := readMem(c.Instance, loc, p.Length)
	if err != nil {
		return nil, err
	}
	return MemReadResult{Loc: loc.String(), Hex: hexRows(b), Bytes: b}, nil
}

type memWriteParams struct {
	InstanceParam
	Loc         string          `json:"loc" desc:"位置"`
	Value       json.RawMessage `json:"value" desc:"値（数、数の配列、\"A9 00\"）"`
	SideEffects bool            `json:"side_effects,omitempty" desc:"CPU アドレス空間でバスの書き込み（レジスタの副作用あり）を使う"`
}

func handleMemWrite(c *Context, raw json.RawMessage) (any, error) {
	var p memWriteParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	loc, err := c.Instance.ResolveLocation(p.Loc)
	if err != nil {
		return nil, err
	}
	b, err := ParseBytes(p.Value)
	if err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	if p.SideEffects && loc.Space != debug.SpaceCPU {
		return nil, Errorf(KindInvalidParams, "side_effects は CPU アドレス空間でだけ使える")
	}
	for i, v := range b {
		if err := c.Instance.Emu.Poke(loc.Space, loc.Addr+i, v, p.SideEffects); err != nil {
			if strings.Contains(err.Error(), "ムービー") {
				return nil, Errorf(KindMovieConflict, "%v", err)
			}
			return nil, Errorf(KindInvalidLocation, "%s+%d に書けない: %v", loc, i, err)
		}
	}
	return struct {
		Loc     string `json:"loc"`
		Written int    `json:"written"`
	}{loc.String(), len(b)}, nil
}

type memFindParams struct {
	InstanceParam
	Bytes json.RawMessage `json:"bytes" desc:"探すバイト列（\"A9 00\"、数の配列）"`
	Space string          `json:"space,omitempty" desc:"空間（cpu・ram・ppu・oam・prg・chr・prgram）" default:"\"cpu\""`
}

var findSpaces = []struct {
	name  string
	space debug.Space
}{
	{"cpu", debug.SpaceCPU}, {"ram", debug.SpaceRAM}, {"ppu", debug.SpacePPU}, {"oam", debug.SpaceOAM},
	{"prg", debug.SpacePRGROM}, {"chr", debug.SpaceCHR}, {"prgram", debug.SpacePRGRAM},
}

func handleMemFind(c *Context, raw json.RawMessage) (any, error) {
	var p memFindParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	pat, err := ParseBytes(p.Bytes)
	if err != nil {
		return nil, err
	}
	if p.Space == "" {
		p.Space = "cpu"
	}
	space, found := debug.SpaceCPU, false
	for _, s := range findSpaces {
		if s.name == p.Space {
			space, found = s.space, true
		}
	}
	if !found {
		return nil, Errorf(KindInvalidParams, "space %q を知らない（cpu・ram・ppu・oam・prg・chr・prgram）", p.Space)
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var all []uint8
	base := space.Base()
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		if n := d.Machine(); n != nil {
			all = make([]uint8, debug.Size(n, space))
			debug.ReadMemory(n, space, 0, all)
		}
	})
	matches := []string{}
	truncated := false
	for i := 0; i+len(pat) <= len(all); i++ {
		if bytes.Equal(all[i:i+len(pat)], pat) {
			if len(matches) == maxFindResults {
				truncated = true
				break
			}
			matches = append(matches, Location{Space: space, Addr: base + i}.String())
		}
	}
	return struct {
		Matches   []string `json:"matches"`
		Truncated bool     `json:"truncated"`
	}{matches, truncated}, nil
}

type freezeParams struct {
	InstanceParam
	Loc   string          `json:"loc" desc:"位置（内蔵 RAM か PRG-RAM）"`
	Value json.RawMessage `json:"value" desc:"固定する値。数の配列か、size と合わせた数（リトルエンディアン）"`
	Size  int             `json:"size,omitempty" desc:"バイト数（1–4）。value が数のときに使う" default:"1"`
}

// freezeValue は固定する値をバイト列にする。
func freezeValue(raw json.RawMessage, size int) ([]uint8, error) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil && size > 1 {
		v, err := n.Int64()
		if err != nil || v < 0 || v >= 1<<(8*size) {
			return nil, Errorf(KindInvalidParams, "値 %s は %d バイトに収まらない", n, size)
		}
		out := make([]uint8, size)
		for i := range out {
			out[i] = uint8(v >> (8 * i))
		}
		return out, nil
	}
	b, err := ParseBytes(raw)
	if err != nil {
		return nil, err
	}
	if size != 0 && len(b) != size {
		return nil, Errorf(KindInvalidParams, "値の長さ %d が size %d と違う", len(b), size)
	}
	return b, nil
}

// FreezeInfo は Freeze 1 件。
type FreezeInfo struct {
	Loc   string   `json:"loc"`
	Value []string `json:"value"`
}

func freezeInfos(list []debug.Freeze) []FreezeInfo {
	out := []FreezeInfo{}
	for _, f := range list {
		fi := FreezeInfo{Loc: hex16(f.Addr)}
		for _, v := range f.Value {
			fi.Value = append(fi.Value, hex8(v))
		}
		out = append(out, fi)
	}
	return out
}

func handleFreeze(c *Context, raw json.RawMessage) (any, error) {
	var p freezeParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Size < 0 || p.Size > 4 {
		return nil, Errorf(KindInvalidParams, "size は 1–4 とする（%d）", p.Size)
	}
	loc, err := c.Instance.ResolveLocation(p.Loc)
	if err != nil {
		return nil, err
	}
	a, err := loc.cpuAddr()
	if err != nil {
		return nil, err
	}
	val, err := freezeValue(p.Value, p.Size)
	if err != nil {
		return nil, err
	}
	addr, err := debug.NormalizeFreezeAddr(a, len(val))
	if err != nil {
		return nil, Errorf(KindInvalidLocation, "%v", err)
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	list := c.Instance.freezes()
	replaced := false
	for i, f := range list {
		if f.Addr == addr {
			list[i] = debug.Freeze{Addr: addr, Value: val}
			replaced = true
		}
	}
	if !replaced {
		if len(list) >= debug.MaxFreezes {
			return nil, Errorf(KindLimitExceeded, "Freeze は %d 件までである", debug.MaxFreezes)
		}
		list = append(list, debug.Freeze{Addr: addr, Value: val})
	}
	// 常時記録に介入として残すため、エミュレータを通して設定する。
	if err := c.Instance.Emu.SetFreezes(list); err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	return freezeInfos(c.Instance.freezes()), nil
}

type unfreezeParams struct {
	InstanceParam
	Loc string `json:"loc,omitempty" desc:"外す位置"`
	All bool   `json:"all,omitempty" desc:"すべて外す"`
}

func handleUnfreeze(c *Context, raw json.RawMessage) (any, error) {
	var p unfreezeParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Loc == "" && !p.All {
		return nil, Errorf(KindInvalidParams, "loc か all を指定する")
	}
	var addr uint16
	if !p.All {
		loc, err := c.Instance.ResolveLocation(p.Loc)
		if err != nil {
			return nil, err
		}
		a, err := loc.cpuAddr()
		if err != nil {
			return nil, err
		}
		if a < 0x2000 {
			a &= 0x07FF
		}
		addr = a
	}
	removed := false
	var keep []debug.Freeze
	for _, f := range c.Instance.freezes() {
		if p.All || f.Addr == addr {
			removed = true
			continue
		}
		keep = append(keep, f)
	}
	if !removed {
		return nil, Errorf(KindInvalidLocation, "%s は固定していない", p.Loc)
	}
	if err := c.Instance.Emu.SetFreezes(keep); err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	return freezeInfos(c.Instance.freezes()), nil
}

func handleFreezes(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	return freezeInfos(c.Instance.freezes()), nil
}

// freezes は Freeze の一覧を返す。
func (inst *Instance) freezes() []debug.Freeze {
	var out []debug.Freeze
	inst.Emu.WithDebugger(func(d *debug.Debugger) { out = d.Freezes() })
	return out
}

func handleCPUGet(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var r CPUInfo
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) { r = cpuInfo(d) })
	return r, nil
}

type cpuSetParams struct {
	InstanceParam
	A  json.RawMessage `json:"a,omitempty" desc:"A"`
	X  json.RawMessage `json:"x,omitempty" desc:"X"`
	Y  json.RawMessage `json:"y,omitempty" desc:"Y"`
	S  json.RawMessage `json:"s,omitempty" desc:"S"`
	PC json.RawMessage `json:"pc,omitempty" desc:"PC"`
	P  json.RawMessage `json:"p,omitempty" desc:"P"`
}

// regValue は数か "$42" の文字列を読む。
func regValue(name string, raw json.RawMessage, limit int64) (uint16, error) {
	var n json.Number
	var v int64
	if err := json.Unmarshal(raw, &n); err == nil {
		x, err := n.Int64()
		if err != nil {
			return 0, Errorf(KindInvalidParams, "%s の値 %s を読めない", name, n)
		}
		v = x
	} else {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, Errorf(KindInvalidParams, "%s の値 %s を読めない", name, raw)
		}
		x, ok := parseNumber(s)
		if !ok {
			return 0, Errorf(KindInvalidParams, "%s の値 %q を読めない", name, s)
		}
		v = x
	}
	if v < 0 || v > limit {
		return 0, Errorf(KindInvalidParams, "%s の値 %d は 0–%d の外である", name, v, limit)
	}
	return uint16(v), nil
}

func handleCPUSet(c *Context, raw json.RawMessage) (any, error) {
	var p cpuSetParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	type set struct {
		reg debug.Register
		v   uint16
	}
	var sets []set
	for _, f := range []struct {
		name  string
		raw   json.RawMessage
		reg   debug.Register
		limit int64
	}{
		{"a", p.A, debug.RegA, 0xFF}, {"x", p.X, debug.RegX, 0xFF}, {"y", p.Y, debug.RegY, 0xFF},
		{"s", p.S, debug.RegS, 0xFF}, {"pc", p.PC, debug.RegPC, 0xFFFF}, {"p", p.P, debug.RegP, 0xFF},
	} {
		if len(f.raw) == 0 {
			continue
		}
		v, err := regValue(f.name, f.raw, f.limit)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set{f.reg, v})
	}
	if len(sets) == 0 {
		return nil, Errorf(KindInvalidParams, "書き換えるレジスタを指定する（a・x・y・s・pc・p）")
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	if c.Instance.Emu.Status().MidInstruction {
		return nil, Errorf(KindInvalidParams, "命令の途中で止まっているためレジスタを書き換えられない（命令単位で進める）")
	}
	for _, s := range sets {
		if err := c.Instance.Emu.SetRegister(s.reg, s.v); err != nil {
			if strings.Contains(err.Error(), "ムービー") {
				return nil, Errorf(KindMovieConflict, "%v", err)
			}
			return nil, Errorf(KindInternalError, "%v", err)
		}
	}
	var r CPUInfo
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) { r = cpuInfo(d) })
	return r, nil
}

type disasmParams struct {
	InstanceParam
	Loc   string `json:"loc,omitempty" desc:"開始位置。省くと PC の少し前から"`
	Count int    `json:"count,omitempty" desc:"行数（1–200）" default:"16"`
}

// DisasmLine は逆アセンブルの 1 行。
type DisasmLine struct {
	Addr      string `json:"addr"`
	Bytes     string `json:"bytes"`
	Text      string `json:"text"`
	Comment   string `json:"comment,omitempty"`
	Label     string `json:"label,omitempty"`
	Official  bool   `json:"official"`
	Estimated bool   `json:"estimated,omitempty"`
	Data      bool   `json:"data,omitempty"`
	Current   bool   `json:"current,omitempty"`
	Source    string `json:"source,omitempty"`
}

func handleDisasm(c *Context, raw json.RawMessage) (any, error) {
	var p disasmParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Count == 0 {
		p.Count = 16
	}
	if p.Count < 1 || p.Count > maxDisasmLines {
		return nil, Errorf(KindInvalidParams, "count は 1–%d とする（%d）", maxDisasmLines, p.Count)
	}
	var anchor uint16
	hasAnchor := false
	if p.Loc != "" {
		loc, err := c.Instance.ResolveLocation(p.Loc)
		if err != nil {
			return nil, err
		}
		a, err := loc.cpuAddr()
		if err != nil {
			return nil, err
		}
		anchor, hasAnchor = a, true
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var lines []DisasmLine
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		if n == nil {
			return
		}
		var ls []debug.Line
		if hasAnchor {
			ls = d.ListingAt(anchor, p.Count)
		} else {
			ls = d.CPUView(p.Count).Lines
		}
		for _, l := range ls {
			var b strings.Builder
			for i, v := range l.Bytes {
				if i > 0 {
					b.WriteByte(' ')
				}
				fmt.Fprintf(&b, "%02X", v)
			}
			text := l.Text()
			if l.Alias != "" {
				text += " (" + l.Alias + ")"
			}
			dl := DisasmLine{
				Addr: hex16(l.Addr), Bytes: b.String(), Text: text, Comment: l.Comment, Label: l.Label,
				Official: l.Official, Estimated: l.Estimated, Data: l.Data, Current: l.Addr == n.CPU.PC,
			}
			if src, ok := d.Source(l.Addr); ok {
				dl.Source = src.String()
			}
			lines = append(lines, dl)
		}
	})
	return struct {
		Lines []DisasmLine `json:"lines"`
		Notes []string     `json:"notes,omitempty"`
	}{lines, []string{"estimated の行は実行の記録から確定していない線形の逆アセンブルである"}}, nil
}

// CallInfo はコールスタックの 1 段。
type CallInfo struct {
	Kind       string `json:"kind"`
	From       string `json:"from"`
	To         string `json:"to"`
	FromSymbol string `json:"from_symbol,omitempty"`
	ToSymbol   string `json:"to_symbol,omitempty"`
}

var frameKindNames = []string{"jsr", "nmi", "irq", "brk"}

func handleCallStack(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var frames []CallInfo
	var notes []string
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		f := d.Features()
		if !f.CPUView {
			// コールスタックの記録は CPU デバッガの機能の一部である。
			// 要求されたら有効にし、以降の呼び出しから記録する。
			f.CPUView = true
			d.SetFeatures(f)
			notes = append(notes, "コールスタックの記録を今有効にした。これ以降の呼び出しから記録する")
		}
		for _, cf := range d.CallStack() {
			kind := "jsr"
			if int(cf.Kind) < len(frameKindNames) {
				kind = frameKindNames[cf.Kind]
			}
			frames = append(frames, CallInfo{
				Kind: kind, From: hex16(cf.From), To: hex16(cf.To),
				FromSymbol: d.NearestLabel(cf.From), ToSymbol: d.Label(cf.To),
			})
		}
	})
	notes = append(notes, "推定である。スタックを直接書き換えるプログラムや RTS による間接ジャンプは区別できない")
	if frames == nil {
		frames = []CallInfo{}
	}
	return struct {
		Frames []CallInfo `json:"frames"`
		Notes  []string   `json:"notes"`
	}{frames, notes}, nil
}
