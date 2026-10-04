//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

// 実装は openfile_darwin.m にある。//export を使うファイルの前置きには
// 定義を書けない。
void shogunInstallOpenHandler(void);
*/
import "C"

//export shogunQueueOpenFile
func shogunQueueOpenFile(path *C.char) { queueOpenFile(C.GoString(path)) }

// installOpenFileHandler は起動の完了の通知を待って Apple イベントの
// 受け取り口を登録する。
func installOpenFileHandler() { C.shogunInstallOpenHandler() }
