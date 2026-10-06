package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// TestMCPArgs は --attach の後ろの PID を読むことを確かめる。
func TestMCPArgs(t *testing.T) {
	cases := map[string][]string{
		"--attach":            {"--attach"},
		"--attach 123":        {"--attach", "--attach-pid", "123"},
		"--attach=45 --rom x": {"--attach", "--attach-pid", "45", "--rom", "x"},
		"--attach --rom x":    {"--attach", "--rom", "x"},
	}
	for in, want := range cases {
		if got := mcpArgs(strings.Fields(in)); !slices.Equal(got, want) {
			t.Errorf("%q = %v, 期待 %v", in, got, want)
		}
	}
	if code, _, _ := runCLI(t, "mcp", "--log", "stdout"); code != exitBadArgs {
		t.Errorf("mcp --log stdout の終了コード = %d", code)
	}
	if code, _, _ := runCLI(t, "mcp", "--attach", "--rom", "x.nes"); code != exitBadArgs {
		t.Errorf("--attach と --rom の併用の終了コード = %d", code)
	}
	if code, _, _ := runCLI(t, "mcp", "--attach"); code != exitROMError {
		t.Errorf("接続先が無い --attach の終了コード = %d", code)
	}
}

// TestAgentFlags は --agent と --agent-listen が設定を上書きすることを確かめる。
func TestAgentFlags(t *testing.T) {
	opts, _, ok := parseArgs([]string{"--agent", "--agent-listen", "tcp:127.0.0.1:0"}, &bytes.Buffer{})
	if !ok {
		t.Fatal("解釈できない")
	}
	ov, _, err := optionOverrides(opts)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	for _, o := range ov {
		o.Apply(c)
	}
	if !c.Agent.Enabled || c.Agent.Listen != "tcp:127.0.0.1:0" {
		t.Errorf("上書きの結果 = %+v", c.Agent)
	}
	opts, _, _ = parseArgs([]string{"--agent-listen", "tcp:0.0.0.0:1"}, &bytes.Buffer{})
	if _, _, err := optionOverrides(opts); err == nil {
		t.Error("127.0.0.1 以外を受け付けた")
	}
}

// normalizeInstance は結果の JSON の Instance の ID をそろえる。
func normalizeInstance(t *testing.T, raw []byte) any {
	t.Helper()
	s := strings.ReplaceAll(strings.ReplaceAll(string(raw), `"i1"`, `"*"`), `"i2"`, `"*"`)
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("JSON を解釈できない: %v\n%s", err, raw)
	}
	// ctl の接続は Control を自動で得るため、その control_changed の分だけ
	// events_pending が違う。比べない。
	// diagnostics は接続ごとの前回の Observation 以降のもの（設計書 14 編
	// §14.20.3）であり、接続の違う 2 つでは違う。比べない。
	if m, ok := v.(map[string]any); ok {
		delete(m, "events_pending")
		delete(m, "diagnostics")
	}
	return v
}

