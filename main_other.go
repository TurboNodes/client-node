//go:build !linux

package main

import "fyne.io/systray"

// runTray hands the main thread to systray, which owns the native run loop
// (NSApp on macOS, a window message loop on Windows). The popup schedules its
// own work onto the right thread from there.
func runTray(onReady, onExit func()) {
	systray.Run(onReady, onExit)
}
