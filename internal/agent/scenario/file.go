// Package scenario は Scenario（設計書 14 編 §14.17）の読み込みと実行を行う。
//
// Scenario は Agent Command の並びとアサーションからなる自動テストの単位で
// ある。ステップの Agent Command は JSON-RPC と同じ名前と引数を使い、
// 実行器はプロセス内の Host に net.Pipe でつないだ JSON-RPC のクライアント
// として実行する。
//
// YAML のライブラリ（github.com/goccy/go-yaml）を参照してよいのはこの
// パッケージだけである（internal/arch で検査する）。
package scenario

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
)

// アサーションの名前（設計書 14 編 §14.17.2）。
const (
	AssertExpr          = "assert"
	AssertStop          = "assert_stop"
	AssertScreen        = "assert_screen"
	AssertMem           = "assert_mem"
	AssertNoDiagnostics = "assert_no_diagnostics"
)

// assertions はアサーションの名前の一覧。
var assertions = []string{AssertExpr, AssertStop, AssertScreen, AssertMem, AssertNoDiagnostics}

// File は読み込んだ Scenario ファイル。
type File struct {
	// Path はファイルのパス（与えられたまま）。
	Path      string
	Scenarios []*Scenario
	// Warnings は実行できるが知らせること（timeout_ms など）。
	Warnings []string
}

// Scenario は Scenario 1 つ。パスはすべて解決済み（Scenario ファイルの
// ディレクトリからの相対パスを結合したもの）である。
type Scenario struct {
	Name string
	// Index はファイル内の番号（1 から）。
	Index int
	Line  int
	ROM   string
	// State は start: {state: …} のセーブステート。電源投入のとき空。
	State       string
	Init        Init
	Symbols     string
	GameState   string
	Diagnostics Diagnostics
	Steps       []Step
}

// Init は Instance の初期化の指定。
type Init struct {
	RAMInit       string `json:"ram_init,omitempty"`
	RAMSeed       uint64 `json:"ram_seed,omitempty"`
	Deterministic bool   `json:"deterministic"`
}

// Diagnostics は Diagnostic の指定。
type Diagnostics struct {
	// Enable は有効にする種類。All が true ならすべて。
	All    bool
	Enable []string
	FailOn []string
}

// Step はステップ 1 つ。
type Step struct {
	// Index は Scenario 内の番号（1 から）。Line はファイルの行番号。
	Index int
	Line  int
	// Name は Agent Command の名前かアサーションの名前。
	Name string
	// Params は Agent Command の引数（JSON の対応）。
	Params json.RawMessage
	// Text はステップを 1 行で表したもの（失敗の知らせに使う）。
	Text string

	// Expr は assert の式、Stop は assert_stop の Stop Reason。
	Expr string
	Stop string
	// Screen は assert_screen の指定。Golden は解決済みのパス。
	Screen *ScreenAssert
	// Mem は assert_mem の指定。
	Mem *MemAssert
	// Kinds は assert_no_diagnostics の種類。空ならすべて。
	Kinds []string
}

// IsAssertion はアサーションかを返す。
func (s Step) IsAssertion() bool { return slices.Contains(assertions, s.Name) }

// ScreenAssert は assert_screen の指定。
type ScreenAssert struct {
	Golden        string `json:"golden"`
	MaxDiffPixels int    `json:"max_diff_pixels,omitempty"`
}

// MemAssert は assert_mem の指定。
type MemAssert struct {
	Loc    string  `json:"loc"`
	Equals []uint8 `json:"equals"`
}

// LoadError は Scenario ファイルの誤り。shogun run は終了コード 6 にする。
type LoadError struct {
	Path string
	// Line は行番号。分からないとき 0。Step はステップの番号。無いとき 0。
	Line int
	Step int
	Msg  string
}

func (e *LoadError) Error() string {
	var b strings.Builder
	b.WriteString(e.Path)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	b.WriteString(": ")
	if e.Step > 0 {
		fmt.Fprintf(&b, "ステップ %d: ", e.Step)
	}
	b.WriteString(e.Msg)
	return b.String()
}

// loader は 1 ファイルの読み込みの状態。
type loader struct {
	path     string
	dir      string
	registry *agent.Registry
	file     *File
}

