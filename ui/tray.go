package ui

import (
	"sync"

	"fyne.io/systray"
)

var (
	trayMu     sync.Mutex
	trayHidden bool
)

// SetupTray creates the tray icon and its menu, and wires the popup to it.
//
// Left click toggles the popup. Right click opens the menu — fyne falls back
// to showing the menu whenever no secondary handler is registered, so the menu
// stays reachable on every platform without extra plumbing.
func SetupTray(icon []byte, onQuit func()) {
	systray.SetTemplateIcon(icon, icon)
	systray.SetTooltip("Turbo running")

	// "Open" duplicates the left-click gesture. It exists because on Linux the
	// icon is a StatusNotifierItem and whether a left click is delivered at all
	// is up to the desktop environment; the menu is the one path that always
	// works.
	openItem := systray.AddMenuItem("Open", "Show the Turbo popup")
	hideItem := systray.AddMenuItem("Hide icon", "Run in the background with no tray icon")
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("Quit", "Quit Turbo")

	systray.SetOnTapped(TogglePopup)

	StartPopup(onQuit, HideToStealth)

	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				ShowPopup()
			case <-hideItem.ClickedCh:
				HideToStealth()
			case <-quitItem.ClickedCh:
				if onQuit != nil {
					onQuit()
				} else {
					systray.Quit()
				}
				return
			}
		}
	}()
}

// HideToStealth removes the tray icon and popup, leaving the node running with
// no visible presence. The app is brought back by launching it again, which the
// single-instance guard turns into a "reveal" of the process already running.
func HideToStealth() {
	trayMu.Lock()
	trayHidden = true
	trayMu.Unlock()

	HidePopup()
	systray.SetVisible(false)
}

// RevealFromStealth restores the tray icon and shows the popup. Safe to call
// when nothing is hidden — a second launch should surface the UI either way.
func RevealFromStealth() {
	trayMu.Lock()
	wasHidden := trayHidden
	trayHidden = false
	trayMu.Unlock()

	if wasHidden {
		systray.SetVisible(true)
	}
	ShowPopup()
}