// TestCtlMatchesJSONRPC は同じ要求を shogun ctl と JSON-RPC で送り、同じ結果を
// 得ることを確かめる（フェーズ 17 の往復テスト。MCP との一致は
// internal/agent/mcpbridge/mcptest）。
func TestCtlMatchesJSONRPC(t *testing.T) {
	r := startServe(t)
	rom := writeLoopROM(t)
	ctx := context.Background()
	d, err := rpc.FindDiscovery(strings.TrimSuffix(strings.TrimSuffix(r.Discovery, "/"+filepathBase(r.Discovery)), "/agent"), 0)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := rpc.Connect(ctx, d, "rpc", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for range 2 {
		if err := c.Call(ctx, "instance.create", map[string]any{"rom": rom, "deterministic": true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// i2 の Control を手放し、ctl の接続が得られるようにする。
	if err := c.Call(ctx, "control.release", map[string]any{"instance": "i2"}, nil); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		method string
		args   []string
		params map[string]any
	}{
		{"exec.step", []string{"exec", "step", "--frames", "5", "--json", `{"observe":{"include":["image"],"scale":1}}`},
			map[string]any{"frames": 5, "observe": map[string]any{"include": []string{"image"}, "scale": 1}}},
		{"mem.write", []string{"mem", "write", "$0300", "--value", "[1,2]"}, map[string]any{"loc": "$0300", "value": []int{1, 2}}},
		{"mem.read", []string{"mem", "read", "$0300", "4"}, map[string]any{"loc": "$0300", "length": 4}},
		{"exec.run_until", []string{"exec", "run-until", "[$2002] >= 0", "--max-frames", "3"},
			map[string]any{"condition": "[$2002] >= 0", "max_frames": 3}},
	}
	for _, s := range steps {
		raw, err := c.CallRaw(ctx, s.method, withInstance(s.params, "i1"))
		if err != nil {
			t.Fatalf("%s: %v", s.method, err)
		}
		code, out, errOut := runCLI(t, append(append([]string{"ctl"}, s.args...), "--instance", "i2")...)
		if code != ctlOK {
			t.Fatalf("ctl %s: %d %s", s.method, code, errOut)
		}
		want := normalizeInstance(t, imagesToSizes(t, raw))
		got := normalizeInstance(t, []byte(out))
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s の結果が違う:\nJSON-RPC %v\nctl      %v", s.method, want, got)
		}
	}
	// エラーの種類も一致する。
	_, rpcErr := c.CallRaw(ctx, "mem.read", map[string]any{"loc": "nope", "instance": "i1"})
	kind, _, _ := rpc.ErrorKind(rpcErr)
	code, _, errOut := runCLI(t, "ctl", "mem", "read", "nope", "--instance", "i2")
	if code != ctlCommandErr || !strings.Contains(errOut, kind) {
		t.Errorf("エラー: JSON-RPC %s、ctl %d %s", kind, code, errOut)
	}
}

func withInstance(p map[string]any, id string) map[string]any {
	out := map[string]any{"instance": id}
	for k, v := range p {
		out[k] = v
	}
	return out
}

func filepathBase(p string) string { return p[strings.LastIndex(p, "/")+1:] }

// imagesToSizes は ctl の表示と同じく、画像を大きさの表示に置き換える。
func imagesToSizes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	json.Unmarshal(raw, &v)
	v = replaceImages(v, func(data []byte) string { return "<画像 " + itoaTest(len(data)) + " バイト>" })
	out, _ := json.Marshal(v)
	return out
}

func itoaTest(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestEventsWatch は shogun ctl events watch が通知を 1 行ずつ表示することを
// 確かめる。
func TestEventsWatch(t *testing.T) {
	startServe(t)
	rom := writeLoopROM(t)
	ctlJSON(t, nil, "instance", "create", rom)
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- runCtl(ctx, []string{"events", "watch", "--kinds", "breakpoint_hit"}, &out, &bytes.Buffer{})
	}()
	// 購読が届くのを待つ。
	time.Sleep(300 * time.Millisecond)
	var bp agent.BreakpointInfo
	ctlJSON(t, &bp, "debug", "bp", "add", "exec", "$8005") // ループの先頭
	// ブレークポイントは ROM ごとのシンボルファイルに保存される。同じ ROM を
	// 使う他のテストに残さないよう外す。
	defer ctlJSON(t, nil, "debug", "bp", "remove", itoaTest(bp.ID))
	var ob agent.Observation
	ctlJSON(t, &ob, "exec", "step")
	if ob.StopReason != agent.StopBreakpoint {
		t.Fatalf("止まらない: %+v", ob)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), `"kind":"breakpoint_hit"`) {
		if time.Now().After(deadline) {
			t.Fatalf("通知が表示されない: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(out.String(), "control_changed") {
		t.Error("--kinds で絞り込んでいない")
	}
	cancel()
	if code := <-done; code != ctlOK {
		t.Errorf("終了コード = %d", code)
	}
}

// syncBuffer は複数のゴルーチンから書ける bytes.Buffer。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
