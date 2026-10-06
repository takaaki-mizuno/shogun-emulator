package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes/cpu"
)

// 解析の Agent Command（設計書 14 編 §14.18–§14.20）: トレース・プロファイル・
// Diagnostic。

// DiagInfo は Diagnostic 1 件の位置と説明。
type DiagInfo struct {
	Kind      string `json:"kind"`
	Frame     uint64 `json:"frame"`
	Scanline  int    `json:"scanline"`
	Dot       int    `json:"dot"`
	PC        string `json:"pc"`
	Symbol    string `json:"symbol,omitempty"`
	Source    string `json:"source,omitempty"`
	PRGOffset *int   `json:"prg_offset,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// DiagSummary は Observation の diagnostics の 1 種類分。
type DiagSummary struct {
	Kind  string   `json:"kind"`
	Count uint64   `json:"count"`
	First DiagInfo `json:"first"`
}

// locName は位置の名前とソース行を返す。
func locName(syms *debug.Symbols, pc uint16, prgOffset int32) (string, string) {
	if syms == nil {
		return "", ""
	}
	loc := debug.SymbolLoc{Space: debug.SymSpaceCPU, Offset: uint32(pc)}
	if prgOffset >= 0 {
		loc = debug.SymbolLoc{Space: debug.SymSpacePRG, Offset: uint32(prgOffset)}
	} else if pc >= 0x8000 {
		loc = debug.SymbolLoc{Space: debug.SymSpacePRGAny, Offset: uint32(pc)}
	}
	name := syms.NameAtLoc(loc, pc)
	src := ""
	if prgOffset >= 0 {
		if s, ok := syms.SourceAt(int(prgOffset)); ok {
			src = s.String()
		}
	}
	return name, src
}

// diagInfo は Diagnostic を DiagInfo にする。
func diagInfo(syms *debug.Symbols, x debug.Diagnostic) DiagInfo {
	info := DiagInfo{Kind: x.Kind.String(), Frame: x.Frame, Scanline: int(x.Scanline), Dot: int(x.Dot),
		PC: hex16(x.PC), Detail: x.Detail}
	info.Symbol, info.Source = locName(syms, x.PC, x.PRGOffset)
	if x.PRGOffset >= 0 {
		off := int(x.PRGOffset)
		info.PRGOffset = &off
	}
	return info
}

// sharedSymbols は Instance の Symbols を返す。エミュレーションゴルーチンから
// 呼んでよい（WithDebugger を使わない）。
func (inst *Instance) sharedSymbols() *debug.Symbols {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.shared != nil {
		return inst.shared.symbols
	}
	return nil
}

// observeDiagnostics は前回の Observation 以降の Diagnostic を種類ごとにまとめる
// （§14.20.3）。prevSeq と prevTotals は接続ごとの前回の値。
func observeDiagnostics(d *debug.Debugger, prevSeq uint64, prevTotals [debug.DiagKindCount]uint64) ([]DiagSummary, uint64, [debug.DiagKindCount]uint64) {
	seq, totals := d.DiagSeq(), d.DiagTotals()
	if seq < prevSeq {
		// ROM を読み込み直して数え直した。
		prevSeq, prevTotals = 0, [debug.DiagKindCount]uint64{}
	}
	if seq == prevSeq {
		return nil, seq, totals
	}
	firsts := d.DiagSince(prevSeq)
	var out []DiagSummary
	for k := debug.DiagKind(0); k < debug.DiagKindCount; k++ {
		cnt := totals[k] - prevTotals[k]
		if cnt == 0 {
			continue
		}
		s := DiagSummary{Kind: k.String(), Count: cnt}
		if x, ok := firsts[k]; ok {
			s.First = diagInfo(d.Symbols(), x)
		} else {
			s.First = DiagInfo{Kind: k.String(), Detail: "最初の 1 件は記録からあふれた（diag.list で見る）"}
		}
		out = append(out, s)
	}
	return out, seq, totals
}

func registerAnalysis(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("diag.configure", ClassConfig, diagConfigureParams{}, nil, "Diagnostic の種類ごとの有効・無効と停止を設定する",
		"Configure Diagnostics (NES-specific bug detectors): enable 'all'/'default'/[kinds], and per-kind {enabled, stop}. stop: true pauses with stop_reason 'diagnostic'.", handleDiagConfigure)
	reg("diag.list", ClassObserve, diagListParams{}, []string{"kinds"}, "検知した Diagnostic を種類と位置ごとに返す（件数・最初と最後のフレーム）",
		"List detected Diagnostics grouped by kind and PC, with counts, first/last frame, symbol and source line.", handleDiagList)
	reg("trace.enable", ClassConfig, traceEnableParams{}, nil, "命令のトレースの記録を始める。bus: true でバスアクセスも記録する",
		"Start recording an instruction trace ring (and bus accesses with bus: true, needed for read/write filters).", handleTraceEnable)
	reg("trace.disable", ClassConfig, InstanceParam{}, nil, "トレースの記録をやめる（記録は残る）",
		"Stop recording the trace (the recorded ring stays queryable).", handleTraceDisable)
	reg("trace.query", ClassObserve, traceQueryParams{}, nil, "トレースから条件（フレーム・位置・種類・アクセス先）に合う命令を取り出す",
		"Query the trace: filter by frames [from,to], pc (symbol name or 'lo..hi'), kinds (jsr, rts, rti, interrupt, branch_taken, read, write), addr. Text output includes source lines.", handleTraceQuery)
	reg("trace.summary", ClassObserve, traceSummaryParams{}, []string{"frames"}, "トレースの範囲の処理の流れを要約する（関数ごとの呼び出し回数とサイクル数、割り込み、よく実行した位置）",
		"Summarize the trace: per-function calls and inclusive/exclusive cycles, callers, interrupt counts, hot spots.", handleTraceSummary)
	reg("profile.start", ClassConfig, profileStartParams{}, []string{"idle"}, "プロファイルを始める。idle でアイドルループの範囲を指定できる",
		"Start profiling CPU time per frame (busy ratio, NMI duration, VBlank margin, per-function cycles, lag frames). Optional idle: idle-loop symbol or 'lo..hi'.", handleProfileStart)
	reg("profile.stop", ClassConfig, InstanceParam{}, nil, "プロファイルを止める（結果は残る）",
		"Stop profiling (the report stays available).", handleProfileStop)
	reg("profile.report", ClassObserve, profileReportParams{}, nil, "プロファイルの結果を返す",
		"Return the profile report: busy cycles/ratio min/avg/max, frames over 90%, NMI cycles and overruns, VBlank margin, functions, lag frames, idle_detection.", handleProfileReport)
}

// --- diag ---

type diagItem struct {
	Enabled *bool `json:"enabled,omitempty" desc:"有効にするか"`
	Stop    *bool `json:"stop,omitempty" desc:"検知したら止めるか"`
}

type diagConfigureParams struct {
	InstanceParam
	Enable json.RawMessage     `json:"enable,omitempty" desc:"all・default・none、または有効にする種類の列（他は無効）"`
	Items  map[string]diagItem `json:"items,omitempty" desc:"種類ごとの {enabled, stop}"`
}

// DiagKindState は種類 1 つの設定。
type DiagKindState struct {
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
	Stop    bool   `json:"stop"`
	Default bool   `json:"default"`
}

func checkDiagKind(name string) (debug.DiagKind, error) {
	k, ok := debug.ParseDiagKind(name)
	if !ok {
		return 0, Errorf(KindInvalidParams, "Diagnostic の種類 %q を知らない（%s）", name, strings.Join(debug.DiagKindNames(), "・"))
	}
	return k, nil
}

// applyDiagEnable は enable の指定を設定に当てる。
func applyDiagEnable(cfg *debug.DiagConfig, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var word string
	if err := json.Unmarshal(raw, &word); err == nil {
		switch word {
		case "all":
			for i := range cfg.Enabled {
				cfg.Enabled[i] = true
			}
		case "default":
			cfg.Enabled = debug.DefaultDiagConfig().Enabled
		case "none":
			cfg.Enabled = [debug.DiagKindCount]bool{}
		default:
			return Errorf(KindInvalidParams, "enable は all・default・none か種類の列とする（%q）", word)
		}
		return nil
	}
	var kinds []string
	if err := json.Unmarshal(raw, &kinds); err != nil {
		return Errorf(KindInvalidParams, "enable は all・default・none か種類の列とする")
	}
	cfg.Enabled = [debug.DiagKindCount]bool{}
	for _, name := range kinds {
		k, err := checkDiagKind(name)
		if err != nil {
			return err
		}
		cfg.Enabled[k] = true
	}
	return nil
}

func diagStates(cfg debug.DiagConfig) []DiagKindState {
	def := debug.DefaultDiagConfig()
	out := make([]DiagKindState, debug.DiagKindCount)
	for k := debug.DiagKind(0); k < debug.DiagKindCount; k++ {
		out[k] = DiagKindState{Kind: k.String(), Enabled: cfg.Enabled[k], Stop: cfg.Stop[k], Default: def.Enabled[k]}
	}
	return out
}

func handleDiagConfigure(c *Context, raw json.RawMessage) (any, error) {
	var p diagConfigureParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	var cfg debug.DiagConfig
	var cerr error
	ok := c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		cfg = d.DiagConfig()
		if cerr = applyDiagEnable(&cfg, p.Enable); cerr != nil {
			return
		}
		names := make([]string, 0, len(p.Items))
		for name := range p.Items {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			k, err := checkDiagKind(name)
			if err != nil {
				cerr = err
				return
			}
			it := p.Items[name]
			if it.Enabled != nil {
				cfg.Enabled[k] = *it.Enabled
			}
			if it.Stop != nil {
				cfg.Stop[k] = *it.Stop
				if *it.Stop && it.Enabled == nil {
					cfg.Enabled[k] = true
				}
			}
		}
		d.SetDiagConfig(cfg)
	})
	if !ok {
		return nil, Errorf(KindInternalError, "エミュレーションが停止している")
	}
	if cerr != nil {
		return nil, cerr
	}
	return struct {
		Kinds []DiagKindState `json:"kinds"`
	}{diagStates(cfg)}, nil
}

type diagListParams struct {
	InstanceParam
	Kinds []string `json:"kinds,omitempty" desc:"種類で絞り込む"`
	Limit int      `json:"limit,omitempty" desc:"件数の上限（1–1000）" default:"1000"`
}

// DiagListItem は diag.list の 1 組。
type DiagListItem struct {
	DiagInfo
	Count      uint64 `json:"count"`
	FirstFrame uint64 `json:"first_frame"`
	LastFrame  uint64 `json:"last_frame"`
}

func handleDiagList(c *Context, raw json.RawMessage) (any, error) {
	var p diagListParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Limit == 0 {
		p.Limit = 1000
	}
	if p.Limit < 1 || p.Limit > 1000 {
		return nil, Errorf(KindInvalidParams, "limit は 1–1000 とする")
	}
	var want []debug.DiagKind
	for _, name := range p.Kinds {
		k, err := checkDiagKind(name)
		if err != nil {
			return nil, err
		}
		want = append(want, k)
	}
	items := []DiagListItem{}
	totals := map[string]uint64{}
	var overflow uint64
	var cfg debug.DiagConfig
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		stats, of := d.DiagStats()
		overflow = of
		cfg = d.DiagConfig()
		tot := d.DiagTotals()
		for k := debug.DiagKind(0); k < debug.DiagKindCount; k++ {
			if tot[k] > 0 && (len(want) == 0 || slices.Contains(want, k)) {
				totals[k.String()] = tot[k]
			}
		}
		for _, st := range stats {
			if len(want) > 0 && !slices.Contains(want, st.Kind) {
				continue
			}
			if len(items) == p.Limit {
				break
			}
			items = append(items, DiagListItem{DiagInfo: diagInfo(d.Symbols(), st.First), Count: st.Count,
				FirstFrame: st.First.Frame, LastFrame: st.LastFrame})
		}
	})
	res := struct {
		Items    []DiagListItem    `json:"items"`
		Totals   map[string]uint64 `json:"totals"`
		Overflow uint64            `json:"overflow,omitempty"`
		Enabled  []string          `json:"enabled"`
		Notes    []string          `json:"notes,omitempty"`
	}{Items: items, Totals: totals, Overflow: overflow, Enabled: []string{}}
	for k := debug.DiagKind(0); k < debug.DiagKindCount; k++ {
		if cfg.Enabled[k] {
			res.Enabled = append(res.Enabled, k.String())
		}
	}
	if overflow > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("種類と位置の組が 1000 を超えたため %d 件を組に数えていない（totals には含む）", overflow))
	}
	return res, nil
}

// --- trace ---

type traceEnableParams struct {
	InstanceParam
	RingSize    int  `json:"ring_size,omitempty" desc:"命令のリングの大きさ（1000–4000000）" default:"1000000"`
	Bus         bool `json:"bus,omitempty" desc:"バスアクセスも記録する（read・write の絞り込みに要る）"`
	BusRingSize int  `json:"bus_ring_size,omitempty" desc:"バスアクセスのリングの大きさ（bus: true のとき）" default:"2000000"`
}

const (
	maxTraceRing   = 4_000_000
	defaultBusRing = 2_000_000
	maxBusRing     = 8_000_000
)

// TraceStatus は trace.enable・trace.disable の結果。
type TraceStatus struct {
	Enabled     bool   `json:"enabled"`
	RingSize    int    `json:"ring_size"`
	BusRingSize int    `json:"bus_ring_size"`
	Recorded    int    `json:"recorded"`
	MemoryBytes int    `json:"memory_bytes"`
	Note        string `json:"note,omitempty"`
}

func traceStatus(d *debug.Debugger) TraceStatus {
	a := d.AgentTraceState()
	t := d.Tracer()
	bus := 0
	if a.Enabled && t.BusEnabled() {
		bus = a.BusRingSize
	}
	return TraceStatus{Enabled: a.Enabled, RingSize: t.Size(), BusRingSize: bus, Recorded: t.Len(), MemoryBytes: t.Bytes() + bus*16}
}

func handleTraceEnable(c *Context, raw json.RawMessage) (any, error) {
	var p traceEnableParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.RingSize == 0 {
		p.RingSize = debug.DefaultTraceRingSize
	}
	if p.RingSize < 1000 || p.RingSize > maxTraceRing {
		return nil, Errorf(KindInvalidParams, "ring_size は 1000–%d とする", maxTraceRing)
	}
	if !p.Bus {
		p.BusRingSize = 0
	} else if p.BusRingSize == 0 {
		p.BusRingSize = defaultBusRing
	}
	if p.BusRingSize < 0 || p.BusRingSize > maxBusRing {
		return nil, Errorf(KindInvalidParams, "bus_ring_size は 1–%d とする", maxBusRing)
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var st TraceStatus
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		d.SetAgentTrace(debug.AgentTrace{Enabled: true, RingSize: p.RingSize, BusRingSize: p.BusRingSize})
		st = traceStatus(d)
	})
	return st, nil
}

func handleTraceDisable(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	var st TraceStatus
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		a := d.AgentTraceState()
		a.Enabled = false
		d.SetAgentTrace(a)
		st = traceStatus(d)
	})
	return st, nil
}

type traceQueryParams struct {
	InstanceParam
	Frames []uint64        `json:"frames,omitempty" desc:"[開始, 終了] のフレーム番号"`
	PC     json.RawMessage `json:"pc,omitempty" desc:"位置。Symbol の名前（次の Symbol まで）、\"lo..hi\"、またはその列"`
	Kinds  []string        `json:"kinds,omitempty" desc:"jsr・rts・rti・interrupt・branch_taken・read・write"`
	Addr   string          `json:"addr,omitempty" desc:"read・write のアクセス先（位置か \"lo..hi\"）"`
	Limit  int             `json:"limit,omitempty" desc:"件数の上限（1–5000）" default:"200"`
	Format string          `json:"format,omitempty" desc:"text（nestest 形式にソース行を添える）・json" default:"\"text\""`
}

// TraceEntryJSON は format: json の 1 件。
type TraceEntryJSON struct {
	Frame     uint64 `json:"frame"`
	Cycles    uint64 `json:"cycles"`
	PC        string `json:"pc"`
	Symbol    string `json:"symbol,omitempty"`
	Source    string `json:"source,omitempty"`
	Bytes     string `json:"bytes"`
	Disasm    string `json:"disasm"`
	A         uint8  `json:"a"`
	X         uint8  `json:"x"`
	Y         uint8  `json:"y"`
	P         uint8  `json:"p"`
	S         uint8  `json:"s"`
	Scanline  int    `json:"scanline"`
	Dot       int    `json:"dot"`
	Kind      string `json:"kind,omitempty"`
	Interrupt string `json:"interrupt,omitempty"`
	Access    *struct {
		Addr  string `json:"addr"`
		Value uint8  `json:"value"`
		Write bool   `json:"write"`
	} `json:"access,omitempty"`
}

// parseSpanList は pc の指定（文字列か文字列の列）を読む。
func parseSpanList(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, Errorf(KindInvalidParams, "pc は位置の文字列か、その列とする")
	}
	return many, nil
}

// resolveSpan は "名前"・"位置"・"lo..hi" を範囲にする。名前だけを書いたときは
// その Symbol から次の Symbol までとする。
func (inst *Instance) resolveSpan(s string) (debug.LocSpan, error) {
	s = strings.TrimSpace(s)
	if lo, hi, ok := strings.Cut(s, ".."); ok {
		a, err := inst.spanPoint(lo)
		if err != nil {
			return debug.LocSpan{}, err
		}
		b, err := inst.spanPoint(hi)
		if err != nil {
			return debug.LocSpan{}, err
		}
		if a.Space != b.Space || b.Lo < a.Lo {
			return debug.LocSpan{}, Errorf(KindInvalidLocation, "範囲 %q の両端が同じ空間に無いか、逆になっている", s)
		}
		return debug.LocSpan{Space: a.Space, Lo: a.Lo, Hi: b.Lo, CPULo: a.CPULo, CPUHi: b.CPULo, Name: s}, nil
	}
	if syms := inst.symbols(); syms != nil {
		if span, ok := syms.SpanOfName(s); ok {
			return span, nil
		}
	}
	return inst.spanPoint(s)
}

// spanPoint は 1 つの位置を、長さ 1 の範囲にする。
func (inst *Instance) spanPoint(s string) (debug.LocSpan, error) {
	loc, err := inst.ResolveLocation(s)
	if err != nil {
		return debug.LocSpan{}, err
	}
	switch loc.Space {
	case debug.SpaceCPU, debug.SpaceRAM:
		addr := uint16(loc.Addr)
		var sl debug.SymbolLoc
		inst.Emu.WithDebugger(func(d *debug.Debugger) {
			var off debug.Offsetter
			if n := d.Machine(); n != nil {
				off = n.Cart.PRGOffset
			}
			sl = debug.LocOf(addr, off)
		})
		return debug.LocSpan{Space: sl.Space, Lo: sl.Offset, Hi: sl.Offset, CPULo: addr, CPUHi: addr, Name: s}, nil
	case debug.SpacePRGROM:
		cpu := loc.CPU
		if cpu < 0 {
			cpu = 0
		}
		return debug.LocSpan{Space: debug.SymSpacePRG, Lo: uint32(loc.Addr), Hi: uint32(loc.Addr),
			CPULo: uint16(cpu), CPUHi: uint16(cpu), Name: s}, nil
	}
	return debug.LocSpan{}, Errorf(KindInvalidLocation, "%q は CPU か PRG-ROM の位置ではない", s)
}

func handleTraceQuery(c *Context, raw json.RawMessage) (any, error) {
	var p traceQueryParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Limit == 0 {
		p.Limit = 200
	}
	if p.Limit < 1 || p.Limit > 5000 {
		return nil, Errorf(KindInvalidParams, "limit は 1–5000 とする")
	}
	if p.Format == "" {
		p.Format = "text"
	}
	if p.Format != "text" && p.Format != "json" {
		return nil, Errorf(KindInvalidParams, "format は text・json とする")
	}
	f := debug.TraceFilter{Limit: p.Limit, Kinds: p.Kinds}
	for _, k := range p.Kinds {
		if !slices.Contains(debug.TraceKinds, k) {
			return nil, Errorf(KindInvalidParams, "kinds の %q を知らない（%s）", k, strings.Join(debug.TraceKinds, "・"))
		}
	}
	if len(p.Frames) > 0 {
		if len(p.Frames) != 2 || p.Frames[1] < p.Frames[0] {
			return nil, Errorf(KindInvalidParams, "frames は [開始, 終了] とする")
		}
		f.Frames = &[2]uint64{p.Frames[0], p.Frames[1]}
	}
	pcs, err := parseSpanList(p.PC)
	if err != nil {
		return nil, err
	}
	for _, s := range pcs {
		span, err := c.Instance.resolveSpan(s)
		if err != nil {
			return nil, err
		}
		f.PC = append(f.PC, debug.AddrSpan{Lo: span.CPULo, Hi: span.CPUHi})
	}
	if p.Addr != "" {
		lo, hi, isRange := strings.Cut(p.Addr, "..")
		a, err := c.Instance.ResolveLocation(lo)
		if err != nil {
			return nil, err
		}
		span := debug.AddrSpan{Lo: uint16(a.Addr), Hi: uint16(a.Addr)}
		if isRange {
			b, err := c.Instance.ResolveLocation(hi)
			if err != nil {
				return nil, err
			}
			span.Hi = uint16(b.Addr)
		}
		f.Addr = &span
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	var res debug.TraceResult
	var qerr error
	var lines []string
	var entries []TraceEntryJSON
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		res, qerr = d.QueryTrace(f)
		if qerr != nil {
			return
		}
		syms := d.Symbols()
		cart := d.Machine().Cart
		for _, h := range res.Hits {
			r := h.Rec
			off := int32(-1)
			if o, ok := cart.PRGOffset(r.PC); ok && r.PC >= 0x8000 {
				off = int32(o)
			}
			name, src := locName(syms, r.PC, off)
			if p.Format == "text" {
				line := fmt.Sprintf("f%-6d %s", h.Frame, r.Line())
				var tags []string
				if h.Interrupt != "" {
					tags = append(tags, "← "+strings.ToUpper(h.Interrupt))
				}
				if h.Access != nil {
					dir := "R"
					if h.Access.Write {
						dir = "W"
					}
					tags = append(tags, fmt.Sprintf("%s $%04X=$%02X", dir, h.Access.Addr, h.Access.Value))
				}
				if name != "" {
					tags = append(tags, name)
				}
				if src != "" {
					tags = append(tags, src)
				}
				if len(tags) > 0 {
					line += "  ; " + strings.Join(tags, "  ")
				}
				lines = append(lines, line)
				continue
			}
			bytes, text := traceDisasm(r, d.Machine().Bus.Peek)
			e := TraceEntryJSON{Frame: h.Frame, Cycles: r.Cycles, PC: hex16(r.PC), Symbol: name, Source: src,
				Bytes: bytes, Disasm: text,
				A: r.A, X: r.X, Y: r.Y, P: r.P, S: r.S, Scanline: int(r.Scanline), Dot: int(r.Dot),
				Kind: h.Kind, Interrupt: h.Interrupt}
			if h.Access != nil {
				e.Access = &struct {
					Addr  string `json:"addr"`
					Value uint8  `json:"value"`
					Write bool   `json:"write"`
				}{hex16(h.Access.Addr), h.Access.Value, h.Access.Write}
			}
			entries = append(entries, e)
		}
	})
	if qerr != nil {
		if errors.Is(qerr, debug.ErrBusNotTraced) {
			return nil, Errorf(KindInvalidParams, "read・write で絞り込むには trace.enable に bus: true を渡す")
		}
		return nil, Errorf(KindInternalError, "%v", qerr)
	}
	out := map[string]any{"count": len(res.Hits), "truncated": res.Truncated, "scanned": res.Scanned,
		"covered_frames": res.Covered}
	if p.Format == "text" {
		out["text"] = strings.Join(lines, "\n")
	} else {
		if entries == nil {
			entries = []TraceEntryJSON{}
		}
		out["entries"] = entries
	}
	if len(res.Notes) > 0 {
		out["notes"] = res.Notes
	}
	return out, nil
}

// traceDisasm は記録した命令のバイト列から逆アセンブルし、バイト列と命令を返す。
// メモリは記録の後で変わっていることがあるため、命令のバイトは記録のものを使う。
func traceDisasm(r cpu.TraceRecord, peek func(uint16) uint8) (string, string) {
	pk := func(a uint16) uint8 {
		if d := a - r.PC; d < 3 {
			return r.Bytes[d]
		}
		return peek(a)
	}
	text, n := cpu.Disassemble(pk, r.PC, r.X, r.Y)
	parts := make([]string, 0, 3)
	for i := 0; i < n && i < 3; i++ {
		parts = append(parts, fmt.Sprintf("%02X", r.Bytes[i]))
	}
	return strings.Join(parts, " "), text
}

type traceSummaryParams struct {
	InstanceParam
	Frames []uint64 `json:"frames,omitempty" desc:"[開始, 終了] のフレーム番号。省くとリング全体"`
}

// FuncSummary は関数 1 つの集計。
type FuncSummary struct {
	Symbol     string   `json:"symbol"`
	Addr       string   `json:"addr"`
	Source     string   `json:"source,omitempty"`
	Calls      uint64   `json:"calls"`
	CyclesIncl uint64   `json:"cycles_incl"`
	CyclesExcl uint64   `json:"cycles_excl"`
	Callers    []string `json:"callers,omitempty"`
	Interrupt  bool     `json:"interrupt,omitempty"`
}

// funcName は関数の入口の名前を返す。無ければ sub_$C340 の形。
func funcName(syms *debug.Symbols, key debug.SymbolLoc, addr uint16) string {
	off := int32(-1)
	if key.Space == debug.SymSpacePRG {
		off = int32(key.Offset)
	}
	if name, _ := locName(syms, addr, off); name != "" {
		return name
	}
	return fmt.Sprintf("sub_$%04X", addr)
}

// funcSummaries は関数の集計を名前つきにする（最大 limit 件）。
func funcSummaries(syms *debug.Symbols, stats []debug.FuncStat, limit int) []FuncSummary {
	addrOf := map[debug.SymbolLoc]uint16{}
	for _, st := range stats {
		addrOf[st.Key] = st.Addr
	}
	out := []FuncSummary{}
	for _, st := range stats {
		if len(out) == limit {
			break
		}
		off := int32(-1)
		if st.Key.Space == debug.SymSpacePRG {
			off = int32(st.Key.Offset)
		}
		_, src := locName(syms, st.Addr, off)
		fs := FuncSummary{Symbol: funcName(syms, st.Key, st.Addr), Addr: hex16(st.Addr), Source: src, Calls: st.Calls,
			CyclesIncl: st.Incl, CyclesExcl: st.Excl, Interrupt: st.Interrupt}
		for _, c := range st.Callers {
			fs.Callers = append(fs.Callers, funcName(syms, c, addrOf[c]))
		}
		out = append(out, fs)
	}
	return out
}

func handleTraceSummary(c *Context, raw json.RawMessage) (any, error) {
	var p traceSummaryParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	var frames *[2]uint64
	if len(p.Frames) > 0 {
		if len(p.Frames) != 2 || p.Frames[1] < p.Frames[0] {
			return nil, Errorf(KindInvalidParams, "frames は [開始, 終了] とする")
		}
		frames = &[2]uint64{p.Frames[0], p.Frames[1]}
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	type hot struct {
		PC     string  `json:"pc"`
		Symbol string  `json:"symbol,omitempty"`
		Count  int     `json:"count"`
		Share  float64 `json:"share"`
	}
	var out struct {
		Frames       [2]uint64     `json:"frames"`
		Instructions int           `json:"instructions"`
		Functions    []FuncSummary `json:"functions"`
		Interrupts   struct {
			NMI uint64 `json:"nmi"`
			IRQ uint64 `json:"irq"`
		} `json:"interrupts"`
		HotSpots []hot    `json:"hot_spots"`
		Notes    []string `json:"notes,omitempty"`
	}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		s := d.SummarizeTrace(frames)
		syms := d.Symbols()
		out.Frames, out.Instructions, out.Notes = s.Frames, s.Instructions, s.Notes
		out.Functions = funcSummaries(syms, s.Functions, 100)
		out.Interrupts.NMI, out.Interrupts.IRQ = s.NMI, s.IRQ
		out.HotSpots = []hot{}
		for _, h := range s.HotSpots {
			out.HotSpots = append(out.HotSpots, hot{PC: hex16(h.PC), Symbol: d.NearestLabel(h.PC), Count: h.Count, Share: h.Share})
		}
	})
	return out, nil
}

// --- profile ---

type profileStartParams struct {
	InstanceParam
	Idle json.RawMessage `json:"idle,omitempty" desc:"アイドルループの範囲。Symbol の名前、\"lo..hi\"、またはその列。省くと名前（idle・wait_vblank・wait_nmi）か自動判定"`
}

func handleProfileStart(c *Context, raw json.RawMessage) (any, error) {
	var p profileStartParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	specs, err := parseSpanList(p.Idle)
	if err != nil {
		return nil, Errorf(KindInvalidParams, "idle は位置の文字列か、その列とする")
	}
	var o debug.ProfileOptions
	for _, s := range specs {
		span, err := c.Instance.resolveSpan(s)
		if err != nil {
			return nil, err
		}
		o.Idle = append(o.Idle, span)
	}
	var method string
	var spans []debug.LocSpan
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		d.StartProfile(o)
		rep, _ := d.ProfileReport()
		method, spans = rep.IdleMethod, rep.IdleSpans
	})
	return struct {
		Started bool        `json:"started"`
		Idle    idleSummary `json:"idle_detection"`
	}{true, idleSummaryOf(method, spans)}, nil
}

type idleSummary struct {
	Method string   `json:"method"`
	Ranges []string `json:"ranges"`
}

func idleSummaryOf(method string, spans []debug.LocSpan) idleSummary {
	s := idleSummary{Method: method, Ranges: []string{}}
	for _, sp := range spans {
		r := fmt.Sprintf("$%04X..$%04X", sp.CPULo, sp.CPUHi)
		if sp.Name != "" && !strings.Contains(sp.Name, "..") {
			r = sp.Name + " " + r
		}
		s.Ranges = append(s.Ranges, r)
	}
	return s
}

func handleProfileStop(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) { d.StopProfile() })
	return struct {
		Stopped bool `json:"stopped"`
	}{true}, nil
}

type profileReportParams struct {
	InstanceParam
	Functions int `json:"functions,omitempty" desc:"返す関数の数（1–500）" default:"30"`
}

// statJSON は最小・平均・最大。
type statJSON struct {
	Min float64 `json:"min"`
	Avg float64 `json:"avg"`
	Max float64 `json:"max"`
}

func statOf(s debug.Stat) statJSON { return statJSON{s.Min, s.Avg, s.Max} }

func handleProfileReport(c *Context, raw json.RawMessage) (any, error) {
	var p profileReportParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Functions == 0 {
		p.Functions = 30
	}
	if p.Functions < 1 || p.Functions > 500 {
		return nil, Errorf(KindInvalidParams, "functions は 1–500 とする")
	}
	type vblank struct {
		Frames uint64   `json:"frames"`
		Margin statJSON `json:"margin_cycles"`
		Worst  any      `json:"worst,omitempty"`
	}
	var out struct {
		Active     bool      `json:"active"`
		Frames     [2]uint64 `json:"frames"`
		FrameCount uint64    `json:"frame_count"`
		Busy       struct {
			Cycles statJSON `json:"cycles"`
			Ratio  statJSON `json:"ratio"`
		} `json:"busy"`
		Over90 []map[string]any `json:"over_90"`
		NMI    struct {
			Count         uint64   `json:"count"`
			Cycles        statJSON `json:"cycles"`
			Overruns      uint64   `json:"overruns"`
			OverrunFrames []uint64 `json:"overrun_frames,omitempty"`
		} `json:"nmi"`
		VBlank    vblank        `json:"vblank_ppu"`
		Functions []FuncSummary `json:"functions"`
		Lag       struct {
			Count  uint64   `json:"count"`
			Frames []uint64 `json:"frames,omitempty"`
		} `json:"lag_frames"`
		IdleDetection idleSummary `json:"idle_detection"`
		Notes         []string    `json:"notes,omitempty"`
	}
	var have bool
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		rep, ok := d.ProfileReport()
		have = ok
		if !ok {
			return
		}
		syms := d.Symbols()
		out.Active, out.Frames, out.FrameCount = rep.Active, [2]uint64{rep.FromFrame, rep.ToFrame}, rep.Frames
		out.Busy.Cycles, out.Busy.Ratio = statOf(rep.BusyCycles), statOf(rep.BusyRatio)
		out.Over90 = []map[string]any{}
		for _, f := range rep.Over90 {
			out.Over90 = append(out.Over90, map[string]any{"frame": f.Frame, "cycles": f.Cycles, "ratio": f.Ratio})
		}
		out.NMI.Count, out.NMI.Cycles = rep.NMICycles.Count, statOf(rep.NMICycles)
		out.NMI.Overruns, out.NMI.OverrunFrames = rep.NMIOverruns, rep.OverrunFrames
		out.VBlank = vblank{Frames: rep.VBlankMargin.Count, Margin: statOf(rep.VBlankMargin)}
		if w := rep.Worst; w != nil {
			out.VBlank.Worst = map[string]any{"frame": w.Frame, "pc": hex16(w.PC), "symbol": d.NearestLabel(w.PC),
				"reg": hex16(w.Reg), "scanline": w.Scanline, "dot": w.Dot, "margin_cycles": w.Margin}
		}
		out.Functions = funcSummaries(syms, rep.Functions, p.Functions)
		out.Lag.Count, out.Lag.Frames = rep.LagCount, rep.LagFrames
		out.IdleDetection = idleSummaryOf(rep.IdleMethod, rep.IdleSpans)
		out.Notes = rep.Notes
	})
	if !have {
		return nil, Errorf(KindInvalidParams, "プロファイルを始めていない（profile.start）")
	}
	return out, nil
}
