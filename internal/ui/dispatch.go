package ui

import "fyne.io/fyne/v2"

// このファイルだけが fyne.Do と fyne.DoAndWait を呼ぶ。
//
// Fyne の UI オブジェクトはメインゴルーチン以外から操作できない。
// 呼び出しをここに集めるのは、Fyne の次のメジャーリリースでこの規則の
// 安全網が外れるときに、直す場所を 1 か所にするためである。
// この決まりは internal/arch の静的検査（TestDispatchIsTheOnlyFyneDoCaller）が
// 機械的に確かめる。

// Post は UI スレッドで fn を実行する。呼び出し元は完了を待たない。
//
// 毎フレームの画面更新には使わない。画面の更新は Fyne のフレーム駆動に
// 載せる（設計書 10 編 §10.4）。これを使うのは、エミュレーション
// ゴルーチンからウィンドウの表示状態を変える必要がある場合に限る。
//
// 終了処理が始まった後、Fyne は fn を呼び出し元のゴルーチンで実行する。
// 終了しうる場面では、fn が UI オブジェクトを触る前に打ち切る。
func Post(fn func()) { fyne.Do(fn) }

// PostAndWait は UI スレッドで fn を実行し、完了を待つ。
//
// UI スレッドから呼ぶと互いに待って進まなくなる。完了を待つ必要が
// ある場合に限って使う。
func PostAndWait(fn func()) { fyne.DoAndWait(fn) }
