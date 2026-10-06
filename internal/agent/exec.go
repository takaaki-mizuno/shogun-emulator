package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// 進行の Agent Command（設計書 14 編 §14.8）。

const (
	maxStepFrames  = 36000
	maxUntilFrames = 216000
	maxStepUnits   = 1000000
)

func registerExec(r *Registry) {
	r.Register(CommandSpec{
		Name: "exec.step", Class: ClassAdvance, Params: stepParams{}, Positional: []string{"frames", "input"},
		DescJA: "入力を押したまま N フレーム進め、止まった理由と Observation を返す",
		DescEN: "Advance N frames holding the given input, then return the stop reason and an observation.",
		GUI:    true, Headless: true, Target: true, Handler: handleStep,
	})
	r.Register(CommandSpec{
		Name: "exec.input_sequence", Class: ClassAdvance, Params: sequenceParams{},
		DescJA: "入力の列（フレーム数と入力の組）を順に流す",
		DescEN: "Play a sequence of (frames, input) steps in order. Stops early on a breakpoint.",
		GUI:    true, Headless: true, Target: true, Handler: handleSequence,
	})
	r.Register(CommandSpec{
		Name: "exec.run_until", Class: ClassAdvance, Params: untilParams{}, Positional: []string{"condition"},
		DescJA: "条件式が成り立つまで進める",
		DescEN: "Run until a condition expression becomes true (checked at each frame start by default), up to max_frames.",
		GUI:    true, Headless: true, Target: true, Handler: handleRunUntil,
	})
	r.Register(CommandSpec{
		Name: "exec.step_unit", Class: ClassAdvance, Params: unitParams{}, Positional: []string{"unit", "count"},
		DescJA: "サイクル・命令・ステップオーバー・ステップアウト・スキャンライン単位で進める。to で指定位置まで",
		DescEN: "Step by cycle, instruction, over, out or scanline; or run to an address with 'to'.",
		GUI:    true, Headless: true, Target: true, Handler: handleStepUnit,
	})
	r.Register(CommandSpec{
		Name: "exec.reset", Class: ClassAdvance, Params: resetParams{},
		DescJA: "リセットする。hard: true で電源を入れ直す",
		DescEN: "Reset the console. hard: true power-cycles it.",
		GUI:    true, Headless: true, Target: true, Handler: handleReset,
	})
	r.Register(CommandSpec{
		Name: "exec.cancel", Class: ClassSession, Params: cancelParams{},
		DescJA: "この接続が先に送った進行の要求を止める",
		DescEN: "Cancel the in-flight and queued requests previously sent on this connection.",
		GUI:    true, Headless: true, Handler: handleCancel,
	})
}

type stepParams struct {
	InstanceParam
	Frames  int             `json:"frames,omitempty" desc:"進めるフレーム数（1–36000）" default:"1"`
	Input   string          `json:"input,omitempty" desc:"ポート 1 の入力（\"R+A\"、\"$81\"、\"\"）"`
	Input2  string          `json:"input2,omitempty" desc:"ポート 2 の入力"`
	Observe json.RawMessage `json:"observe,omitempty" desc:"観測の指定。false で frame と stop_reason だけ"`
	Break   *bool           `json:"break,omitempty" desc:"ブレークポイントで止まるか" default:"true"`
}

// inputs は入力の引数を読み、ムービーの再生中なら入力の指定を断る
// （設計書 14 編 §14.7.3）。
func inputs(inst *Instance, in1, in2 string) (*[2]uint8, error) {
	a, err := ParseInput(in1)
	if err != nil {
		return nil, err
	}
	b, err := ParseInput(in2)
	if err != nil {
		return nil, err
	}
	if inst.Emu.Status().Movie.Playing {
		if in1 != "" || in2 != "" {
			return nil, Errorf(KindMovieConflict, "ムービーの再生中は入力を指定できない")
		}
		return nil, nil
	}
	return &[2]uint8{a, b}, nil
}

// requireLoaded は ROM を読み込んでいることを確かめる。
func requireLoaded(inst *Instance) error {
	if !inst.Emu.Status().Loaded {
		return Errorf(KindNotLoaded, "Instance %s は ROM を読み込んでいない", inst.ID)
	}
	return nil
}

