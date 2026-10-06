//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#import <AppKit/AppKit.h>

static bool shogunIsFullScreen(uintptr_t win) {
	NSWindow *w = (__bridge NSWindow *)(void *)win;
	return ([w styleMask] & NSWindowStyleMaskFullScreen) != 0;
}
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// nativeFullScreen はウィンドウが実際にフルスクリーンかを OS に問い合わせる。
//
// macOS の Fyne は OS のフルスクリーンを使う。緑のボタンや Esc で
// 抜けたときは Fyne の FullScreen() が true のまま残る。UI スレッドで呼ぶ。
func nativeFullScreen(w fyne.Window) (full, ok bool) {
	nw, isNative := w.(driver.NativeWindow)
	if !isNative {
		return false, false
	}
	nw.RunNative(func(ctx any) {
		if mc, isMac := ctx.(driver.MacWindowContext); isMac && mc.NSWindow != 0 {
			full, ok = bool(C.shogunIsFullScreen(C.uintptr_t(mc.NSWindow))), true
		}
	})
	return full, ok
}
