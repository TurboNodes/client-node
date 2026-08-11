package quic

import (
	"client/platform/config"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

type Message struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Addr string `json:"addr,omitempty"`
	Data string `json:"data,omitempty"`
	// Token carries a single-use install token on "hello" — see
	// config.PairToken. Empty on every other message, and on every node that
	// was not installed from the website's terminal command.
	Token string `json:"token,omitempty"`
}

var (
	quicConn   *quic.Conn
	quicStream *quic.Stream
	quicMutex  sync.Mutex
)

/* Retry policy after the initial connect fails:
1. Launch: one automatic attempt against the known/cached hosts, no fetch.
2. If that fails: exactly one automatic GitHub refresh + reprobe.
3. If that also fails: stop retrying automatically. From then on the loop
   only wakes on the 5-minute heartbeat (reprobes cached hosts, no fetch)
   or a manual Retry from the UI (RequestReconnect), which always refreshes
   from GitHub first. A successful connection resets back to step 1 so the
   next disconnect starts a fresh attempt. */

// retryStage tracks where ConnectQuicServer is in the policy above.
type retryStage int

const (
	stageInitial   retryStage = iota // launch, or right after a successful connection drops
	stageAutoFetch                   // the one automatic post-failure GitHub refresh + reprobe
	stageHeartbeat                   // automatic retries exhausted; 5-min heartbeat + manual retry only
)

func ConnectQuicServer() {
	const noHostRetry = 5 * time.Minute
	const dialTimeout = 8 * time.Second

	for {
		setConnecting(true)
		cache, refetched, err := ensureHosts()
		if err != nil {
			log.Println("host discovery failed:", err)
			log.Println("Retrying in 5 minutes...")
			sleepInterruptible(noHostRetry)
			continue
		}

		// With a preferred host, only re-probe after a successful GitHub refetch.
		if cache.Preferred != "" {
			if refetched {
				if better, ok := findBetterHost(cache.Hosts, cache.Preferred); ok {
					setPreferredHost(better)
				} else {
					log.Println("latency probe: preferred host remains optimal")
				}
			}
			break
		}

		// No preferred yet (first launch or lost): probe after refetch, or against
		// a stale list if GitHub is unreachable.
		best, err := selectBestHost(cache.Hosts)
		if err != nil {
			log.Println("no reachable QUIC hosts:", err)
			log.Println("Retrying in 5 minutes...")
			sleepInterruptible(noHostRetry)
			continue
		}
		setPreferredHost(best)
		break
	}

	stage := stageInitial
	fetchFirst := false // whether this iteration should refresh from GitHub before probing

	for {
		setConnecting(true)

		if fetchFirst {
			if _, _, refreshErr := forceRefreshHosts(); refreshErr != nil {
				log.Println("host-servers refresh failed:", refreshErr)
			}
			fetchFirst = false
		}

		hosts := cachedHosts()
		if len(hosts) == 0 {
			cache, _, err := ensureHosts()
			if err != nil || cache == nil || len(cache.Hosts) == 0 {
				log.Println("no known hosts; waiting for heartbeat or manual retry...")
				stage = stageHeartbeat
				fetchFirst = sleepInterruptible(noHostRetry)
				continue
			}
			hosts = cache.Hosts
		}

		// Race every known host in parallel so the connecting UI reflects real
		// concurrent probing (and so a dead preferred host doesn't block the
		// others behind a long sequential timeout).
		results := probeHosts(hosts)

		connected := false
		if len(results) == 0 {
			log.Println("no reachable QUIC hosts this round")
		} else {
			// Stick with the known-good preferred host if it answered this round;
			// otherwise take the fastest reachable alternative for this attempt.
			preferred := preferredHost()
			addr := results[0].Addr
			for _, r := range results {
				if r.Addr == preferred {
					addr = preferred
					break
				}
			}

			setConnecting(true)
			markHostProbing(addr)

			dialCtx, dialCancel := context.WithTimeout(context.Background(), dialTimeout)
			conn, err := quic.DialAddr(dialCtx, addr, clientTLSConfig(addr), nil)
			dialCancel()
			if err != nil {
				log.Printf("Failed to connect to QUIC server %s: %v", addr, err)
				updateHostView(addr, HostDown, 0)
			} else {
				log.Printf("Connected to QUIC server %s", addr)

				// let the server accept our bidirectional stream and register us
				time.Sleep(100 * time.Millisecond)

				// A fresh context: the dial's context was already canceled above
				// and must not be reused here, or OpenStreamSync fails immediately
				// with "context canceled" on every single connection attempt.
				streamCtx, streamCancel := context.WithTimeout(context.Background(), dialTimeout)
				stream, err := conn.OpenStreamSync(streamCtx)
				streamCancel()
				if err != nil {
					log.Println("Failed to open QUIC stream:", err)
					conn.CloseWithError(1, "failed to open stream")
					updateHostView(addr, HostDown, 0)
				} else {
					if addr != preferred {
						setPreferredHost(addr)
					}

					quicMutex.Lock()
					quicConn = conn
					quicStream = stream
					quicMutex.Unlock()
					setConnected(true)

					// Must be the first write on the stream: the server blocks the
					// rest of this session's handling on reading it before anything
					// else, so it can key pairing/earnings on a stable identity
					// instead of the connection's (possibly reused) source IP.
					//
					// Token is usually empty. It is set only for an install that
					// came from the website's terminal command, and only until the
					// server claims it — pairing this node to that account without
					// anyone opening a browser here.
					SendMessage(&Message{
						Type:  "hello",
						Data:  config.DeviceID(),
						Token: config.PairToken(),
					})

					go acceptTunnels(conn)
					quicReader(stream)

					setConnected(false)
					log.Println("QUIC connection closed, reconnecting...")
					connected = true
				}
			}
		}

		if connected {
			// Back to a clean slate: the next disconnect gets its own fresh
			// probe-first attempt, same as launch.
			stage = stageInitial
			fetchFirst = false
			sleepInterruptible(time.Second * 5)
			continue
		}

		switch stage {
		case stageInitial:
			log.Println("initial connect failed; refreshing host-servers from GitHub once...")
			stage = stageAutoFetch
			fetchFirst = true
		case stageAutoFetch:
			log.Println("automatic retry exhausted; waiting for heartbeat or manual retry...")
			stage = stageHeartbeat
			fetchFirst = sleepInterruptible(noHostRetry)
		default: // stageHeartbeat: keep waiting, only a manual retry triggers a fetch.
			fetchFirst = sleepInterruptible(noHostRetry)
		}
	}
}

