//go:build windows

package rpc

import "os"

// processAlive は pid のプロセスがあるかを返す。Windows の FindProcess は
// プロセスが無いとき誤りを返す。
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}
