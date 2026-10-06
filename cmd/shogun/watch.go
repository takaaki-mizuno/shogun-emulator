package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// runEventsWatch は events.subscribe を送り、届いた通知を 1 行ずつ表示し続ける
// （shogun ctl events watch、設計書 14 編 §14.5.3）。ctx が終わったら
// events.unsubscribe を送って終える。
func runEventsWatch(ctx context.Context, o subOptions, args []string, stdout, stderr io.Writer) int {
	var kinds []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--kinds" && i+1 < len(args):
			kinds = config.SplitList(args[i+1])
			i++
		case strings.HasPrefix(a, "--kinds="):
			kinds = config.SplitList(strings.TrimPrefix(a, "--kinds="))
		default:
			fmt.Fprintf(stderr, "%s: events watch の引数 %q を知らない（--kinds だけ）\n", appName, a)
			return exitBadArgs
		}
	}
	store, code, ok := subStore(o, stderr)
	if !ok {
		return code
	}
	d, err := rpc.FindDiscovery(store.Paths.Cache, o.pid)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return ctlConnectFail
	}
	var mu sync.Mutex
	c, _, err := rpc.Connect(ctx, d, "shogun-ctl-watch", func(method string, params json.RawMessage) {
		if method != "events.event" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(stdout, "%s\n", params)
	})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %s へ接続できない: %v\n", appName, d.Endpoint, err)
		return ctlConnectFail
	}
	defer c.Close()
	if err := c.Call(ctx, "events.subscribe", map[string]any{"kinds": kinds}, nil); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return ctlCommandErr
	}
	select {
	case <-ctx.Done():
		_ = c.Call(context.Background(), "events.unsubscribe", nil, nil)
	case <-c.Done():
		fmt.Fprintf(stderr, "%s: 接続先のエミュレータが終了した\n", appName)
	}
	return ctlOK
}
