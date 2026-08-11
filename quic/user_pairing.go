package quic

import (
	"client/platform/config"
	"log"
	"sync"
)

// PairingStatus is the node's account-pairing state from the server.
type PairingStatus int

const (
	// PairingUnknown means no pairing_url or paired message yet.
	PairingUnknown PairingStatus = iota
	// PairingNeeded means the server sent pairing_url — show Connect UI.
	PairingNeeded
	// PairingDone means the server sent paired — tray only.
	PairingDone
)

type pairingListener func(status PairingStatus, url string)

// justPairedListener fires only on the transition into PairingDone, never on
// a message that finds the node already there. See handlePaired.
type justPairedListener func()

var (
	pairingMu           sync.Mutex
	pairingURL          string
	pairingStatus       = PairingUnknown
	pairingListeners    []pairingListener
	justPairedListeners []justPairedListener
)

// PairingURL is the latest connect URL received from the server.
func PairingURL() string {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairingURL
}

// Status returns the current pairing status.
func Status() PairingStatus {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairingStatus
}

// OnPairingChange registers a listener for pairing_url / paired updates.
// If status is already known, the listener is called immediately.
func OnPairingChange(fn pairingListener) {
	if fn == nil {
		return
	}
	pairingMu.Lock()
	pairingListeners = append(pairingListeners, fn)
	status := pairingStatus
	url := pairingURL
	pairingMu.Unlock()

	if status != PairingUnknown {
		fn(status, url)
	}
}

// OnJustPaired registers a listener for the moment this node newly becomes
// paired — never for a "paired" message that just reconfirms it.
//
// The server resends "paired" on every reconnect of an already-paired node
// (a fresh per-connection watcher on its side, with no memory of what it told
// the previous connection), which is normal and not news to the user. Only
// the transition from not-paired is: the one time pairing actually just
// happened. Unlike OnPairingChange, an already-paired node does not get an
// immediate call on registration — there is no transition to report yet.
func OnJustPaired(fn justPairedListener) {
	if fn == nil {
		return
	}
	pairingMu.Lock()
	justPairedListeners = append(justPairedListeners, fn)
	pairingMu.Unlock()
}

// RequestPairing asks the server to start (or resume) actively watching for
// this node's pairing and to mint a connect link if one is not already live.
// The server never sends a real link unprompted — see quic/pairing.go on the
// server — so this is the only thing that makes one appear; the link itself
// arrives asynchronously as a later "pairing_url" message (see ui/popup.go's
// OnPairingChange callback, which opens it automatically once it lands).
//
// Called when the user clicks "Pair to Account". Safe to call any time the
// connection is up — while already paired or already mid-attempt it is a
// harmless no-op or resend on the server's side.
func RequestPairing() {
	if err := SendMessage(&Message{Type: "pairing"}); err != nil {
		log.Println("pairing request:", err)
	}
}

// restorePaired marks the node as paired from persisted state at startup, so
// the popup can show the connected view without waiting for the server. Lives
// here rather than in rewards.go so pairingStatus is only ever touched under
// pairingMu.
func restorePaired() {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	pairingStatus = PairingDone
}

func handlePairingURL(msg Message) {
	pairingMu.Lock()
	pairingURL = msg.Data
	pairingStatus = PairingNeeded
	listeners := append([]pairingListener(nil), pairingListeners...)
	url := pairingURL
	pairingMu.Unlock()

	// The server is the authority on pairing: if it asks this node to pair,
	// a stale cached "paired" flag from a previous run must not win.
	persistNodeState()

	for _, fn := range listeners {
		fn(PairingNeeded, url)
	}
}

// handlePaired records that this node is attached to an account. The message
// used to carry Supabase session tokens; the client no longer speaks to
// Supabase, so the payload is ignored and only the state is kept.
func handlePaired(msg Message) {
	// Whatever pairing this node, an install token has no further use: it is
	// single-use, so the server would refuse it from here on anyway. Dropping
	// it on every "paired" rather than only on the transition keeps this
	// simple and is harmless — removing a file that is already gone is a
	// no-op, and that is the case for all but the first time.
	config.ClearPairToken()

	pairingMu.Lock()
	wasPaired := pairingStatus == PairingDone
	pairingStatus = PairingDone
	listeners := append([]pairingListener(nil), pairingListeners...)
	var justPaired []justPairedListener
	if !wasPaired {
		justPaired = append([]justPairedListener(nil), justPairedListeners...)
	}
	url := pairingURL
	pairingMu.Unlock()

	persistNodeState()

	for _, fn := range listeners {
		fn(PairingDone, url)
	}
	for _, fn := range justPaired {
		fn()
	}
}
