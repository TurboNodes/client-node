//go:build darwin

package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include "popup_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	"time"
	"unsafe"
)

// anchorWait is how long a show waits for the menu bar to place the status
// item before giving up and anchoring to the pointer. Placement takes a couple
// of hundred milliseconds after the tray is created; the ceiling is generous
// because it is only ever reached when something is badly wrong, and until then
// the popup would be opening at the corner of the screen.
const anchorWait = 2 * time.Second

func initPopup(html string) {
	chtml := C.CString(html)
	defer C.free(unsafe.Pointer(chtml))
	C.popupInit(chtml)
}

func setPopupAnchor(centerX, bottomY float64, valid bool) {
	v := C.int(0)
	if valid {
		v = C.int(1)
	}
	C.popupSetAnchor(C.double(centerX), C.double(bottomY), v)
}

func togglePopup() { C.popupToggle() }

func showPopup() { C.popupShow() }

func hidePopup() { C.popupHide() }

func setPopupState(jsonState string) {
	cjson := C.CString(jsonState)
	defer C.free(unsafe.Pointer(cjson))
	C.popupSetState(cjson)
}

//export PopupOnAction
func PopupOnAction(action *C.char) {
	handlePopupAction(C.GoString(action))
}
