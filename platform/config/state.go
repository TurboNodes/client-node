package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const stateFile = "state.json"

// legacySessionFile held Supabase access and refresh tokens back when the
// client signed in to Supabase directly.
const legacySessionFile = "session.json"

// RemoveLegacySession deletes the Supabase session file left behind by older
// versions. The client no longer talks to Supabase, so those are live
// credentials sitting on disk that nothing will ever use or rotate. Best
// effort: failing to remove it must not stop the app from starting.
func RemoveLegacySession() {
	dir, err := configDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, legacySessionFile))
}

// NodeState is the small bit of UI state worth surviving a restart, so the
// popup can render the connected view immediately instead of flashing the
// connecting view until the server speaks.
//
// TotalRewards is this node's own total, not the account's: one account can
// have several nodes paired. It is kept as the raw string the server sent
// (a plain USD amount, e.g. "0.0015") so no precision is lost to a float
// round-trip before it reaches the UI.
type NodeState struct {
	Paired       bool   `json:"paired"`
	TotalRewards string `json:"total_rewards,omitempty"`
}

func statePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFile), nil
}

// LoadNodeState reads persisted node state. A missing or unreadable file is
// not an error: it just means "nothing known yet", which is the same thing
// the UI shows before the server reports in.
func LoadNodeState() NodeState {
	path, err := statePath()
	if err != nil {
		return NodeState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return NodeState{}
	}
	var state NodeState
	if err := json.Unmarshal(data, &state); err != nil {
		return NodeState{}
	}
	return state
}

// SaveNodeState writes node state to disk, creating the config directory if needed.
func SaveNodeState(state NodeState) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	path, err := statePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding node state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing node state: %w", err)
	}
	return nil
}
