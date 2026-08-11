package main

import (
	"client/platform/autostart"
	"client/platform/config"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

// pairTokenEnvVar is how the install script hands over the token.
//
// It is deliberately not passed as an argument: on Linux /proc/<pid>/cmdline
// is world-readable, so anyone with a shell on the box could read the token
// out of `ps` while this runs, whereas /proc/<pid>/environ is readable only by
// the process owner. The flag still exists for pairing by hand, where that
// tradeoff is the operator's to make.
const pairTokenEnvVar = "TURBO_PAIR_TOKEN"

// runInstall sets this machine up as a paired, self-starting node, then exits.
//
// It is the second half of the website's terminal install command: the script
// downloads this binary and puts it where it belongs, then re-runs it with
// --install to register it with the OS and leave the token where the daemon
// will find it. Actually running the node is the freshly registered service's
// job, not this invocation's — which is why this returns instead of falling
// through to ConnectQuicServer.
func runInstall(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("no pairing token: pass --pair-token=<token> or set " +
			pairTokenEnvVar + ". Generate one from the Download page of your Turbo dashboard")
	}

	// Left for the daemon to send with its first "hello"; the server claims it
	// and answers "paired", and it is deleted at that point. Written before
	// autostart so the service cannot start and connect without finding it.
	if err := config.SavePairToken(token); err != nil {
		return fmt.Errorf("saving pairing token: %w", err)
	}

	// Create the device identity now rather than on first connect, so the id
	// can be printed below — it is what support and the dashboard identify
	// this node by, and it is much easier to note down now than to go looking
	// for later.
	deviceID := config.DeviceID()

	if err := autostart.EnableHeadlessAutoStart(); err != nil {
		// The token is still on disk and the binary still works, so say what
		// does and does not work rather than implying nothing happened.
		return fmt.Errorf("registering the node to start automatically: %w\n"+
			"The node is installed and will pair when started by hand", err)
	}

	log.Printf("Turbo node installed and started (device %s).", deviceID)
	log.Println("It will pair to your account within a few seconds, and start again on boot.")
	return nil
}

// resolveToken prefers the flag, falling back to the environment.
func resolveToken(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	return os.Getenv(pairTokenEnvVar)
}

var (
	installMode = flag.Bool("install", false,
		"install this node as a background service that starts on boot, then exit")
	pairTokenFlag = flag.String("pair-token", "",
		"single-use pairing token from your Turbo dashboard (or set "+pairTokenEnvVar+")")
)
