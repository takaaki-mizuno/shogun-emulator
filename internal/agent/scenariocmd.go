package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
)

// scenario.mark と scenario.export（設計書 14 編 §14.17.4）。
//
// Agent Command の記録（devloop.go）から進行と書き換えを取り出し、Scenario の
// ファイルとして書き出す。YAML のライブラリは internal/agent/scenario だけが
// 参照するため、ここでは値を JSON の流れ形式で書く（JSON は YAML として
// 読める）。

// exportAnchor は書き出しの開始。
type exportAnchor struct {
	// seq はこの開始の直前の Agent Command の通し番号。これより後のものを書く。
	seq uint64
	// state は開始のセーブステート。nil のとき電源投入から始める。
	state []byte
	// romPath は開始の時点の ROM のパス。
	romPath string
	// err はセーブステートを取り出せなかった理由。
	err string
	// breakpoints と freezes は開始の時点の数。
	breakpoints, freezes int
}

// anchorCommands は記録の途中で開始を作り直す Agent Command。
var anchorCommands = []string{"state.load", "rom.load", "rom.reload"}

// exportedConfig は ClassConfig だが書き出す Agent Command。ブレークポイントは
// 進行の止まる位置を変える。
var exportedConfig = []string{"debug.bp.add", "debug.bp.remove", "debug.bp.enable"}

// newAnchor は今の時点の開始を作る。snapshot が true ならセーブステートを
// 取り出す。inst.mu を持たずに呼ぶ。
func (inst *Instance) newAnchor(romPath string, snapshot bool) exportAnchor {
	a := exportAnchor{romPath: romPath}
	if snapshot {
		st, err := inst.Emu.SaveState()
		if err != nil {
			a.err = err.Error()
		} else {
			a.state = st
		}
	}
	inst.Emu.WithDebugger(func(d *debug.Debugger) {
		a.breakpoints, a.freezes = len(d.Breakpoints()), len(d.Freezes())
	})
	inst.mu.Lock()
	a.seq = inst.cmdSeq
	inst.mu.Unlock()
	return a
}

// setExportStart は from: start の開始を置き換える。
func (inst *Instance) setExportStart(a exportAnchor) {
	inst.mu.Lock()
	inst.exportStart = a
	inst.mu.Unlock()
}

// noteExportStart は記録の途中で開始を作り直す Agent Command が成功したとき、
// その直後を新しい開始とする。
func (inst *Instance) noteExportStart(method string, params json.RawMessage) {
	if !slices.Contains(anchorCommands, method) {
		return
	}
	inst.mu.Lock()
	rom := inst.romPath
	inst.mu.Unlock()
	snapshot := true
	switch method {
	case "rom.load":
		snapshot = false
	case "rom.reload":
		var p reloadParams
		_ = json.Unmarshal(params, &p)
		snapshot = p.ReReach != "" && p.ReReach != "none"
	}
	inst.setExportStart(inst.newAnchor(rom, snapshot))
}

func registerScenario(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			Headless: true, Target: true, Handler: h})
	}
	reg("scenario.mark", ClassConfig, InstanceParam{}, nil, "scenario.export の書き出しを始める位置に印を付ける（最新の 1 つ）",
		"Mark the current point (keeps a save state) so scenario_export with from: 'mark' starts here.", handleScenarioMark)
	reg("scenario.export", ClassObserve, scenarioExportParams{}, []string{"path"}, "この Instance で実行した進行と書き換えを Scenario として書き出す",
		"Export the advance/mutate commands run on this instance as a Scenario file (YAML, or JSON for .json) that `shogun run` can replay. Add assertions afterwards.", handleScenarioExport)
}

func handleScenarioMark(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	inst := c.Instance
	if err := requireLoaded(inst); err != nil {
		return nil, err
	}
	inst.mu.Lock()
	rom := inst.romPath
	inst.mu.Unlock()
	a := inst.newAnchor(rom, true)
	if a.err != "" {
		return nil, Errorf(KindInternalError, "状態を取り出せない: %s", a.err)
	}
	inst.mu.Lock()
	inst.mark = &a
	inst.mu.Unlock()
	return struct {
		Frame uint64 `json:"frame"`
	}{inst.Emu.Status().Frames}, nil
}

