package mcpbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
)

// commandInfo は session.commands の 1 件のうち、ブリッジが使うもの。
type commandInfo struct {
	Name     string          `json:"name"`
	MCPName  string          `json:"mcp_name"`
	DescEN   string          `json:"desc_en"`
	GUI      bool            `json:"gui"`
	Headless bool            `json:"headless"`
	Params   json.RawMessage `json:"params"`
}

// skipped は MCP のツールにしない Agent Command。
//
// session.hello はブリッジが接続のときに済ませる。events.subscribe と
// events.unsubscribe は通知を受け取る接続のためのもので、MCP クライアントの
// 多くはサーバからの通知をエージェントに渡さない（設計書 14 編 §14.14）。
var skipped = []string{"session.hello", "events.subscribe", "events.unsubscribe"}

// Bridge は MCP のツールの呼び出しを JSON-RPC の要求へ変換する。
//
// 1 つの MCP セッションを 1 つの JSON-RPC 接続に対応させる。Control と
// Observation の差分は接続ごとに持つためである。
type Bridge struct {
	client *rpc.Client
	server *mcp.Server
	// names は MCP のツール名から JSON-RPC の名前を引く。名前にアンダースコアを
	// 含むため、文字の置き換えでは戻せない（設計書 14 編 §14.5.4）。
	names map[string]string

	mu    sync.Mutex
	tools []string
}

// Options はブリッジの設定。
type Options struct {
	// Version はサーバの版（MCP の serverInfo に出す）。
	Version string
	// Kind は接続先の Host の種類（gui・serve）。その種類で使えない Agent
	// Command はツールにしない。
	Kind string
}

// New はブリッジを作る。接続先の session.commands からツールの一覧を作る。
// ツールの一覧を接続先から作るのは、--attach 先のプロセスの版と一致させる
// ためである。
func New(ctx context.Context, client *rpc.Client, o Options) (*Bridge, error) {
	var infos []commandInfo
	if err := client.Call(ctx, "session.commands", nil, &infos); err != nil {
		return nil, fmt.Errorf("mcpbridge: Agent Command の一覧を得られない: %w", err)
	}
	b := &Bridge{
		client: client,
		server: mcp.NewServer(&mcp.Implementation{Name: "shogun", Title: "Shogun Emulator", Version: o.Version}, &mcp.ServerOptions{
			Instructions: instructions,
		}),
		names: map[string]string{},
	}
	for _, info := range infos {
		if slices.Contains(skipped, info.Name) {
			continue
		}
		if o.Kind == "gui" && !info.GUI || o.Kind != "gui" && !info.Headless {
			continue
		}
		b.names[info.MCPName] = info.Name
		b.tools = append(b.tools, info.MCPName)
		name := info.Name
		schema := info.Params
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		b.server.AddTool(&mcp.Tool{Name: info.MCPName, Description: info.DescEN, InputSchema: schema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return b.call(ctx, name, req.Params.Arguments), nil
			})
	}
	return b, nil
}

// instructions は MCP クライアントに渡す使い方の要約。
const instructions = `Shogun Emulator: an NES/Famicom emulator you can drive and inspect.
Typical loop: instance_create (headless) or control_acquire (GUI), then exec_step / exec_run_until / exec_input_sequence,
which return an observation (frame, stop_reason, CPU position, watch and Game State changes since your last observation).
Ask for a screenshot only when needed (observe.include ["image"]); prefer obs_sprites / obs_nametable / gamestate_get.
Locations accept $0300, symbol names from the ROM's .dbg file, expressions, bankN:$ADDR and ppu:/oam:/pal:/chr:/prg: prefixes.
Conditions use expressions like "game.mode == 'play' && [score].w >= 100".`

// Tools はツールの名前を登録した順に返す。
func (b *Bridge) Tools() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.tools)
}

// Server は MCP のサーバを返す。
func (b *Bridge) Server() *mcp.Server { return b.server }

// Run は transport で MCP のサーバを動かす。JSON-RPC の接続が切れたら終える。
func (b *Bridge) Run(ctx context.Context, t mcp.Transport) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-b.client.Done():
			// 接続先のプロセスが終わった。MCP のサーバを止める。
			cancel()
		case <-ctx.Done():
		}
	}()
	return b.server.Run(ctx, t)
}

// call は Agent Command を送り、結果を MCP の結果に変換する（設計書 14 編
// §14.5.2）。
//
// MCP のキャンセル通知は SDK が ctx を取り消す形で届く。そのときは exec.cancel を
// 送り、止まった位置の結果（stop_reason: cancelled）を返す。
func (b *Bridge) call(ctx context.Context, name string, args json.RawMessage) *mcp.CallToolResult {
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	raw, err := b.client.CallCancelable(ctx, name, args)
	if err != nil {
		return errorResult(err)
	}
	return toResult(raw)
}

// errorResult はエラーを isError の結果にする。LLM が誤りを読んで直せるよう、
// プロトコルのエラーにしない。
func errorResult(err error) *mcp.CallToolResult {
	text := err.Error()
	if kind, msg, ok := rpc.ErrorKind(err); ok {
		data, _ := json.Marshal(map[string]string{"error": kind, "message": msg})
		text = string(data)
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// toResult は Agent Command の結果を MCP の結果にする。画像（mime が image/ の
// 値）は ImageContent に移し、JSON からは取り除いて大きさだけを残す。Base64 を
// 二重に送らないためである。
func toResult(raw json.RawMessage) *mcp.CallToolResult {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}
	}
	var images []mcp.Content
	v = extractImages(v, &images)
	text, _ := json.Marshal(v)
	content := append([]mcp.Content{&mcp.TextContent{Text: string(text)}}, images...)
	return &mcp.CallToolResult{Content: content, StructuredContent: v}
}

// StripImages は結果の JSON から画像を取り除いた形を返す。MCP の
// structuredContent と同じ形であり、往復のテストで比べるために使う。
func StripImages(raw json.RawMessage) (any, [][]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, nil, err
	}
	var images []mcp.Content
	v = extractImages(v, &images)
	var data [][]byte
	for _, c := range images {
		data = append(data, c.(*mcp.ImageContent).Data)
	}
	return v, data, nil
}

// extractImages は v の中の画像を images に移す。
func extractImages(v any, images *[]mcp.Content) any {
	switch x := v.(type) {
	case map[string]any:
		if mime, ok := x["mime"].(string); ok && strings.HasPrefix(mime, "image/") {
			if s, ok := x["data"].(string); ok {
				if data, err := base64.StdEncoding.DecodeString(s); err == nil {
					*images = append(*images, &mcp.ImageContent{MIMEType: mime, Data: data})
					return map[string]any{"mime": mime, "bytes": len(data), "content_index": len(*images)}
				}
			}
		}
		for k, e := range x {
			x[k] = extractImages(e, images)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = extractImages(e, images)
		}
		return x
	}
	return v
}

// RunStdio は標準入力と標準出力で MCP のサーバを動かす。標準出力は MCP の
// メッセージだけに使う。ログを書くとクライアントがメッセージを読めなくなる。
func (b *Bridge) RunStdio(ctx context.Context) error {
	return b.Run(ctx, &mcp.StdioTransport{})
}
