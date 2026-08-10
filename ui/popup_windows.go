//go:build windows

package ui

import (
	"log"
	"runtime"
	"sync"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	popupWidth   = 300
	popupHeight  = 400
	screenMargin = 8
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procMonitorFromPoint = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfo   = user32.NewProc("GetMonitorInfoW")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procSetForegroundWin = user32.NewProc("SetForegroundWindow")
	procLoadCursor       = user32.NewProc("LoadCursorW")
)

const (
	wsPopup   = 0x80000000
	wsVisible = 0x10000000

	wsExToolWindow = 0x00000080
	wsExTopMost    = 0x00000008

	swHide   = 0
	swShow   = 5
	swpNoZ   = 0x0004
	swpNoAct = 0x0010

	wmDestroy  = 0x0002
	wmSize     = 0x0005
	wmActivate = 0x0006
	wmClose    = 0x0010
	wmApp      = 0x8000

	// Cross-thread commands. Every popup entry point is callable from any
	// goroutine, but every Win32 and WebView2 call has to happen on the thread
	// owning the window, so they are posted to its message loop instead.
	cmdToggle   = wmApp + 1
	cmdShow     = wmApp + 2
	cmdHide     = wmApp + 3
	cmdSetState = wmApp + 4

	waInactive = 0

	monitorDefaultToNearest = 0x00000002
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var (
	popupHwnd windows.Handle
	popupView *edge.Chromium

	// pendingState carries the JSON for cmdSetState; PostMessage cannot safely
	// carry a Go pointer, so the payload is parked here and picked up by the
	// window procedure.
	pendingMu    sync.Mutex
	pendingState string

	// Where to hang the popup from: the centre of the tray icon and the top of
	// its rectangle, in screen pixels. Refreshed before every show, since the
	// icon shifts as neighbouring icons come and go.
	anchorMu      sync.Mutex
	anchorCenterX int32
	anchorTopY    int32
	anchorValid   bool
)

func setPopupAnchor(centerX, topY float64, valid bool) {
	anchorMu.Lock()
	anchorCenterX = int32(centerX)
	anchorTopY = int32(topY)
	anchorValid = valid
	anchorMu.Unlock()
}

func initPopup(html string) {
	go func() {
		// The window, its message loop and every WebView2 call must all live on
		// one thread for the lifetime of the popup.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := createPopupWindow(html); err != nil {
			log.Println("popup: creating window:", err)
			return
		}
		runMessageLoop()
	}()
}

func createPopupWindow(html string) error {
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return err
	}

	className, err := windows.UTF16PtrFromString("TurboPopupWindow")
	if err != nil {
		return err
	}

	cursor, _, _ := procLoadCursor.Call(0, 32512 /* IDC_ARROW */)

	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   windows.NewCallback(wndProc),
		Instance:  instance,
		Cursor:    windows.Handle(cursor),
		ClassName: className,
	}
	if ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return err
	}

	title, err := windows.UTF16PtrFromString("Turbo")
	if err != nil {
		return err
	}

	// Tool window keeps it out of the taskbar and Alt+Tab; topmost keeps it
	// above the shell like a menu.
	hwnd, _, err := procCreateWindowEx.Call(
		uintptr(wsExToolWindow|wsExTopMost),
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup),
		0, 0, popupWidth, popupHeight,
		0, 0, uintptr(instance), 0,
	)
	if hwnd == 0 {
		return err
	}
	popupHwnd = windows.Handle(hwnd)

	popupView = edge.NewChromium()
	popupView.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		handlePopupAction(message)
	}
	// Embed pumps messages until the runtime is ready, so it has to run here on
	// the UI thread rather than being posted like the other calls.
	if !popupView.Embed(hwnd) {
		return errWebView2Unavailable
	}
	popupView.Resize()
	popupView.NavigateToString(html)

	return nil
}

var errWebView2Unavailable = webview2Error("WebView2 runtime unavailable; install the Edge WebView2 runtime")

type webview2Error string

func (e webview2Error) Error() string { return string(e) }

