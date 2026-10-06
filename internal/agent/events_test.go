package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// guiSession は GUI 版の Host に、headless の Instance のエミュレータを i1 として
// 登録したもの。人間が Control を持ち、Real-Time で始まる。
func guiSession(t *testing.T) (*agentSession, *Instance) {
	t.Helper()
	src := newSession(t)
	e := src.h.Instances()[0].Emu
	h := newTestHost(t, KindGUI)
	inst := h.AttachGUI(e)
	// headless で作ったエミュレータは入力がエージェントの経路にある。GUI の
	// 人間の状態に戻す。
	e.SetAgentInput(false)
	return &agentSession{t: t, h: h, conn: h.Connect()}, inst
}

// TestBreakpointEvent はブレークポイントの停止がイベントになり、events.poll で
// 取り出せることを確かめる。
func TestBreakpointEvent(t *testing.T) {
	s := newSession(t)
	s.warm()
	since := s.quiet()
	s.must("debug.bp.add", map[string]any{"kind": "exec", "loc": romAddr("nmi")}, nil)
	var ob Observation
	s.must("exec.step", map[string]any{"frames": 3}, &ob)
	if ob.StopReason != StopBreakpoint || ob.EventsPending != 1 {
		t.Fatalf("Observation = %+v", ob)
	}
	var r PollResult
	s.must("events.poll", map[string]any{"since": since}, &r)
	if len(r.Events) != 1 || r.Events[0].Kind != EventBreakpointHit || r.LastSeq != r.Events[0].Seq {
		t.Fatalf("events.poll = %+v", r)
	}
	s.must("events.poll", map[string]any{"since": r.LastSeq}, &r)
	if len(r.Events) != 0 {
		t.Errorf("受け取った後の events.poll = %+v", r)
	}
	ob = Observation{}
	s.must("obs.get", nil, &ob)
	if ob.EventsPending != 0 {
		t.Errorf("受け取った後の events_pending = %d", ob.EventsPending)
	}
	if err := s.call("events.poll", map[string]any{"kinds": []string{"nope"}}, nil); err == nil {
		t.Error("知らない種類を受け付けた")
	}
}

// TestEventQueueOverflow は 1000 件を超えたら古いものを捨て、dropped で知らせる
// ことを確かめる。
func TestEventQueueOverflow(t *testing.T) {
	s := newSession(t)
	inst := s.h.Instances()[0]
	for i := range 1005 {
		s.h.PushEvent(inst, EventROMChanged, uint64(i), nil)
	}
	var r PollResult
	s.must("events.poll", map[string]any{"max": 10}, &r)
	// 1005 件のうち最新の 1000 件（seq 6–1005）を残す。
	if r.Dropped != 5 || len(r.Events) != 10 || r.Events[0].Seq != 6 {
		t.Errorf("あふれた後 = dropped %d、%d 件、最初の seq %d", r.Dropped, len(r.Events), r.Events[0].Seq)
	}
}

// TestSubscribeNotifies は events.subscribe した接続に通知が届き、kinds で
// 絞り込めることを確かめる。
func TestSubscribeNotifies(t *testing.T) {
	s := newSession(t)
	var mu sync.Mutex
	var got []Event
	sub := s.h.Connect()
	if err := call(t, s.h, sub, "events.subscribe", nil, nil); err == nil {
		t.Error("通知の送り先の無い接続で購読できた")
	}
	sub.SetNotifier(func(method string, params any) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, params.(Event))
	})
	mustCall(t, s.h, sub, "events.subscribe", map[string]any{"kinds": []string{EventInstanceClosed}}, nil)
	inst := s.h.Instances()[0]
	s.h.PushEvent(inst, EventROMChanged, 0, nil)
	s.must("instance.close", nil, nil)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Kind != EventInstanceClosed || got[0].Instance != "i1" {
		t.Errorf("通知 = %+v", got)
	}
}

// TestGUIControlEvents は GUI 版で Control の取得と取り返しが control_changed の
// イベントになり、進行モードが変わることを確かめる（フェーズ 17 の完了判定）。
func TestGUIControlEvents(t *testing.T) {
	s, inst := guiSession(t)
	var changes []ControlStatus
	var mu sync.Mutex
	inst.SetOnControl(func(st ControlStatus) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, st)
	})
	s.must("control.acquire", nil, nil)
	waitFor(t, "一時停止", func() bool { return inst.Emu.Status().Paused })
	if !s.h.TakeBack(inst) {
		t.Fatal("取り返せない")
	}
	waitFor(t, "走り出す", func() bool { return !inst.Emu.Status().Paused })
	var r PollResult
	s.must("events.poll", map[string]any{"kinds": []string{EventControlChanged}}, &r)
	if len(r.Events) != 2 {
		t.Fatalf("control_changed = %+v", r.Events)
	}
	data, _ := json.Marshal(r.Events[1].Data)
	var st ControlStatus
	json.Unmarshal(data, &st)
	if st.Owner != "human" || st.Mode != ModeRealTime {
		t.Errorf("取り返した後 = %+v", st)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 2 || changes[0].Owner != "agent" {
		t.Errorf("GUI への知らせ = %+v", changes)
	}
}

// TestControlLost は進行中に人間が取り返すと stop_reason: control_lost で終わる
// ことを確かめる。
func TestControlLost(t *testing.T) {
	s, inst := guiSession(t)
	s.must("control.acquire", nil, nil)
	done := make(chan *Observation, 1)
	go func() {
		p, _ := json.Marshal(map[string]any{"condition": "[$0011] == $FF", "max_frames": 216000})
		res, err := s.h.Dispatch(context.Background(), s.conn, "exec.run_until", p)
		if err != nil {
			t.Error(err)
			done <- nil
			return
		}
		done <- res.(*Observation)
	}()
	time.Sleep(50 * time.Millisecond)
	s.h.TakeBack(inst)
	select {
	case ob := <-done:
		if ob == nil || ob.StopReason != StopControlLost {
			t.Fatalf("結果 = %+v", ob)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取り返しても進行が終わらない")
	}
	waitFor(t, "人間の Real-Time で走る", func() bool { return !inst.Emu.Status().Paused })
}

// TestRunPause は GUI 版の exec.run と exec.pause を確かめ、headless で断ることを
// 確かめる。
func TestRunPause(t *testing.T) {
	s, inst := guiSession(t)
	s.must("control.acquire", nil, nil)
	var st ControlStatus
	s.must("exec.run", nil, &st)
	if st.Mode != ModeRealTime || st.Owner != "agent" {
		t.Errorf("exec.run = %+v", st)
	}
	waitFor(t, "走る", func() bool { return !inst.Emu.Status().Paused })
	s.must("exec.pause", nil, nil)
	if st := inst.ControlStatus(); st.Mode != ModeAgentPaced {
		t.Errorf("exec.pause の後 = %+v", st)
	}
	h := newSession(t)
	if err := h.call("exec.run", nil, nil); err == nil || err.Kind != KindUnsupportedHeadless {
		t.Errorf("headless の exec.run: %v", err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s を待てない", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
