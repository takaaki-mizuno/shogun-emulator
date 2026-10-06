package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// TestDispatchErrors は振り分けの共通の誤りを確かめる。
func TestDispatchErrors(t *testing.T) {
	h := newTestHost(t, KindServe)
	c := h.Connect()
	if err := call(t, h, c, "no.such", nil, nil); err == nil || err.Kind != KindMethodNotFound {
		t.Errorf("未知の名前: %v", err)
	}
	if err := call(t, h, c, "instance.create", map[string]any{"rom": 5}, nil); err == nil || err.Kind != KindInvalidParams {
		t.Errorf("引数の型の誤り: %v", err)
	}
	if err := call(t, h, c, "control.status", nil, nil); err == nil || err.Kind != KindInstanceNotFound {
		t.Errorf("Instance が無いとき: %v", err)
	}
	rom := writeTestROM(t)
	mustCall(t, h, c, "instance.create", map[string]any{"rom": rom}, nil)
	mustCall(t, h, c, "instance.create", map[string]any{"rom": rom}, nil)
	if err := call(t, h, c, "control.status", nil, nil); err == nil || err.Kind != KindInstanceRequired {
		t.Errorf("Instance が 2 つのとき: %v", err)
	}
	if err := call(t, h, c, "control.status", map[string]any{"instance": "i9"}, nil); err == nil || err.Kind != KindInstanceNotFound {
		t.Errorf("無い Instance: %v", err)
	}
	if err := call(t, h, c, "instance.create", map[string]any{"rom": rom, "ram_init": "bogus"}, nil); err == nil || err.Kind != KindInvalidParams {
		t.Errorf("知らない ram_init: %v", err)
	}
}

// TestControlRequired は Control が要る分類の Agent Command を、Control を
// 持たない接続から呼ぶと control_required になることを確かめる。
func TestControlRequired(t *testing.T) {
	h := newTestHost(t, KindServe)
	h.registry.Register(CommandSpec{
		Name: "test.advance", Class: ClassAdvance, Params: InstanceParam{}, Headless: true, Target: true,
		Handler: func(*Context, json.RawMessage) (any, error) { return "ok", nil },
	})
	owner, other := h.Connect(), h.Connect()
	mustCall(t, h, owner, "instance.create", map[string]any{"rom": writeTestROM(t)}, nil)
	var res string
	mustCall(t, h, owner, "test.advance", nil, &res)
	if err := call(t, h, other, "test.advance", nil, nil); err == nil || err.Kind != KindControlRequired {
		t.Errorf("Control を持たない接続: %v", err)
	}
}

// TestGUIHostRefusesInstanceManagement は GUI 版の Host が Instance の作成・
// Fork・閉じる操作を断ることを確かめる（設計書 14 編 §14.3.1）。
func TestGUIHostRefusesInstanceManagement(t *testing.T) {
	h := newTestHost(t, KindGUI)
	src := newTestHost(t, KindServe)
	owner := src.Connect()
	mustCall(t, src, owner, "instance.create", map[string]any{"rom": writeTestROM(t)}, nil)
	inst := h.AttachGUI(src.Instances()[0].Emu)
	if inst.ID != "i1" {
		t.Errorf("GUI の Instance の ID = %s, 期待 i1", inst.ID)
	}
	c := h.Connect()
	for _, m := range []string{"instance.create", "instance.fork", "instance.close"} {
		if err := call(t, h, c, m, nil, nil); err == nil || err.Kind != KindUnsupportedInGUI {
			t.Errorf("%s: %v", m, err)
		}
	}
	var list []InstanceInfo
	mustCall(t, h, c, "instance.list", nil, &list)
	if len(list) != 1 || list[0].Control.Owner != "human" || list[0].Control.Mode != ModeRealTime {
		t.Errorf("GUI の Instance = %+v", list)
	}
	// エージェントが奪うと Agent-Paced、返すと人間の Real-Time に戻る。
	var st ControlStatus
	mustCall(t, h, c, "control.acquire", nil, &st)
	if st.Owner != "agent" || st.Mode != ModeAgentPaced {
		t.Errorf("取得後 = %+v", st)
	}
	mustCall(t, h, c, "control.release", nil, &st)
	if st.Owner != "human" || st.Mode != ModeRealTime {
		t.Errorf("返却後 = %+v", st)
	}
	mustCall(t, h, c, "control.acquire", nil, nil)
	if !h.TakeBack(inst) || inst.ControlStatus().Owner != "human" {
		t.Error("人間が取り返せない")
	}
}

// TestInstanceLifecycle は作成・一覧・閉じる操作を確かめる。
func TestInstanceLifecycle(t *testing.T) {
	h := newTestHost(t, KindServe)
	c := h.Connect()
	var info InstanceInfo
	mustCall(t, h, c, "instance.create", map[string]any{"rom": writeTestROM(t), "deterministic": true}, &info)
	if info.ID != "i1" || info.ROM != "agent" || !info.Loaded || !info.Paused {
		t.Errorf("作成した Instance = %+v", info)
	}
	if info.Control.Owner != "agent" || info.Control.Conn != c.ID {
		t.Errorf("作った接続が Control を持たない: %+v", info.Control)
	}
	mustCall(t, h, c, "instance.close", map[string]any{"instance": "i1"}, nil)
	mustCall(t, h, c, "instance.create", map[string]any{"rom": writeTestROM(t)}, &info)
	if info.ID != "i2" {
		t.Errorf("閉じた ID を再利用した: %s", info.ID)
	}
	var list []InstanceInfo
	mustCall(t, h, c, "instance.list", nil, &list)
	if len(list) != 1 || list[0].ID != "i2" {
		t.Errorf("一覧 = %+v", list)
	}
}

