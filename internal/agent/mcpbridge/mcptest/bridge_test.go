package mcptest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/mcpbridge"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
	"github.com/takaakimizuno/shogun-emulator/internal/emu"
)

// newHost は headless の Host を作る。
func newHost(t *testing.T) *agent.Host {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	h := agent.NewHost(agent.Options{
		Kind: agent.KindServe, Server: "shogun test",
		EmuConfig: emu.Config{
			Emulation: cfg.Emulation, Input: cfg.Input, Paths: cfg.Paths, State: cfg.State,
			Movie: cfg.Movie, Debug: cfg.Debug,
			Dirs: config.Paths{Config: dir, Data: dir, Cache: dir, Logs: dir, Screenshots: dir},
		},
	})
	t.Cleanup(h.Close)
	return h
}

// gameROM はテスト用 ca65 プロジェクトの ROM を一時ディレクトリに写す。
func gameROM(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../../testdata/agent/game.nes")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "game.nes")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// mcpSession はプロセス内の Host にブリッジをつなぎ、MCP のクライアントの
// セッションを返す。stdio の代わりにメモリ上のパイプでつなぐ。
func mcpSession(t *testing.T, h *agent.Host) (*mcp.ClientSession, *mcpbridge.Bridge) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	client, closeFn, err := rpc.InProcess(ctx, h, "mcp-test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := mcpbridge.New(ctx, client, mcpbridge.Options{Version: "test", Kind: "serve"})
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		_ = b.Run(ctx, st)
	}()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		cancel()
		<-ran
		closeFn()
	})
	return cs, b
}

// callTool はツールを呼ぶ。
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// TestToolsList は全 Agent Command がツールとして出ることを確かめる（フェーズ
// 17 の完了判定）。
func TestToolsList(t *testing.T) {
	h := newHost(t)
	cs, b := mcpSession(t, h)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" {
			t.Errorf("%s に説明が無い", tool.Name)
		}
	}
	for _, spec := range agent.NewDefaultRegistry().All() {
		want := spec.Headless && !slices.Contains([]string{"session.hello", "events.subscribe", "events.unsubscribe"}, spec.Name)
		if got := slices.Contains(names, agent.MCPName(spec.Name)); got != want {
			t.Errorf("%s がツールに %v（期待 %v）", spec.Name, got, want)
		}
	}
	if len(b.Tools()) != len(names) {
		t.Errorf("ブリッジのツールの数 = %d、一覧 = %d", len(b.Tools()), len(names))
	}
}

// roundTripScript は往復テストの要求の列。
var roundTripScript = []struct {
	method string
	params string
}{
	{"exec.run_until", `{"condition":"[frame_count].w == 20"}`},
	{"exec.step", `{"frames":2,"input":"A","observe":{"include":["image"],"scale":1}}`},
	{"mem.read", `{"loc":"score","length":3}`},
	{"mem.write", `{"loc":"$0310","value":[1,2,3]}`},
	{"mem.read", `{"loc":"$0310","length":3}`},
	{"gamestate.get", `{"names":["sym:player_x"]}`},
	{"mem.read", `{"loc":"no_such_name"}`},
}

// TestRoundTrip は同じ要求を JSON-RPC と MCP ブリッジで送り、同じ結果を得ることを
// 確かめる（フェーズ 17 の完了判定。shogun ctl との一致は cmd/shogun のテスト）。
func TestRoundTrip(t *testing.T) {
	rom := gameROM(t)
	ctx := context.Background()

	// JSON-RPC
	h1 := newHost(t)
	c1, close1, err := rpc.InProcess(ctx, h1, "rpc")
	if err != nil {
		t.Fatal(err)
	}
	defer close1()
	if err := c1.Call(ctx, "instance.create", map[string]any{"rom": rom, "deterministic": true}, nil); err != nil {
		t.Fatal(err)
	}

	// MCP
	h2 := newHost(t)
	cs, _ := mcpSession(t, h2)
	if r := callTool(t, cs, "instance_create", map[string]any{"rom": rom, "deterministic": true}); r.IsError {
		t.Fatalf("instance_create: %+v", r.Content)
	}

	for _, step := range roundTripScript {
		var params map[string]any
		json.Unmarshal([]byte(step.params), &params)
		raw, rpcErr := c1.CallRaw(ctx, step.method, params)
		res := callTool(t, cs, agent.MCPName(step.method), params)
		if rpcErr != nil {
			kind, _, _ := rpc.ErrorKind(rpcErr)
			if !res.IsError {
				t.Errorf("%s: JSON-RPC はエラー（%s）、MCP は成功", step.method, kind)
				continue
			}
			var e map[string]string
			json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &e)
			if e["error"] != kind {
				t.Errorf("%s: エラーの種類 %s と %s", step.method, kind, e["error"])
			}
			continue
		}
		if res.IsError {
			t.Errorf("%s: MCP がエラー: %+v", step.method, res.Content[0])
			continue
		}
		want, images, err := mcpbridge.StripImages(raw)
		if err != nil {
			t.Fatal(err)
		}
		var got any
		data, _ := json.Marshal(res.StructuredContent)
		json.Unmarshal(data, &got)
		if !reflect.DeepEqual(normalize(want), normalize(got)) {
			t.Errorf("%s の結果が違う:\nJSON-RPC %v\nMCP      %v", step.method, want, got)
		}
		var mcpImages [][]byte
		for _, c := range res.Content[1:] {
			img, ok := c.(*mcp.ImageContent)
			if !ok || img.MIMEType != "image/png" {
				t.Errorf("%s: 画像でない内容 %T", step.method, c)
				continue
			}
			mcpImages = append(mcpImages, img.Data)
		}
		if !reflect.DeepEqual(images, mcpImages) {
			t.Errorf("%s: 画像が違う（JSON-RPC %d 枚、MCP %d 枚）", step.method, len(images), len(mcpImages))
		}
		if step.method == "exec.step" && len(mcpImages) != 1 {
			t.Errorf("exec_step に画像が付かない")
		}
	}
}

// normalize は数の表現を揃える。
func normalize(v any) any {
	data, _ := json.Marshal(v)
	var out any
	json.Unmarshal(data, &out)
	return out
}

// TestCancelNotification は MCP のキャンセル通知で run_until が止まることを
// 確かめる（フェーズ 17 の完了判定）。
func TestCancelNotification(t *testing.T) {
	h := newHost(t)
	cs, _ := mcpSession(t, h)
	callTool(t, cs, "instance_create", map[string]any{"rom": gameROM(t)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "exec_run_until",
			Arguments: map[string]any{"condition": "A == $FF && X == $FF && Y == $FE", "max_frames": 216000}})
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	// 取り消しが届いていれば、次の進行はすぐに受け付けられる（届いていなければ
	// run_until が終わるまで待たされる）。
	finished := make(chan *mcp.CallToolResult, 1)
	go func() { finished <- callTool(t, cs, "exec_step", map[string]any{"observe": false}) }()
	select {
	case r := <-finished:
		var ob agent.Observation
		json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &ob)
		if r.IsError || ob.StopReason != agent.StopFramesDone {
			t.Errorf("取り消しの後の exec_step = %s", r.Content[0].(*mcp.TextContent).Text)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("キャンセル通知で run_until が止まらない")
	}
	var r agent.PollResult
	_ = r
}