func runMessageLoop() {
	var m msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		// 0 is WM_QUIT, -1 is an error; both end the loop.
		if int32(ret) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case cmdToggle:
		if isPopupVisible() {
			hidePopupWindow()
		} else {
			showPopupWindow()
		}
		return 0

	case cmdShow:
		if !isPopupVisible() {
			showPopupWindow()
		}
		return 0

	case cmdHide:
		hidePopupWindow()
		return 0

	case cmdSetState:
		pendingMu.Lock()
		state := pendingState
		pendingMu.Unlock()
		if popupView != nil && state != "" {
			popupView.Eval("window.turbo && window.turbo.setState(" + state + ");")
		}
		return 0

	case wmSize:
		if popupView != nil {
			popupView.Resize()
		}
		return 0

	case wmActivate:
		// Losing activation means the user clicked elsewhere: dismiss, the way
		// a menu would.
		if wParam == waInactive {
			hidePopupWindow()
		}
		return 0

	case wmClose:
		// Never destroy the window; hiding keeps the loaded page alive so the
		// next open is instant.
		hidePopupWindow()
		return 0

	case wmDestroy:
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func isPopupVisible() bool {
	if popupHwnd == 0 {
		return false
	}
	ret, _, _ := procIsWindowVisible.Call(uintptr(popupHwnd))
	return ret != 0
}

// positionPopup centres the popup on the tray icon and clamps it into the
// monitor work area. The work area excludes the taskbar, so the popup flips
// above the anchor on a bottom taskbar and below it on a top one.
//
// The icon's rectangle is used when the shell reports it, so the popup lands
// under the icon however it was opened, including a relaunch where the pointer
// is nowhere near the tray. The pointer is the fallback — for a click it is on
// the icon anyway, and for an icon hidden in the overflow flyout there is no
// rectangle to be had.
func positionPopup() {
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	anchorMu.Lock()
	if anchorValid {
		pt = point{X: anchorCenterX, Y: anchorTopY}
	}
	anchorMu.Unlock()

	mon, _, _ := procMonitorFromPoint.Call(
		uintptr(pt.X), uintptr(pt.Y), monitorDefaultToNearest)

	mi := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	work := rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	if ret, _, _ := procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); ret != 0 {
		work = mi.RcWork
	}

	x := pt.X - popupWidth/2
	if x < work.Left+screenMargin {
		x = work.Left + screenMargin
	}
	if x+popupWidth > work.Right-screenMargin {
		x = work.Right - screenMargin - popupWidth
	}

	y := pt.Y + screenMargin
	if y+popupHeight > work.Bottom-screenMargin {
		y = pt.Y - popupHeight - screenMargin
	}
	if y < work.Top+screenMargin {
		y = work.Top + screenMargin
	}

	procSetWindowPos.Call(
		uintptr(popupHwnd), 0,
		uintptr(x), uintptr(y), popupWidth, popupHeight,
		swpNoZ|swpNoAct,
	)
}

func showPopupWindow() {
	if popupHwnd == 0 {
		return
	}
	positionPopup()
	procShowWindow.Call(uintptr(popupHwnd), swShow)
	// Foreground activation is what makes the WM_ACTIVATE dismissal work.
	procSetForegroundWin.Call(uintptr(popupHwnd))

	if popupView != nil {
		var r rect
		procGetClientRect.Call(uintptr(popupHwnd), uintptr(unsafe.Pointer(&r)))
		popupView.Resize()
	}
}

// hidePopupWindow tells the page it is going away before hiding, so a leftover
// text selection is not still highlighted the next time the popup opens.
func hidePopupWindow() {
	if popupHwnd == 0 {
		return
	}
	if popupView != nil {
		popupView.Eval("window.turbo && window.turbo.onHidden();")
	}
	procShowWindow.Call(uintptr(popupHwnd), swHide)
}

func post(command uint32) {
	if popupHwnd == 0 {
		return
	}
	procPostMessage.Call(uintptr(popupHwnd), uintptr(command), 0, 0)
}

func togglePopup() { post(cmdToggle) }

func showPopup() { post(cmdShow) }

func hidePopup() { post(cmdHide) }

func setPopupState(jsonState string) {
	pendingMu.Lock()
	pendingState = jsonState
	pendingMu.Unlock()
	post(cmdSetState)
}
