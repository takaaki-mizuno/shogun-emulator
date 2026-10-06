package agent

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/debug"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// イベント（設計書 14 編 §14.14）。
//
// エージェントが要求していない時点で起きること（人間が遊んでいる GUI での
// ブレークポイントの停止など）を Instance ごとのキューに積み、events.poll で
// 取り出すか、events.subscribe した接続へ通知する。

// イベントの種類。
const (
	EventBreakpointHit  = "breakpoint_hit"
	EventDiagnostic     = "diagnostic"
	EventControlChanged = "control_changed"
	EventROMLoaded      = "rom_loaded"
	EventROMChanged     = "rom_changed"
	EventMovieDesync    = "movie_desync"
	EventInstanceClosed = "instance_closed"
)

// EventKinds はイベントの種類の一覧。
var EventKinds = []string{EventBreakpointHit, EventDiagnostic, EventControlChanged, EventROMLoaded,
	EventROMChanged, EventMovieDesync, EventInstanceClosed}

// eventQueueSize は Instance ごとに保持するイベントの数。
const eventQueueSize = 1000

// Event はイベント 1 件。
type Event struct {
	Instance InstanceID `json:"instance"`
	Seq      uint64     `json:"seq"`
	Kind     string     `json:"kind"`
	Frame    uint64     `json:"frame"`
	Time     string     `json:"time"`
	Data     any        `json:"data,omitempty"`
}

// eventQueue は Instance のイベントのキュー。
type eventQueue struct {
	mu     sync.Mutex
	events []Event
	seq    uint64
	// polled は接続ごとの、events.poll で受け取った最後の seq。
	polled map[ConnID]uint64
}

func (q *eventQueue) push(ev Event) Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	ev.Seq = q.seq
	if len(q.events) == eventQueueSize {
		q.events = append(q.events[:0], q.events[1:]...)
	}
	q.events = append(q.events, ev)
	return ev
}

// poll は since より後のイベントを最大 max 件返す。あふれて捨てたイベントの
// 数も返す。
func (q *eventQueue) poll(conn ConnID, since uint64, max int, kinds []string) ([]Event, uint64, uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var dropped uint64
	if len(q.events) > 0 && q.events[0].Seq > since+1 {
		dropped = q.events[0].Seq - since - 1
	}
	out := []Event{}
	last := since
	for _, ev := range q.events {
		if ev.Seq <= since {
			continue
		}
		if len(out) == max {
			break
		}
		last = ev.Seq
		if len(kinds) == 0 || slices.Contains(kinds, ev.Kind) {
			out = append(out, ev)
		}
	}
	if q.polled == nil {
		q.polled = map[ConnID]uint64{}
	}
	if last > q.polled[conn] {
		q.polled[conn] = last
	}
	return out, last, dropped
}

// pending は接続がまだ受け取っていないイベントの数を返す。
func (q *eventQueue) pending(conn ConnID) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := int(q.seq - q.polled[conn])
	return min(n, len(q.events))
}

// PushEvent は Instance にイベントを積み、購読している接続へ通知する。
// エミュレーションゴルーチンからも呼ばれる。待たずに戻る。
func (h *Host) PushEvent(inst *Instance, kind string, frame uint64, data any) {
	ev := inst.events.push(Event{Instance: inst.ID, Kind: kind, Frame: frame,
		Time: time.Now().Format(time.RFC3339), Data: data})
	h.mu.Lock()
	conns := slices.Clone(h.conns)
	h.mu.Unlock()
	for _, c := range conns {
		c.notifyEvent(ev)
	}
}

// observe は Instance のエミュレータの知らせをイベントにする。
func (h *Host) watchEmulator(inst *Instance) {
	inst.Emu.SetObserver(&emu.Observer{
		OnBreak: func(info debug.BreakInfo, frame uint64) {
			if info.Diagnostic != nil {
				// Diagnostic のイベントは OnDiagnostic で積んである。
				return
			}
			h.PushEvent(inst, EventBreakpointHit, frame, breakDetail(&info))
		},
		OnDiagnostic: func(x debug.Diagnostic) {
			h.PushEvent(inst, EventDiagnostic, x.Frame, diagInfo(inst.sharedSymbols(), x))
		},
		OnLoad: func(name string) {
			h.PushEvent(inst, EventROMLoaded, 0, map[string]string{"rom": name})
		},
		OnDesync: func(err error, frame uint64) {
			h.PushEvent(inst, EventMovieDesync, frame, map[string]string{"error": err.Error()})
		},
	})
}