// finish は進行の結果から Observation を作る。
func finish(c *Context, o observeSpec, r emu.StepResult, reason string, detail any) (*Observation, error) {
	ob, err := c.Host.observe(c.Instance, c.Conn, o)
	if err != nil {
		return nil, err
	}
	if reason == StopBreakpoint && r.Break != nil && r.Break.Diagnostic != nil {
		// Diagnostic の stop で止まった（設計書 14 編 §14.20.3）。
		reason, detail = StopDiagnostic, diagInfo(c.Instance.sharedSymbols(), *r.Break.Diagnostic)
	}
	ob.StopReason = reason
	if reason == StopBreakpoint && r.Break != nil {
		detail = breakDetail(r.Break)
	}
	ob.StopDetail = detail
	ob.Frame = r.Frame
	return ob, nil
}

// cancelReason は取り消された進行の理由を決める。
func cancelReason(c *Context, timeout context.Context) string {
	if c.Instance != nil && c.Instance.takeControlLost() {
		return StopControlLost
	}
	if timeout != nil && errors.Is(timeout.Err(), context.DeadlineExceeded) && c.Ctx.Err() == nil {
		return StopTimeout
	}
	return StopCancelled
}

func handleStep(c *Context, raw json.RawMessage) (any, error) {
	var p stepParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Frames == 0 {
		p.Frames = 1
	}
	if p.Frames < 1 || p.Frames > maxStepFrames {
		return nil, Errorf(KindInvalidParams, "frames は 1–%d とする（%d）", maxStepFrames, p.Frames)
	}
	o, err := parseObserve(p.Observe)
	if err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	in, err := inputs(c.Instance, p.Input, p.Input2)
	if err != nil {
		return nil, err
	}
	r, err := c.Instance.Emu.StepWith(c.Ctx, emu.StepOptions{
		Kind: emu.StepFrame, Count: p.Frames, Input: in, NoBreak: p.Break != nil && !*p.Break,
	})
	if err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	reason := stopName(r.Reason)
	if r.Reason == emu.StopCancelled {
		reason = cancelReason(c, nil)
	}
	return finish(c, o, r, reason, nil)
}

type sequenceStep struct {
	Frames int    `json:"frames" desc:"フレーム数"`
	Input  string `json:"input,omitempty" desc:"ポート 1 の入力"`
	Input2 string `json:"input2,omitempty" desc:"ポート 2 の入力"`
}

type sequenceParams struct {
	InstanceParam
	Steps       []sequenceStep  `json:"steps" desc:"入力の列"`
	ObserveEach bool            `json:"observe_each,omitempty" desc:"各要素の終わりの要約を per_step に並べる"`
	Observe     json.RawMessage `json:"observe,omitempty" desc:"最後の観測の指定"`
	Break       *bool           `json:"break,omitempty" desc:"ブレークポイントで止まるか" default:"true"`
}

// SequenceResult は exec.input_sequence の結果。
type SequenceResult struct {
	*Observation
	CompletedSteps int            `json:"completed_steps"`
	PerStep        []*Observation `json:"per_step,omitempty"`
}

func handleSequence(c *Context, raw json.RawMessage) (any, error) {
	var p sequenceParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Steps) == 0 {
		return nil, Errorf(KindInvalidParams, "steps が空である")
	}
	total := 0
	ins := make([]*[2]uint8, len(p.Steps))
	for i, s := range p.Steps {
		if s.Frames < 1 || s.Frames > maxStepFrames {
			return nil, Errorf(KindInvalidParams, "steps[%d].frames は 1–%d とする（%d）", i, maxStepFrames, s.Frames)
		}
		total += s.Frames
		in, err := inputs(c.Instance, s.Input, s.Input2)
		if err != nil {
			return nil, err
		}
		ins[i] = in
	}
	if total > maxUntilFrames {
		return nil, Errorf(KindInvalidParams, "steps のフレーム数の合計は %d までとする（%d）", maxUntilFrames, total)
	}
	o, err := parseObserve(p.Observe)
	if err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	res := &SequenceResult{}
	var last emu.StepResult
	reason := StopSequenceDone
	for i, s := range p.Steps {
		r, err := c.Instance.Emu.StepWith(c.Ctx, emu.StepOptions{
			Kind: emu.StepFrame, Count: s.Frames, Input: ins[i], NoBreak: p.Break != nil && !*p.Break,
		})
		if err != nil {
			return nil, Errorf(KindInternalError, "%v", err)
		}
		last = r
		if r.Reason != emu.StopFramesDone {
			reason = stopName(r.Reason)
			if r.Reason == emu.StopCancelled {
				reason = cancelReason(c, nil)
			}
			break
		}
		res.CompletedSteps++
		if p.ObserveEach {
			ob, err := c.Host.observe(c.Instance, c.Conn, observeSpec{})
			if err != nil {
				return nil, err
			}
			ob.StopReason = StopFramesDone
			res.PerStep = append(res.PerStep, ob)
		}
	}
	ob, err := finish(c, o, last, reason, nil)
	if err != nil {
		return nil, err
	}
	res.Observation = ob
	return res, nil
}

