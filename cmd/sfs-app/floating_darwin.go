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
    
    // Set style mask to borderless
    [window setStyleMask:NSWindowStyleMaskBorderless];
    
    // Make background transparent/clear
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
    
    [window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
}

void hideNSWindow(void* wndPtr) {
    NSWindow* window = (NSWindow*)wndPtr;
    if (window == nil) return;
    
    [window orderOut:nil];
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

func resizeWindow(ptr unsafe.Pointer, width, height int) {
	if ptr == nil {
		return
	}
	C.resizeNSWindow(ptr, C.int(width), C.int(height))
}
