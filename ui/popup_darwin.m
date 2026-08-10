#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include "popup_darwin.h"
#include "_cgo_export.h"

static const CGFloat kPopupWidth = 300;
static const CGFloat kPopupHeight = 400;
// Gap between the menu bar and the top of the popup.
static const CGFloat kMenuBarGap = 6;
// Keep the popup this far from the screen edges when clamping.
static const CGFloat kScreenMargin = 8;

static NSPanel *popupWindow = nil;
static WKWebView *popupWebView = nil;

// Where to hang the popup from: the centre of the tray icon and the bottom of
// the menu bar slot it occupies, in Cocoa screen coordinates. Refreshed by Go
// before every show, because the icon moves when other menu extras come and go.
static CGFloat anchorCenterX = 0;
static CGFloat anchorBottomY = 0;
static BOOL anchorValid = NO;

// Receives window.webkit.messageHandlers.turbo.postMessage(...) from the page.
@interface PopupBridge : NSObject <WKScriptMessageHandler>
@end

@implementation PopupBridge
- (void)userContentController:(WKUserContentController *)controller
      didReceiveScriptMessage:(WKScriptMessage *)message {
  if (![message.body isKindOfClass:[NSString class]]) {
    return;
  }
  PopupOnAction((char *)[(NSString *)message.body UTF8String]);
}
@end

static PopupBridge *popupBridge = nil;

static void hidePopupNow(void);

// handleReopen answers a relaunch of an already-running Turbo.app.
//
// The single-instance socket never sees these: LaunchServices will not start a
// second process for a bundle that is already running, it activates the running
// one and sends this instead. Without it, double-clicking Turbo.app while
// hidden in stealth mode would do nothing at all.
static BOOL handleReopen(id self, SEL _cmd, NSApplication *app, BOOL hasVisibleWindows) {
  PopupOnAction((char *)"reveal");
  return YES;
}

// installReopenHandler grafts the method onto whichever delegate systray
// installed, rather than replacing the delegate and breaking the tray.
static void installReopenHandler(void) {
  id delegate = [NSApp delegate];
  if (delegate == nil) {
    return;
  }
  class_addMethod([delegate class],
                  @selector(applicationShouldHandleReopen:hasVisibleWindows:),
                  (IMP)handleReopen,
                  "c@:@c");
}

void popupInit(const char *html) {
  // Copy the string NOW: the caller frees it as soon as this function returns,
  // long before the block below runs on the main queue.
  NSString *htmlStr = [NSString stringWithUTF8String:html];

  dispatch_async(dispatch_get_main_queue(), ^{
    // A menu-bar app has no Dock icon and no regular windows. Without an
    // explicit accessory policy the process cannot put windows on screen.
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    installReopenHandler();

    NSRect frame = NSMakeRect(0, 0, kPopupWidth, kPopupHeight);
    popupWindow = [[NSPanel alloc] initWithContentRect:frame
                                             styleMask:(NSWindowStyleMaskBorderless |
                                                        NSWindowStyleMaskNonactivatingPanel)
                                               backing:NSBackingStoreBuffered
                                                 defer:NO];
    [popupWindow setLevel:NSPopUpMenuWindowLevel];
    [popupWindow setOpaque:NO];
    [popupWindow setBackgroundColor:[NSColor clearColor]];
    [popupWindow setHasShadow:YES];
    // NOT hidesOnDeactivate: this app is never the active app, so that would
    // order the panel straight back out the moment it is shown.
    [popupWindow setHidesOnDeactivate:NO];
    [popupWindow setReleasedWhenClosed:NO];
    [popupWindow setCollectionBehavior:(NSWindowCollectionBehaviorCanJoinAllSpaces |
                                        NSWindowCollectionBehaviorTransient)];

    popupBridge = [PopupBridge new];

    WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
    [config.userContentController addScriptMessageHandler:popupBridge name:@"turbo"];

    popupWebView = [[WKWebView alloc] initWithFrame:frame configuration:config];
    [popupWebView setValue:@NO forKey:@"drawsBackground"];
    popupWebView.wantsLayer = YES;
    popupWebView.layer.cornerRadius = 12;
    popupWebView.layer.masksToBounds = YES;
    [popupWebView loadHTMLString:htmlStr baseURL:nil];
    [popupWindow setContentView:popupWebView];

    // Dismiss when the user clicks anywhere outside the popup, like a menu.
    [NSEvent addGlobalMonitorForEventsMatchingMask:(NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown)
                                           handler:^(NSEvent *event) {
      if (popupWindow != nil && [popupWindow isVisible]) {
        hidePopupNow();
      }
    }];
  });
}

