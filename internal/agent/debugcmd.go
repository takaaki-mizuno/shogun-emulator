package agent

import (
	"encoding/json"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// ブレークポイントとウォッチの Agent Command（設計書 14 編 §14.7.2）。

func registerDebug(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("debug.bp.add", ClassConfig, bpAddParams{}, []string{"kind", "loc"}, "ブレークポイントを加える",
		"Add a breakpoint: exec/read/write at loc (optionally to end), ppu at scanline/dot, or event (nmi, irq, reset, sprite0, mapperIRQ, uninitRead, mmc3Reload). Optional condition.", handleBPAdd)
	reg("debug.bp.remove", ClassConfig, bpIDParams{}, []string{"id"}, "ブレークポイントを取り除く",
		"Remove a breakpoint by id.", handleBPRemove)
	reg("debug.bp.list", ClassObserve, InstanceParam{}, nil, "ブレークポイントの一覧",
		"List breakpoints.", handleBPList)
	reg("debug.bp.enable", ClassConfig, bpEnableParams{}, []string{"id", "enabled"}, "ブレークポイントの有効・無効を切り替える",
		"Enable or disable a breakpoint.", handleBPEnable)
	reg("debug.watch.add", ClassConfig, watchParams{}, []string{"loc"}, "ウォッチを加える。値は Observation の watch に入る",
		"Add a watch; its value and changes appear in every observation.", handleWatchAdd)
	reg("debug.watch.remove", ClassConfig, watchParams{}, []string{"loc"}, "ウォッチを外す",
		"Remove a watch.", handleWatchRemove)
	reg("debug.watch.list", ClassObserve, InstanceParam{}, nil, "ウォッチの一覧と現在値",
		"List watches with current values.", handleWatchList)
	reg("expr.eval", ClassObserve, evalParams{}, []string{"expr"}, "式を評価し、値と真偽を返す",
		"Evaluate an expression (registers, [addr], symbols, game.<item>, frame, ...) and return its value and truth.", handleEval)
}

type evalParams struct {
	InstanceParam
	Expr string `json:"expr" desc:"式（game.mode == 'play' など）"`
}

// EvalResult は expr.eval の結果。Value は数値か、列挙の名前・文字列。
type EvalResult struct {
	Expr  string `json:"expr"`
	Value any    `json:"value"`
	Truth bool   `json:"truth"`
}

// handleEval は式を評価する（設計書 14 編 §14.11.4）。Scenario の assert が使う。
func handleEval(c *Context, raw json.RawMessage) (any, error) {
	var p evalParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Expr) == "" {
		return nil, Errorf(KindInvalidParams, "expr を指定する")
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	cond, err := c.Instance.parseCondition(p.Expr)
	if err != nil {
		return nil, conditionError(err)
	}
	res := EvalResult{Expr: p.Expr}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		v := cond.Value(d)
		res.Truth = cond.Eval(d)
		if v.HasStr {
			res.Value = v.Str
		} else {
			res.Value = v.Num
		}
	})
	return res, nil
}

type bpAddParams struct {
	InstanceParam
	Kind      string `json:"kind" desc:"exec・read・write・ppu・event"`
	Loc       string `json:"loc,omitempty" desc:"位置（exec・read・write）"`
	End       string `json:"end,omitempty" desc:"範囲の終わりの位置"`
	Scanline  int    `json:"scanline,omitempty" desc:"スキャンライン（ppu）"`
	Dot       int    `json:"dot,omitempty" desc:"ドット（ppu）"`
	Event     string `json:"event,omitempty" desc:"イベント（nmi・irq・reset・sprite0・mapperIRQ・uninitRead・mmc3Reload）"`
	Condition string `json:"condition,omitempty" desc:"止まる条件式"`
	Enabled   *bool  `json:"enabled,omitempty" desc:"有効にするか" default:"true"`
}

