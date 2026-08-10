//go:build linux

package main

import (
	"client/ui"

	"fyne.io/systray"
)

// runTray keeps the main thread for GTK.
//
// On Linux the tray is a StatusNotifierItem driven over D-Bus, so systray does
// not need the main thread — but GTK does, and the popup is a GTK window.
// systray therefore runs with an external loop and GTK owns main.
func runTray(onReady, onExit func()) {
	// Quitting the tray has to break gtk_main as well, or the process would
	// keep running with no icon and no window after Quit.
	// GTK must be initialised before the tray goroutine can queue any popup
	// work onto the loop.
	ui.PrepareGTK()

	start, end := systray.RunWithExternalLoop(onReady, func() {
		if onExit != nil {
			onExit()
		}
		ui.StopPopupLoop()
	})
	start()

	ui.RunPopupLoop() // blocks in gtk_main until the tray exits

	end()
}
