package quic

import (
	"bufio"
	"client/platform/config"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const hostServersURL = "https://raw.githubusercontent.com/TurboNodes/Turbo/main/.github/host-servers"

// defaultHostPort is used for host-servers entries that omit a port.
const defaultHostPort = "443"

var (
	hostStateMu sync.Mutex
	hostCache   *config.HostCache
)

// ensureHosts loads the host list from cache or fetches it from GitHub.
// refetched is true only when the list was successfully pulled from GitHub
// (cache missing/expired); callers should run a latency probe only then.
func ensureHosts() (cache *config.HostCache, refetched bool, err error) {
	hostStateMu.Lock()
	defer hostStateMu.Unlock()

	if hostCache != nil && !hostCache.IsExpired() {
		return hostCache, false, nil
	}

	cached, loadErr := config.LoadHostCache()
	if loadErr != nil {
		log.Println("hosts cache load:", loadErr)
	}

	if cached != nil && !cached.IsExpired() {
		hostCache = cached
		return hostCache, false, nil
	}

	hosts, fetchErr := fetchHostServers()
	if fetchErr != nil {
		if cached != nil && len(cached.Hosts) > 0 {
			log.Println("host-servers fetch failed, using stale cache:", fetchErr)
			hostCache = cached
			return hostCache, false, nil
		}
		return nil, false, fmt.Errorf("fetching host-servers: %w", fetchErr)
	}

	preferred := ""
	if cached != nil {
		preferred = cached.Preferred
	}

	hostCache = &config.HostCache{
		FetchedAt: time.Now().UTC(),
		Hosts:     hosts,
		Preferred: preferred,
	}
	if err := config.SaveHostCache(hostCache); err != nil {
		log.Println("saving hosts cache:", err)
	} else {
		log.Printf("logged %d hosts to hosts.json (preferred=%q): %v", len(hosts), preferred, hosts)
	}
	return hostCache, true, nil
}

// forceRefreshHosts fetches host-servers from GitHub even if the local cache is still fresh.
// Preferred is preserved. On fetch failure with an existing cache, that cache is kept and
// refetched is false.
func forceRefreshHosts() (cache *config.HostCache, refetched bool, err error) {
	hostStateMu.Lock()
	defer hostStateMu.Unlock()

	preferred := ""
	var prev *config.HostCache
	if hostCache != nil {
		preferred = hostCache.Preferred
		prev = hostCache
	} else if cached, loadErr := config.LoadHostCache(); loadErr == nil && cached != nil {
		preferred = cached.Preferred
		prev = cached
	}

	hosts, fetchErr := fetchHostServers()
	if fetchErr != nil {
		if prev != nil && len(prev.Hosts) > 0 {
			hostCache = prev
			return hostCache, false, fetchErr
		}
		return nil, false, fmt.Errorf("fetching host-servers: %w", fetchErr)
	}

	hostCache = &config.HostCache{
		FetchedAt: time.Now().UTC(),
		Hosts:     hosts,
		Preferred: preferred,
	}
	if err := config.SaveHostCache(hostCache); err != nil {
		log.Println("saving hosts cache:", err)
	} else {
		log.Printf("refreshed %d hosts from GitHub (preferred=%q): %v", len(hosts), preferred, hosts)
	}
	return hostCache, true, nil
}

func fetchHostServers() ([]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, hostServersURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Turbo-client/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return parseHostServers(resp.Body)
}

func parseHostServers(r io.Reader) ([]string, error) {
	var hosts []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, _, err := net.SplitHostPort(line); err != nil {
			if addrErr, ok := err.(*net.AddrError); ok && strings.Contains(addrErr.Err, "missing port") {
				line = net.JoinHostPort(line, defaultHostPort)
			} else {
				log.Printf("skipping invalid host entry %q: %v", line, err)
				continue
			}
		}
		hosts = append(hosts, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no valid hosts in host-servers list")
	}
	return hosts, nil
}

func preferredHost() string {
	hostStateMu.Lock()
	defer hostStateMu.Unlock()
	if hostCache == nil {
		return ""
	}
	return hostCache.Preferred
}

// cachedHosts returns a copy of the persisted host list (GitHub fallback pool).
func cachedHosts() []string {
	hostStateMu.Lock()
	defer hostStateMu.Unlock()
	if hostCache == nil || len(hostCache.Hosts) == 0 {
		return nil
	}
	out := make([]string, len(hostCache.Hosts))
	copy(out, hostCache.Hosts)
	return out
}

func setPreferredHost(addr string) {
	hostStateMu.Lock()
	defer hostStateMu.Unlock()
	if hostCache == nil {
		hostCache = &config.HostCache{}
	}
	hostCache.Preferred = addr
	if err := config.SaveHostCache(hostCache); err != nil {
		log.Println("saving preferred host:", err)
	}
}
