package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/takaakimizuno/shogun-emulator/internal/agent"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/rpc"
	"github.com/takaakimizuno/shogun-emulator/internal/agent/scenario"
)

// 終了コード（設計書 11 編 §11.5.2、設計書 14 編 §14.17.3）。
const (
	exitScenarioFailed  = scenario.ExitFailed
	exitScenarioInvalid = scenario.ExitInvalidFile
)

// runScenarios は Scenario を headless で実行する（shogun run）。
//
// プロセス内に Host を持ち、net.Pipe でつないだ JSON-RPC のクライアントとして
// 実行する（設計書 14 編 §14.2.1）。MCP や shogun ctl と同じ経路を通すため、
// Scenario で合格した操作は他の Transport でも同じ結果になる。
func runScenarios(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	var o subOptions
	var junit, reproDir string
	var updateGolden bool
	fs := subFlagSet("run", &o, stderr)
	fs.StringVar(&junit, "junit", "", "JUnit XML の書き出し先")
	fs.BoolVar(&updateGolden, "update-golden", false, "お手本の PNG が無いか一致しないとき、今の画面で上書きする")
	fs.StringVar(&reproDir, "repro-dir", "", "失敗した Scenario の Repro の書き出し先（省くとデータディレクトリの repros/）")
	if err := fs.Parse(interleaved(fs, args)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitBadArgs
	}
	if o.help {
		fmt.Fprintf(stderr, "使い方: %s run <Scenario ファイル...> [--junit PATH] [--update-golden] [--repro-dir DIR] [オプション]\n\n", appName)
		fmt.Fprintf(stderr, "終了コード: 0 合格、1 ROM などの読み込みの失敗、5 アサーションの失敗、6 Scenario ファイルの不正\n\n")
		fs.PrintDefaults()
		return exitOK
	}
	if fs.NArg() == 0 {
		fmt.Fprintf(stderr, "%s: run には Scenario ファイルを 1 つ以上渡す\n", appName)
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
	host := agent.NewHost(agent.Options{
		Kind: agent.KindServe, Server: appName + " " + version,
		MaxInstances: cfg.Agent.MaxInstances,
		EmuConfig:    emuConfig(cfg, store.Paths, logWriter(logOut), stderr),
	})
	defer host.Close()
	client, closeClient, err := rpc.InProcess(ctx, host, appName+" run")
	if err != nil {
		fmt.Fprintf(stderr, "%s: Host につなげない: %v\n", appName, err)
		return exitROMError
	}
	defer closeClient()

	runner := scenario.NewRunner(client, scenario.Options{UpdateGolden: updateGolden, ReproDir: reproDir})
	var results []scenario.FileResult
	for _, path := range fs.Args() {
		res := runner.RunFile(ctx, path)
		scenario.WriteFileResult(stdout, res)
		results = append(results, res)
		if ctx.Err() != nil {
			break
		}
	}
	scenario.WriteSummary(stdout, results)
	if junit != "" {
		f, err := os.Create(junit)
		if err == nil {
			err = scenario.WriteJUnit(f, results)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			fmt.Fprintf(stderr, "%s: JUnit XML を書けない: %v\n", appName, err)
		}
	}
	return scenario.ExitCode(results)
}

// interleaved は位置引数の後ろに書いたオプションも読めるよう、オプションを
// 前に並べ直す（shogun run a.yaml --junit r.xml）。
func interleaved(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' {
			flags = append(flags, a)
			name := a
			for len(name) > 0 && name[0] == '-' {
				name = name[1:]
			}
			if strings.IndexByte(name, '=') >= 0 {
				continue
			}
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(append(flags, "--"), pos...)
}
