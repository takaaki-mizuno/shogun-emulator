package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/emu"
	"github.com/takaakimizuno/shogun-emulator/internal/emu/movie"
	"github.com/takaakimizuno/shogun-emulator/internal/nes"
)

// 開発ループと再現（設計書 14 編 §14.15、§14.16）。

const (
	// maxCommandLog は Instance ごとに残す Agent Command の記録の数。
	maxCommandLog = 10000
	// watchInterval は ROM ファイルを調べる間隔。
	watchInterval = 500 * time.Millisecond
)

// CommandLogEntry は Agent Command の実行の記録 1 件。
type CommandLogEntry struct {
	// Seq は Instance での通し番号。記録があふれても増え続ける。
	Seq    uint64          `json:"seq"`
	Time   string          `json:"time"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	// StartFrame と EndFrame は実行の前後のフレーム番号（scenario.export が
	// exec.run を exec.step に直すときに使う）。
	StartFrame uint64 `json:"start_frame"`
	EndFrame   uint64 `json:"end_frame"`
	// Result は結果の要約（frame・stop_reason）、Error は誤りの種類。
	Result map[string]any `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// logCommand は Agent Command の実行を記録する。画像を含む結果は大きさだけを
// 残す（結果の全体は記録せず、frame と stop_reason だけを残す）。
func (inst *Instance) logCommand(method string, params json.RawMessage, startFrame uint64, res any, err error) {
	switch method {
	case "events.poll", "session.commands":
		return
	}
	e := CommandLogEntry{Time: time.Now().Format(time.RFC3339Nano), Method: method,
		StartFrame: startFrame, EndFrame: inst.Emu.Status().Frames}
	if len(params) > 0 && string(params) != "null" {
		e.Params = append(json.RawMessage(nil), params...)
	}
	if err != nil {
		e.Error = AsError(err).Kind
	} else if summary := resultSummary(res); summary != nil {
		e.Result = summary
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cmdSeq++
	e.Seq = inst.cmdSeq
	if len(inst.cmdLog) == maxCommandLog {
		inst.cmdLog = append(inst.cmdLog[:0], inst.cmdLog[1:]...)
	}
	inst.cmdLog = append(inst.cmdLog, e)
}

// resultSummary は結果の要約を返す。Observation を含むものだけを要約する。
func resultSummary(res any) map[string]any {
	var ob *Observation
	switch r := res.(type) {
	case *Observation:
		ob = r
	case *SequenceResult:
		ob = r.Observation
	}
	if ob == nil {
		return nil
	}
	s := map[string]any{"frame": ob.Frame}
	if ob.StopReason != "" {
		s["stop_reason"] = ob.StopReason
	}
	return s
}

// CommandLog は Agent Command の実行の記録の写しを返す。
func (inst *Instance) CommandLog() []CommandLogEntry {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]CommandLogEntry(nil), inst.cmdLog...)
}

func registerDevLoop(r *Registry) {
	reg := func(name string, class CommandClass, params any, pos []string, ja, en string, h Handler) {
		r.Register(CommandSpec{Name: name, Class: class, Params: params, Positional: pos, DescJA: ja, DescEN: en,
			GUI: true, Headless: true, Target: true, Handler: h})
	}
	reg("record.status", ClassObserve, InstanceParam{}, nil, "常時記録の状態（開始方法、フレーム数、介入の数、Re-Reach できるか）",
		"Show the always-on input/intervention recording: start kind, frames, interventions, and whether Re-Reach is possible.", handleRecordStatus)
	reg("rom.reload", ClassMutate, reloadParams{}, []string{"re_reach"}, "同じパスから ROM を読み直す。re_reach で同じ場面まで再生する",
		"Reload the ROM from the same path (after rebuilding). re_reach: 'frame' replays recorded input to the same frame, 'condition' replays until a condition holds.", handleReload)
	reg("rom.watch", ClassConfig, romWatchParams{}, []string{"enabled"}, "ROM ファイルの監視を切り替える。変わったら rom_changed を積む",
		"Watch the ROM file; when it changes, push a rom_changed event (then call rom_reload).", handleROMWatch)
	reg("repro.export", ClassObserve, reproParams{}, []string{"path"}, "人間が再現するための一式（ムービー・実行の記録・画面・説明）を書き出す",
		"Export a reproduction bundle (<name>.repro/ with repro.shgm movie, commands.jsonl, final.png, README.md) that a human can replay in the GUI.", handleReproExport)
}

