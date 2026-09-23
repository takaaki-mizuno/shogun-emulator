// Package ui は Fyne による画面を提供する。
//
// メインウィンドウ、メニュー、設定画面、デバッグ用のビューアを組み立てる。
// ビューアは別ウィンドウとタブの両方の配置に対応する。
//
// Fyne の描画は main goroutine だけが触れる。他の goroutine から UI を
// 変更する経路は dispatch.go に集める。fyne.Do と fyne.DoAndWait を呼ぶ
// ファイルをそこだけに限ることで、スレッド境界を機械的に検査できる。
package ui
