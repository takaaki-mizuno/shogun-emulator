//go:build darwin

// アプリケーションメニューとウインドウメニューの文言を差し替える。
// Go 側の宣言は appmenu_darwin.go にある。

#import <Cocoa/Cocoa.h>

// retitle は menu の中で action を持つ項目の文言を title にする。
static void retitle(NSMenu *menu, SEL action, const char *title) {
	NSInteger i = [menu indexOfItemWithTarget:nil andAction:action];
	if (i < 0) {
		for (NSMenuItem *item in [menu itemArray]) {
			if ([item action] == action) {
				i = [menu indexOfItem:item];
				break;
			}
		}
	}
	if (i >= 0) {
		[[menu itemAtIndex:i] setTitle:[NSString stringWithUTF8String:title]];
	}
}

void shogunLocalizeAppMenu(const char *about, const char *services, const char *hide,
	const char *hideOthers, const char *showAll, const char *quit, const char *window,
	const char *minimize, const char *zoom, const char *bringAll, const char *fullScreen) {
	NSMenu *bar = [NSApp mainMenu];
	if (bar == nil || [bar numberOfItems] == 0) {
		return;
	}
	NSMenu *app = [[bar itemAtIndex:0] submenu];
	if (app != nil) {
		retitle(app, @selector(orderFrontStandardAboutPanel:), about);
		retitle(app, @selector(hide:), hide);
		retitle(app, @selector(hideOtherApplications:), hideOthers);
		retitle(app, @selector(unhideAllApplications:), showAll);
		retitle(app, @selector(terminate:), quit);
		for (NSMenuItem *item in [app itemArray]) {
			if ([item submenu] != nil && [item submenu] == [NSApp servicesMenu]) {
				[item setTitle:[NSString stringWithUTF8String:services]];
			}
		}
	}
	NSMenu *win = [NSApp windowsMenu];
	if (win != nil) {
		NSString *t = [NSString stringWithUTF8String:window];
		[win setTitle:t];
		for (NSMenuItem *item in [bar itemArray]) {
			if ([item submenu] == win) {
				[item setTitle:t];
			}
		}
		retitle(win, @selector(performMiniaturize:), minimize);
		retitle(win, @selector(performZoom:), zoom);
		retitle(win, @selector(arrangeInFront:), bringAll);
		retitle(win, @selector(toggleFullScreen:), fullScreen);
	}
}
