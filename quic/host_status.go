package quic

import "sync"

// Host probe UI statuses.
const (
	HostPending = "pending"
	HostProbing = "probing"
	HostUp      = "up"
	HostDown    = "down"
)

// HostProbeView is a JSON-friendly snapshot of one host for the connecting UI.
type HostProbeView struct {
	Addr   string `json:"addr"`
	Status string `json:"status"`
	PingMs int64  `json:"pingMs,omitempty"`
}

type connectingListener func(connecting bool)
type hostsListener func(hosts []HostProbeView)

var (
	probeStatusMu       sync.Mutex
	quicConnecting      bool
	hostProbeViews      []HostProbeView
	connectingListeners []connectingListener
	hostsListeners      []hostsListener
)

// IsConnecting reports whether the client is actively probing or dialing.
func IsConnecting() bool {
	probeStatusMu.Lock()
	defer probeStatusMu.Unlock()
	return quicConnecting
}

// HostStatuses returns a copy of the current host probe snapshot.
func HostStatuses() []HostProbeView {
	probeStatusMu.Lock()
	defer probeStatusMu.Unlock()
	return copyHostViews(hostProbeViews)
}

// OnConnectingChange registers a listener for probe/dial activity.
// If already connecting, the listener is called immediately with true.
func OnConnectingChange(fn connectingListener) {
	if fn == nil {
		return
	}
	probeStatusMu.Lock()
	connectingListeners = append(connectingListeners, fn)
	connecting := quicConnecting
	probeStatusMu.Unlock()
	if connecting {
		fn(true)
	}
}

// OnHostsChange registers a listener for host probe snapshot updates.
// If a non-empty snapshot exists, the listener is called immediately.
func OnHostsChange(fn hostsListener) {
	if fn == nil {
		return
	}
	probeStatusMu.Lock()
	hostsListeners = append(hostsListeners, fn)
	hosts := copyHostViews(hostProbeViews)
	probeStatusMu.Unlock()
	if len(hosts) > 0 {
		fn(hosts)
	}
}

// setConnecting, setHostViews, updateHostView and initHostViewsPending all
// notify listeners while still holding probeStatusMu, instead of capturing a
// snapshot and unlocking before the notify loop. With concurrent probes
// (probeHosts launches one goroutine per host) that used to let goroutines
// race the mutex: a goroutine that captured its snapshot first could get
// descheduled and deliver its now-stale snapshot to the frontend *after* a
// goroutine that mutated (and captured) later already delivered the current
// one — leaving a host's UI status stuck on a stale value (e.g. "probing"
// forever after the real result was already "down"). Holding the lock across
// the notify loop makes each update's mutate+notify fully atomic relative to
// the others, so listeners always see updates in true chronological order.
// This is safe as long as listeners (ultimately Wails' EventsEmit) don't
// call back into this package synchronously.

func setConnecting(connecting bool) {
	probeStatusMu.Lock()
	defer probeStatusMu.Unlock()
	if quicConnecting == connecting {
		return
	}
	quicConnecting = connecting
	for _, fn := range connectingListeners {
		fn(connecting)
	}
}

func setHostViews(views []HostProbeView) {
	probeStatusMu.Lock()
	defer probeStatusMu.Unlock()
	hostProbeViews = copyHostViews(views)
	snapshot := copyHostViews(hostProbeViews)
	for _, fn := range hostsListeners {
		fn(snapshot)
	}
}

func clearHostViews() {
	setHostViews(nil)
}

func updateHostView(addr, status string, pingMs int64) {
	probeStatusMu.Lock()
	defer probeStatusMu.Unlock()
	found := false
	for i := range hostProbeViews {
		if hostProbeViews[i].Addr == addr {
			hostProbeViews[i].Status = status
			hostProbeViews[i].PingMs = pingMs
			found = true
			break
		}
	}
	if !found {
		hostProbeViews = append(hostProbeViews, HostProbeView{
			Addr:   addr,
			Status: status,
			PingMs: pingMs,
		})
	}
	snapshot := copyHostViews(hostProbeViews)
	for _, fn := range hostsListeners {
		fn(snapshot)
	}
}

// initHostViewsPending resets the given hosts to "pending" ahead of a probe
// round. Hosts already tracked but outside this round (e.g. a failed
// preferred host excluded from fallback candidates) are kept as-is instead
// of being dropped from the UI snapshot.
func initHostViewsPending(hosts []string) {
	probeStatusMu.Lock()
	defer probeStatusMu.Unlock()
	seen := make(map[string]bool, len(hosts))
	merged := make([]HostProbeView, 0, len(hosts)+len(hostProbeViews))
	for _, addr := range hosts {
		seen[addr] = true
		merged = append(merged, HostProbeView{Addr: addr, Status: HostPending})
	}
	for _, v := range hostProbeViews {
		if !seen[v.Addr] {
			merged = append(merged, v)
		}
	}
	hostProbeViews = merged
	snapshot := copyHostViews(hostProbeViews)
	for _, fn := range hostsListeners {
		fn(snapshot)
	}
}

func markHostProbing(addr string) {
	updateHostView(addr, HostProbing, 0)
}

func copyHostViews(in []HostProbeView) []HostProbeView {
	if len(in) == 0 {
		return nil
	}
	out := make([]HostProbeView, len(in))
	copy(out, in)
	return out
}
