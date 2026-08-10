package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	appDirName   = "turbo"
	hostsFile    = "hosts.json"
	hostsTTL     = 5 * 24 * time.Hour
)

// HostCache is the persisted host list and preferred server.
type HostCache struct {
	FetchedAt time.Time `json:"fetched_at"`
	Hosts     []string  `json:"hosts"`
	Preferred string    `json:"preferred"`
}

func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(base, appDirName), nil
}

func hostsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hostsFile), nil
}

// LoadHostCache reads the cached host list from disk.
// Returns nil, nil if the file does not exist.
func LoadHostCache() (*HostCache, error) {
	path, err := hostsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading hosts cache: %w", err)
	}
	var cache HostCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parsing hosts cache: %w", err)
	}
	return &cache, nil
}

// SaveHostCache writes the host cache to disk, creating the config directory if needed.
func SaveHostCache(cache *HostCache) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	path, err := hostsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding hosts cache: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing hosts cache: %w", err)
	}
	return nil
}

// IsExpired reports whether the host list should be refetched.
func (c *HostCache) IsExpired() bool {
	if c == nil || len(c.Hosts) == 0 {
		return true
	}
	return time.Since(c.FetchedAt) > hostsTTL
}
