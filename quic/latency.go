package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

const (
	probeSamples = 5
	probeTimeout = 2 * time.Second
	fasterRatio  = 0.8 // new median must be <= 80% of current to count as significantly faster

	// minProbeVisibleDuration floors how long a host stays in "probing" before
	// its result lands. Hosts that fail via an instant DNS error (e.g. a
	// nonexistent subdomain) would otherwise flip pending -> down in a couple
	// milliseconds, which reads as "never actually tried" even though a real
	// dial attempt did happen.
	minProbeVisibleDuration = 400 * time.Millisecond
)

type hostLatency struct {
	Addr    string
	Samples []time.Duration
	Median  time.Duration
}

// probeHosts dials each host probeSamples times and returns results sorted by median RTT ascending.
// Unreachable hosts are omitted from the return value but still appear in the UI snapshot as down.
func probeHosts(hosts []string) []hostLatency {
	if len(hosts) == 0 {
		return nil
	}

	initHostViewsPending(hosts)

	results := make([]hostLatency, len(hosts))
	var wg sync.WaitGroup

	for i, addr := range hosts {
		wg.Add(1)
		go func(i int, addr string) {
			defer wg.Done()
			markHostProbing(addr)
			start := time.Now()
			results[i] = probeHost(addr, clientTLSConfig(addr))
			if elapsed := time.Since(start); elapsed < minProbeVisibleDuration {
				time.Sleep(minProbeVisibleDuration - elapsed)
			}
			if len(results[i].Samples) > 0 {
				updateHostView(addr, HostUp, results[i].Median.Milliseconds())
			} else {
				updateHostView(addr, HostDown, 0)
			}
		}(i, addr)
	}
	wg.Wait()

	var ok []hostLatency
	for _, r := range results {
		if len(r.Samples) > 0 {
			ok = append(ok, r)
		}
	}
	sort.Slice(ok, func(i, j int) bool {
		return ok[i].Median < ok[j].Median
	})
	return ok
}

// maxDeadFailures bounds how many consecutive failed samples we'll eat before
// giving up on a host with zero successes so far. A host that fails outright
// (refused, unreachable, no QUIC listener, or a black-holed address that eats
// the full probeTimeout with no response at all) isn't going to start
// answering on the next sample either, so retrying it just makes "down" take
// longer to report — up to maxDeadFailures*probeTimeout per host otherwise.
const maxDeadFailures = 1

func probeHost(addr string, tlsConf *tls.Config) hostLatency {
	var samples []time.Duration
	failures := 0
	for i := 0; i < probeSamples; i++ {
		rtt, err := measureDialRTT(addr, tlsConf)
		if err != nil {
			failures++
			if len(samples) == 0 && failures >= maxDeadFailures {
				break
			}
			continue
		}
		samples = append(samples, rtt)
	}
	hl := hostLatency{Addr: addr, Samples: samples}
	if len(samples) > 0 {
		hl.Median = medianDuration(samples)
	}
	return hl
}

func measureDialRTT(addr string, tlsConf *tls.Config) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	start := time.Now()
	conn, err := quic.DialAddr(ctx, addr, tlsConf, nil)
	rtt := time.Since(start)
	if err != nil {
		return 0, err
	}
	_ = conn.CloseWithError(0, "latency probe")
	return rtt, nil
}

func medianDuration(samples []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// selectBestHost probes all hosts and returns the address with the lowest median RTT.
func selectBestHost(hosts []string) (string, error) {
	results := probeHosts(hosts)
	if len(results) == 0 {
		return "", errNoReachableHosts
	}
	best := results[0]
	log.Printf("selected host %s (median RTT %s)", best.Addr, best.Median)
	return best.Addr, nil
}

// isReliablyFaster reports whether candidate is reliably faster than current.
// Requires lower median, median <= 80% of current, and a majority of pairwise sample comparisons favoring candidate.
func isReliablyFaster(current, candidate hostLatency) bool {
	if len(candidate.Samples) == 0 || len(current.Samples) == 0 {
		return false
	}
	if candidate.Median >= current.Median {
		return false
	}
	if candidate.Median > time.Duration(float64(current.Median)*fasterRatio) {
		return false
	}

	wins := 0
	n := min(len(current.Samples), len(candidate.Samples))
	for i := 0; i < n; i++ {
		if candidate.Samples[i] < current.Samples[i] {
			wins++
		}
	}
	return wins > n/2
}

// findBetterHost probes all hosts and returns a reliably faster alternative to preferred, if any.
func findBetterHost(hosts []string, preferred string) (string, bool) {
	results := probeHosts(hosts)
	if len(results) == 0 {
		return "", false
	}

	var current *hostLatency
	for i := range results {
		if results[i].Addr == preferred {
			current = &results[i]
			break
		}
	}
	// Preferred unreachable during probe: still probe current alone for comparison baseline.
	if current == nil {
		markHostProbing(preferred)
		start := time.Now()
		probed := probeHost(preferred, clientTLSConfig(preferred))
		if elapsed := time.Since(start); elapsed < minProbeVisibleDuration {
			time.Sleep(minProbeVisibleDuration - elapsed)
		}
		if len(probed.Samples) == 0 {
			updateHostView(preferred, HostDown, 0)
			// Preferred down; take the best reachable host.
			log.Printf("preferred host %s unreachable; switching to %s", preferred, results[0].Addr)
			return results[0].Addr, true
		}
		updateHostView(preferred, HostUp, probed.Median.Milliseconds())
		current = &probed
	}

	for i := range results {
		if results[i].Addr == preferred {
			continue
		}
		if isReliablyFaster(*current, results[i]) {
			log.Printf("host %s reliably faster than %s (%s vs %s)",
				results[i].Addr, preferred, results[i].Median, current.Median)
			return results[i].Addr, true
		}
	}
	return "", false
}

var errNoReachableHosts = errors.New("no reachable QUIC hosts")
