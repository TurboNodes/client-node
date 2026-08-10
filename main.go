package main

import (
	"client/platform/autostart"
	"client/platform/config"
	"client/platform/singleinstance"
	"client/platform/update"
	"client/quic"
	"client/ui"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fyne.io/systray"
)

func main() {
	logPath, err := config.InitLogging()
	if err != nil {
		log.Println("file logging unavailable:", err)
	} else if logPath != "" {
		log.Println("logging to", logPath)
	}

	// Old versions stored Supabase tokens here; nothing uses them now.
	config.RemoveLegacySession()

	// Before anything with side effects: a second launch must not open its own
	// QUIC session or add a second tray icon. It hands the reveal request to
	// the running instance and exits.
	primary, err := singleinstance.Acquire(ui.RevealFromStealth)
	if err != nil {
		log.Println("single instance:", err)
	}
	if !primary {
		log.Println("Turbo is already running; asked it to show and exiting")
		return
	}

	go quic.ConnectQuicServer()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		systray.Quit()
		// Fallback in case the native teardown hangs, so Ctrl+C is never a no-op.
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()

	runTray(onReady, nil)
}

func onReady() {
	ui.SetupTray(iconData, systray.Quit)

	if err := autostart.EnableAutoStart(); err != nil {
		log.Println(err)
	}

	if err := update.AutoUpdate(); err != nil {
		log.Println(err)
		quic.SendMessage(&quic.Message{
			Type: "stacktrace",
			Data: "Auto-update failed: " + err.Error(),
		})
	}
}
