package quic

import (
	"log"
	"net"
	"time"

	"github.com/quic-go/quic-go"
)

// connectDialTimeout bounds how long the node waits for the target to accept.
// Keep it below the server's connectTimeout so the server gets an explicit
// failure and can retry another node instead of timing out blind.
const connectDialTimeout = 4 * time.Second

// handleTunnel serves one server-opened QUIC stream: read the target address
// (and any bundled first payload) the server wants us to reach, dial it, ack
// with a status byte, then relay the stream <-> target connection raw until
// either side closes. Unlike the old single-stream design, this stream *is*
// the connection to the target -- no JSON envelope, no shared mutex.
func handleTunnel(stream *quic.Stream) {
	if err := stream.SetReadDeadline(time.Now().Add(connectDialTimeout)); err != nil {
		log.Println("tunnel: set read deadline:", err)
		stream.Close()
		return
	}
	addr, payload, err := readTunnelHeader(stream)
	if err != nil {
		log.Println("tunnel: read header:", err)
		stream.Close()
		return
	}
	stream.SetReadDeadline(time.Time{})

	conn, err := net.DialTimeout("tcp", addr, connectDialTimeout)
	if err != nil {
		log.Printf("connect to %s failed: %v", addr, err)
		stream.Write([]byte{tunnelFailed})
		stream.Close()
		return
	}

	if _, err := stream.Write([]byte{tunnelOK}); err != nil {
		log.Printf("ack tunnel to %s: %v", addr, err)
		conn.Close()
		stream.Close()
		return
	}

	if len(payload) > 0 {
		if _, err := conn.Write(payload); err != nil {
			log.Printf("write initial payload to %s: %v", addr, err)
			conn.Close()
			stream.Close()
			return
		}
	}

	relayTunnel(stream, conn)
}
