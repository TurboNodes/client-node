//go:build linux

package ui

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include "popup_linux.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// anchorWait is zero here: a StatusNotifierItem is drawn by the desktop
// environment, which never reports its geometry back over the bus, so there is
// nothing to wait for. The popup goes to the pointer, which on a click is on
// the icon anyway.
const anchorWait = 0

func initPopup(html string) {
	chtml := C.CString(html)
	defer C.free(unsafe.Pointer(chtml))
	C.popupInit(chtml)
}

func setPopupAnchor(centerX, topY float64, valid bool) {
	v := C.int(0)
	if valid {
		v = C.int(1)
	}
	C.popupSetAnchor(C.double(centerX), C.double(topY), v)
}

func togglePopup() { C.popupToggle() }

func showPopup() { C.popupShow() }

func hidePopup() { C.popupHide() }

func setPopupState(jsonState string) {
	cjson := C.CString(jsonState)
	defer C.free(unsafe.Pointer(cjson))
	C.popupSetState(cjson)
}

// PrepareGTK initialises GTK. It must run on the main thread before anything
// touches a widget: popupInit is queued from the tray goroutine, and if GTK
// were still uninitialised when that ran, widget construction would abort with
// "Can't create a GtkStyleContext without a display connection".
func PrepareGTK() {
	C.popupPrepare()
}

// RunPopupLoop runs gtk_main on the calling thread and blocks until the app
// quits. Linux is the one platform where systray does not want the main
// thread — GTK does — so main hands it over here.
func RunPopupLoop() {
	C.popupRunLoop()
}

// StopPopupLoop ends gtk_main so the process can exit.
func StopPopupLoop() {
	C.popupQuitLoop()
}

//export PopupOnAction
func PopupOnAction(action *C.char) {
	handlePopupAction(C.GoString(action))
}
