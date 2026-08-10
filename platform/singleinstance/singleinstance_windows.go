//go:build windows

package singleinstance

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// pipeName is per-user rather than global: two different users on the same
// machine each get their own node, and neither should block the other.
const pipeName = `\\.\pipe\turbo-single-instance`

// listen creates the named pipe. Unlike a unix socket there is no file left
// behind to go stale — the pipe disappears with the process that owned it, so
// a crashed instance never blocks the next launch.
func listen() (net.Listener, error) {
	return winio.ListenPipe(pipeName, nil)
}

func dial() (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(pipeName, &timeout)
}