// Load は Scenario ファイルを読み込み、ステップを登録簿で検証する。
// 誤りは *LoadError で返す。
func Load(path string, registry *agent.Registry) (*File, error) {
	if registry == nil {
		registry = agent.NewDefaultRegistry()
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
	default:
		return nil, &LoadError{Path: path, Msg: "拡張子は .yaml・.yml・.json とする"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &LoadError{Path: path, Msg: err.Error()}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, &LoadError{Path: path, Msg: err.Error()}
	}
	l := &loader{path: path, dir: filepath.Dir(abs), registry: registry, file: &File{Path: path}}
	// JSON は YAML として読める。同じ解析器で読み、行番号を得る。
	f, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, &LoadError{Path: path, Line: errorLine(err), Msg: yaml.FormatError(err, false, false)}
	}
	if len(f.Docs) != 1 || f.Docs[0].Body == nil {
		return nil, &LoadError{Path: path, Msg: "文書を 1 つだけ書く"}
	}
	body := unwrap(f.Docs[0].Body)
	switch n := body.(type) {
	case *ast.MappingNode, *ast.MappingValueNode:
		sc, err := l.scenario(n, 1, filepath.Base(path))
		if err != nil {
			return nil, err
		}
		l.file.Scenarios = append(l.file.Scenarios, sc)
	case *ast.SequenceNode:
		if len(n.Values) == 0 {
			return nil, l.errAt(n, 0, "Scenario が無い")
		}
		for i, v := range n.Values {
			sc, err := l.scenario(unwrap(v), i+1, fmt.Sprintf("%s#%d", filepath.Base(path), i+1))
			if err != nil {
				return nil, err
			}
			l.file.Scenarios = append(l.file.Scenarios, sc)
		}
	default:
		return nil, l.errAt(body, 0, "最上位は Scenario の対応か、Scenario の列とする")
	}
	return l.file, nil
}

// errorLine は構文の誤りの行番号を返す。
func errorLine(err error) int {
	msg := yaml.FormatError(err, false, false)
	// 形式は "[行:桁] 内容"。
	var line, col int
	if _, e := fmt.Sscanf(msg, "[%d:%d]", &line, &col); e == nil {
		return line
	}
	return 0
}

// unwrap はアンカーとタグを外す。
func unwrap(n ast.Node) ast.Node {
	for {
		switch v := n.(type) {
		case *ast.AnchorNode:
			n = v.Value
		case *ast.TagNode:
			n = v.Value
		default:
			return n
		}
	}
}

func lineOf(n ast.Node) int {
	if n == nil {
		return 0
	}
	if t := n.GetToken(); t != nil && t.Position != nil {
		return t.Position.Line
	}
	return 0
}

func (l *loader) errAt(n ast.Node, step int, format string, args ...any) *LoadError {
	return &LoadError{Path: l.path, Line: lineOf(n), Step: step, Msg: fmt.Sprintf(format, args...)}
}

// entries は対応の要素を返す。
func entries(n ast.Node) ([]*ast.MappingValueNode, bool) {
	switch v := unwrap(n).(type) {
	case *ast.MappingNode:
		return v.Values, true
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}, true
	}
	return nil, false
}

// keyOf は対応のキーの文字列を返す。
func keyOf(mv *ast.MappingValueNode) string {
	var s string
	if err := yaml.NodeToValue(mv.Key, &s); err != nil {
		return strings.TrimSpace(mv.Key.String())
	}
	return s
}

// isNull は値が省略（空か null）かを返す。
func isNull(n ast.Node) bool {
	if n == nil {
		return true
	}
	_, ok := unwrap(n).(*ast.NullNode)
	return ok
}