// BreakpointInfo はブレークポイント 1 件。
type BreakpointInfo struct {
	ID          int    `json:"id"`
	Kind        string `json:"kind"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	Scanline    int    `json:"scanline,omitempty"`
	Dot         int    `json:"dot,omitempty"`
	Event       string `json:"event,omitempty"`
	Condition   string `json:"condition,omitempty"`
	Enabled     bool   `json:"enabled"`
	HitCount    uint64 `json:"hit_count"`
	Description string `json:"description"`
}

func bpInfo(b debug.Breakpoint) BreakpointInfo {
	bi := BreakpointInfo{ID: b.ID, Kind: debug.BreakKindName(b.Kind), Enabled: b.Enabled, HitCount: b.HitCount,
		Description: b.Describe()}
	switch b.Kind {
	case debug.BreakExec, debug.BreakRead, debug.BreakWrite:
		bi.Start, bi.End = hex16(b.AddrStart), hex16(b.AddrEnd)
	case debug.BreakPPUPosition:
		bi.Scanline, bi.Dot = b.Scanline, b.Dot
	case debug.BreakEvent:
		bi.Event = debug.EventName(b.Event)
	}
	if b.Condition != nil {
		bi.Condition = b.Condition.Expr
	}
	return bi
}

func handleBPAdd(c *Context, raw json.RawMessage) (any, error) {
	var p bpAddParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	kind, ok := debug.ParseBreakKind(p.Kind)
	if !ok {
		return nil, Errorf(KindInvalidParams, "kind %q を知らない（exec・read・write・ppu・event）", p.Kind)
	}
	b := debug.Breakpoint{Kind: kind, Enabled: p.Enabled == nil || *p.Enabled}
	switch kind {
	case debug.BreakExec, debug.BreakRead, debug.BreakWrite:
		if p.Loc == "" {
			return nil, Errorf(KindInvalidParams, "kind %s には loc を指定する", p.Kind)
		}
		start, err := c.Instance.ResolveLocation(p.Loc)
		if err != nil {
			return nil, err
		}
		a, err := start.cpuAddr()
		if err != nil {
			return nil, err
		}
		b.AddrStart, b.AddrEnd = a, a
		if p.End != "" {
			end, err := c.Instance.ResolveLocation(p.End)
			if err != nil {
				return nil, err
			}
			e, err := end.cpuAddr()
			if err != nil {
				return nil, err
			}
			if e < a {
				return nil, Errorf(KindInvalidParams, "end が loc より前にある")
			}
			b.AddrEnd = e
		}
	case debug.BreakPPUPosition:
		if p.Scanline < -1 || p.Scanline > 311 || p.Dot < 0 || p.Dot > 340 {
			return nil, Errorf(KindInvalidParams, "scanline は -1–311、dot は 0–340 とする")
		}
		b.Scanline, b.Dot = p.Scanline, p.Dot
	case debug.BreakEvent:
		ev, ok := debug.ParseEvent(p.Event)
		if !ok {
			return nil, Errorf(KindInvalidParams, "event %q を知らない（%s）", p.Event, strings.Join(debug.EventNames(), "・"))
		}
		b.Event = ev
	}
	if p.Condition != "" {
		cond, err := c.Instance.parseCondition(p.Condition)
		if err != nil {
			return nil, conditionError(err)
		}
		b.Condition = cond
	}
	var out debug.Breakpoint
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		id := d.AddBreakpoint(b)
		for _, x := range d.Breakpoints() {
			if x.ID == id {
				out = x
			}
		}
	})
	// GUI で加えたものと同じく ROM ごとのファイルへ保存する。
	c.Instance.Emu.SaveSymbols()
	if c.Host.opts.Kind == KindGUI && c.Host.opts.OnAgentBreakpoint != nil {
		c.Instance.mu.Lock()
		held := c.Instance.control.heldBy(c.Conn)
		c.Instance.mu.Unlock()
		if !held {
			c.Host.opts.OnAgentBreakpoint()
		}
	}
	return bpInfo(out), nil
}

type bpIDParams struct {
	InstanceParam
	ID int `json:"id" desc:"ブレークポイントの ID"`
}

// findBP は ID のブレークポイントがあるかを確かめる。
func findBP(d *debug.Debugger, id int) bool {
	for _, b := range d.Breakpoints() {
		if b.ID == id {
			return true
		}
	}
	return false
}

func handleBPRemove(c *Context, raw json.RawMessage) (any, error) {
	var p bpIDParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	found := false
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		if found = findBP(d, p.ID); found {
			d.RemoveBreakpoint(p.ID)
		}
	})
	if !found {
		return nil, Errorf(KindInvalidParams, "ブレークポイント %d は無い", p.ID)
	}
	c.Instance.Emu.SaveSymbols()
	return handleBPList(c, nil)
}

func handleBPList(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	out := []BreakpointInfo{}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		for _, b := range d.Breakpoints() {
			out = append(out, bpInfo(b))
		}
	})
	return out, nil
}

type bpEnableParams struct {
	InstanceParam
	ID      int  `json:"id" desc:"ブレークポイントの ID"`
	Enabled bool `json:"enabled" desc:"有効にするか"`
}

func handleBPEnable(c *Context, raw json.RawMessage) (any, error) {
	var p bpEnableParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	found := false
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		if found = findBP(d, p.ID); found {
			d.SetBreakpointEnabled(p.ID, p.Enabled)
		}
	})
	if !found {
		return nil, Errorf(KindInvalidParams, "ブレークポイント %d は無い", p.ID)
	}
	c.Instance.Emu.SaveSymbols()
	return handleBPList(c, nil)
}

type watchParams struct {
	InstanceParam
	Loc string `json:"loc" desc:"CPU アドレス空間の位置"`
}

// WatchInfo はウォッチ 1 件。
type WatchInfo struct {
	Loc   string `json:"loc"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value"`
}

func watchAddr(c *Context, s string) (uint16, error) {
	loc, err := c.Instance.ResolveLocation(s)
	if err != nil {
		return 0, err
	}
	return loc.cpuAddr()
}

func handleWatchAdd(c *Context, raw json.RawMessage) (any, error) {
	var p watchParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	a, err := watchAddr(c, p.Loc)
	if err != nil {
		return nil, err
	}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) { d.Symbols().AddWatch(a) })
	c.Instance.Emu.SaveSymbols()
	return handleWatchList(c, nil)
}

func handleWatchRemove(c *Context, raw json.RawMessage) (any, error) {
	var p watchParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	a, err := watchAddr(c, p.Loc)
	if err != nil {
		return nil, err
	}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) { d.Symbols().RemoveWatch(a) })
	c.Instance.Emu.SaveSymbols()
	return handleWatchList(c, nil)
}

func handleWatchList(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	out := []WatchInfo{}
	c.Instance.Emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		for _, a := range d.Symbols().Watch() {
			w := WatchInfo{Loc: hex16(a), Name: d.Label(a)}
			if n != nil {
				w.Value = hex8(n.Bus.Peek(a))
			}
			out = append(out, w)
		}
	})
	return out, nil
}
