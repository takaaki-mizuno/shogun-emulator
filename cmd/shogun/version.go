package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// ビルド時に -ldflags の -X で埋め込む。埋め込まれなかったときの値は、
// ソースから直接ビルドしたことが分かるものにしてある。
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

// appName は実行ファイルの名前。バージョン表示とヘルプで使う。
const appName = "shogun"

// appTitle は利用者に見せるアプリケーション名。
//
// 音量調整の UI に出る名前として使う。
const appTitle = "将軍エミュレータ"

// printVersion はバージョン情報を書き出す。
//
// -ldflags で埋め込んだ値と、実行ファイルに記録された VCS の情報の両方を
// 出す。前者はリリースビルドで、後者は `go build` や `go install` で
// 直接作ったバイナリで埋まる。どちらの作り方でも由来が分かるようにする。
func printVersion(w io.Writer) {
	fmt.Fprintf(w, "%s %s\n", appName, version)
	fmt.Fprintf(w, "  コミット:     %s\n", commit)
	fmt.Fprintf(w, "  ビルド日時:   %s\n", buildDate)
	fmt.Fprintf(w, "  Go:           %s\n", runtime.Version())
	fmt.Fprintf(w, "  プラットフォーム: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			fmt.Fprintf(w, "  vcs.revision: %s\n", s.Value)
		case "vcs.time":
			fmt.Fprintf(w, "  vcs.time:     %s\n", s.Value)
		case "vcs.modified":
			fmt.Fprintf(w, "  vcs.modified: %s\n", s.Value)
		}
	}
}
