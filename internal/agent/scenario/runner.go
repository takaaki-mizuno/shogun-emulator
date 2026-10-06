package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
)

// Options は実行の指定。
type Options struct {
	// UpdateGolden は、お手本の PNG が無いか一致しないとき現在の画面で
	// 上書きする（shogun run --update-golden）。
	UpdateGolden bool
	// ReproDir は失敗した Scenario の Repro の書き出し先。空なら repro.export の
	// 既定の場所。
	ReproDir string
}

// Status は Scenario の結果。
type Status int

const (
	// Passed は合格。
	Passed Status = iota
	// Failed はアサーションの失敗（ステップの誤りと fail_on を含む）。
	Failed
	// SetupError は ROM・セーブステート・Symbol・Game State Definition の
	// 読み込みの失敗。
	SetupError
)

// Failure は失敗の内容。
type Failure struct {
	// Step はステップの番号（1 から）。開始の前の失敗では 0。
	Step int
	Line int
	// Text はステップを 1 行で表したもの。
	Text    string
	Message string
	// Expr は assert の式、Actual は実際の値。
	Expr   string
	Actual string
	// Summary は失敗した時点の Observation の要約。
	Summary string
}

// CaseResult は Scenario 1 つの結果。
type CaseResult struct {
	Name     string
	Status   Status
	Duration time.Duration
	Failure  *Failure
	// Repro は失敗したとき書き出した Repro のディレクトリ。ReproErr は
	// 書き出せなかった理由。
	Repro    string
	ReproErr string
	// Updated は --update-golden で書いたお手本。Actual は書いた実際の画面。
	Updated []string
	Actual  []string
	// Frame は終わった時点のフレーム番号。Notes は実行器からの知らせ。
	Frame uint64
	Notes []string
	// Final は終わった時点の Game State（Scenario の決定論の確認に使う）。
	Final map[string]any
}

// FileResult は Scenario ファイル 1 つの結果。
type FileResult struct {
	Path string
	// Err はファイルの誤り。誤りがあれば Scenario を実行しない。
	Err      *LoadError
	Warnings []string
	Cases    []CaseResult
	Duration time.Duration
}

// Runner は Scenario を実行する。
type Runner struct {
	client   *rpc.Client
	registry *agent.Registry
	opts     Options
}

// NewRunner は実行器を作る。client はプロセス内の Host へつないだ JSON-RPC の
// クライアント（rpc.InProcess）とする（設計書 14 編 §14.2.1）。
func NewRunner(client *rpc.Client, opts Options) *Runner {
	return &Runner{client: client, registry: agent.NewDefaultRegistry(), opts: opts}
}

// RunFile は Scenario ファイルを読み込んで実行する。
func (r *Runner) RunFile(ctx context.Context, path string) FileResult {
	start := time.Now()
	res := FileResult{Path: path}
	f, err := Load(path, r.registry)
	if err != nil {
		le, ok := err.(*LoadError)
		if !ok {
			le = &LoadError{Path: path, Msg: err.Error()}
		}
		res.Err = le
		res.Duration = time.Since(start)
		return res
	}
	res.Warnings = f.Warnings
	for i, sc := range f.Scenarios {
		res.Cases = append(res.Cases, r.Run(ctx, f, i, sc))
	}
	res.Duration = time.Since(start)
	return res
}

// run は Scenario 1 つの実行の状態。
type run struct {
	r    *Runner
	ctx  context.Context
	file *File
	sc   *Scenario
	inst string
	res  *CaseResult

	// lastStop は直前の進行の Stop Reason。進行が無いとき空。
	lastStop string
	// evSeq は受け取った最後のイベントの seq。diags は集めた Diagnostic。
	evSeq uint64
	diags []diagnostic
}

// diagnostic は集めた Diagnostic 1 件。
type diagnostic struct {
	kind  string
	frame uint64
	step  int
}

