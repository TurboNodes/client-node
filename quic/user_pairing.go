package quic

import "sync"

var (
	pairingMu  sync.Mutex
	pairingURL string
)

// PairingURL is the last connect URL received from the server.
func PairingURL() string {
	pairingMu.Lock()
	defer pairingMu.Unlock()
	return pairingURL
}

func handlePairingURL(msg Message) {
	pairingMu.Lock()
	pairingURL = msg.Data
	pairingMu.Unlock()
}