func handleRecordStatus(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &InstanceParam{}); err != nil {
		return nil, err
	}
	st, err := c.Instance.Emu.JournalStatus()
	if err != nil {
		return nil, Errorf(KindNotLoaded, "%v", err)
	}
	return struct {
		Start         string `json:"start"`
		Frames        uint64 `json:"frames"`
		Inputs        int    `json:"inputs"`
		Interventions int    `json:"interventions"`
		Rerecords     uint64 `json:"rerecords"`
		ReReachable   bool   `json:"re_reachable"`
	}{st.Start, st.Frames, st.Inputs, st.Interventions, st.Rerecords, st.ReReachable}, nil
}

type reloadParams struct {
	InstanceParam
	ReReach     string          `json:"re_reach,omitempty" desc:"none・frame・condition" default:"\"none\""`
	TargetFrame *uint64         `json:"target_frame,omitempty" desc:"re_reach: frame の到達先。省くと読み直す前のフレーム"`
	Condition   string          `json:"condition,omitempty" desc:"re_reach: condition の条件式"`
	MaxFrames   int             `json:"max_frames,omitempty" desc:"re_reach: condition の上限" default:"600"`
	Observe     json.RawMessage `json:"observe,omitempty" desc:"観測の指定"`
}

// ReloadOptions は ROM の読み直しの指定（GUI からも使う）。
type ReloadOptions struct {
	// ReReach は none・frame・condition。
	ReReach     string
	TargetFrame *uint64
	Condition   string
	MaxFrames   int
}

func handleReload(c *Context, raw json.RawMessage) (any, error) {
	var p reloadParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	o, err := parseObserve(p.Observe)
	if err != nil {
		return nil, err
	}
	return c.Host.reload(c.Ctx, c.Instance, c.Conn, ReloadOptions{ReReach: p.ReReach, TargetFrame: p.TargetFrame,
		Condition: p.Condition, MaxFrames: p.MaxFrames}, o)
}

// Reload は Instance の ROM を読み直す（GUI の agent.romWatchAction が使う）。
func (h *Host) Reload(ctx context.Context, inst *Instance, o ReloadOptions) error {
	_, err := h.reload(ctx, inst, nil, o, observeSpec{none: true})
	return err
}

