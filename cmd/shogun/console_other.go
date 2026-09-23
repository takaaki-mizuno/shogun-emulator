//go:build !windows

package main

// attachConsoleIfNeeded は Windows 以外では何もしない。コンソールは起動した
// 端末のものがそのまま使える。
func attachConsoleIfNeeded() {}
