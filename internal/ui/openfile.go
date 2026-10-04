package ui

import "sync"

// openRequests は OS から届いた「ファイルを開く」要求（設計書 13 編 §13.5）。
//
// macOS では Finder で開いた .nes が Apple イベントで届く。届くのは
// Cocoa のメインスレッドであり、UI の部品を直接触らず溜めておく。画面の
// 更新のときに UI スレッドで取り出して開く。
var openRequests struct {
	mu    sync.Mutex
	paths []string
}

// queueOpenFile は開く要求を溜める。
func queueOpenFile(path string) {
	openRequests.mu.Lock()
	defer openRequests.mu.Unlock()
	openRequests.paths = append(openRequests.paths, path)
}

// takeOpenFiles は溜まった要求を取り出す。
func takeOpenFiles() []string {
	openRequests.mu.Lock()
	defer openRequests.mu.Unlock()
	p := openRequests.paths
	openRequests.paths = nil
	return p
}

// InstallOpenFileHandler は OS から届く「ファイルを開く」要求の受け取りを
// 始める。Fyne のアプリケーションを作る前に呼ぶ。macOS 以外では何もしない。
func InstallOpenFileHandler() { installOpenFileHandler() }

// openQueuedFiles は溜まった要求のうち最後のものを開く。ROM は 1 つずつしか
// 開けないため、複数届いたときは最後に選ばれたものを使う。
func (u *UI) openQueuedFiles() {
	paths := takeOpenFiles()
	if len(paths) == 0 {
		return
	}
	u.OpenROM(paths[len(paths)-1])
}
