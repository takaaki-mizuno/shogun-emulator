package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
)

// startServe は shogun serve を動かし、待ち受けを始めるまで待つ。
func startServe(t *testing.T, args ...string) *rpc.Running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan *rpc.Running, 1)
	done := make(chan int, 1)
	var stderr bytes.Buffer
	go func() {
		done <- runServe(ctx, args, &bytes.Buffer{}, &stderr, func(r *rpc.Running) { ready <- r })
	}()
	t.Cleanup(func() {
		cancel()
		if code := <-done; code != exitOK {
			t.Errorf("serve の終了コード = %d", code)
		}
	})
	select {
	case r := <-ready:
		return r
	case code := <-done:
		t.Fatalf("serve が終わった（%d）: %s", code, stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatal("serve が待ち受けを始めない")
	}
	return nil
}

// ctlJSON は shogun ctl を実行し、結果の JSON を v へ読む。
func ctlJSON(t *testing.T, v any, args ...string) {
	t.Helper()
	code, out, errOut := runCLI(t, append([]string{"ctl"}, args...)...)
	if code != ctlOK {
		t.Fatalf("ctl %v の終了コード = %d（%s）", args, code, errOut)
	}
	if v != nil {
		if err := json.Unmarshal([]byte(out), v); err != nil {
			t.Fatalf("ctl %v の出力を解釈できない: %v\n%s", args, err, out)
		}
	}
}

// TestServeAndCtl は shogun serve を起動し、shogun ctl から Instance の作成・
// Fork・一覧・閉じる操作を行えることを確かめる（フェーズ 14 の完了判定）。
func TestServeAndCtl(t *testing.T) {
	r := startServe(t)
	if !strings.HasPrefix(r.Endpoint, "unix:") {
		t.Errorf("既定で Unix ドメインソケットを使っていない: %s", r.Endpoint)
	}
	rom := writeLoopROM(t)

	var info agent.InstanceInfo
	ctlJSON(t, &info, "instance", "create", rom, "--deterministic")
	if info.ID != "i1" || !info.Loaded {
		t.Fatalf("instance.create = %+v", info)
	}
	var fr agent.ForkResult
	ctlJSON(t, &fr, "instance", "fork", "i1")
	if fr.ID != "i2" {
		t.Errorf("instance.fork = %+v", fr)
	}
	var list []agent.InstanceInfo
	ctlJSON(t, &list, "instance", "list")
	if len(list) != 2 || list[0].ID != "i1" || list[1].ID != "i2" {
		t.Errorf("instance.list = %+v", list)
	}
	// ctl の接続が切れたので、Control は誰も持っていない。
	if list[0].Control.Owner != "none" {
		t.Errorf("切断した接続の Control が残る: %+v", list[0].Control)
	}
	var st agent.ControlStatus
	ctlJSON(t, &st, "control", "status", "--instance", "i2")
	if st.Mode != agent.ModeAgentPaced {
		t.Errorf("control.status = %+v", st)
	}
	ctlJSON(t, nil, "instance", "close", "i1")
	ctlJSON(t, &list, "instance", "list")
	if len(list) != 1 || list[0].ID != "i2" {
		t.Errorf("閉じた後の instance.list = %+v", list)
	}

	// --text は人が読む形で出す。
	code, out, _ := runCLI(t, "ctl", "--text", "control", "status", "i2")
	if code != ctlOK || !strings.Contains(out, "mode: agent_paced") {
		t.Errorf("--text の出力 = %q", out)
	}
	// Agent Command の誤りは終了コード 1。
	code, _, errOut := runCLI(t, "ctl", "instance", "close", "i9")
	if code != ctlCommandErr || !strings.Contains(errOut, agent.KindInstanceNotFound) {
		t.Errorf("誤りの終了コード = %d（%s）", code, errOut)
	}
	// 知らない Agent Command は終了コード 2 で候補を示す。
	code, _, errOut = runCLI(t, "ctl", "instance", "nope")
	if code != exitBadArgs || !strings.Contains(errOut, "instance list") {
		t.Errorf("知らない名前の終了コード = %d（%s）", code, errOut)
	}
	// --json と引数の併用では引数が勝つ。
	ctlJSON(t, &st, "--json", `{"instance":"i9"}`, "control", "status", "--instance", "i2")
}

// TestCtlWithoutServer は接続先が無いとき終了コード 2 で終えることを確かめる。
func TestCtlWithoutServer(t *testing.T) {
	code, _, errOut := runCLI(t, "ctl", "instance", "list")
	if code != ctlConnectFail {
		t.Errorf("終了コード = %d（%s）", code, errOut)
	}
}

// TestCtlHelp は登録簿から作ったヘルプを表示することを確かめる。
func TestCtlHelp(t *testing.T) {
	code, out, _ := runCLI(t, "ctl", "--help")
	if code != ctlOK || !strings.Contains(out, "instance fork") || !strings.Contains(out, "control acquire") {
		t.Errorf("一覧のヘルプ = %d %q", code, out)
	}
	code, out, _ = runCLI(t, "ctl", "instance", "create", "--help")
	if code != ctlOK || !strings.Contains(out, "--ram-init") || !strings.Contains(out, "[rom]") {
		t.Errorf("引数のヘルプ = %d %q", code, out)
	}
}

// TestServeRejectsBadListen は使えない待ち受け先を断ることを確かめる。
func TestServeRejectsBadListen(t *testing.T) {
	if code, _, _ := runCLI(t, "serve", "--listen", "tcp:0.0.0.0:0"); code != exitBadArgs {
		t.Errorf("終了コード = %d", code)
	}
	if code, _, _ := runCLI(t, "serve", "game.nes"); code != exitBadArgs {
		t.Errorf("位置引数を受け付けた: %d", code)
	}
}

// TestCtlParams は CLI の引数を params にする規則を確かめる。
func TestCtlParams(t *testing.T) {
	spec, _ := agent.NewDefaultRegistry().Lookup("instance.create")
	p, err := ctlParams(spec, []string{"game.nes", "--ram-seed", "42", "--deterministic", "--ram-init=zero", "--extra", "1.5"}, `{"state":"s.state","ram_seed":1}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"rom": "game.nes", "ram_seed": int64(42), "deterministic": true, "ram_init": "zero", "state": "s.state", "extra": 1.5}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("%s = %#v, 期待 %#v", k, p[k], v)
		}
	}
	if _, err := ctlParams(spec, []string{"a", "b"}, ""); err == nil {
		t.Error("多すぎる位置引数を受け付けた")
	}
	if _, err := ctlParams(spec, []string{"--ram-seed", "x"}, ""); err == nil {
		t.Error("整数でない値を受け付けた")
	}
}