// reload は同じパスから ROM を読み直し、必要なら Re-Reach する（設計書 14 編
// §14.15.1、§14.15.3）。
func (h *Host) reload(ctx context.Context, inst *Instance, conn *Conn, o ReloadOptions, spec observeSpec) (*Observation, error) {
	if o.ReReach == "" {
		o.ReReach = "none"
	}
	if o.ReReach != "none" && o.ReReach != "frame" && o.ReReach != "condition" {
		return nil, Errorf(KindInvalidParams, "re_reach は none・frame・condition とする（%q）", o.ReReach)
	}
	if o.MaxFrames == 0 {
		o.MaxFrames = 600
	}
	if o.MaxFrames < 1 || o.MaxFrames > maxUntilFrames {
		return nil, Errorf(KindInvalidParams, "max_frames は 1–%d とする", maxUntilFrames)
	}
	inst.mu.Lock()
	path := inst.romPath
	inst.mu.Unlock()
	if path == "" {
		return nil, Errorf(KindNotLoaded, "読み直す ROM のパスが分からない（rom.load か instance.create で読み込む）")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, Errorf(KindIOError, "ROM を読めない: %v", err)
	}
	// 手順 1: 読み直す前に常時記録を取り出す。
	journal, _, jerr := inst.Emu.Journal()
	prevFrame := inst.Emu.Status().Frames
	if o.ReReach == "condition" && o.Condition == "" {
		return nil, Errorf(KindInvalidParams, "re_reach: condition には condition を指定する")
	}
	// .dbg と Game State Definition を読み直させる。
	if syms := inst.symbols(); syms != nil {
		syms.ResetProject()
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if err := inst.Emu.LoadROMDataFrom(data, name, path); err != nil {
		return nil, Errorf(KindIOError, "ROM を読み込めない: %v", err)
	}
	inst.Emu.Pause()
	h.detachShared(inst)
	inst.mu.Lock()
	inst.romData = data
	inst.states = nil
	inst.mu.Unlock()
	h.attachShared(inst)
	notes := inst.Emu.ProjectNotes()

	var r emu.StepResult
	reason, detail := "", any(nil)
	if o.ReReach != "none" {
		switch {
		case jerr != nil || journal == nil:
			notes = append(notes, "re_reach_unavailable: 常時記録が無い")
		case journal.Header.Start != movie.StartPowerOn:
			notes = append(notes, "re_reach_unavailable: 記録がセーブステートから始まっているため、作り直した ROM では再生できない。電源投入の直後で止めた")
		default:
			r, reason, detail, err = h.reReach(ctx, inst, journal, prevFrame, o)
			if err != nil {
				return nil, err
			}
		}
	}
	if spec.none && conn == nil {
		return nil, nil
	}
	ob, err := h.observe(inst, conn, spec)
	if err != nil {
		return nil, err
	}
	ob.Notes = append(ob.Notes, notes...)
	if reason != "" {
		ob.StopReason, ob.StopDetail, ob.Frame = reason, detail, r.Frame
	}
	return ob, nil
}

// reReach は記録の入力と介入を新しい ROM で再生し、同じ場面まで進める
// （手順 2–4）。チェックサムは比べない。記録の終わりに達したら、残りは入力
// なしで進める。
func (h *Host) reReach(ctx context.Context, inst *Instance, journal *movie.Movie, prevFrame uint64, o ReloadOptions) (emu.StepResult, string, any, error) {
	if err := inst.Emu.Replay(journal); err != nil {
		return emu.StepResult{}, "", nil, Errorf(KindInternalError, "%v", err)
	}
	defer inst.Emu.StopReplay()
	zero := [2]uint8{}
	opts := emu.StepOptions{Kind: emu.StepFrame, Input: &zero}
	var detail any
	if o.ReReach == "frame" {
		target := prevFrame
		if o.TargetFrame != nil {
			target = *o.TargetFrame
		}
		if target == 0 {
			return emu.StepResult{}, StopFramesDone, nil, nil
		}
		if target > maxUntilFrames {
			return emu.StepResult{}, "", nil, Errorf(KindInvalidParams, "target_frame は %d までとする", maxUntilFrames)
		}
		opts.Count = int(target)
	} else {
		cond, err := inst.parseCondition(o.Condition)
		if err != nil {
			return emu.StepResult{}, "", nil, conditionError(err)
		}
		d := inst.Emu.Debugger()
		opts.Count, opts.Cond = o.MaxFrames, func(*nes.NES) bool { return cond.Eval(d) }
		detail = UntilDetail{Expr: o.Condition}
	}
	r, err := inst.Emu.StepWith(ctx, opts)
	if err != nil {
		return r, "", nil, Errorf(KindInternalError, "%v", err)
	}
	reason := stopName(r.Reason)
	switch {
	case r.Reason == emu.StopFramesDone && o.ReReach == "condition":
		reason = StopMaxFrames
	case r.Reason == emu.StopCancelled:
		reason = StopCancelled
	}
	if reason != StopCondition {
		detail = nil
	}
	return r, reason, detail, nil
}

type romWatchParams struct {
	InstanceParam
	Enabled *bool `json:"enabled,omitempty" desc:"監視するか" default:"true"`
}

func handleROMWatch(c *Context, raw json.RawMessage) (any, error) {
	var p romWatchParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	on := p.Enabled == nil || *p.Enabled
	if on {
		if err := c.Host.Watch(c.Instance); err != nil {
			return nil, err
		}
	} else {
		c.Instance.stopWatch()
	}
	return struct {
		Watching bool `json:"watching"`
	}{on}, nil
}

// Watch は Instance の ROM ファイルの監視を始める（設計書 14 編 §14.15.2）。
//
// 500 ミリ秒ごとに更新時刻と大きさを調べ、変わったら、さらに 500 ミリ秒待って
// 変化が止まったことを確かめてから rom_changed を積む。ビルドの途中の
// 書きかけのファイルを扱わないためである。監視のゴルーチンは Machine State に
// 触れない。
func (h *Host) Watch(inst *Instance) error {
	inst.mu.Lock()
	path := inst.romPath
	if inst.watchStop != nil {
		inst.mu.Unlock()
		return nil
	}
	if path == "" {
		inst.mu.Unlock()
		return Errorf(KindNotLoaded, "監視する ROM のパスが分からない")
	}
	stop := make(chan struct{})
	inst.watchStop = stop
	inst.mu.Unlock()
	go h.watchLoop(inst, path, stop)
	return nil
}

// Unwatch は Instance の ROM の監視を止める（GUI で ROM を開き直すとき）。
func (h *Host) Unwatch(inst *Instance) { inst.stopWatch() }

// stopWatch は ROM の監視を止める。
func (inst *Instance) stopWatch() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.watchStop != nil {
		close(inst.watchStop)
		inst.watchStop = nil
	}
}

