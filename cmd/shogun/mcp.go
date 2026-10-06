package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/mcpbridge"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// mcpArgs は --attach の後ろの PID を --attach-pid に直す。flag は省略できる
// 値を扱えないためである（shogun mcp --attach [PID]）。
func mcpArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--attach" && i+1 < len(args) {
			if _, err := strconv.Atoi(args[i+1]); err == nil {
				out = append(out, "--attach", "--attach-pid", args[i+1])
				i++
				continue
			}
		}
		if v, ok := strings.CutPrefix(a, "--attach="); ok {
			out = append(out, "--attach", "--attach-pid", v)
			continue
		}
		out = append(out, a)
	}
	return out
}

// runMCP は MCP の stdio サーバを動かす（設計書 14 編 §14.5.2）。
//
// 標準出力は MCP のメッセージだけに使う。知らせとログは標準エラーかファイルへ
// 出す。
func runMCP(ctx context.Context, args []string, stderr io.Writer) int {
	var o subOptions
	fs := subFlagSet("mcp", &o, stderr)
	fs.BoolVar(&o.attach, "attach", false, "起動中のエミュレータへ接続する（GUI で AI からの接続を許可したもの）。--attach PID で選ぶ")
	fs.IntVar(&o.attachPID, "attach-pid", 0, "--attach で接続するプロセスの ID")
	fs.StringVar(&o.rom, "rom", "", "起動時に Instance を作って読み込む ROM")
	if err := fs.Parse(mcpArgs(args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitBadArgs
	}
	if o.help {
		fmt.Fprintf(stderr, "使い方: %s mcp [--attach [PID]] [--rom PATH] [オプション]\n\n", appName)
		fs.PrintDefaults()
		return exitOK
	}
	if o.logOutput == config.LogStdout {
		fmt.Fprintf(stderr, "%s: mcp では標準出力を MCP のメッセージに使うため --log=stdout を使えない\n", appName)
		return exitBadArgs
	}
	if o.attach && o.rom != "" {
		fmt.Fprintf(stderr, "%s: --attach と --rom は同時に指定できない\n", appName)
		return exitBadArgs
	}
	store, code, ok := subStore(o, stderr)
	if !ok {
		return code
	}
	cfg := store.Config()
	if cfg.Debug.LogOutput == config.LogStdout {
		// 設定ファイルの指定も標準出力には出さない。
		cfg = cfg.Clone()
		cfg.Debug.LogOutput = config.LogStderr
	}
	logOut, err := openLog(cfg, store.Paths)
	if err != nil {
		logOut = nil
	}
	if logOut != nil {
		defer logOut.Close()
	}

	var client *rpc.Client
	kind := string(agent.KindServe)
	if o.attach {
		d, err := rpc.FindDiscovery(store.Paths.Cache, o.attachPID)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitROMError
		}
		c, hello, err := rpc.Connect(ctx, d, "shogun-mcp", nil)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %s へ接続できない: %v\n", appName, d.Endpoint, err)
			return exitROMError
		}
		defer c.Close()
		client, kind = c, string(hello.Kind)
	} else {
		host := agent.NewHost(agent.Options{
			Kind: agent.KindServe, Server: appName + " " + version,
			MaxInstances: cfg.Agent.MaxInstances, ImageScale: cfg.Agent.ObserveImageScale,
			EmuConfig: emuConfig(cfg, store.Paths, logWriter(logOut), stderr),
		})
		defer host.Close()
		c, closeFn, err := rpc.InProcess(ctx, host, "shogun-mcp")
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
			return exitROMError
		}
		defer closeFn()
		client = c
		if o.rom != "" {
			if err := client.Call(ctx, "instance.create", map[string]any{"rom": o.rom, "deterministic": o.deterministic}, nil); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", appName, err)
				return exitROMError
			}
		}
	}
	b, err := mcpbridge.New(ctx, client, mcpbridge.Options{Version: version, Kind: kind})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return exitROMError
	}
	if err := b.RunStdio(ctx); err != nil && !errors.Is(err, context.Canceled) {
		select {
		case <-client.Done():
			fmt.Fprintf(stderr, "%s: 接続先のエミュレータが終了した\n", appName)
		default:
			fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		}
		return exitROMError
	}
	return exitOK
}