// acceptTunnels accepts the server-opened streams used for proxied
// connections -- one per target site, separate from the control stream.
// It returns once the connection drops, same as quicReader.
func acceptTunnels(conn *quic.Conn) {
	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		go handleTunnel(stream)
	}
}

func quicReader(stream *quic.Stream) {
	decoder := json.NewDecoder(stream)

	for {
		var msg Message
		err := decoder.Decode(&msg)
		if err != nil {
			log.Println("QUIC read error:", err)

			quicMutex.Lock()
			quicStream = nil
			quicConn = nil
			quicMutex.Unlock()

			return
		}

		log.Printf("received %+v", msg.Type)

		switch msg.Type {
		case "ping":
			err := SendMessage(&Message{
				Type: "pong",
				ID:   msg.ID,
				Data: strconv.FormatInt(time.Now().UnixMicro(), 10),
			})
			if err != nil {
				log.Fatal("error sending pong:", err)
			}
		case "pairing_url":
			handlePairingURL(msg)
		case "paired":
			handlePaired(msg)
		case "total_rewards":
			handleTotalRewards(msg)
		}
	}
}

func SendMessage(msg *Message) error {
	quicMutex.Lock()
	defer quicMutex.Unlock()

	if quicStream == nil {
		log.Println("Cannot send message: no active QUIC stream")
		return fmt.Errorf("no active QUIC stream")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message of type %s: %v", msg.Type, err)
		return err
	}
	data = append(data, '\n')

	_, err = quicStream.Write(data)
	if err != nil {
		log.Printf("Error writing to QUIC stream: %v", err)
		return err
	}

	return nil
}