// fileStamp はファイルの更新時刻と大きさ。
type fileStamp struct {
	mod  time.Time
	size int64
	ok   bool
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mod: fi.ModTime(), size: fi.Size(), ok: true}
}

func (h *Host) watchLoop(inst *Instance, path string, stop chan struct{}) {
	t := time.NewTicker(watchInterval)
	defer t.Stop()
	last := stampOf(path)
	changing := false
	var seen fileStamp
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		cur := stampOf(path)
		if !cur.ok {
			// 消えたか読めない。監視は続け、現れたら変化として扱う。
			continue
		}
		if !changing {
			if cur != last {
				changing, seen = true, cur
			}
			continue
		}
		if cur != seen {
			// まだ書いている。
			seen = cur
			continue
		}
		changing, last = false, cur
		h.PushEvent(inst, EventROMChanged, inst.Emu.Status().Frames, map[string]string{"path": path})
		if h.opts.OnROMChanged != nil {
			h.opts.OnROMChanged(inst)
		}
	}
}

type reproParams struct {
	InstanceParam
	Path      string  `json:"path,omitempty" desc:"書き出し先のディレクトリ（<名前>.repro）。省くとデータディレクトリの repros/"`
	FromFrame *uint64 `json:"from_frame,omitempty" desc:"このフレーム以前の巻き戻しの状態から始める短い記録にする"`
	Note      string  `json:"note,omitempty" desc:"人間への説明"`
}

// ReproResult は repro.export の結果。
type ReproResult struct {
	Path      string   `json:"path"`
	Frames    uint64   `json:"frames"`
	StartedAt uint64   `json:"started_at"`
	Files     []string `json:"files"`
}

