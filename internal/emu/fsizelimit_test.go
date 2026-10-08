//go:build darwin || linux

package emu

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fsizeChildEnv は子プロセスで実行しているテストの名前を渡す環境変数。
// cmd/shogun の TestMain は SHOGUN_ で始まる変数を消すため、その接頭辞を
// 使わない（消えると子が再び子を作り続ける）。
const fsizeChildEnv = "FSIZE_LIMIT_TEST_CHILD"

// runWithFileSizeLimit は body を、このテストだけを実行する子プロセスで動かす。
// body に渡す limit を呼ぶと、その時点からプロセスが書けるファイルの大きさに
// 上限（RLIMIT_FSIZE）が付く。上限を越える書き込みは EFBIG で失敗する
// （Go のランタイムは SIGXFSZ を無視する）。本番のコードに差し込み口を
// 作らずに、録画の途中で本物の書き込みの失敗を起こすために使う。上限は
// プロセス全体に効くため、親のテストプロセスには付けない。
func runWithFileSizeLimit(t *testing.T, body func(t *testing.T, limit func(n uint64))) {
	t.Helper()
	child, isChild := os.LookupEnv(fsizeChildEnv)
	if isChild && child != t.Name() {
		// 子プロセスの中で別のテストが呼ばれた。孫を作らない。
		t.Skipf("子プロセス（%s）の中では実行しない", child)
	}
	if isChild {
		body(t, func(n uint64) {
			var rl syscall.Rlimit
			if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &rl); err != nil {
				t.Fatal(err)
			}
			rl.Cur = min(n, rl.Max)
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &rl); err != nil {
				t.Fatal(err)
			}
		})
		return
	}
	// 子が戻らないときにテスト全体を止めないよう、時間を区切る。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), fsizeChildEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("子プロセスのテストが失敗した: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: "+t.Name()) {
		t.Fatalf("子プロセスでテストが実行されていない:\n%s", out)
	}
}
