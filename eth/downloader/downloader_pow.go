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
	"errors"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

var errNoPeers = errors.New("no peers available")

// PoWSync initiates a traditional PoW sync for perpetual PoW networks.
// This is the entry point called by the chainSyncerPoW.
// It handles the setup, events, and cleanup, then delegates to syncToHead.
func (d *Downloader) PoWSync(peerID string, head common.Hash, mode ethconfig.SyncMode) error {
	// Make sure only one goroutine is ever allowed past this point at once
	if !d.synchronising.CompareAndSwap(false, true) {
		return errBusy
	}
	defer d.synchronising.Store(false)

	// Post a user notification of the sync (only once per session)
	if d.notified.CompareAndSwap(false, true) {
		log.Info("Block synchronisation started")
	}

	// Reset the queue, peer set and wake channels to clean any internal leftover state
	d.queue.Reset(blockCacheMaxItems, blockCacheInitialItems)
	d.peers.Reset()

	for _, ch := range []chan bool{d.queue.blockWakeCh, d.queue.receiptWakeCh} {
		select {
		case <-ch:
		default:
		}
	}
	for empty := false; !empty; {
		select {
		case <-d.headerProcCh:
		default:
			empty = true
		}
	}

	// Create cancel channel for aborting mid-flight and mark the master peer
	d.cancelLock.Lock()
	d.cancelCh = make(chan struct{})
	d.cancelLock.Unlock()

	defer d.Cancel() // No matter what, we can't leave the cancel channel open

	// Atomically set the requested sync mode
	d.mode.Store(uint32(mode))

	// Store the peer's head hash for use by syncToHeadPoW
	d.peerHeadHash = head

	// Run the sync using the PoW strategy
	return d.syncToHead()
}

// syncToHeadPoW performs traditional PoW sync for perpetual PoW networks
// using concurrent fetchers for better performance.
// This is the sync strategy used when syncStrategy field is set.
// Note: Event posting (StartEvent, DoneEvent, FailedEvent) is handled by syncToHead().
func (d *Downloader) syncToHeadPoW() (err error) {
	mode := d.getMode()

	defer func(start time.Time) {
		log.Debug("PoW sync terminated", "elapsed", common.PrettyDuration(time.Since(start)))
	}(time.Now())

	// Get best peer to find common ancestor
	peer := d.peers.BestPeer()
	if peer == nil {
		return errNoPeers
	}

	// Find common ancestor and get peer's head number
	origin, remoteHeight, err := d.findAncestorPoW(peer)
	if err != nil {
		return err
	}

	// Store sync stats
	d.syncStatsLock.Lock()
	d.syncStatsChainOrigin = origin
	d.syncStatsChainHeight = remoteHeight
	d.syncStatsLock.Unlock()

	// Set PoW sync targets for the header fetcher
	d.powSyncOrigin = origin
	d.powSyncTarget = remoteHeight
	d.peerHeadNumber = remoteHeight

	// Initialize queue for this sync
	d.queue.Prepare(origin+1, mode)

	// Spawn concurrent fetchers (reuses existing go-ethereum infrastructure)
	fetchers := []func() error{
		func() error { return d.fetchHeadersPoW(origin) },
		func() error { return d.fetchBodies(origin + 1) },
	}
	if mode == ethconfig.FullSync {
		fetchers = append(fetchers, func() error { return d.processFullSyncContent() })
	}
	return d.spawnSync(fetchers)
}

// findAncestorPoW finds the common ancestor using binary search.
// Returns the ancestor block number and the remote peer's head height.
func (d *Downloader) findAncestorPoW(p *peerConnection) (uint64, uint64, error) {
	peerHeadHash := d.peerHeadHash
	if peerHeadHash == (common.Hash{}) {
		return 0, 0, errors.New("peer head hash not set")
	}

	// Fetch peer's head by HASH (not number 0)
	headers, _, err := d.fetchHeadersByHash(p, peerHeadHash, 1, 0, false)
	if err != nil {
		return 0, 0, err
	}
	if len(headers) == 0 {
		return 0, 0, errBadPeer
	}
	peerHead := headers[0]

	if peerHead.Hash() != peerHeadHash {
		p.log.Warn("Peer returned different header", "requested", peerHeadHash, "got", peerHead.Hash())
		return 0, 0, errBadPeer
	}

	remoteHeight := peerHead.Number.Uint64()
	localHeight := d.blockchain.CurrentBlock().Number.Uint64()

	log.Debug("PoW sync starting", "localHeight", localHeight, "remoteHeight", remoteHeight,
		"peerHead", peerHeadHash.TerminalString())

	if localHeight >= remoteHeight {
		return localHeight, remoteHeight, nil
	}

	// Binary search for common ancestor
	from, to := uint64(0), min(remoteHeight, localHeight)
	for from+1 < to {
		mid := (from + to) / 2
		headers, _, err := d.fetchHeadersByNumber(p, mid, 1, 0, false)
		if err != nil || len(headers) == 0 {
			return 0, 0, err
		}
		if d.blockchain.HasHeader(headers[0].Hash(), mid) {
			from = mid
		} else {
			to = mid
		}
	}

	log.Debug("Found common ancestor", "number", from, "target", remoteHeight)
	return from, remoteHeight, nil
}

// fetchHeadersPoW downloads headers from peers and schedules them for body download.
// It uses concurrentFetch with multiple peers in parallel for improved performance.
func (d *Downloader) fetchHeadersPoW(from uint64) error {
	target := d.powSyncTarget
	start := from + 1

	log.Debug("Downloading headers for PoW sync (concurrent)", "from", start, "to", target)
	defer log.Debug("Header download terminated")

	// Create task manager for the range of headers
	d.headerTaskMgr = newHeaderTaskManager(start, target)
	defer func() { d.headerTaskMgr = nil }()

	// Create queue adapter for concurrentFetch
	queue := &headerQueuePoW{d: d, taskMgr: d.headerTaskMgr}

	// Use concurrentFetch - multiple peers in parallel
	if err := d.concurrentFetch(queue); err != nil {
		return err
	}

	// Signal header download complete
	for _, ch := range []chan bool{d.queue.blockWakeCh, d.queue.receiptWakeCh} {
		select {
		case ch <- false: // false signals completion
		case <-d.cancelCh:
			return errCanceled
		}
	}
	return nil
}

// fetchHeadersByNumber fetches headers by block number from a specific peer.
func (d *Downloader) fetchHeadersByNumber(p *peerConnection, number uint64, amount int, skip int, reverse bool) ([]*types.Header, []common.Hash, error) {
	start := time.Now()
	resCh := make(chan *eth.Response)

	req, err := p.peer.RequestHeadersByNumber(number, amount, skip, reverse, resCh)
	if err != nil {
		return nil, nil, err
	}
	defer req.Close()

	ttl := d.peers.rates.TargetTimeout()
	timeoutTimer := time.NewTimer(ttl)
	defer timeoutTimer.Stop()

	select {
	case <-d.cancelCh:
		return nil, nil, errCanceled
	case <-timeoutTimer.C:
		p.log.Debug("Header request timed out", "elapsed", ttl)
		headerTimeoutMeter.Mark(1)
		return nil, nil, errTimeout
	case res := <-resCh:
		headerReqTimer.Update(time.Since(start))
		headers := *res.Res.(*eth.BlockHeadersRequest)
		headerInMeter.Mark(int64(len(headers)))
		res.Done <- nil
		return headers, res.Meta.([]common.Hash), nil
	}
}
