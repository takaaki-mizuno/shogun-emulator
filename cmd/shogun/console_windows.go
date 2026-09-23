//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachParentProcess は AttachConsole に渡す ATTACH_PARENT_PROCESS（(DWORD)-1）。
const attachParentProcess = ^uintptr(0) & 0xFFFFFFFF

// procAttachConsole は kernel32.dll の AttachConsole。golang.org/x/sys/windows は
// この関数を持たないため、DLL から呼ぶ。
var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachConsoleIfNeeded は引数付きで起動されたとき、親プロセスのコンソールへ
// 接続して標準出力と標準エラー出力を有効にする（設計書 11 編 §11.5.3）。
//
// -H windowsgui でビルドした実行ファイルはコンソールを持たない。GUI として
// 起動したときにコンソールウィンドウを出さないためである。その代わり、
// コマンドラインから --version や --headless を指定したときの出力が見えない。
func attachConsoleIfNeeded() {
	if len(os.Args) <= 1 {
		return
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		// 親にコンソールが無い（エクスプローラから起動した）。
		return
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout = out
	os.Stderr = out
}
