package quic

import (
	"io"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

// relayTunnel pipes raw bytes between a dedicated QUIC stream and the target
// TCP connection until either side closes, then closes both.
func relayTunnel(stream *quic.Stream, target net.Conn) {
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			stream.Close()
			target.Close()
		})
	}

	done := make(chan struct{})
	go func() {
		io.Copy(stream, target)
		closeBoth()
		close(done)
	}()

	io.Copy(target, stream)
	closeBoth()
	<-done
}