func registerEvents(r *Registry) {
	r.Register(CommandSpec{
		Name: "events.poll", Class: ClassObserve, Params: pollParams{}, Positional: []string{"since"},
		DescJA: "イベントを取り出す。since に前回受け取った seq を渡す",
		DescEN: "Fetch events (breakpoint_hit, control_changed, rom_loaded, ...) newer than 'since' (the last seq you received).",
		GUI:    true, Headless: true, Target: true, Handler: handlePoll,
	})
	r.Register(CommandSpec{
		Name: "events.subscribe", Class: ClassSession, Params: subscribeParams{},
		DescJA: "イベントの通知（events.event）を受け取る。JSON-RPC と CLI のみ",
		DescEN: "Subscribe to JSON-RPC 'events.event' notifications (not for MCP; use events_poll).",
		GUI:    true, Headless: true, Handler: handleSubscribe,
	})
	r.Register(CommandSpec{
		Name: "events.unsubscribe", Class: ClassSession,
		DescJA: "イベントの通知をやめる",
		DescEN: "Stop event notifications.",
		GUI:    true, Headless: true, Handler: handleUnsubscribe,
	})
}

type pollParams struct {
	InstanceParam
	Since uint64   `json:"since,omitempty" desc:"前回受け取った最後の seq" default:"0"`
	Max   int      `json:"max,omitempty" desc:"最大の件数（1–1000）" default:"100"`
	Kinds []string `json:"kinds,omitempty" desc:"種類で絞り込む"`
}

// PollResult は events.poll の結果。
type PollResult struct {
	Events []Event `json:"events"`
	// LastSeq は次の since に渡す値。
	LastSeq uint64 `json:"last_seq"`
	// Dropped はキューからあふれて捨てたため受け取れなかった数。
	Dropped uint64 `json:"dropped"`
}

func checkKinds(kinds []string) error {
	for _, k := range kinds {
		if !slices.Contains(EventKinds, k) {
			return Errorf(KindInvalidParams, "kinds の %q を知らない（%v）", k, EventKinds)
		}
	}
	return nil
}

func handlePoll(c *Context, raw json.RawMessage) (any, error) {
	var p pollParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Max == 0 {
		p.Max = 100
	}
	if p.Max < 1 || p.Max > eventQueueSize {
		return nil, Errorf(KindInvalidParams, "max は 1–%d とする", eventQueueSize)
	}
	if err := checkKinds(p.Kinds); err != nil {
		return nil, err
	}
	evs, last, dropped := c.Instance.events.poll(c.Conn.ID, p.Since, p.Max, p.Kinds)
	return PollResult{Events: evs, LastSeq: last, Dropped: dropped}, nil
}

type subscribeParams struct {
	Kinds []string `json:"kinds,omitempty" desc:"種類で絞り込む。省くとすべて"`
}

func handleSubscribe(c *Context, raw json.RawMessage) (any, error) {
	var p subscribeParams
	if err := DecodeParams(raw, &p); err != nil {
		return nil, err
	}
	if err := checkKinds(p.Kinds); err != nil {
		return nil, err
	}
	if !c.Conn.subscribe(p.Kinds) {
		return nil, Errorf(KindInvalidRequest, "この接続は通知を受け取れない")
	}
	return struct {
		Subscribed bool     `json:"subscribed"`
		Kinds      []string `json:"kinds,omitempty"`
	}{true, p.Kinds}, nil
}

func handleUnsubscribe(c *Context, raw json.RawMessage) (any, error) {
	if err := DecodeParams(raw, &struct{}{}); err != nil {
		return nil, err
	}
	c.Conn.unsubscribe()
	return struct {
		Subscribed bool `json:"subscribed"`
	}{false}, nil
}