// positionPopup centres the popup under the tray icon.
//
// The icon's own rectangle is used when the platform can report it, so the
// popup lands under the icon however it was opened — including a relaunch,
// where the pointer is wherever the user happened to double-click. The pointer
// is only a fallback for when the icon cannot be located (Linux, or a Windows
// icon tucked into the overflow flyout); on a click it sits on the icon anyway.
static void positionPopup(void) {
  NSPoint anchor = [NSEvent mouseLocation];
  if (anchorValid) {
    anchor = NSMakePoint(anchorCenterX, anchorBottomY);
  }

  NSScreen *screen = [NSScreen mainScreen];
  for (NSScreen *candidate in [NSScreen screens]) {
    if (NSPointInRect(anchor, [candidate frame])) {
      screen = candidate;
      break;
    }
  }

  NSRect visible = [screen visibleFrame];
  CGFloat x = anchor.x - (kPopupWidth / 2);
  if (x < NSMinX(visible) + kScreenMargin) {
    x = NSMinX(visible) + kScreenMargin;
  }
  if (x + kPopupWidth > NSMaxX(visible) - kScreenMargin) {
    x = NSMaxX(visible) - kScreenMargin - kPopupWidth;
  }

  // visibleFrame already excludes the menu bar, so its top edge is where the
  // popup can start hanging down from.
  CGFloat y = NSMaxY(visible) - kMenuBarGap - kPopupHeight;
  if (y < NSMinY(visible) + kScreenMargin) {
    y = NSMinY(visible) + kScreenMargin;
  }

  [popupWindow setFrameOrigin:NSMakePoint(x, y)];
}

// hidePopupNow tells the page it is going away before ordering the window out,
// so a leftover text selection does not come back highlighted on the next open.
static void hidePopupNow(void) {
  if (popupWebView != nil) {
    [popupWebView evaluateJavaScript:@"window.turbo && window.turbo.onHidden();"
                   completionHandler:nil];
  }
  [popupWindow orderOut:nil];
}

static void showPopupNow(void) {
  if (popupWindow == nil) {
    return;
  }
  positionPopup();
  // orderFrontRegardless shows the panel without requiring the app to become
  // active, which a nonactivating accessory app never does.
  [popupWindow orderFrontRegardless];
  [popupWindow makeKeyWindow];
}

void popupToggle(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    if (popupWindow == nil) {
      return;
    }
    if ([popupWindow isVisible]) {
      hidePopupNow();
      return;
    }
    showPopupNow();
  });
}

void popupShow(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    if (popupWindow != nil && ![popupWindow isVisible]) {
      showPopupNow();
    }
  });
}

void popupHide(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    if (popupWindow != nil) {
      hidePopupNow();
    }
  });
}

void popupSetAnchor(double centerX, double bottomY, int valid) {
  anchorCenterX = centerX;
  anchorBottomY = bottomY;
  anchorValid = valid ? YES : NO;
}

void popupSetState(const char *json) {
  // Same lifetime rule as popupInit: copy before the block outlives the caller.
  NSString *jsonStr = [NSString stringWithUTF8String:json];

  dispatch_async(dispatch_get_main_queue(), ^{
    if (popupWebView == nil) {
      return;
    }
    NSString *js = [NSString stringWithFormat:@"window.turbo && window.turbo.setState(%@);", jsonStr];
    // Errors here are expected and harmless before the page finishes loading;
    // the page re-requests state itself via the "ready" action once it has.
    [popupWebView evaluateJavaScript:js completionHandler:nil];
  });
}