// Run は Scenario 1 つを実行する。index はファイル内の位置（0 から）で、
// Repro の名前に使う。
func (r *Runner) Run(ctx context.Context, f *File, index int, sc *Scenario) CaseResult {
	start := time.Now()
	res := CaseResult{Name: sc.Name}
	x := &run{r: r, ctx: ctx, file: f, sc: sc, res: &res}
	x.execute(index)
	res.Duration = time.Since(start)
	return res
}

func (x *run) call(method string, params map[string]any, out any) error {
	if params == nil {
		params = map[string]any{}
	}
	if x.inst != "" {
		params["instance"] = x.inst
	}
	return x.r.client.Call(x.ctx, method, params, out)
}

// callErr は Agent Command の誤りを説明にする。
func callErr(method string, err error) string {
	if kind, msg, ok := rpc.ErrorKind(err); ok {
		return fmt.Sprintf("%s が誤りを返した（%s）: %s", method, kind, msg)
	}
	return fmt.Sprintf("%s が誤りを返した: %v", method, err)
}

func (x *run) setupFail(msg string) {
	x.res.Status = SetupError
	x.res.Failure = &Failure{Message: msg}
}

func (x *run) execute(index int) {
	sc := x.sc
	create := map[string]any{"rom": sc.ROM, "deterministic": sc.Init.Deterministic}
	if sc.State != "" {
		create["state"] = sc.State
	}
	if sc.Init.RAMInit != "" {
		create["ram_init"] = sc.Init.RAMInit
	}
	if sc.Init.RAMSeed != 0 {
		create["ram_seed"] = sc.Init.RAMSeed
	}
	var info agent.InstanceInfo
	if err := x.call("instance.create", create, &info); err != nil {
		x.setupFail(callErr("instance.create", err))
		return
	}
	x.inst = string(info.ID)
	defer func() {
		_ = x.call("instance.close", nil, nil)
	}()
	if sc.Symbols != "" {
		if err := x.call("symbol.load", map[string]any{"path": sc.Symbols}, nil); err != nil {
			x.setupFail(callErr("symbol.load", err))
			return
		}
	}
	if sc.GameState != "" {
		if err := x.call("gamestate.load", map[string]any{"path": sc.GameState}, nil); err != nil {
			x.setupFail(callErr("gamestate.load", err))
			return
		}
	}
	if sc.Diagnostics.All || len(sc.Diagnostics.Enable) > 0 {
		if _, ok := x.r.registry.Lookup("diag.configure"); ok {
			enable := any(sc.Diagnostics.Enable)
			if sc.Diagnostics.All {
				enable = "all"
			}
			if err := x.call("diag.configure", map[string]any{"enable": enable}, nil); err != nil {
				x.setupFail(callErr("diag.configure", err))
				return
			}
		} else {
			x.res.Notes = append(x.res.Notes, "この版には diag.configure が無いため、diagnostics.enable は読むだけとした")
		}
	}
	// 作成までに積まれたイベントを読み飛ばす。
	var poll agent.PollResult
	if err := x.call("events.poll", map[string]any{"max": 1000}, &poll); err == nil {
		x.evSeq = poll.LastSeq
	}

	for _, st := range sc.Steps {
		if err := x.ctx.Err(); err != nil {
			x.fail(st, Failure{Message: "中断した"})
			break
		}
		failure := x.step(st)
		if failure == nil {
			failure = x.collectDiagnostics(st)
		}
		if failure != nil {
			x.fail(st, *failure)
			break
		}
	}
	x.finish(index)
}

// fail は失敗を記録し、その時点の Observation の要約を添える。
func (x *run) fail(st Step, f Failure) {
	f.Step, f.Line, f.Text = st.Index, st.Line, st.Text
	if f.Summary == "" {
		f.Summary = x.summary()
	}
	x.res.Status = Failed
	x.res.Failure = &f
}

