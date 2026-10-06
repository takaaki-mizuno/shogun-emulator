//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -Wl,-sectcreate,__TEXT,__info_plist,${SRCDIR}/info_darwin.plist

#include <stdlib.h>

// 実装は appmenu_darwin.m にある。
void shogunLocalizeAppMenu(const char *about, const char *services, const char *hide,
	const char *hideOthers, const char *showAll, const char *quit, const char *window,
	const char *minimize, const char *zoom, const char *bringAll, const char *fullScreen);
*/
import "C"

import (
	"unsafe"

	"github.com/takaakimizuno/shogun-emulator/internal/ui/i18n"
)

// macAppName はメニューバーに出るアプリケーション名。.app の Info.plist の
// CFBundleName と同じにする（設計書 13 編 §13.5）。
//
// バンドルに入れずに実行したときもこの名前になるよう、info_darwin.plist を
// 実行ファイルの __TEXT,__info_plist に埋め込む。
const macAppName = "Shogun Emulator"

// localizeAppMenu はアプリケーションメニューとウインドウメニューの項目を
// 日本語にする。
//
// これらの項目は GLFW が英語の固定の文言で作る。Fyne は自分で足した項目だけを
// 作り直すため、一度書き換えれば残る。UI スレッドで呼ぶ。
func localizeAppMenu() {
	strs := []string{
		i18n.T(i18n.MacAbout, macAppName), i18n.T(i18n.MacServices), i18n.T(i18n.MacHide, macAppName),
		i18n.T(i18n.MacHideOthers), i18n.T(i18n.MacShowAll), i18n.T(i18n.MacQuit, macAppName),
		i18n.T(i18n.MacWindow), i18n.T(i18n.MacMinimize), i18n.T(i18n.MacZoom),
		i18n.T(i18n.MacBringAllToFront), i18n.T(i18n.MacEnterFullScreen),
	}
	cs := make([]*C.char, len(strs))
	for i, s := range strs {
		cs[i] = C.CString(s)
		defer C.free(unsafe.Pointer(cs[i]))
	}
	C.shogunLocalizeAppMenu(cs[0], cs[1], cs[2], cs[3], cs[4], cs[5], cs[6], cs[7], cs[8], cs[9], cs[10])
}