// TestInstanceLimit は上限を超える作成を断ることを確かめる。
func TestInstanceLimit(t *testing.T) {
	h := newTestHost(t, KindServe)
	c := h.Connect()
	rom := writeTestROM(t)
	for range 4 {
		mustCall(t, h, c, "instance.create", map[string]any{"rom": rom}, nil)
	}
	if err := call(t, h, c, "instance.create", map[string]any{"rom": rom}, nil); err == nil || err.Kind != KindLimitExceeded {
		t.Errorf("上限を超えた作成: %v", err)
	}
	if err := call(t, h, c, "instance.fork", map[string]any{"instance": "i1"}, nil); err == nil || err.Kind != KindLimitExceeded {
		t.Errorf("上限を超えた Fork: %v", err)
	}
}

// TestControlContention は 2 つの接続が同時に Control を取ろうとすると
// 一方だけが得ることと、切断で Control が返ることを確かめる。
func TestControlContention(t *testing.T) {
	h := newTestHost(t, KindServe)
	creator := h.Connect()
	mustCall(t, h, creator, "instance.create", map[string]any{"rom": writeTestROM(t)}, nil)
	mustCall(t, h, creator, "control.release", nil, nil)

	a, b := h.Connect(), h.Connect()
	var wg sync.WaitGroup
	errs := make([]*Error, 2)
	for i, c := range []*Conn{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = h.Dispatch(context.Background(), c, "control.acquire", nil)
		}()
	}
	wg.Wait()
	got, held := 0, 0
	for _, e := range errs {
		switch {
		case e == nil:
			got++
		case e.Kind == KindControlHeld:
			held++
		}
	}
	if got != 1 || held != 1 {
		t.Fatalf("得た数 %d、control_held の数 %d（期待 1 と 1）: %v", got, held, errs)
	}
	winner := a
	if errs[0] != nil {
		winner = b
	}
	// 同じ接続がもう一度取っても誤りにしない。
	mustCall(t, h, winner, "control.acquire", nil, nil)
	h.Disconnect(winner)
	if st := h.Instances()[0].ControlStatus(); st.Owner != "none" {
		t.Errorf("切断しても Control が返らない: %+v", st)
	}
	loser := a
	if winner == a {
		loser = b
	}
	mustCall(t, h, loser, "control.acquire", nil, nil)
}

// TestForkCopiesState は Fork した 2 つの Instance を同じだけ進めると
// 状態が一致し、一方だけを進めるともう一方が変わらないことを確かめる。
func TestForkCopiesState(t *testing.T) {
	h := newTestHost(t, KindServe)
	c := h.Connect()
	mustCall(t, h, c, "instance.create", map[string]any{"rom": writeTestROM(t), "deterministic": true}, nil)
	src := h.Instances()[0]
	stepFrames(t, src, 30)

	var fr ForkResult
	mustCall(t, h, c, "instance.fork", map[string]any{"instance": "i1"}, &fr)
	if fr.ID != "i2" || fr.Control.Conn != c.ID {
		t.Errorf("Fork の結果 = %+v", fr)
	}
	dst, _ := h.Instance("i2")
	if stateHash(t, src) != stateHash(t, dst) {
		t.Fatal("Fork した直後の状態が一致しない")
	}
	if src.shared == nil || src.shared != dst.shared {
		t.Error("Symbol を共有していない")
	}
	stepFrames(t, src, 10)
	stepFrames(t, dst, 10)
	if stateHash(t, src) != stateHash(t, dst) {
		t.Error("同じだけ進めた状態が一致しない")
	}
	before := stateHash(t, dst)
	stepFrames(t, src, 5)
	if stateHash(t, dst) != before {
		t.Error("元を進めると Fork した側が変わった")
	}
	if stateHash(t, src) == before {
		t.Error("元が進んでいない")
	}
	// 共有物は最後の Instance が閉じたときに捨てる。
	mustCall(t, h, c, "instance.close", map[string]any{"instance": "i1"}, nil)
	if len(h.shared) != 1 {
		t.Errorf("共有物の数 = %d, 期待 1", len(h.shared))
	}
	mustCall(t, h, c, "instance.close", map[string]any{"instance": "i2"}, nil)
	if len(h.shared) != 0 {
		t.Errorf("共有物が残っている: %d", len(h.shared))
	}
}

// TestForkWithoutROM は ROM を読み込んでいない Instance の Fork を断る
// ことを確かめる。
func TestForkWithoutROM(t *testing.T) {
	h := newTestHost(t, KindServe)
	c := h.Connect()
	mustCall(t, h, c, "instance.create", nil, nil)
	if err := call(t, h, c, "instance.fork", nil, nil); err == nil || err.Kind != KindNotLoaded {
		t.Errorf("ROM の無い Fork: %v", err)
	}
}

// TestSessionCommands は session.commands が全 Agent Command を返すことを
// 確かめる。
func TestSessionCommands(t *testing.T) {
	h := newTestHost(t, KindServe)
	c := h.Connect()
	var infos []CommandInfo
	mustCall(t, h, c, "session.commands", nil, &infos)
	if len(infos) != len(h.registry.All()) {
		t.Fatalf("数 = %d, 期待 %d", len(infos), len(h.registry.All()))
	}
	for _, info := range infos {
		if info.DescJA == "" || info.DescEN == "" || info.Params == nil {
			t.Errorf("%s: 説明かスキーマが無い", info.Name)
		}
	}
}
