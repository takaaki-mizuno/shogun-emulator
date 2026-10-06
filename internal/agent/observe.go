package agent

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// Observation は Agent Command の結果として返す Instance の様子
// （設計書 14 編 §14.9）。既定では要約で、画像などは求められたときだけ付ける。
type Observation struct {
	Instance   InstanceID `json:"instance"`
	Frame      uint64     `json:"frame"`
	StopReason string     `json:"stop_reason,omitempty"`
	StopDetail any        `json:"stop_detail,omitempty"`
	CPU        *ObsCPU    `json:"cpu,omitempty"`
	// GameStateChanged は前回の Observation からの Game State の差分。
	// Game State はフェーズ 16 で作る。
	GameStateChanged map[string]Change `json:"gamestate_changed,omitempty"`
	// GameState は全項目の現在値（full、include: gamestate）。
	GameState map[string]any `json:"gamestate,omitempty"`
	// Watch はウォッチの現在値。WatchChanged は前回からの差分。
	Watch        map[string]int64  `json:"watch,omitempty"`
	WatchChanged map[string]Change `json:"watch_changed,omitempty"`
	// Diagnostics は前回の Observation 以降に検知した Diagnostic（§14.20.3）。
	Diagnostics   []DiagSummary `json:"diagnostics,omitempty"`
	EventsPending int           `json:"events_pending"`
	Notes         []string      `json:"notes,omitempty"`

	Image     *Image           `json:"image,omitempty"`
	CPUFull   *CPUInfo         `json:"cpu_full,omitempty"`
	Sprites   *SpritesResult   `json:"sprites,omitempty"`
	Nametable *NametableResult `json:"nametable,omitempty"`
	PPUWrites *PPUWritesResult `json:"ppu_writes,omitempty"`
}

// ObsCPU は Observation の CPU の位置。
type ObsCPU struct {
	PC             string `json:"pc"`
	Symbol         string `json:"symbol,omitempty"`
	Source         string `json:"source,omitempty"`
	MidInstruction bool   `json:"mid_instruction"`
}

// Change は値の差分。初めての Observation では From が null。
type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// ObserveParams は観測の指定（設計書 14 編 §14.9.3）。
type ObserveParams struct {
	Include []string `json:"include,omitempty" desc:"付ける内容（image・gamestate・cpu_full・sprites・nametable・ppu_writes）"`
	Scale   int      `json:"scale,omitempty" desc:"画像の拡大率（1–4）"`
	Full    bool     `json:"full,omitempty" desc:"差分ではなく全項目の現在値を返す"`
}

// includeNames は include に書ける名前。
var includeNames = []string{"image", "gamestate", "cpu_full", "sprites", "nametable", "ppu_writes"}

// observeSpec は観測の指定を解釈したもの。
type observeSpec struct {
	none bool
	ObserveParams
}

// parseObserve は引数 observe（false、または ObserveParams）を読む。
func parseObserve(raw json.RawMessage) (observeSpec, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "true" {
		return observeSpec{}, nil
	}
	if string(raw) == "false" {
		return observeSpec{none: true}, nil
	}
	var o observeSpec
	if err := DecodeParams(raw, &o.ObserveParams); err != nil {
		return o, err
	}
	for _, n := range o.Include {
		if !slices.Contains(includeNames, n) {
			return o, Errorf(KindInvalidParams, "include の %q を知らない（%v）", n, includeNames)
		}
	}
	if o.Scale != 0 && (o.Scale < 1 || o.Scale > 4) {
		return o, Errorf(KindInvalidParams, "scale は 1–4 とする（%d）", o.Scale)
	}
	return o, nil
}

func (o observeSpec) has(name string) bool { return slices.Contains(o.Include, name) }

// observedValues は接続ごとの前回の観測値（設計書 14 編 §14.9.2）。
type observedValues struct {
	watch     map[string]int64
	gamestate map[string]any
	// diagSeq と diagTotals は前回の Observation の時点の Diagnostic の通し番号と
	// 種類ごとの累積件数。
	diagSeq    uint64
	diagTotals [debug.DiagKindCount]uint64
}

