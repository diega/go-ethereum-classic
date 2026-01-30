// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package downloader

import (
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

// peerStats tracks failure information for a peer.
type peerStats struct {
	lastFailure time.Time
	failCount   int
}

var (
	peerFailures   = make(map[string]*peerStats)
	peerFailuresMu sync.RWMutex
)

// MarkPeerFailed records that a peer failed to provide data.
// This is used by IdlePeer to avoid selecting peers that recently failed.
func MarkPeerFailed(id string) {
	peerFailuresMu.Lock()
	defer peerFailuresMu.Unlock()
	if stats, ok := peerFailures[id]; ok {
		stats.lastFailure = time.Now()
		stats.failCount++
	} else {
		peerFailures[id] = &peerStats{lastFailure: time.Now(), failCount: 1}
	}
}

// peerFailureCooldown is the time a peer must wait after a failure
// before being selected again by IdlePeer.
const peerFailureCooldown = 5 * time.Second

// BestPeer returns the best peer for syncing (for PoW sync).
// Currently returns any available peer, can be enhanced to pick based on TD.
func (ps *peerSet) BestPeer() *peerConnection {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	// Return first available peer
	for _, p := range ps.peers {
		return p
	}
	return nil
}

// IdlePeer returns an idle peer for making requests.
// It avoids peers that have recently failed (within peerFailureCooldown).
// If all peers are in cooldown, it returns any available peer as fallback.
// Peers are sorted by header capacity (descending) to prefer faster peers.
func (ps *peerSet) IdlePeer() *peerConnection {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	var candidates []*peerConnection
	var cooldownCount int
	now := time.Now()

	for _, peer := range ps.peers {
		// Skip peers that failed recently (cooldown period with exponential backoff)
		peerFailuresMu.RLock()
		if stats, ok := peerFailures[peer.id]; ok {
			// Exponential backoff: 5s, 10s, 20s, 40s, 80s, 160s (max)
			backoff := peerFailureCooldown * time.Duration(1<<min(stats.failCount-1, 5))
			if now.Sub(stats.lastFailure) < backoff {
				cooldownCount++
				peerFailuresMu.RUnlock()
				continue
			}
		}
		peerFailuresMu.RUnlock()
		candidates = append(candidates, peer)
	}

	// Sort candidates by header capacity (descending) - prefer faster peers
	if len(candidates) > 1 && ps.rates != nil {
		targetRTT := ps.rates.TargetRoundTrip()
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].HeaderCapacity(targetRTT) > candidates[j].HeaderCapacity(targetRTT)
		})
	}

	// Log peer selection for diagnostics
	if len(candidates) > 0 {
		selected := candidates[0]
		var maxCap, selectedCap int
		if ps.rates != nil {
			targetRTT := ps.rates.TargetRoundTrip()
			selectedCap = selected.HeaderCapacity(targetRTT)
			for _, c := range candidates {
				cap := c.HeaderCapacity(targetRTT)
				if cap > maxCap {
					maxCap = cap
				}
			}
		}
		log.Debug("IdlePeer selection",
			"totalPeers", len(ps.peers),
			"candidates", len(candidates),
			"inCooldown", cooldownCount,
			"selectedPeer", selected.id[:8],
			"selectedCapacity", selectedCap,
			"maxCapacity", maxCap)
		return selected
	}

	// Fallback: if all peers are in cooldown, return any peer
	// This prevents complete stalls when all peers have recent failures
	log.Debug("IdlePeer fallback - all peers in cooldown",
		"totalPeers", len(ps.peers),
		"inCooldown", cooldownCount)
	for _, peer := range ps.peers {
		return peer
	}
	return nil
}
