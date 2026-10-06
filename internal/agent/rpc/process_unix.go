//go:build !windows

package rpc

import (
	"errors"
	"os"
	"syscall"
)

// processAlive は pid のプロセスがあるかを返す。
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