// finish は終わった時点の値を取り、失敗していれば Repro を書き出す。
func (x *run) finish(index int) {
	var ob agent.Observation
	if err := x.call("obs.get", map[string]any{"include": []string{"gamestate"}}, &ob); err == nil {
		x.res.Frame, x.res.Final = ob.Frame, ob.GameState
	}
	if x.res.Status != Failed {
		return
	}
	params := map[string]any{"note": x.reproNote()}
	if x.r.opts.ReproDir != "" {
		base := strings.TrimSuffix(filepath.Base(x.file.Path), filepath.Ext(x.file.Path))
		params["path"] = filepath.Join(x.r.opts.ReproDir, fmt.Sprintf("%s-%d.repro", base, index+1))
	}
	var rr agent.ReproResult
	if err := x.call("repro.export", params, &rr); err != nil {
		x.res.ReproErr = callErr("repro.export", err)
		return
	}
	x.res.Repro = rr.Path
}

func (x *run) reproNote() string {
	f := x.res.Failure
	note := fmt.Sprintf("Scenario %q（%s）がステップ %d で失敗した。\n\n- ステップ: %s\n- 内容: %s", x.sc.Name, x.file.Path, f.Step, f.Text, f.Message)
	if f.Actual != "" {
		note += "\n- 実際の値: " + f.Actual
	}
	return note
}

// summary は今の Observation の要約を 1 行で返す。
func (x *run) summary() string {
	var ob agent.Observation
	if err := x.call("obs.get", map[string]any{"include": []string{"gamestate"}}, &ob); err != nil {
		return ""
	}
	parts := []string{fmt.Sprintf("frame %d", ob.Frame)}
	if x.lastStop != "" {
		parts = append(parts, "直前の Stop Reason "+x.lastStop)
	}
	if ob.CPU != nil {
		pc := "PC " + ob.CPU.PC
		if ob.CPU.Symbol != "" {
			pc += "（" + ob.CPU.Symbol + "）"
		}
		parts = append(parts, pc)
	}
	if len(ob.GameState) > 0 {
		data, _ := json.Marshal(ob.GameState)
		parts = append(parts, "game "+string(data))
	}
	return strings.Join(parts, "、")
}

// step はステップ 1 つを実行する。失敗したとき内容を返す。
func (x *run) step(st Step) *Failure {
	switch st.Name {
	case AssertExpr:
		return x.assertExpr(st)
	case AssertStop:
		if x.lastStop == "" {
			return &Failure{Message: "直前に進行が無い（assert_stop は exec.step などの後に書く）"}
		}
		if x.lastStop != st.Stop {
			return &Failure{Message: fmt.Sprintf("Stop Reason が %s ではない", st.Stop), Actual: x.lastStop}
		}
		return nil
	case AssertScreen:
		return x.assertScreen(st)
	case AssertMem:
		return x.assertMem(st)
	case AssertNoDiagnostics:
		var found []string
		for _, d := range x.diags {
			if len(st.Kinds) == 0 || slices.Contains(st.Kinds, d.kind) {
				found = append(found, fmt.Sprintf("%s（frame %d、ステップ %d）", d.kind, d.frame, d.step))
			}
		}
		if len(found) > 0 {
			return &Failure{Message: fmt.Sprintf("Diagnostic が %d 件ある", len(found)), Actual: strings.Join(found, "、")}
		}
		return nil
	}
	return x.command(st)
}

// command は Agent Command のステップを実行する。
func (x *run) command(st Step) *Failure {
	var params map[string]any
	if err := json.Unmarshal(st.Params, &params); err != nil {
		return &Failure{Message: err.Error()}
	}
	raw, err := x.callRaw(st.Name, params)
	if err != nil {
		return &Failure{Message: callErr(st.Name, err)}
	}
	if spec, ok := x.r.registry.Lookup(st.Name); ok && spec.Class == agent.ClassAdvance {
		var ob struct {
			StopReason string `json:"stop_reason"`
		}
		_ = json.Unmarshal(raw, &ob)
		x.lastStop = ob.StopReason
	}
	return nil
}

