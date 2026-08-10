package quic

import (
	"sync"
	"time"
)

type connectionListener func(connected bool)

var (
	connStatusMu        sync.Mutex
	quicConnected       bool
	reconnectSignal     = make(chan struct{}, 1)
	connectionListeners []connectionListener
)

// IsConnected reports whether the control QUIC stream is active.
func IsConnected() bool {
	connStatusMu.Lock()
	defer connStatusMu.Unlock()
	return quicConnected
}

// OnConnectionChange registers a listener for QUIC up/down.
// If already connected, the listener is called immediately with true.
func OnConnectionChange(fn connectionListener) {
	if fn == nil {
		return
	}
	connStatusMu.Lock()
	connectionListeners = append(connectionListeners, fn)
	connected := quicConnected
	connStatusMu.Unlock()
	if connected {
		fn(true)
	}
}

// RequestReconnect wakes any reconnect backoff sleep so dialing can resume promptly.
func RequestReconnect() {
	select {
	case reconnectSignal <- struct{}{}:
	default:
	}
}

func setConnected(connected bool) {
	connStatusMu.Lock()
	if quicConnected == connected {
		connStatusMu.Unlock()
		return
	}
	quicConnected = connected
	listeners := append([]connectionListener(nil), connectionListeners...)
	connStatusMu.Unlock()

	if connected {
		clearHostViews()
		setConnecting(false)
	}

	for _, fn := range listeners {
		fn(connected)
	}
}

// sleepInterruptible waits for d or until RequestReconnect is called.
// Marks connecting activity false while sleeping so the UI can enable Retry.
// Returns true if woken by a manual retry request, false if d elapsed.
func sleepInterruptible(d time.Duration) bool {
	setConnecting(false)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return false
	case <-reconnectSignal:
		return true
	}
}