type scenarioExportParams struct {
	InstanceParam
	Path string `json:"path" desc:"書き出し先（.yaml・.yml・.json）"`
	From string `json:"from,omitempty" desc:"start（Instance の開始）・mark（scenario.mark の印）" default:"\"start\""`
	Name string `json:"name,omitempty" desc:"Scenario の name。省くとファイル名"`
}

// ScenarioExportResult は scenario.export の結果。
type ScenarioExportResult struct {
	Path  string `json:"path"`
	Steps int    `json:"steps"`
	// Start は power-on か、書いたセーブステートのパス。
	Start string   `json:"start"`
	Notes []string `json:"notes,omitempty"`
}

// exportStep は書き出すステップ 1 つ。
type exportStep struct {
	method string
	params json.RawMessage
}

func handleScenarioExport(c *Context, raw json.RawMessage) (any, error) {
	var p scenarioExportParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Path == "" {
		return nil, Errorf(KindInvalidParams, "path を指定する")
	}
	if p.From == "" {
		p.From = "start"
	}
	inst := c.Instance
	inst.mu.Lock()
	log := slices.Clone(inst.cmdLog)
	seq := inst.cmdSeq
	var anchor exportAnchor
	switch p.From {
	case "start":
		anchor = inst.exportStart
	case "mark":
		if inst.mark == nil {
			inst.mu.Unlock()
			return nil, Errorf(KindInvalidParams, "印が無い（scenario.mark で付ける）")
		}
		anchor = *inst.mark
	default:
		inst.mu.Unlock()
		return nil, Errorf(KindInvalidParams, "from は start・mark とする（%q）", p.From)
	}
	opts := inst.opts
	inst.mu.Unlock()
	if anchor.err != "" {
		return nil, Errorf(KindInternalError, "開始の状態を取り出せなかった: %s", anchor.err)
	}
	if anchor.romPath == "" {
		return nil, Errorf(KindNotLoaded, "ROM のパスが分からない")
	}
	// 記録があふれて開始の直後を失っていないかを確かめる。
	if (len(log) > 0 && log[0].Seq > anchor.seq+1) || (len(log) == 0 && seq > anchor.seq) {
		return nil, Errorf(KindLimitExceeded, "Agent Command の記録（最新 %d 件）があふれ、開始からのステップを失っている。scenario.mark で印を付けて from: mark で書き出す", maxCommandLog)
	}

	var steps []exportStep
	var notes []string
	if anchor.breakpoints > 0 || anchor.freezes > 0 {
		notes = append(notes, fmt.Sprintf("開始の時点でブレークポイントが %d 個、Freeze が %d 個あった。これらは Scenario に含まれない", anchor.breakpoints, anchor.freezes))
	}
	for _, e := range log {
		if e.Seq <= anchor.seq || e.Error != "" {
			continue
		}
		if slices.Contains(anchorCommands, e.Method) {
			// from: start の開始はこれらの後に置き直しているため、ここに来るのは印の後だけである。
			return nil, Errorf(KindInvalidParams, "印の後に %s があるため from: mark では書き出せない（from: start を使う）", e.Method)
		}
		spec, ok := c.Host.registry.Lookup(e.Method)
		if !ok {
			continue
		}
		if spec.Class != ClassAdvance && spec.Class != ClassMutate && !slices.Contains(exportedConfig, e.Method) {
			continue
		}
		params, err := exportParams(e.Params)
		if err != nil {
			return nil, Errorf(KindInternalError, "%s の引数を読めない: %v", e.Method, err)
		}
		if sr, _ := e.Result["stop_reason"].(string); sr == StopCancelled || sr == StopTimeout {
			notes = append(notes, fmt.Sprintf("ステップ %d（%s）は %s で止まった。再実行すると止まる位置が変わることがある", len(steps)+1, e.Method, sr))
		}
		steps = append(steps, exportStep{method: e.Method, params: params})
	}

	path, err := filepath.Abs(p.Path)
	if err != nil {
		return nil, Errorf(KindIOError, "%v", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, Errorf(KindIOError, "%v", err)
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name := p.Name
	if name == "" {
		name = base
	}
	doc := scenarioDoc{name: name, rom: relPath(dir, anchor.romPath), steps: steps,
		init: map[string]any{"deterministic": opts.Deterministic}}
	if opts.RAMInit != "" {
		doc.init["ram_init"] = opts.RAMInit
	}
	if opts.RAMSeed != 0 {
		doc.init["ram_seed"] = opts.RAMSeed
	}
	if opts.RAMInit == "random" && opts.RAMSeed == 0 {
		notes = append(notes, "ram_init: random でシードを指定していないため、再実行すると RAM の初期値が変わる。instance.create で ram_seed を指定する")
	}
	start := "power-on"
	if anchor.state != nil {
		statePath := filepath.Join(dir, base+".state")
		if err := writeAtomic(statePath, anchor.state); err != nil {
			return nil, Errorf(KindIOError, "セーブステートを書けない: %v", err)
		}
		doc.state = relPath(dir, statePath)
		start = statePath
	}
	if syms := inst.symbols(); syms != nil {
		if dbg := syms.DbgPath(); dbg != "" {
			doc.symbols = relPath(dir, dbg)
		}
		if gs := syms.GameState().Path(); gs != "" {
			if _, err := os.Stat(gs); err == nil {
				doc.gamestate = relPath(dir, gs)
			}
		}
	}
	var data []byte
	if strings.EqualFold(filepath.Ext(path), ".json") {
		data, err = doc.json()
		if err != nil {
			return nil, Errorf(KindInternalError, "%v", err)
		}
	} else {
		data = doc.yaml()
	}
	if err := writeAtomic(path, data); err != nil {
		return nil, Errorf(KindIOError, "書けない: %v", err)
	}
	return ScenarioExportResult{Path: path, Steps: len(steps), Start: start, Notes: notes}, nil
}

// exportParams は記録の引数から instance と observe を除く。
func exportParams(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return json.RawMessage("{}"), nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	delete(m, "instance")
	delete(m, "observe")
	return json.Marshal(m)
}

// relPath は dir からの相対パスを返す。作れないときは絶対パスを返す。
func relPath(dir, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(dir, abs); err == nil {
		return filepath.ToSlash(rel)
	}
	return abs
}

// scenarioDoc は書き出す Scenario。
type scenarioDoc struct {
	name, rom, state, symbols, gamestate string
	init                                 map[string]any
	steps                                []exportStep
}

// yaml は YAML で書く。値は JSON の流れ形式にする（YAML として読める）。
func (d scenarioDoc) yaml() []byte {
	var b bytes.Buffer
	q := func(s string) string {
		data, _ := json.Marshal(s)
		return string(data)
	}
	b.WriteString("# scenario.export で書き出した Scenario。assert などのアサーションを足して使う。\n")
	fmt.Fprintf(&b, "name: %s\n", q(d.name))
	fmt.Fprintf(&b, "rom: %s\n", q(d.rom))
	if d.state != "" {
		fmt.Fprintf(&b, "start: {state: %s}\n", q(d.state))
	} else {
		b.WriteString("start: power-on\n")
	}
	init, _ := json.Marshal(d.init)
	fmt.Fprintf(&b, "init: %s\n", init)
	if d.symbols != "" {
		fmt.Fprintf(&b, "symbols: %s\n", q(d.symbols))
	}
	if d.gamestate != "" {
		fmt.Fprintf(&b, "gamestate: %s\n", q(d.gamestate))
	}
	if len(d.steps) == 0 {
		b.WriteString("steps: []\n")
		return b.Bytes()
	}
	b.WriteString("steps:\n")
	for _, s := range d.steps {
		fmt.Fprintf(&b, "  - %s: %s\n", s.method, s.params)
	}
	return b.Bytes()
}

// json は JSON で書く。
func (d scenarioDoc) json() ([]byte, error) {
	type field struct {
		key string
		val any
	}
	steps := make([]map[string]json.RawMessage, len(d.steps))
	for i, s := range d.steps {
		steps[i] = map[string]json.RawMessage{s.method: s.params}
	}
	fields := []field{{"name", d.name}, {"rom", d.rom}}
	if d.state != "" {
		fields = append(fields, field{"start", map[string]string{"state": d.state}})
	} else {
		fields = append(fields, field{"start", "power-on"})
	}
	fields = append(fields, field{"init", d.init})
	if d.symbols != "" {
		fields = append(fields, field{"symbols", d.symbols})
	}
	if d.gamestate != "" {
		fields = append(fields, field{"gamestate", d.gamestate})
	}
	fields = append(fields, field{"steps", steps})
	// 項目の順を保つため、1 つずつ書く。
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, f := range fields {
		val, err := json.MarshalIndent(f.val, "  ", "  ")
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %q: %s", f.key, val)
		if i < len(fields)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}
