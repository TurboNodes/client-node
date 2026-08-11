package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const pairTokenFile = "pair_token"

// PairToken is a single-use token handed to this install by the website's
// terminal install command, to pair the node to the account that generated it
// without a browser on this machine (see cmd/turbod's --install).
//
// It is written once at install time and sent with the next "hello"; the
// server claims it and answers "paired", at which point ClearPairToken deletes
// it. A missing file is the normal steady state — every node that paired the
// ordinary way, and every node that has already paired this way, has none.
func PairToken() string {
	path, err := pairTokenPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SavePairToken persists the token for the daemon to pick up on its next
// connect. Written 0600 like the device id: it is a credential that pairs this
// machine to someone's account, and it is short-lived, but it should not be
// readable by every user on the box while it lasts.
func SavePairToken(token string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, pairTokenFile), []byte(token), 0o600); err != nil {
		return fmt.Errorf("writing pair token: %w", err)
	}
	return nil
}

// ClearPairToken removes the token once it has done its job. Best effort, and
// safe to call when there is nothing there: the token is single-use, so a
// leftover file is only ever a token the server would refuse anyway.
func ClearPairToken() {
	path, err := pairTokenPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

func pairTokenPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pairTokenFile), nil
}