func handleReproExport(c *Context, raw json.RawMessage) (any, error) {
	var p reproParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	inst := c.Instance
	if err := requireLoaded(inst); err != nil {
		return nil, err
	}
	j, blob, err := inst.Emu.Journal()
	if err != nil {
		return nil, Errorf(KindNotLoaded, "%v", err)
	}
	st := inst.Emu.Status()
	base := inst.Emu.JournalBase()
	m := j.From(0, blob)
	startedAt := base
	if p.FromFrame != nil {
		if *p.FromFrame > st.Frames {
			return nil, Errorf(KindInvalidParams, "from_frame %d は今のフレーム %d より後である", *p.FromFrame, st.Frames)
		}
		at, data, earliest, err := inst.Emu.RewindState(*p.FromFrame)
		if err != nil {
			return nil, Errorf(KindInvalidParams, "from_frame %d: %v（取り出せる最も古いフレームは %d）", *p.FromFrame, err, earliest)
		}
		if at < base {
			return nil, Errorf(KindInvalidParams, "from_frame %d は常時記録の始まり（フレーム %d）より前である", *p.FromFrame, base)
		}
		m, startedAt = j.From(at-base, data), at
	}
	m.Header.Author = emu.ReproAuthor
	m.Header.Comment = p.Note

	dir := p.Path
	if dir == "" {
		dir = filepath.Join(c.Host.opts.EmuConfig.Dirs.Data, "repros", st.ROMKey,
			time.Now().Format("20060102-150405")+".repro")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, Errorf(KindIOError, "%v", err)
	}
	files := []string{"repro.shgm", "commands.jsonl", "README.md"}
	if err := writeAtomic(filepath.Join(dir, "repro.shgm"), m.Encode()); err != nil {
		return nil, Errorf(KindIOError, "%v", err)
	}
	var lines strings.Builder
	for _, e := range inst.CommandLog() {
		data, _ := json.Marshal(e)
		lines.Write(data)
		lines.WriteByte('\n')
	}
	if err := writeAtomic(filepath.Join(dir, "commands.jsonl"), []byte(lines.String())); err != nil {
		return nil, Errorf(KindIOError, "%v", err)
	}
	if png, ok, err := inst.Emu.FramePNG(1); err == nil && ok {
		if err := writeAtomic(filepath.Join(dir, "final.png"), png); err != nil {
			return nil, Errorf(KindIOError, "%v", err)
		}
		files = append(files, "final.png")
	}
	readme := reproReadme(st, m, startedAt, p.Note, inst)
	if err := writeAtomic(filepath.Join(dir, "README.md"), []byte(readme)); err != nil {
		return nil, Errorf(KindIOError, "%v", err)
	}
	return ReproResult{Path: dir, Frames: m.Header.TotalFrames, StartedAt: startedAt, Files: files}, nil
}

// reproReadme は Repro の説明を作る。
func reproReadme(st emu.Status, m *movie.Movie, startedAt uint64, note string, inst *Instance) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Repro: %s\n\n", st.ROMName)
	if note != "" {
		fmt.Fprintf(&b, "%s\n\n", note)
	}
	fmt.Fprintf(&b, "| 項目 | 値 |\n|---|---|\n")
	fmt.Fprintf(&b, "| ROM | %s |\n", st.ROMName)
	fmt.Fprintf(&b, "| ROM ハッシュ | %x |\n", m.Header.ROMHash)
	fmt.Fprintf(&b, "| 開始のフレーム | %d |\n", startedAt)
	fmt.Fprintf(&b, "| フレーム数 | %d |\n", m.Header.TotalFrames)
	fmt.Fprintf(&b, "| 最後のフレーム | %d |\n", st.Frames)
	log := inst.CommandLog()
	for i := len(log) - 1; i >= 0; i-- {
		if sr, ok := log[i].Result["stop_reason"]; ok {
			fmt.Fprintf(&b, "| 最後の Stop Reason | %v（%s） |\n", sr, log[i].Method)
			break
		}
	}
	fmt.Fprintf(&b, "\n## 再生\n\n将軍エミュレータの「ムービー → 再生…」で `repro.shgm`（またはこのディレクトリ）を開くと、最後のフレームまで再生して一時停止する。コマンドラインでは `shogun --movie <このディレクトリ>/repro.shgm <ROM>`。\n\n")
	fmt.Fprintf(&b, "`commands.jsonl` はエージェントが実行した Agent Command の記録、`final.png` は書き出した時点の画面である。\n")
	return b.String()
}

// writeAtomic は一時ファイルに書いてから名前を変える。
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
