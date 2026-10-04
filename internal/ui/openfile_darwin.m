//go:build darwin

// 「書類を開く」の Apple イベントを受け取る（設計書 13 編 §13.5）。
// Go 側の宣言は openfile_darwin.go にある。

#import <Cocoa/Cocoa.h>


extern void shogunQueueOpenFile(char *path);

// ShogunOpenHandler は「書類を開く」の Apple イベントを受け取る。
@interface ShogunOpenHandler : NSObject
- (void)handleOpen:(NSAppleEventDescriptor *)event withReply:(NSAppleEventDescriptor *)reply;
- (void)willFinishLaunching:(NSNotification *)note;
@end

@implementation ShogunOpenHandler
// deliver は 1 つのファイルの記述子からパスを取り出して Go へ渡す。
- (void)deliver:(NSAppleEventDescriptor *)item {
	NSAppleEventDescriptor *u = [item coerceToDescriptorType:typeFileURL];
	if (u == nil) {
		return;
	}
	NSString *s = [[NSString alloc] initWithData:[u data] encoding:NSUTF8StringEncoding];
	NSURL *url = [NSURL URLWithString:s];
	if (url != nil && url.path != nil) {
		shogunQueueOpenFile((char *)[url.path fileSystemRepresentation]);
	}
}

- (void)handleOpen:(NSAppleEventDescriptor *)event withReply:(NSAppleEventDescriptor *)reply {
	NSAppleEventDescriptor *list = [event paramDescriptorForKeyword:keyDirectObject];
	if (list == nil) {
		return;
	}
	NSInteger n = [list numberOfItems];
	if (n == 0) {
		[self deliver:list];
		return;
	}
	for (NSInteger i = 1; i <= n; i++) {
		[self deliver:[list descriptorAtIndex:i]];
	}
}

// willFinishLaunching は NSApplication が既定の受け取り口を登録した後に
// 呼ばれる。ここで登録すると既定のものを上書きできる。
- (void)willFinishLaunching:(NSNotification *)note {
	[[NSAppleEventManager sharedAppleEventManager]
		setEventHandler:self
		    andSelector:@selector(handleOpen:withReply:)
		  forEventClass:kCoreEventClass
		     andEventID:kAEOpenDocuments];
}
@end

static ShogunOpenHandler *shogunHandler;

void shogunInstallOpenHandler(void) {
	shogunHandler = [ShogunOpenHandler new];
	[[NSNotificationCenter defaultCenter]
		addObserver:shogunHandler
		   selector:@selector(willFinishLaunching:)
		       name:NSApplicationWillFinishLaunchingNotification
		     object:nil];
}
