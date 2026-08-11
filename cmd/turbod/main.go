// Command turbod runs a Turbo node with no tray icon, popup, autostart or
// self-update — the pieces of main.go that only make sense on a desktop.
// Meant for containers and servers: quic.ConnectQuicServer is the whole job.
package main

import (
	"client/platform/config"
	"client/quic"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	flag.Parse()

	logPath, err := config.InitLogging()
	if err != nil {
		log.Println("file logging unavailable:", err)
	} else if logPath != "" {
		log.Println("logging to", logPath)
	}

	// --install is a one-shot setup step, not a way to run the node: it hands
	// off to the service manager and exits. See install.go.
	if *installMode {
		if err := runInstall(resolveToken(*pairTokenFlag)); err != nil {
			log.Println("install failed:", err)
			os.Exit(1)
		}
		return
	}

	// Old versions stored Supabase tokens here; nothing uses them now.
	config.RemoveLegacySession()

	go watchPairing()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		os.Exit(0)
	}()

	quic.ConnectQuicServer()
}

// watchPairing logs the pairing URL since there is no popup to show a
// "Pair to Account" button — so nothing ever clicks it, and the server no
// longer mints a link on its own (see quic/pairing.go on the server: it now
// waits to be asked). This is the ask, standing in for that click.
//
// It repeats on a ticker while unpaired: partly so the URL stays visible in
// `docker logs` even if nobody was watching when it first arrived, and partly
// because that is also what re-mints a link once the previous one expires —
// the server does not do that on its own either.
func watchPairing() {
	const repeatEvery = 5 * time.Minute

	quic.OnPairingChange(func(status quic.PairingStatus, url string) {
		switch status {
		case quic.PairingNeeded:
			if url == "" {
				// Just learned pairing is needed, but no link yet — ask for
				// one now rather than waiting out the ticker below.
				quic.RequestPairing()
				return
			}
			log.Println("=== Pairing required: open this URL to pair the node ===")
			log.Println(url)
		case quic.PairingDone:
			log.Println("node is paired")
		}
	})

	ticker := time.NewTicker(repeatEvery)
	defer ticker.Stop()

	for range ticker.C {
		if quic.Status() != quic.PairingNeeded {
			continue
		}
		// Cheap no-op on the server if the current link is still good; mints
		// and sends a fresh one otherwise. Either way, log whatever is
		// currently cached.
		quic.RequestPairing()
		if url := quic.PairingURL(); url != "" {
			log.Println("=== Pairing still required: open this URL to pair the node ===")
			log.Println(url)
		}
	}
}
