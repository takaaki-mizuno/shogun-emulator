//go:build !darwin

package ui

// installOpenFileHandler は macOS 以外では何もしない。Windows と Linux では
// 関連付けから開いたファイルが起動引数で届く。
func installOpenFileHandler() {}
