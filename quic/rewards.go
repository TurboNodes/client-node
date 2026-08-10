package quic

import (
	"log"
	"strings"
	"sync"

	"client/platform/config"
)

type rewardsListener func(total string)

var (
	rewardsMu        sync.Mutex
	totalRewards     string
	rewardsListeners []rewardsListener
)

func init() {
	// Seed from disk so the connected view can render a number immediately on
	// launch, before the server sends its first total_rewards.
	state := config.LoadNodeState()
	totalRewards = state.TotalRewards
	if state.Paired {
		restorePaired()
	}
}

// TotalRewards is the latest per-node reward total reported by the server,
// as a plain USD amount (e.g. "0.0015"). Empty means nothing reported yet.
func TotalRewards() string {
	rewardsMu.Lock()
	defer rewardsMu.Unlock()
	return totalRewards
}

// OnRewardsChange registers a listener for total_rewards updates.
// If a total is already known, the listener is called immediately.
func OnRewardsChange(fn rewardsListener) {
	if fn == nil {
		return
	}
	rewardsMu.Lock()
	rewardsListeners = append(rewardsListeners, fn)
	total := totalRewards
	rewardsMu.Unlock()

	if total != "" {
		fn(total)
	}
}

func handleTotalRewards(msg Message) {
	total := strings.TrimSpace(msg.Data)
	if total == "" {
		return
	}

	rewardsMu.Lock()
	if totalRewards == total {
		rewardsMu.Unlock()
		return
	}
	totalRewards = total
	listeners := append([]rewardsListener(nil), rewardsListeners...)
	rewardsMu.Unlock()

	persistNodeState()

	for _, fn := range listeners {
		fn(total)
	}
}

// persistNodeState writes the current paired flag and reward total to disk.
// Both live behind different mutexes, so it reads them through their own
// accessors rather than assuming either lock is held.
func persistNodeState() {
	state := config.NodeState{
		Paired:       Status() == PairingDone,
		TotalRewards: TotalRewards(),
	}
	if err := config.SaveNodeState(state); err != nil {
		log.Println("persisting node state:", err)
	}
}
