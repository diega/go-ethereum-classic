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
	"math/big"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// forceSyncCycle is the duration between forced syncs to ensure the node
	// periodically checks for new peers with higher block height.
	forceSyncCycle = 10 * time.Second

	// minSyncPeers is the minimum number of peers required before starting sync.
	minSyncPeers = 1

	// minMESSPeers is the minimum number of peers required for MESS to be active.
	// This prevents MESS from blocking reorgs when the node is isolated.
	minMESSPeers = 5
)

// chainSyncerPoW coordinates chain synchronization for perpetual PoW networks.
// It monitors connected peers and initiates sync when a peer with higher
// TD (Total Difficulty) is found.
type chainSyncerPoW struct {
	handler     *handler
	force       *time.Timer
	forced      bool
	peerEventCh chan struct{}
	doneCh      chan struct{} // used to signal that the syncer is done
	running     atomic.Bool
}

// newChainSyncerPoW creates a new chain syncer for PoW networks.
func newChainSyncerPoW(handler *handler) *chainSyncerPoW {
	return &chainSyncerPoW{
		handler:     handler,
		peerEventCh: make(chan struct{}, 1),
		doneCh:      make(chan struct{}),
	}
}

// handlePeerEvent notifies the syncer that a peer event occurred.
// This is called when a peer connects or updates its head.
func (cs *chainSyncerPoW) handlePeerEvent() {
	select {
	case cs.peerEventCh <- struct{}{}:
	default:
		// Already notified, skip
	}
}

// loop runs the main sync loop. It waits for peer events and periodically
// checks if sync should be started.
func (cs *chainSyncerPoW) loop() {
	defer cs.handler.wg.Done()
	defer close(cs.doneCh)

	cs.force = time.NewTimer(forceSyncCycle)
	defer cs.force.Stop()

	for {
		log.Debug("chainSyncerPoW loop iteration", "forced", cs.forced)
		if op := cs.nextSyncOp(); op != nil {
			cs.startSync(op)
		}

		select {
		case <-cs.peerEventCh:
			// Peer event occurred, check if we should sync
			log.Debug("chainSyncerPoW: peer event received")
			cs.forced = false
		case <-cs.force.C:
			// Force sync timer expired
			log.Debug("chainSyncerPoW: force timer expired")
			cs.forced = true
		case <-cs.handler.quitSync:
			// Handler is shutting down
			return
		}

		// Reset the force timer
		cs.force.Reset(forceSyncCycle)
	}
}

// syncOp represents a sync operation to perform.
type syncOp struct {
	mode   downloader.SyncMode
	peer   *ethPeer
	head   common.Hash
	td     *big.Int
	peerID string
}

// nextSyncOp determines if sync should be started and with which peer.
// Uses TD (Total Difficulty) comparison to decide if we need to sync.
// Returns nil if sync should not start.
func (cs *chainSyncerPoW) nextSyncOp() *syncOp {
	// Don't start sync if already running
	if cs.running.Load() {
		log.Debug("nextSyncOp: sync already running")
		return nil
	}

	// Check if we have enough peers
	peerCount := cs.handler.peers.len()
	if peerCount < minSyncPeers && !cs.forced {
		log.Debug("nextSyncOp: not enough peers", "count", peerCount, "min", minSyncPeers, "forced", cs.forced)
		return nil
	}

	// Find the peer with highest TD
	peer := cs.handler.peers.peerWithHighestTD()
	if peer == nil {
		log.Debug("nextSyncOp: no peer with TD found", "peerCount", peerCount)
		return nil
	}

	// Get peer's head and TD from the handshake data
	peerHead, peerTD := eth.GetPeerHead(peer.ID())
	if peerTD == nil {
		log.Debug("nextSyncOp: peer has no TD in handshake data", "peer", peer.ID()[:8])
		return nil
	}

	// Get our local TD
	localHead := cs.handler.chain.CurrentBlock()
	localTD := cs.handler.chain.GetTd(localHead.Hash(), localHead.Number.Uint64())
	if localTD == nil {
		// This should not happen as TD is always written with the genesis block
		log.Error("nextSyncOp: local TD not available", "localBlock", localHead.Number, "hash", localHead.Hash())
		return nil
	}

	// Only sync if peer has higher TD than us
	if peerTD.Cmp(localTD) <= 0 {
		log.Debug("nextSyncOp: peer TD not higher", "peerTD", peerTD, "localTD", localTD, "localBlock", localHead.Number)

		// We're synced - check if we should enable MESS
		cs.checkMESSActivation()
		return nil
	}

	log.Debug("nextSyncOp: will start sync", "peer", peer.ID()[:8], "peerTD", peerTD, "localTD", localTD)

	// Determine sync mode - always use full sync for PoW chains
	mode := downloader.FullSync

	return &syncOp{
		mode:   mode,
		peer:   peer,
		head:   peerHead,
		td:     peerTD,
		peerID: peer.ID(),
	}
}

// startSync starts a sync operation with the given parameters.
func (cs *chainSyncerPoW) startSync(op *syncOp) {
	cs.running.Store(true)
	defer cs.running.Store(false)

	log.Info("Starting PoW chain sync", "peer", op.peerID[:8], "head", op.head.Hex()[:10], "peerTD", op.td)

	// Call the handler's doSync method which will use the downloader
	err := cs.handler.doSync(op.mode, op.peer, op.head)
	if err != nil {
		log.Warn("PoW sync failed", "peer", op.peerID[:8], "err", err)
	} else {
		log.Info("PoW sync completed", "peer", op.peerID[:8])
	}
}

// shouldEnableMESS determines if MESS should be activated.
// Returns true if:
// - messForceDisable is NOT set
// - AND (messForceEnable is set OR chainConfig.IsECBP1100(currentBlock) is true)
func (cs *chainSyncerPoW) shouldEnableMESS() bool {
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
func (cs *chainSyncerPoW) checkMESSActivation() {
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
