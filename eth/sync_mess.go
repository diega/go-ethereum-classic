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

package eth

import (
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
)

// MESS (Modified Exponential Subjective Scoring) related constants and functions.
// MESS is an artificial finality mechanism for Ethereum Classic (ECIP-1100).

const (
	// minMESSPeers is the minimum number of peers required for MESS to be active.
	// This prevents MESS from blocking reorgs when the node is isolated.
	minMESSPeers = 5
)

// shouldEnableMESS determines if MESS should be activated.
// Returns true if:
// - messForceDisable is NOT set
// - AND (messForceEnable is set OR chainConfig.IsECBP1100(currentBlock) is true)
func (cs *chainSyncer) shouldEnableMESS() bool {
	// Force disable always wins
	if cs.handler.messForceDisable {
		return false
	}
	// Force enable activates even without config
	if cs.handler.messForceEnable {
		return true
	}
	// By default, follow the block config
	return cs.handler.chain.Config().IsECBP1100(cs.handler.chain.CurrentHeader().Number)
}

// checkMESSActivation checks if MESS should be enabled or disabled based on current conditions.
// MESS activation conditions:
// 1. shouldEnableMESS() returns true (config + flags check)
// 2. Node is synced (FullSync mode)
// 3. >= minMESSPeers peers connected
// 4. Head is not stale
func (cs *chainSyncer) checkMESSActivation() {
	peerCount := cs.handler.peers.len()
	chainMESSEnabled := cs.handler.chain.IsMESSEnabled()

	// Check if we should disable MESS due to low peers
	if chainMESSEnabled && peerCount < minMESSPeers {
		cs.handler.chain.EnableMESS(false, "low peers")
		return
	}

	// Check if we should enable MESS
	if !chainMESSEnabled && cs.shouldEnableMESS() && peerCount >= minMESSPeers {
		cs.handler.chain.EnableMESS(true, "synced")
	}
}

// messSafetyLoop monitors conditions for MESS safety and disables it if the head becomes stale.
// This runs for PoW chains only and periodically checks if the chain head is stale.
func (h *handler) messSafetyLoop() {
	defer h.wg.Done()

	// Safety interval: 30 * block time (DurationLimit seconds for ETC)
	// ~6.5 minutes, allowing time for natural variance while catching stale heads
	// Reference: ECIP-1100 specifies 30 * 13 seconds as the stale safety interval
	safetyInterval := 30 * time.Duration(params.DurationLimit.Int64()) * time.Second
	ticker := time.NewTicker(safetyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if h.chain.IsMESSEnabled() && isHeadStale(h.chain, safetyInterval) {
				h.chain.EnableMESS(false, "stale head")
			}
		case <-h.quitSync:
			return
		}
	}
}

// isHeadStale returns true if the chain head is older than the given interval.
// This is used by MESS to detect if the node might be stuck or isolated.
func isHeadStale(chain *core.BlockChain, interval time.Duration) bool {
	head := chain.CurrentHeader()
	if head == nil {
		return true
	}
	headTime := time.Unix(int64(head.Time), 0)
	return time.Since(headTime) > interval
}
