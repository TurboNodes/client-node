package quic

import (
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
