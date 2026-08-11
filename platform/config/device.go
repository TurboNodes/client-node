package config

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const deviceIDFile = "device_id"

var (
	deviceIDOnce sync.Once
	deviceID     string
)

// DeviceID is this installation's permanent identity: generated once on first
// run and persisted to disk, then reused for the life of the install.
//
// It exists because nodeIp is not a stable identity — it is whatever address
// the network handed this process right now, and on a dynamic IP a
// completely different machine can be handed the same one later. Server-side
// pairing and earnings are keyed on this instead, so the same install keeps
// its earnings across unpair/re-pair and reconnects, while a different
// machine that happens to inherit its old IP starts with nothing.
//
// Wiping the config directory (reinstall, factory reset) forfeits it — a
// wiped machine has no claim on a previous install's earnings, which is the
// intended behavior, not a bug to work around.
func DeviceID() string {
	deviceIDOnce.Do(loadOrCreateDeviceID)
	return deviceID
}

func loadOrCreateDeviceID() {
	path, err := deviceIDPath()
	if err != nil {
		deviceID = generateDeviceID()
		return
	}

	if data, readErr := os.ReadFile(path); readErr == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			deviceID = id
			return
		}
	}

	deviceID = generateDeviceID()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(deviceID), 0o600)
}

func deviceIDPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, deviceIDFile), nil
}

func generateDeviceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing means the OS entropy source is broken. There is no
		// sane fallback that keeps the id unguessable, but it still has to be
		// unique enough to not collide with another install.
		binary.BigEndian.PutUint64(buf[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(buf[8:], uint64(os.Getpid()))
	}
	return hex.EncodeToString(buf)
}