type untilParams struct {
	InstanceParam
	Condition string          `json:"condition" desc:"条件式（A == $42 && [$0300] > 3 など）"`
	MaxFrames int             `json:"max_frames,omitempty" desc:"上限のフレーム数（1–216000）" default:"600"`
	Check     string          `json:"check,omitempty" desc:"判定の頻度（frame・instruction）" default:"\"frame\""`
	Input     string          `json:"input,omitempty" desc:"進める間押し続けるポート 1 の入力"`
	Input2    string          `json:"input2,omitempty" desc:"ポート 2 の入力"`
	TimeoutMS int             `json:"timeout_ms,omitempty" desc:"実時間の上限（ミリ秒）。Scenario では使わない"`
	Observe   json.RawMessage `json:"observe,omitempty" desc:"観測の指定"`
	Break     *bool           `json:"break,omitempty" desc:"ブレークポイントで止まるか" default:"true"`
}

// UntilDetail は stop_reason が condition のときの stop_detail。
type UntilDetail struct {
	Expr string `json:"expr"`
}

// conditionError は条件式の解析の誤りを invalid_expression にする。
func conditionError(err error) error {
	var pe *debug.ParseError
	if errors.As(err, &pe) {
		e := Errorf(KindInvalidExpression, "%s", pe.Msg)
		e.Position = pe.Pos
		return e
	}
	return Errorf(KindInvalidExpression, "%v", err)
}

