// Package singleinstance keeps exactly one Turbo process alive per user and
// turns a second launch into a request that the first one show itself.
//
// This matters more than the usual "don't run twice" tidiness: a second
// process would add its own tray icon and open its own QUIC session, so the
// user would see two icons and the server would see two nodes. It also gives
// stealth mode its way back — with no icon and no window, relaunching the app
// is the only gesture left, and it has to reach the running process.
package singleinstance

import (
	"bufio"
	"net"
	"time"
)

// showCommand is the single message understood over the channel.
const showCommand = "show"

// Acquire tries to become the primary instance.
//
// If it succeeds it returns true and calls onShow whenever another launch asks
// the UI to be revealed. If another instance already holds the channel it sends
// the show request to it and returns false — the caller must then exit
// immediately, before starting QUIC or a tray icon.
func Acquire(onShow func()) (bool, error) {
	ln, err := listen()
	if err == nil {
		go serve(ln, onShow)
		return true, nil
	}

	// Someone holds it. Ask them to surface, then report that we are secondary.
	// A failure to notify is not fatal: the other process is still running, so
	// exiting remains the right move.
	if notifyErr := notifyShow(); notifyErr != nil {
		return false, notifyErr
	}
	return false, nil
}

func serve(ln net.Listener, onShow func()) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			line, err := bufio.NewReader(c).ReadString('\n')
			if err != nil && line == "" {
				return
			}
			if trimNewline(line) == showCommand && onShow != nil {
				onShow()
			}
		}(conn)
	}
}

func notifyShow() error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write([]byte(showCommand + "\n"))
	return err
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