func (x *run) callRaw(method string, params map[string]any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	params["instance"] = x.inst
	return x.r.client.CallRaw(x.ctx, method, params)
}

// collectDiagnostics はステップの後に Diagnostic のイベントを集め、fail_on の
// 種類があれば失敗を返す。
func (x *run) collectDiagnostics(st Step) *Failure {
	for {
		var poll agent.PollResult
		if err := x.call("events.poll", map[string]any{"since": x.evSeq, "max": 1000, "kinds": []string{agent.EventDiagnostic}}, &poll); err != nil {
			return nil
		}
		for _, ev := range poll.Events {
			x.diags = append(x.diags, diagnostic{kind: diagnosticKind(ev.Data), frame: ev.Frame, step: st.Index})
		}
		if poll.LastSeq > x.evSeq {
			x.evSeq = poll.LastSeq
		}
		if len(poll.Events) < 1000 {
			break
		}
	}
	for _, d := range x.diags {
		if d.step == st.Index && slices.Contains(x.sc.Diagnostics.FailOn, d.kind) {
			return &Failure{Message: fmt.Sprintf("diagnostics.fail_on の Diagnostic %s を検知した（frame %d）", d.kind, d.frame)}
		}
	}
	return nil
}

// diagnosticKind は diagnostic イベントの種類を取り出す。
func diagnosticKind(data any) string {
	if m, ok := data.(map[string]any); ok {
		if k, ok := m["kind"].(string); ok {
			return k
		}
	}
	return "unknown"
}

// assertExpr は assert を評価する。
func (x *run) assertExpr(st Step) *Failure {
	var ev agent.EvalResult
	if err := x.call("expr.eval", map[string]any{"expr": st.Expr}, &ev); err != nil {
		return &Failure{Message: callErr("expr.eval", err), Expr: st.Expr}
	}
	if ev.Truth {
		return nil
	}
	return &Failure{Message: "式が偽である", Expr: st.Expr, Actual: x.explain(st.Expr)}
}

// explain は式の比較の両辺を評価し、実際の値を説明する。
func (x *run) explain(expr string) string {
	var parts []string
	for _, side := range operands(expr) {
		var ev agent.EvalResult
		if err := x.call("expr.eval", map[string]any{"expr": side}, &ev); err != nil {
			continue
		}
		data, _ := json.Marshal(ev.Value)
		parts = append(parts, fmt.Sprintf("%s = %s", side, data))
	}
	if len(parts) == 0 {
		var ev agent.EvalResult
		if err := x.call("expr.eval", map[string]any{"expr": expr}, &ev); err == nil {
			data, _ := json.Marshal(ev.Value)
			return fmt.Sprintf("%s = %s", expr, data)
		}
	}
	return strings.Join(parts, "、")
}

// assertMem は assert_mem を評価する。
func (x *run) assertMem(st Step) *Failure {
	raw, err := x.callRaw("mem.read", map[string]any{"loc": st.Mem.Loc, "length": len(st.Mem.Equals)})
	if err != nil {
		return &Failure{Message: callErr("mem.read", err)}
	}
	var got struct {
		Loc   string `json:"loc"`
		Bytes []int  `json:"bytes"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return &Failure{Message: err.Error()}
	}
	for i, want := range st.Mem.Equals {
		if i >= len(got.Bytes) || got.Bytes[i] != int(want) {
			return &Failure{Message: fmt.Sprintf("%s の %d バイト目が一致しない", got.Loc, i),
				Expr: fmt.Sprintf("%s == %s", st.Mem.Loc, hexBytes(intsToBytes(st.Mem.Equals))), Actual: hexBytes(got.Bytes)}
		}
	}
	return nil
}

func intsToBytes(b []uint8) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

func hexBytes(b []int) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("$%02X", v)
	}
	return "[" + strings.Join(parts, " ") + "]"
}
