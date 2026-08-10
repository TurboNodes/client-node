//go:build !windows

package singleinstance

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const sockName = "turbo.sock"

func socketPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	dir := filepath.Join(base, "turbo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating config dir: %w", err)
	}
	return filepath.Join(dir, sockName), nil
}

// listen binds the unix socket, clearing a stale one left by a process that
// died without cleaning up. Staleness is decided by trying to connect: a
// socket file nobody is accepting on is dead, and only then is it safe to
// unlink — unlinking a live one would silently steal it from a running
// instance and reintroduce the double-tray problem.
func listen() (net.Listener, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}

	ln, err := net.Listen("unix", path)
	if err == nil {
		return ln, nil
	}

	conn, dialErr := net.DialTimeout("unix", path, time.Second)
	if dialErr == nil {
		conn.Close()
		return nil, fmt.Errorf("another instance is running")
	}

	if rmErr := os.Remove(path); rmErr != nil {
		return nil, fmt.Errorf("removing stale socket: %w", rmErr)
	}
	return net.Listen("unix", path)
}

func dial() (net.Conn, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	return net.DialTimeout("unix", path, 2*time.Second)
}
