package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// subcommands は Agent Interface のサブコマンド（設計書 11 編 §11.5.4）。
var subcommands = []string{"serve", "mcp", "ctl", "run"}

// isSubcommand は第 1 引数がサブコマンドかを返す。同じ名前の ROM は
// ./serve のようにパスで渡す。
func isSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, s := range subcommands {
		if args[0] == s {
			return true
		}
	}
	return false
}

// subOptions はサブコマンドの引数。
type subOptions struct {
	options
	// serve
	listen string
	// mcp
	attach    bool
	attachPID int
	rom       string
	// ctl
	pid  int
	text bool
	out  string
	json string
	help bool
}

// subFlagSet はサブコマンドが共通に受け付けるオプションを持つ FlagSet を作る
// （設計書 11 編 §11.5.4 の表）。
func subFlagSet(name string, o *subOptions, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(appName+" "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.configPath, "config", "", "設定ファイルのパス")
	fs.BoolVar(&o.portable, "portable", false, "実行ファイルのディレクトリを保存先にする")
	fs.StringVar(&o.region, "region", "", "リージョン（auto, ntsc, pal, dendy）")
	fs.StringVar(&o.ramInit, "ram-init", "", "RAM の初期化パターン（zero, ff, pattern, random）")
	fs.Uint64Var(&o.ramSeed, "ram-seed", 0, "RAM 初期化の乱数シード")
	fs.BoolVar(&o.deterministic, "deterministic", false, "値が定まらない状態をすべて固定値にする")
	fs.StringVar(&o.logOutput, "log", "", "ログの出力先（stdout, stderr, file）")
	fs.StringVar(&o.logDir, "log-dir", "", "ログファイルの保存先")
	fs.StringVar(&o.logCategories, "log-categories", "", "ログカテゴリ（カンマ区切り）")
	fs.BoolVar(&o.help, "help", false, "このヘルプを表示する（-h も同じ）")
	return fs
}

// runSubcommand はサブコマンドを実行して終了コードを返す。
func runSubcommand(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr, nil)
	case "ctl":
		return runCtl(ctx, args[1:], stdout, stderr)
	case "mcp":
		return runMCP(ctx, args[1:], stderr)
	case "run":
		return runScenarios(ctx, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "%s: %s はまだ実装していない\n", appName, args[0])
	return exitBadArgs
}

// subStore はサブコマンドの設定を読む。パスの解決・設定ファイル・環境変数と
// 引数の上書きを GUI の起動と同じ順に行う（設計書 11 編 §11.6）。
func subStore(o subOptions, stderr io.Writer) (*config.Store, int, bool) {
	overrides, warnings, err := optionOverrides(o.options)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return nil, exitBadArgs, false
	}
	store, w2, err := loadStore(o.options, overrides, os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return nil, exitROMError, false
	}
	for _, w := range append(w2, warnings...) {
		fmt.Fprintf(stderr, "%s: %s\n", appName, w)
	}
	return store, exitOK, true
}

// runServe は headless の Host を持ち、JSON-RPC で待ち受ける。ctx が
// 終わるまで戻らない。ready は待ち受けを始めたときに呼ぶ（テスト用。nil 可）。
func runServe(ctx context.Context, args []string, stdout, stderr io.Writer, ready func(*rpc.Running)) int {
	var o subOptions
	fs := subFlagSet("serve", &o, stderr)
	fs.StringVar(&o.listen, "listen", "", "待ち受け先（unix、tcp:127.0.0.1:PORT、tcp:[::1]:PORT）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitBadArgs
	}
	if o.help {
		fmt.Fprintf(stderr, "使い方: %s serve [--listen ADDR] [オプション]\n\n", appName)
		fs.PrintDefaults()
		return exitOK
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "%s: serve は位置引数を受け付けない（ROM は instance.create で読み込む）\n", appName)
		return exitBadArgs
	}
	if o.listen != "" && !config.ValidAgentListen(o.listen) {
		fmt.Fprintf(stderr, "%s: --listen %q は使えない（unix、tcp:127.0.0.1:PORT、tcp:[::1]:PORT）\n", appName, o.listen)
		return exitBadArgs
	}
	store, code, ok := subStore(o, stderr)
	if !ok {
		return code
	}
	cfg := store.Config()
	logOut, err := openLog(cfg, store.Paths)
	if err != nil {
		fmt.Fprintf(stderr, "%s: ログを開けないため標準エラー出力へ出す: %v\n", appName, err)
		logOut = nil
	}
	if logOut != nil {
		defer logOut.Close()
	}
	listen := cfg.Agent.Listen
	if o.listen != "" {
		listen = o.listen
	}

	host := agent.NewHost(agent.Options{
		Kind: agent.KindServe, Server: appName + " " + version,
		MaxInstances: cfg.Agent.MaxInstances,
		EmuConfig:    emuConfig(cfg, store.Paths, logWriter(logOut), stderr),
	})
	defer host.Close()
	running, err := rpc.Start(host, listen, store.Paths.Cache, "")
	if err != nil {
		fmt.Fprintf(stderr, "%s: 待ち受けを始められない: %v\n", appName, err)
		return exitBadArgs
	}
	defer running.Close()
	fmt.Fprintf(stderr, "%s: %s で待ち受ける（発見ファイル %s）\n", appName, running.Endpoint, running.Discovery)
	if ready != nil {
		ready(running)
	}
	<-ctx.Done()
	return exitOK
}
