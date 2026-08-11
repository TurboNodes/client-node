package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The install token is written by `turbod --install` and read back by a
// different process — the service the installer just registered — so the
// round-trip through disk is the whole contract.
func TestPairTokenRoundTrip(t *testing.T) {
	withTempConfigDir(t)

	if got := PairToken(); got != "" {
		t.Fatalf("PairToken() on a fresh install = %q, want empty", got)
	}

	const token = "5f1c3a7e-0b2d-4c8a-9e1f-7a6b5c4d3e2f"
	if err := SavePairToken(token); err != nil {
		t.Fatalf("SavePairToken: %v", err)
	}
	if got := PairToken(); got != token {
		t.Errorf("PairToken() = %q, want %q", got, token)
	}

	ClearPairToken()
	if got := PairToken(); got != "" {
		t.Errorf("PairToken() after clear = %q, want empty", got)
	}

	// Single use: clearing twice is normal, since every "paired" message
	// clears it and the server resends that on every reconnect.
	ClearPairToken()
}

// The token pairs a machine to an account, so it must not be world-readable
// while it is on disk.
func TestSavePairTokenIsNotWorldReadable(t *testing.T) {
	dir := withTempConfigDir(t)

	if err := SavePairToken("token"); err != nil {
		t.Fatalf("SavePairToken: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, pairTokenFile))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %#o, want 0600", perm)
	}
}

// A token written with a trailing newline (redirected in by hand, or by an
// editor) must still be sent as the bare token.
func TestPairTokenTrimsWhitespace(t *testing.T) {
	dir := withTempConfigDir(t)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, pairTokenFile)
	if err := os.WriteFile(path, []byte("  token-with-space\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := PairToken(); got != "token-with-space" {
		t.Errorf("PairToken() = %q, want %q", got, "token-with-space")
	}
}

// withTempConfigDir redirects the config directory into a temp directory and
// returns the resolved path, so tests never touch the real one. Each platform
// derives it differently (XDG on Linux, the home directory on macOS, AppData
// on Windows), so all three are set and configDir has the final say.
func withTempConfigDir(t *testing.T) string {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("AppData", tmp)

	dir, err := configDir()
	if err != nil {
		t.Fatalf("configDir: %v", err)
	}
	if !strings.HasPrefix(dir, tmp) {
		t.Fatalf("config dir %q escaped the temp dir %q", dir, tmp)
	}
	return dir
}
