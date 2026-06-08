//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

void makeWindowFloating(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) {
        return;
    }

    // QUAN TRỌNG: KHÔNG dùng NSWindowStyleMaskBorderless thuần — window đó trả
    // NO từ canBecomeKeyWindow nên KHÔNG NHẬN bàn phím/chuột (lỗi gõ không được).
    // Dùng Titled + FullSizeContentView rồi ẩn titlebar → trông như borderless
    // NHƯNG vẫn nhận input.
    [window setStyleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskFullSizeContentView | NSWindowStyleMaskResizable];
    [window setTitlebarAppearsTransparent:YES];
    [window setTitleVisibility:NSWindowTitleHidden];
    [window setMovableByWindowBackground:YES];
    // Ẩn 3 nút đỏ/vàng/xanh
    [[window standardWindowButton:NSWindowCloseButton] setHidden:YES];
    [[window standardWindowButton:NSWindowMiniaturizeButton] setHidden:YES];
    [[window standardWindowButton:NSWindowZoomButton] setHidden:YES];

    // Make background transparent/clear (glass cho qua từ CSS)
    [window setOpaque:NO];
    [window setBackgroundColor:[NSColor clearColor]];
    [window setHasShadow:YES];

    // Set level to floating (above other windows)
    [window setLevel:NSFloatingWindowLevel];

    // Position the window centered horizontally, in the upper-third of the screen
    NSRect screenRect = [[NSScreen mainScreen] visibleFrame];
    NSRect windowRect = [window frame];

    windowRect.origin.x = (screenRect.size.width - windowRect.size.width) / 2 + screenRect.origin.x;
    windowRect.origin.y = screenRect.origin.y + (screenRect.size.height * 2 / 3) - (windowRect.size.height / 2);

    [window setFrame:windowRect display:YES];

    // Make WKWebView transparent if found
    @try {
        NSView* contentView = [window contentView];
        if (contentView != nil) {
            NSArray* subviews = [contentView subviews];
            for (NSUInteger i = 0; i < [subviews count]; i++) {
                NSView* subview = [subviews objectAtIndex:i];
                if ([NSStringFromClass([subview class]) containsString:@"WKWebView"]) {
                    [subview setValue:[NSNumber numberWithBool:NO] forKey:@"drawsBackground"];
                }
            }
        }
    } @catch (NSException* exception) {
        // Ignore
    }
}

void makeWindowNormal(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) {
        return;
    }

    // Restore titled, closable, resizable style mask
    [window setStyleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable];

    // Set opaque and window background color
    [window setOpaque:YES];
    [window setBackgroundColor:[NSColor windowBackgroundColor]];

    // Restore window level to normal
    [window setLevel:NSNormalWindowLevel];

    // Restore WKWebView drawsBackground
    @try {
        NSView* contentView = [window contentView];
        if (contentView != nil) {
            NSArray* subviews = [contentView subviews];
            for (NSUInteger i = 0; i < [subviews count]; i++) {
                NSView* subview = [subviews objectAtIndex:i];
                if ([NSStringFromClass([subview class]) containsString:@"WKWebView"]) {
                    [subview setValue:[NSNumber numberWithBool:YES] forKey:@"drawsBackground"];
                }
            }
        }
    } @catch (NSException* exception) {
        // Ignore
    }

    [window display];
}

void centerNSWindow(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) return;
    [window center];
}

void showNSWindow(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) return;

    [NSApp activateIgnoringOtherApps:YES];
    [window makeKeyAndOrderFront:nil];
    [window makeFirstResponder:[window contentView]];
    // Bo góc mềm cho content (glass) + cắt viền thừa
    NSView* cv = [window contentView];
    if (cv != nil) {
        [cv setWantsLayer:YES];
        cv.layer.cornerRadius = 16.0;
        cv.layer.masksToBounds = YES;
    }
}

void hideNSWindow(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) return;

    [window orderOut:nil];
}

// windowIsVisible trả 1 nếu cửa sổ đang hiện VÀ là key window (đang focus).
// Dùng cho toggle hotkey: đọc trạng thái THẬT của cửa sổ thay vì cờ isBarVisible
// dễ lệch (click ra ngoài làm cửa sổ mất focus mà cờ không cập nhật).
int windowIsVisible(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) return 0;
    return ([window isVisible] && [window isKeyWindow]) ? 1 : 0;
}

void resizeNSWindow(void* wndPtr, int width, int height) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) return;

    NSRect frame = [window frame];
    CGFloat diffY = height - frame.size.height;

    frame.size.width = width;
    frame.size.height = height;
    frame.origin.y -= diffY; // Grow downwards (top-left stays fixed)

    [window setFrame:frame display:YES animate:YES];
}
*/
import "C"
import "unsafe"

func makeFloating(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	C.makeWindowFloating(ptr)
}

func makeNormalWindow(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	C.makeWindowNormal(ptr)
}

func centerWindow(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	C.centerNSWindow(ptr)
}

func showWindow(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	C.showNSWindow(ptr)
}

func hideWindow(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	C.hideNSWindow(ptr)
}

// windowVisible reports whether the window is currently shown AND focused.
func windowVisible(ptr unsafe.Pointer) bool {
	if ptr == nil {
		return false
	}
	return C.windowIsVisible(ptr) == 1
}

func resizeWindow(ptr unsafe.Pointer, width, height int) {
	if ptr == nil {
		return
	}
	C.resizeNSWindow(ptr, C.int(width), C.int(height))
}