// observe は Instance の Observation を作る。
//
// 状態の読み出しはエミュレーションゴルーチンの命令境界で行い、値の写しだけを
// 取り出す。差分は conn ごとの前回値と比べる。
func (h *Host) observe(inst *Instance, conn *Conn, o observeSpec) (*Observation, error) {
	st := inst.Emu.Status()
	ob := &Observation{Instance: inst.ID, Frame: st.Frames, EventsPending: inst.events.pending(conn.ID)}
	if o.none {
		return ob, nil
	}
	if !st.Loaded {
		ob.Notes = append(ob.Notes, "ROM が読み込まれていない")
		return ob, nil
	}
	watch := map[string]int64{}
	var full *CPUInfo
	var sprites *SpritesResult
	var nt *NametableResult
	var pw *PPUWritesResult
	var gs map[string]any
	var hidden []string
	inst.mu.Lock()
	var prevSeq uint64
	var prevTotals [debug.DiagKindCount]uint64
	if pv := inst.observed[conn.ID]; pv != nil {
		prevSeq, prevTotals = pv.diagSeq, pv.diagTotals
	}
	inst.mu.Unlock()
	var diagSeq uint64
	var diagTotals [debug.DiagKindCount]uint64
	ok := inst.Emu.WithDebugger(func(d *debug.Debugger) {
		n := d.Machine()
		if n == nil {
			return
		}
		ob.Diagnostics, diagSeq, diagTotals = observeDiagnostics(d, prevSeq, prevTotals)
		gs = d.GameStateValues()
		hidden = d.HiddenGameState()
		ob.Frame = n.Frames()
		ob.CPU = &ObsCPU{PC: hex16(n.CPU.PC), Symbol: d.NearestLabel(n.CPU.PC), MidInstruction: st.MidInstruction}
		if src, ok := d.Source(n.CPU.PC); ok {
			ob.CPU.Source = src.String()
		}
		for _, a := range d.Symbols().Watch() {
			watch[watchKey(d, a)] = int64(n.Bus.Peek(a))
		}
		if o.has("cpu_full") {
			c := cpuInfo(d)
			full = &c
		}
		var snap *debug.Snapshot
		if o.has("sprites") || o.has("nametable") {
			snap = takeSnapshot(n)
		}
		if o.has("sprites") {
			s := spritesFrom(snap, false)
			sprites = &s
		}
		if o.has("ppu_writes") {
			r := ppuWrites(d)
			pw = &r
		}
		if o.has("nametable") {
			// 記録を有効にしているときだけ、途中のスクロールの変更を調べる。
			// 調べるために記録を有効にはしない。
			check := pw
			if check == nil && d.PPUWriteLogEnabled() {
				r := ppuWrites(d)
				check = &r
			}
			r := visibleNametable(snap, check)
			nt = &r
		}
	})
	if !ok {
		return nil, Errorf(KindInternalError, "エミュレーションが停止している")
	}
	ob.CPUFull, ob.Sprites, ob.Nametable, ob.PPUWrites = full, sprites, nt, pw
	if len(watch) > 0 {
		ob.Watch = watch
	}
	if (o.Full || o.has("gamestate")) && len(gs) > 0 {
		ob.GameState = gs
	}
	// hidden の項目は差分に含めない（毎フレーム変わるタイマーなど）。
	diffable := map[string]any{}
	for k, v := range gs {
		if !slices.Contains(hidden, k) {
			diffable[k] = v
		}
	}

	inst.mu.Lock()
	if inst.observed == nil {
		inst.observed = map[ConnID]*observedValues{}
	}
	prev := inst.observed[conn.ID]
	first := prev == nil
	if first {
		prev = &observedValues{}
	}
	ob.WatchChanged = diffInts(prev.watch, watch, first)
	ob.GameStateChanged = diffValues(prev.gamestate, diffable, first)
	inst.observed[conn.ID] = &observedValues{watch: watch, gamestate: diffable, diagSeq: diagSeq, diagTotals: diagTotals}
	inst.mu.Unlock()

	if o.has("image") {
		scale := o.Scale
		if scale == 0 {
			scale = h.opts.ImageScale
		}
		img, have, err := inst.Emu.FramePNG(scale)
		if err != nil {
			return nil, Errorf(KindInternalError, "画像を作れない: %v", err)
		}
		if have {
			ob.Image = &Image{MIME: "image/png", Data: img}
		} else {
			ob.Notes = append(ob.Notes, "まだ完成したフレームが無いため画像を付けていない")
		}
	}
	return ob, nil
}

// watchKey はウォッチの表示名を返す。名前があれば名前、無ければ "$0300"。
func watchKey(d *debug.Debugger, a uint16) string {
	if l := d.Label(a); l != "" {
		return l
	}
	return hex16(a)
}

// diffInts は整数の値の差分を返す。first のときは全項目を null からの変化とする。
func diffInts(prev, cur map[string]int64, first bool) map[string]Change {
	out := map[string]Change{}
	for k, v := range cur {
		old, had := prev[k]
		switch {
		case first || !had:
			out[k] = Change{From: nil, To: v}
		case old != v:
			out[k] = Change{From: old, To: v}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// diffValues は値の差分を返す。比較は JSON の表現で行う。
func diffValues(prev, cur map[string]any, first bool) map[string]Change {
	out := map[string]Change{}
	for k, v := range cur {
		old, had := prev[k]
		if first || !had {
			out[k] = Change{From: nil, To: v}
			continue
		}
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(v)
		if !bytes.Equal(a, b) {
			out[k] = Change{From: old, To: v}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// forgetObserved は接続の前回値を捨てる。接続が切れたときに呼ぶ。
func (inst *Instance) forgetObserved(conn *Conn) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	delete(inst.observed, conn.ID)
}

// takeSnapshot は現在の PPU とメモリの写しを取る。エミュレーション
// ゴルーチンで呼ぶ。
func takeSnapshot(n *nes.NES) *debug.Snapshot {
	set := debug.NewSnapshotSet()
	set.Capture(n, -1)
	var s debug.Snapshot
	set.Latest(&s)
	return &s
}
