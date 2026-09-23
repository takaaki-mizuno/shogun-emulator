package main

import (
	"os"
	"strings"
	"testing"

	"github.com/takaakimizuno/shogun-emulator/internal/config"
)

// TestMain はテストをポータブルモードで動かす。
//
// 保存先をテストバイナリのある一時ディレクトリにし、実行者の設定ファイルを
// 読まず、データディレクトリを作らないためである。実行者の環境の SHOGUN_
// で始まる変数も消す。
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, config.EnvPrefix) &&
			!strings.HasPrefix(name, "SHOGUN_UPDATE_") {
			os.Unsetenv(name)
		}
	}
	os.Setenv(config.EnvPortable, "1")
	os.Exit(m.Run())
}