// decode は節を v に読む。知らない項目を誤りとする。
func (l *loader) decode(n ast.Node, step int, what string, v any) error {
	if err := yaml.NodeToValue(n, v, yaml.DisallowUnknownField()); err != nil {
		return l.errAt(n, step, "%s を読めない: %s", what, firstLine(yaml.FormatError(err, false, false)))
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// resolve は Scenario ファイルのディレクトリからの相対パスを解決する。
func (l *loader) resolve(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(l.dir, filepath.FromSlash(p))
}

// scenario は Scenario 1 つを読む。
func (l *loader) scenario(n ast.Node, index int, defaultName string) (*Scenario, error) {
	items, ok := entries(n)
	if !ok {
		return nil, l.errAt(n, 0, "Scenario は対応で書く")
	}
	sc := &Scenario{Index: index, Line: lineOf(n), Name: defaultName, Init: Init{Deterministic: true}}
	var steps ast.Node
	seen := map[string]bool{}
	for _, mv := range items {
		key := keyOf(mv)
		if seen[key] {
			return nil, l.errAt(mv, 0, "%s を 2 回書いている", key)
		}
		seen[key] = true
		val := mv.Value
		switch key {
		case "name":
			if err := l.decode(val, 0, "name", &sc.Name); err != nil {
				return nil, err
			}
		case "rom":
			var s string
			if err := l.decode(val, 0, "rom", &s); err != nil {
				return nil, err
			}
			sc.ROM = l.resolve(s)
		case "start":
			if err := l.start(sc, val); err != nil {
				return nil, err
			}
		case "init":
			if err := l.decode(val, 0, "init", &sc.Init); err != nil {
				return nil, err
			}
			if v := sc.Init.RAMInit; v != "" && !slices.Contains([]string{"zero", "ff", "pattern", "random"}, v) {
				return nil, l.errAt(val, 0, "init.ram_init %q は使えない（zero・ff・pattern・random）", v)
			}
		case "symbols":
			var s string
			if err := l.decode(val, 0, "symbols", &s); err != nil {
				return nil, err
			}
			sc.Symbols = l.resolve(s)
		case "gamestate":
			var s string
			if err := l.decode(val, 0, "gamestate", &s); err != nil {
				return nil, err
			}
			sc.GameState = l.resolve(s)
		case "diagnostics":
			if err := l.diagnostics(sc, val); err != nil {
				return nil, err
			}
		case "steps":
			steps = val
		default:
			return nil, l.errAt(mv, 0, "知らない項目 %q（name・rom・start・init・symbols・gamestate・diagnostics・steps）", key)
		}
	}
	if sc.ROM == "" {
		return nil, l.errAt(n, 0, "rom を指定する")
	}
	if sc.Init.Deterministic && sc.Init.RAMInit == "random" && sc.Init.RAMSeed == 0 {
		// シードを省くと実行のたびに変わる。決定論的な Scenario では固定する。
		sc.Init.RAMSeed = 1
	}
	if steps == nil || isNull(steps) {
		return nil, l.errAt(n, 0, "steps を指定する")
	}
	seq, ok := unwrap(steps).(*ast.SequenceNode)
	if !ok {
		return nil, l.errAt(steps, 0, "steps は列で書く")
	}
	for i, v := range seq.Values {
		st, err := l.step(v, i+1)
		if err != nil {
			return nil, err
		}
		sc.Steps = append(sc.Steps, st)
	}
	return sc, nil
}

func (l *loader) start(sc *Scenario, val ast.Node) error {
	var s string
	if err := yaml.NodeToValue(val, &s); err == nil {
		if s != "power-on" {
			return l.errAt(val, 0, "start は power-on か {state: <パス>} とする（%q）", s)
		}
		return nil
	}
	var st struct {
		State string `json:"state"`
	}
	if err := l.decode(val, 0, "start", &st); err != nil {
		return err
	}
	if st.State == "" {
		return l.errAt(val, 0, "start.state にセーブステートのパスを書く")
	}
	sc.State = l.resolve(st.State)
	return nil
}

func (l *loader) diagnostics(sc *Scenario, val ast.Node) error {
	items, ok := entries(val)
	if !ok {
		return l.errAt(val, 0, "diagnostics は {enable, fail_on} の対応で書く")
	}
	for _, mv := range items {
		switch keyOf(mv) {
		case "enable":
			var all string
			if err := yaml.NodeToValue(mv.Value, &all); err == nil {
				if all != "all" {
					return l.errAt(mv.Value, 0, "diagnostics.enable は all か種類の列とする")
				}
				sc.Diagnostics.All = true
				continue
			}
			if err := l.decode(mv.Value, 0, "diagnostics.enable", &sc.Diagnostics.Enable); err != nil {
				return err
			}
		case "fail_on":
			if err := l.decode(mv.Value, 0, "diagnostics.fail_on", &sc.Diagnostics.FailOn); err != nil {
				return err
			}
		default:
			return l.errAt(mv, 0, "diagnostics の知らない項目 %q（enable・fail_on）", keyOf(mv))
		}
	}
	return nil
}

// step はステップ 1 つを読み、検証する。
func (l *loader) step(n ast.Node, index int) (Step, error) {
	items, ok := entries(n)
	if !ok || len(items) != 1 {
		return Step{}, l.errAt(n, index, "ステップはキーを 1 つだけ持つ対応（exec.step: {frames: 60} など）で書く")
	}
	mv := items[0]
	st := Step{Index: index, Line: lineOf(n), Name: keyOf(mv)}
	if l := lineOf(mv.Key); l > 0 {
		st.Line = l
	}
	val := mv.Value
	var err error
	switch st.Name {
	case AssertExpr:
		err = l.decode(val, index, "assert の式", &st.Expr)
		if err == nil && strings.TrimSpace(st.Expr) == "" {
			err = l.errAt(val, index, "assert に式を書く")
		}
		st.Text = fmt.Sprintf("assert: %s", st.Expr)
	case AssertStop:
		err = l.decode(val, index, "assert_stop の Stop Reason", &st.Stop)
		if err == nil && !slices.Contains(stopReasons, st.Stop) {
			err = l.errAt(val, index, "assert_stop %q は Stop Reason ではない（%s）", st.Stop, strings.Join(stopReasons, "・"))
		}
		st.Text = fmt.Sprintf("assert_stop: %s", st.Stop)
	case AssertScreen:
		var sa ScreenAssert
		err = l.decode(val, index, "assert_screen", &sa)
		if err == nil && sa.Golden == "" {
			err = l.errAt(val, index, "assert_screen に golden（お手本の PNG）を書く")
		}
		if err == nil && sa.MaxDiffPixels < 0 {
			err = l.errAt(val, index, "max_diff_pixels は 0 以上とする")
		}
		st.Text = fmt.Sprintf("assert_screen: %s", sa.Golden)
		sa.Golden = l.resolve(sa.Golden)
		st.Screen = &sa
	case AssertMem:
		var ma MemAssert
		err = l.decode(val, index, "assert_mem", &ma)
		if err == nil && (ma.Loc == "" || len(ma.Equals) == 0) {
			err = l.errAt(val, index, "assert_mem に loc と equals（バイトの列）を書く")
		}
		if err == nil && len(ma.Equals) > 4096 {
			err = l.errAt(val, index, "equals は 4096 バイトまでとする")
		}
		st.Text = fmt.Sprintf("assert_mem: %s == %v", ma.Loc, ma.Equals)
		st.Mem = &ma
	case AssertNoDiagnostics:
		if !isNull(val) {
			err = l.decode(val, index, "assert_no_diagnostics の種類", &st.Kinds)
		}
		st.Text = "assert_no_diagnostics"
		if len(st.Kinds) > 0 {
			st.Text += ": " + strings.Join(st.Kinds, ", ")
		}
	default:
		err = l.command(&st, val)
	}
	if err != nil {
		return Step{}, err
	}
	return st, nil
}

// stopReasons は assert_stop に書ける Stop Reason（設計書 14 編 §14.8.4）。
var stopReasons = []string{agent.StopFramesDone, agent.StopSequenceDone, agent.StopCondition, agent.StopMaxFrames,
	agent.StopBreakpoint, agent.StopDiagnostic, agent.StopStepDone, agent.StopCancelled, agent.StopTimeout,
	agent.StopControlLost, agent.StopCPUHalted}

// command は Agent Command のステップを登録簿で検証する。
func (l *loader) command(st *Step, val ast.Node) error {
	spec, ok := l.registry.Lookup(st.Name)
	if !ok {
		return l.errAt(val, st.Index, "%q は Agent Command でもアサーションでもない", st.Name)
	}
	if spec.Class == agent.ClassSession {
		return l.errAt(val, st.Index, "%s は Scenario では使えない（Instance の作成と Control は実行器が行う）", st.Name)
	}
	if !spec.Headless {
		return l.errAt(val, st.Index, "%s は headless では使えない", st.Name)
	}
	params := json.RawMessage("{}")
	if !isNull(val) {
		var v any
		if err := yaml.NodeToValue(val, &v); err != nil {
			return l.errAt(val, st.Index, "%s の引数を読めない: %s", st.Name, firstLine(yaml.FormatError(err, false, false)))
		}
		m, ok := v.(map[string]any)
		if !ok {
			return l.errAt(val, st.Index, "%s の引数は対応（{frames: 60} など）で書く", st.Name)
		}
		if _, has := m["instance"]; has {
			return l.errAt(val, st.Index, "引数に instance を書けない（Scenario は Instance を 1 つだけ使う）")
		}
		if _, has := m["timeout_ms"]; has {
			l.file.Warnings = append(l.file.Warnings, fmt.Sprintf("%s:%d: ステップ %d: timeout_ms は実行環境の速さで止まる位置が変わる。max_frames を使う",
				l.path, st.Line, st.Index))
		}
		data, err := json.Marshal(m)
		if err != nil {
			return l.errAt(val, st.Index, "%s の引数を変換できない: %v", st.Name, err)
		}
		params = data
	}
	if spec.Params != nil {
		target := reflect.New(reflect.TypeOf(spec.Params)).Interface()
		if err := agent.DecodeParams(params, target); err != nil {
			msg := err.Error()
			var ae *agent.Error
			if errors.As(err, &ae) {
				msg = ae.Message
			}
			return l.errAt(val, st.Index, "%s: %s", st.Name, msg)
		}
	} else if !bytes.Equal(params, []byte("{}")) {
		return l.errAt(val, st.Index, "%s は引数を取らない", st.Name)
	}
	st.Params = params
	st.Text = fmt.Sprintf("%s: %s", st.Name, params)
	return nil
}