func handleRunUntil(c *Context, raw json.RawMessage) (any, error) {
	var p untilParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.MaxFrames == 0 {
		p.MaxFrames = 600
	}
	if p.MaxFrames < 1 || p.MaxFrames > maxUntilFrames {
		return nil, Errorf(KindInvalidParams, "max_frames は 1–%d とする（%d）", maxUntilFrames, p.MaxFrames)
	}
	if p.Check == "" {
		p.Check = "frame"
	}
	if p.Check != "frame" && p.Check != "instruction" {
		return nil, Errorf(KindInvalidParams, "check は frame か instruction とする（%q）", p.Check)
	}
	cond, err := c.Instance.parseCondition(p.Condition)
	if err != nil {
		return nil, conditionError(err)
	}
	o, err := parseObserve(p.Observe)
	if err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	in, err := inputs(c.Instance, p.Input, p.Input2)
	if err != nil {
		return nil, err
	}
	ctx := c.Ctx
	var tctx context.Context
	if p.TimeoutMS > 0 {
		var cancel context.CancelFunc
		tctx, cancel = context.WithTimeout(c.Ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
		defer cancel()
		ctx = tctx
	}
	d := c.Instance.Emu.Debugger()
	r, err := c.Instance.Emu.StepWith(ctx, emu.StepOptions{
		Kind: emu.StepFrame, Count: p.MaxFrames, Input: in,
		Cond:                func(*nes.NES) bool { return cond.Eval(d) },
		CondEachInstruction: p.Check == "instruction",
		NoBreak:             p.Break != nil && !*p.Break,
	})
	if err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	var detail any
	reason := stopName(r.Reason)
	switch r.Reason {
	case emu.StopCondition:
		detail = UntilDetail{Expr: p.Condition}
	case emu.StopFramesDone:
		reason = StopMaxFrames
	case emu.StopCancelled:
		reason = cancelReason(c, tctx)
	}
	return finish(c, o, r, reason, detail)
}

type unitParams struct {
	InstanceParam
	Unit    string          `json:"unit,omitempty" desc:"単位（cycle・instruction・over・out・scanline）" default:"\"instruction\""`
	Count   int             `json:"count,omitempty" desc:"回数" default:"1"`
	To      string          `json:"to,omitempty" desc:"この位置に達するまで実行する（unit の代わり）"`
	Observe json.RawMessage `json:"observe,omitempty" desc:"観測の指定"`
}

var stepUnits = []struct {
	name string
	kind emu.StepKind
}{
	{"cycle", emu.StepCycle}, {"instruction", emu.StepInstruction}, {"over", emu.StepOver},
	{"out", emu.StepOut}, {"scanline", emu.StepScanline},
}

func handleStepUnit(c *Context, raw json.RawMessage) (any, error) {
	var p unitParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Count == 0 {
		p.Count = 1
	}
	if p.Count < 1 || p.Count > maxStepUnits {
		return nil, Errorf(KindInvalidParams, "count は 1–%d とする（%d）", maxStepUnits, p.Count)
	}
	o, err := parseObserve(p.Observe)
	if err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	opts := emu.StepOptions{Count: p.Count}
	if p.To != "" {
		loc, err := c.Instance.ResolveLocation(p.To)
		if err != nil {
			return nil, err
		}
		addr, err := loc.cpuAddr()
		if err != nil {
			return nil, err
		}
		opts.Kind, opts.Addr, opts.Count = emu.RunToCursor, addr, 1
	} else {
		if p.Unit == "" {
			p.Unit = "instruction"
		}
		found := false
		for _, u := range stepUnits {
			if u.name == p.Unit {
				opts.Kind, found = u.kind, true
			}
		}
		if !found {
			return nil, Errorf(KindInvalidParams, "unit %q を知らない（cycle・instruction・over・out・scanline）", p.Unit)
		}
	}
	// ステップオーバー・アウト・スキャンライン・位置までは回数を持たない。
	// 回数だけ繰り返す。
	var r emu.StepResult
	repeat := 1
	if opts.Kind != emu.StepCycle && opts.Kind != emu.StepInstruction && opts.Kind != emu.RunToCursor {
		repeat, opts.Count = opts.Count, 1
	}
	for range repeat {
		r, err = c.Instance.Emu.StepWith(c.Ctx, opts)
		if err != nil {
			return nil, Errorf(KindInternalError, "%v", err)
		}
		if r.Reason != emu.StopStepDone {
			break
		}
	}
	reason := stopName(r.Reason)
	if r.Reason == emu.StopCancelled {
		reason = cancelReason(c, nil)
	}
	return finish(c, o, r, reason, nil)
}

type resetParams struct {
	InstanceParam
	Hard    bool            `json:"hard,omitempty" desc:"電源を入れ直す"`
	Observe json.RawMessage `json:"observe,omitempty" desc:"観測の指定"`
}

func handleReset(c *Context, raw json.RawMessage) (any, error) {
	var p resetParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	o, err := parseObserve(p.Observe)
	if err != nil {
		return nil, err
	}
	if err := requireLoaded(c.Instance); err != nil {
		return nil, err
	}
	if c.Instance.Emu.Status().Movie.Playing {
		return nil, Errorf(KindMovieConflict, "ムービーの再生中はリセットできない")
	}
	if err := c.Instance.Emu.Reset(p.Hard); err != nil {
		return nil, Errorf(KindInternalError, "%v", err)
	}
	return c.Host.observe(c.Instance, c.Conn, o)
}

type cancelParams struct {
	ID json.RawMessage `json:"id,omitempty" desc:"取り消す要求の JSON-RPC の id。省くと、この接続が先に送った要求をすべて取り消す"`
}

// handleCancel は何もしない。取り消しは Transport が受け取った時点で行う
// （設計書 14 編 §14.2.4）。実行中の要求が無くても成功を返す。
func handleCancel(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &cancelParams{}); err != nil {
		return nil, err
	}
	return struct {
		OK bool `json:"ok"`
	}{true}, nil
}

// parseCondition は条件式を解析する。Symbol と Game State の名前を、命令境界で
// デバッガに確かめる（設計書 14 編 §14.11.4）。
func (inst *Instance) parseCondition(expr string) (*debug.Condition, error) {
	var cond *debug.Condition
	var err error
	if !inst.Emu.WithDebugger(func(d *debug.Debugger) { cond, err = debug.ParseConditionWith(expr, d) }) {
		return nil, Errorf(KindInternalError, "エミュレーションが停止している")
	}
	return cond, err
}

// symbols は Instance の Symbols を返す。
func (inst *Instance) symbols() *debug.Symbols {
	var s *debug.Symbols
	inst.Emu.WithDebugger(func(d *debug.Debugger) { s = d.Symbols() })
	return s
}
