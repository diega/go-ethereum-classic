// Copyright 2015 The go-ethereum Authors
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
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	eth "github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

var (
	errUnknownPeer     = errors.New("peer is unknown or unhealthy")
	errNoAncestorFound = errors.New("no common ancestor found")
)

var (
	MaxSkeletonSize = 128 // Number of header fetches needed for a skeleton assembly
)

// LegacySync tries to sync up our local block chain with a remote peer, both
// adding various sanity checks as well as wrapping it with various log entries.
func (d *Downloader) LegacySync(id string, head common.Hash, td *big.Int, mode SyncMode) error {
	err := d.synchronisePoW(id, head, td, mode)

	switch err {
	case nil, errBusy, errCanceled:
		return err
	}
	if errors.Is(err, errInvalidChain) || errors.Is(err, errBadPeer) || errors.Is(err, errTimeout) ||
		errors.Is(err, errStallingPeer) || errors.Is(err, errUnsyncedPeer) || errors.Is(err, errEmptyHeaderSet) ||
		errors.Is(err, errPeersUnavailable) || errors.Is(err, errTooOld) || errors.Is(err, errInvalidAncestor) {
		log.Warn("Synchronisation failed, dropping peer", "peer", id, "err", err)
		if d.dropPeer == nil {
			// The dropPeer method is nil when `--copydb` is used for a local copy.
			// Timeouts can occur if e.g. compaction hits at the wrong time, and can be ignored
			log.Warn("Downloader wants to drop peer, but peerdrop-function is not set", "peer", id)
		} else {
			d.dropPeer(id)
		}
		return err
	}
	log.Warn("Synchronisation failed, retrying", "err", err)
	return err
}

// synchronisePoW will select the peer and use it for synchronising. If an empty string is given
// it will use the best peer possible and synchronize if its TD is higher than our own. If any of the
// checks fail an error will be returned. This method is synchronous
func (d *Downloader) synchronisePoW(id string, hash common.Hash, td *big.Int, mode SyncMode) error {
	// Make sure only one goroutine is ever allowed past this point at once
	if !d.synchronising.CompareAndSwap(false, true) {
		return errBusy
	}
	defer d.synchronising.Store(false)

	// Post a user notification of the sync (only once per session)
	if d.notified.CompareAndSwap(false, true) {
		log.Info("Block synchronisation started")
	}
	if mode == ethconfig.SnapSync {
		// Snap sync will directly modify the persistent state, making the entire
		// trie database unusable until the state is fully synced. To prevent any
		// subsequent state reads, explicitly disable the trie database and state
		// syncer is responsible to address and correct any state missing.
		if d.blockchain.TrieDB().Scheme() == rawdb.PathScheme {
			if err := d.blockchain.TrieDB().Disable(); err != nil {
				return err
			}
		}
		// Snap sync uses the snapshot namespace to store potentially flaky data until
		// sync completely heals and finishes. Pause snapshot maintenance in the mean-
		// time to prevent access.
		if snapshots := d.blockchain.Snapshots(); snapshots != nil { // Only nil in tests
			snapshots.Disable()
		}
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
	d.cancelPeer = id
	d.cancelLock.Unlock()

	defer d.Cancel() // No matter what, we can't leave the cancel channel open

	// Atomically set the requested sync mode
	d.mode.Store(uint32(mode))

	// Retrieve the origin peer and initiate the downloading process
	p := d.peers.Peer(id)
	if p == nil {
		return errUnknownPeer
	}
	return d.syncWithPeer(p, hash, td)
}

// syncWithPeer starts a block synchronization based on the hash chain from the
// specified peer and head hash. This is for PoW networks only (no beacon mode).
func (d *Downloader) syncWithPeer(p *peerConnection, hash common.Hash, td *big.Int) (err error) {
	d.mux.Post(StartEvent{})
	defer func() {
		// reset on error
		if err != nil {
			d.mux.Post(FailedEvent{err})
		} else {
			latest := d.blockchain.CurrentHeader()
			d.mux.Post(DoneEvent{latest})
		}
	}()
	mode := d.getMode()

	log.Debug("Synchronising with the network", "peer", p.id, "eth", p.version, "head", hash, "td", td, "mode", mode)
	defer func(start time.Time) {
		log.Debug("Synchronisation terminated", "elapsed", common.PrettyDuration(time.Since(start)))
	}(time.Now())

	// Look up the sync boundaries: the common ancestor and the target block
	// In legacy mode, use the master peer to retrieve the headers from
	latest, pivot, err := d.fetchHead(p)
	if err != nil {
		return err
	}
	// If no pivot block was returned, the head is below the min full block
	// threshold (i.e. new chain). In that case we won't really snap sync
	// anyway, but still need a valid pivot block to avoid some code hitting
	// nil panics on access.
	if mode == ethconfig.SnapSync && pivot == nil {
		pivot = d.blockchain.CurrentBlock()
	}
	height := latest.Number.Uint64()

	// In legacy mode, reach out to the network and find the ancestor
	origin, err := d.findAncestor(p, latest)
	if err != nil {
		return err
	}
	d.syncStatsLock.Lock()
	if d.syncStatsChainHeight <= origin || d.syncStatsChainOrigin > origin {
		d.syncStatsChainOrigin = origin
	}
	d.syncStatsChainHeight = height
	d.syncStatsLock.Unlock()

	// Ensure our origin point is below any snap sync pivot point
	if mode == ethconfig.SnapSync {
		if height <= uint64(fsMinFullBlocks) {
			origin = 0
		} else {
			pivotNumber := pivot.Number.Uint64()
			if pivotNumber <= origin {
				origin = pivotNumber - 1
			}
			// Write out the pivot into the database so a rollback beyond it will
			// reenable snap sync
			rawdb.WriteLastPivotNumber(d.stateDB, pivotNumber)
		}
	}
	d.committed.Store(true)
	if mode == ethconfig.SnapSync && pivot.Number.Uint64() != 0 {
		d.committed.Store(false)
	}
	if mode == ethconfig.SnapSync {
		// Set the ancient data limitation. Legacy sync, use the best
		// announcement we have from the remote peer.
		if height > fullMaxForkAncestry+1 {
			d.ancientLimit = height - fullMaxForkAncestry - 1
		} else {
			d.ancientLimit = 0
		}
		// Extend the ancient chain segment range if the ancient limit is even
		// below the pre-configured chain cutoff.
		if d.chainCutoffNumber != 0 && d.chainCutoffNumber > d.ancientLimit {
			d.ancientLimit = d.chainCutoffNumber
			log.Info("Extend the ancient range with configured cutoff", "cutoff", d.chainCutoffNumber)
		}
		frozen, _ := d.stateDB.Ancients() // Ignore the error here since light client can also hit here.

		// If a part of blockchain data has already been written into active store,
		// disable the ancient style insertion explicitly.
		if origin >= frozen && origin != 0 {
			d.ancientLimit = 0
			var ancient string
			if frozen == 0 {
				ancient = "null"
			} else {
				ancient = fmt.Sprintf("%d", frozen-1)
			}
			log.Info("Disabling direct-ancient mode", "origin", origin, "ancient", ancient)
		} else if d.ancientLimit > 0 {
			log.Debug("Enabling direct-ancient mode", "ancient", d.ancientLimit)
		}
		// Rewind the ancient store and blockchain if reorg happens.
		if origin+1 < frozen {
			if err := d.blockchain.SetHead(origin); err != nil {
				return err
			}
			log.Info("Truncated excess ancient chain segment", "oldhead", frozen-1, "newhead", origin)
		}
	}
	// Skip ancient chain segments if Geth is running with a configured chain cutoff.
	// These segments are not guaranteed to be available in the network.
	chainOffset := origin + 1
	if mode == ethconfig.SnapSync && d.chainCutoffNumber != 0 {
		if chainOffset < d.chainCutoffNumber {
			chainOffset = d.chainCutoffNumber
			log.Info("Skip chain segment before cutoff", "origin", origin, "cutoff", d.chainCutoffNumber)
		}
	}
	// Initiate the sync using a concurrent header and content retrieval algorithm
	d.queue.Prepare(chainOffset, mode)

	// Create queueWithHeaders for header fetching
	qwh := newQueueWithHeaders(d.queue)
	qwh.ResetHeaders()

	// In legacy mode, headers are retrieved from the network
	fetchers := []func() error{
		func() error { return d.fetchHeadersPoW(p, origin+1, latest.Number.Uint64(), qwh) },
		func() error { return d.fetchBodies(chainOffset) },   // Bodies are retrieved during normal and snap sync
		func() error { return d.fetchReceipts(chainOffset) }, // Receipts are retrieved during snap sync
		func() error { return d.processHeadersPoW(origin+1, td) },
	}
	if mode == ethconfig.SnapSync {
		d.pivotLock.Lock()
		d.pivotHeader = pivot
		d.pivotLock.Unlock()

		fetchers = append(fetchers, func() error { return d.processSnapSyncContent() })
	} else if mode == ethconfig.FullSync {
		fetchers = append(fetchers, func() error { return d.processFullSyncContent() })
	}
	return d.spawnSync(fetchers)
}

// fetchHead retrieves the head header and optionally the pivot header from
// a remote peer.
func (d *Downloader) fetchHead(p *peerConnection) (head *types.Header, pivot *types.Header, err error) {
	p.log.Debug("Retrieving remote chain head")
	mode := d.getMode()

	// Request the advertised remote head block and wait for the response
	latest, _ := p.peer.Head()

	fetch := 1
	if mode == ethconfig.SnapSync {
		fetch = 2 // head + pivot headers
	}
	headers, hashes, err := d.fetchHeadersByHash(p, latest, fetch, fsMinFullBlocks-1, true)
	if err != nil {
		return nil, nil, err
	}
	// Make sure the peer gave us at least one and at most the requested headers
	if len(headers) == 0 || len(headers) > fetch {
		return nil, nil, fmt.Errorf("%w: returned headers %d != requested %d", errBadPeer, len(headers), fetch)
	}
	// The first header needs to be the head, validate against the request. If
	// only 1 header was returned, make sure there's no pivot or there was not
	// one requested.
	head = headers[0]
	if len(headers) == 1 {
		if mode == ethconfig.SnapSync && head.Number.Uint64() > uint64(fsMinFullBlocks) {
			return nil, nil, fmt.Errorf("%w: no pivot included along head header", errBadPeer)
		}
		p.log.Debug("Remote head identified, no pivot", "number", head.Number, "hash", hashes[0])
		return head, nil, nil
	}
	// At this point we have 2 headers in total and the first is the
	// validated head of the chain. Check the pivot number and return,
	pivot = headers[1]
	if pivot.Number.Uint64() != head.Number.Uint64()-uint64(fsMinFullBlocks) {
		return nil, nil, fmt.Errorf("%w: remote pivot %d != requested %d", errInvalidChain, pivot.Number, head.Number.Uint64()-uint64(fsMinFullBlocks))
	}
	return head, pivot, nil
}

// findAncestor tries to locate the common ancestor link of the local chain and
// a remote peers blockchain. In the general case when our node was in sync and
// on the correct chain, checking the top N links should already get us a match.
// In the rare scenario when we ended up on a long reorganisation (i.e. none of
// the head links match), we do a binary search to find the common ancestor.
func (d *Downloader) findAncestor(p *peerConnection, remoteHeader *types.Header) (uint64, error) {
	// Figure out the valid ancestor range to prevent rewrite attacks
	var (
		floor        = int64(-1)
		localHeight  uint64
		remoteHeight = remoteHeader.Number.Uint64()
	)
	mode := d.getMode()
	switch mode {
	case ethconfig.FullSync:
		localHeight = d.blockchain.CurrentBlock().Number.Uint64()
	case ethconfig.SnapSync:
		localHeight = d.blockchain.CurrentSnapBlock().Number.Uint64()
	default:
		localHeight = d.blockchain.CurrentHeader().Number.Uint64()
	}
	p.log.Debug("Looking for common ancestor", "local", localHeight, "remote", remoteHeight)

	// Recap floor value for binary search
	maxForkAncestry := fullMaxForkAncestry
	if localHeight >= maxForkAncestry {
		// We're above the max reorg threshold, find the earliest fork point
		floor = int64(localHeight - maxForkAncestry)
	}

	ancestor, err := d.findAncestorSpanSearch(p, mode, remoteHeight, localHeight, floor)
	if err == nil {
		return ancestor, nil
	}
	// The returned error was not nil.
	// If the error returned does not reflect that a common ancestor was not found, return it.
	// If the error reflects that a common ancestor was not found, continue to binary search,
	// where the error value will be reassigned.
	if !errors.Is(err, errNoAncestorFound) {
		return 0, err
	}

	ancestor, err = d.findAncestorBinarySearch(p, mode, remoteHeight, floor)
	if err != nil {
		return 0, err
	}
	return ancestor, nil
}

// calculateRequestSpan calculates what headers to request from a peer when trying to determine the
// common ancestor.
// It returns parameters to be used for peer.RequestHeadersByNumber:
//
//	from  - starting block number
//	count - number of headers to request
//	skip  - number of headers to skip
//
// and also returns 'max', the last block which is expected to be returned by the remote peers,
// given the (from,count,skip)
func calculateRequestSpan(remoteHeight, localHeight uint64) (int64, int, int, uint64) {
	var (
		from     int
		count    int
		MaxCount = MaxHeaderFetch / 16
	)
	// requestHead is the highest block that we will ask for. If requestHead is not offset,
	// the highest block that we will get is 16 blocks back from head, which means we
	// will fetch 14 or 15 blocks unnecessarily in the case the height difference
	// between us and the peer is 1-2 blocks, which is most common
	requestHead := int(remoteHeight) - 1
	if requestHead < 0 {
		requestHead = 0
	}
	// requestBottom is the lowest block we want included in the query
	// Ideally, we want to include the one just below our own head
	requestBottom := int(localHeight - 1)
	if requestBottom < 0 {
		requestBottom = 0
	}
	totalSpan := requestHead - requestBottom
	span := 1 + totalSpan/MaxCount
	if span < 2 {
		span = 2
	}
	if span > 16 {
		span = 16
	}

	count = 1 + totalSpan/span
	if count > MaxCount {
		count = MaxCount
	}
	if count < 2 {
		count = 2
	}
	from = requestHead - (count-1)*span
	if from < 0 {
		from = 0
	}
	max := from + (count-1)*span
	return int64(from), count, span - 1, uint64(max)
}

// findAncestorSpanSearch searches for a common ancestor using a span search.
func (d *Downloader) findAncestorSpanSearch(p *peerConnection, mode SyncMode, remoteHeight, localHeight uint64, floor int64) (uint64, error) {
	from, count, skip, max := calculateRequestSpan(remoteHeight, localHeight)

	p.log.Trace("Span searching for common ancestor", "count", count, "from", from, "skip", skip)
	headers, hashes, err := d.fetchHeadersByNumber(p, uint64(from), count, skip, false)
	if err != nil {
		return 0, err
	}
	// Wait for the remote response to the head fetch
	number, hash := uint64(0), common.Hash{}

	// Make sure the peer actually gave something valid
	if len(headers) == 0 {
		p.log.Warn("Empty head header set")
		return 0, errEmptyHeaderSet
	}
	// Make sure the peer's reply conforms to the request
	for i, header := range headers {
		expectNumber := from + int64(i)*int64(skip+1)
		if number := header.Number.Int64(); number != expectNumber {
			p.log.Warn("Head headers broke chain ordering", "index", i, "requested", expectNumber, "received", number)
			return 0, fmt.Errorf("%w: %v", errInvalidChain, errors.New("head headers broke chain ordering"))
		}
	}
	// Check if a common ancestor was found
	for i := len(headers) - 1; i >= 0; i-- {
		// Skip any headers that underflow/overflow our requested set
		if headers[i].Number.Int64() < from || headers[i].Number.Uint64() > max {
			continue
		}
		// Otherwise check if we already know the header or not
		h := hashes[i]
		n := headers[i].Number.Uint64()

		var known bool
		switch mode {
		case ethconfig.FullSync:
			known = d.blockchain.HasBlock(h, n)
		case ethconfig.SnapSync:
			known = d.blockchain.HasFastBlock(h, n)
		default:
			known = d.blockchain.HasHeader(h, n)
		}
		if known {
			number, hash = n, h
			break
		}
	}
	// If the head fetch already found an ancestor, return
	if hash != (common.Hash{}) {
		if int64(number) <= floor {
			p.log.Warn("Ancestor below allowance", "number", number, "hash", hash, "allowance", floor)
			return 0, errInvalidAncestor
		}
		p.log.Debug("Found common ancestor", "number", number, "hash", hash)
		return number, nil
	}
	return 0, errNoAncestorFound
}

// findAncestorBinarySearch searches for a common ancestor using binary search.
func (d *Downloader) findAncestorBinarySearch(p *peerConnection, mode SyncMode, remoteHeight uint64, floor int64) (uint64, error) {
	hash := common.Hash{}

	// Ancestor not found, we need to binary search over our chain
	start, end := uint64(0), remoteHeight
	if floor > 0 {
		start = uint64(floor)
	}
	p.log.Trace("Binary searching for common ancestor", "start", start, "end", end)

	for start+1 < end {
		// Split our chain interval in two, and request the hash to cross check
		check := (start + end) / 2

		headers, hashes, err := d.fetchHeadersByNumber(p, check, 1, 0, false)
		if err != nil {
			return 0, err
		}
		// Make sure the peer actually gave something valid
		if len(headers) != 1 {
			p.log.Warn("Multiple headers for single request", "headers", len(headers))
			return 0, fmt.Errorf("%w: multiple headers (%d) for single request", errBadPeer, len(headers))
		}
		// Modify the search interval based on the response
		h := hashes[0]
		n := headers[0].Number.Uint64()

		var known bool
		switch mode {
		case ethconfig.FullSync:
			known = d.blockchain.HasBlock(h, n)
		case ethconfig.SnapSync:
			known = d.blockchain.HasFastBlock(h, n)
		default:
			known = d.blockchain.HasHeader(h, n)
		}
		if !known {
			end = check
			continue
		}
		header := d.blockchain.GetHeaderByHash(h) // Independent of sync mode, header surely exists
		if header.Number.Uint64() != check {
			p.log.Warn("Received non requested header", "number", header.Number, "hash", header.Hash(), "request", check)
			return 0, fmt.Errorf("%w: non-requested header (%d)", errBadPeer, header.Number)
		}
		start = check
		hash = h
	}
	// Ensure valid ancestry and return
	if int64(start) <= floor {
		p.log.Warn("Ancestor below allowance", "number", start, "hash", hash, "allowance", floor)
		return 0, errInvalidAncestor
	}
	p.log.Debug("Found common ancestor", "number", start, "hash", hash)
	return start, nil
}

// fetchHeadersPoW keeps retrieving headers concurrently from the number
// requested, until no more are returned, potentially throttling on the way. To
// facilitate concurrency but still protect against malicious nodes sending bad
// headers, we construct a header chain skeleton using the "origin" peer we are
// syncing with, and fill in the missing headers using anyone else. Headers from
// other peers are only accepted if they map cleanly to the skeleton. If no one
// can fill in the skeleton - not even the origin peer - it's assumed invalid and
// the origin is dropped.
func (d *Downloader) fetchHeadersPoW(p *peerConnection, from uint64, head uint64, qwh *queueWithHeaders) error {
	p.log.Debug("Directing header downloads", "origin", from)
	defer p.log.Debug("Header download terminated")

	// Start pulling the header chain skeleton until all is done
	var (
		skeleton = true  // Skeleton assembly phase or finishing up
		pivoting = false // Whether the next request is pivot verification
		ancestor = from
	)
	for {
		// Pull the next batch of headers, it either:
		//   - Skeleton retrieval to permit concurrent header fetches
		//   - Full header retrieval if we're near the chain head
		var (
			headers []*types.Header
			hashes  []common.Hash
			err     error
		)
		switch {
		case pivoting:
			d.pivotLock.RLock()
			pivot := d.pivotHeader.Number.Uint64()
			d.pivotLock.RUnlock()

			p.log.Trace("Fetching next pivot header", "number", pivot+uint64(fsMinFullBlocks))
			headers, hashes, err = d.fetchHeadersByNumber(p, pivot+uint64(fsMinFullBlocks), 2, fsMinFullBlocks-9, false) // move +64 when it's 2x64-8 deep

		case skeleton:
			p.log.Trace("Fetching skeleton headers", "count", MaxHeaderFetch, "from", from)
			headers, hashes, err = d.fetchHeadersByNumber(p, from+uint64(MaxHeaderFetch)-1, MaxSkeletonSize, MaxHeaderFetch-1, false)

		default:
			p.log.Trace("Fetching full headers", "count", MaxHeaderFetch, "from", from)
			headers, hashes, err = d.fetchHeadersByNumber(p, from, MaxHeaderFetch, 0, false)
		}
		switch err {
		case nil:
			// Headers retrieved, continue with processing

		case errCanceled:
			// Sync cancelled, no issue, propagate up
			return err

		default:
			// Header retrieval either timed out, or the peer failed in some strange way
			// (e.g. disconnect). Consider the master peer bad and drop
			d.dropPeer(p.id)

			// Finish the sync gracefully instead of dumping the gathered data though
			for _, ch := range []chan bool{d.queue.blockWakeCh, d.queue.receiptWakeCh} {
				select {
				case ch <- false:
				case <-d.cancelCh:
				}
			}
			select {
			case d.headerProcCh <- nil:
			case <-d.cancelCh:
			}
			return fmt.Errorf("%w: header request failed: %v", errBadPeer, err)
		}
		// If the pivot is being checked, move if it became stale and run the real retrieval
		var pivot uint64

		d.pivotLock.RLock()
		if d.pivotHeader != nil {
			pivot = d.pivotHeader.Number.Uint64()
		}
		d.pivotLock.RUnlock()

		if pivoting {
			if len(headers) == 2 {
				if have, want := headers[0].Number.Uint64(), pivot+uint64(fsMinFullBlocks); have != want {
					log.Warn("Peer sent invalid next pivot", "have", have, "want", want)
					return fmt.Errorf("%w: next pivot number %d != requested %d", errInvalidChain, have, want)
				}
				if have, want := headers[1].Number.Uint64(), pivot+2*uint64(fsMinFullBlocks)-8; have != want {
					log.Warn("Peer sent invalid pivot confirmer", "have", have, "want", want)
					return fmt.Errorf("%w: next pivot confirmer number %d != requested %d", errInvalidChain, have, want)
				}
				log.Warn("Pivot seemingly stale, moving", "old", pivot, "new", headers[0].Number)
				pivot = headers[0].Number.Uint64()

				d.pivotLock.Lock()
				d.pivotHeader = headers[0]
				d.pivotLock.Unlock()

				// Write out the pivot into the database so a rollback beyond
				// it will reenable snap sync and update the state root that
				// the state syncer will be downloading.
				rawdb.WriteLastPivotNumber(d.stateDB, pivot)
			}
			// Disable the pivot check and fetch the next batch of headers
			pivoting = false
			continue
		}
		// If the skeleton's finished, pull any remaining head headers directly from the origin
		if skeleton && len(headers) == 0 {
			// A malicious node might withhold advertised headers indefinitely
			if from+uint64(MaxHeaderFetch)-1 <= head {
				p.log.Warn("Peer withheld skeleton headers", "advertised", head, "withheld", from+uint64(MaxHeaderFetch)-1)
				return fmt.Errorf("%w: withheld skeleton headers: advertised %d, withheld #%d", errStallingPeer, head, from+uint64(MaxHeaderFetch)-1)
			}
			p.log.Debug("No skeleton, fetching headers directly")
			skeleton = false
			continue
		}
		// If no more headers are inbound, notify the content fetchers and return
		if len(headers) == 0 {
			// Don't abort header fetches while the pivot is downloading
			if !d.committed.Load() && pivot <= from {
				p.log.Debug("No headers, waiting for pivot commit")
				select {
				case <-time.After(fsHeaderContCheck):
					continue
				case <-d.cancelCh:
					return errCanceled
				}
			}
			// Pivot done (or not in snap sync) and no more headers, terminate the process
			p.log.Debug("No more headers available")
			select {
			case d.headerProcCh <- nil:
				return nil
			case <-d.cancelCh:
				return errCanceled
			}
		}
		// If we received a skeleton batch, resolve internals concurrently
		var progressed bool
		if skeleton {
			filled, hashset, proced, err := d.fillHeaderSkeleton(from, headers, qwh)
			if err != nil {
				p.log.Debug("Skeleton chain invalid", "err", err)
				return fmt.Errorf("%w: %v", errInvalidChain, err)
			}
			headers = filled[proced:]
			hashes = hashset[proced:]

			progressed = proced > 0
			from += uint64(proced)
		} else {
			// A malicious node might withhold advertised headers indefinitely
			if n := len(headers); n < MaxHeaderFetch && headers[n-1].Number.Uint64() < head {
				p.log.Warn("Peer withheld headers", "advertised", head, "delivered", headers[n-1].Number.Uint64())
				return fmt.Errorf("%w: withheld headers: advertised %d, delivered %d", errStallingPeer, head, headers[n-1].Number.Uint64())
			}
			// If we're closing in on the chain head, but haven't yet reached it, delay
			// the last few headers so mini reorgs on the head don't cause invalid hash
			// chain errors.
			if n := len(headers); n > 0 {
				// Retrieve the current head we're at
				var head uint64
				head = d.blockchain.CurrentSnapBlock().Number.Uint64()
				if full := d.blockchain.CurrentBlock().Number.Uint64(); head < full {
					head = full
				}
				// If the head is below the common ancestor, we're actually deduplicating
				// already existing chain segments, so use the ancestor as the fake head.
				// Otherwise, we might end up delaying header deliveries pointlessly.
				if head < ancestor {
					head = ancestor
				}
				// If the head is way older than this batch, delay the last few headers
				if head+uint64(reorgProtThreshold) < headers[n-1].Number.Uint64() {
					delay := reorgProtHeaderDelay
					if delay > n {
						delay = n
					}
					headers = headers[:n-delay]
					hashes = hashes[:n-delay]
				}
			}
		}
		// If no headers have been delivered, or all of them have been delayed,
		// sleep a bit and retry. Take care with headers already consumed during
		// skeleton filling
		if len(headers) == 0 && !progressed {
			p.log.Trace("All headers delayed, waiting")
			select {
			case <-time.After(fsHeaderContCheck):
				continue
			case <-d.cancelCh:
				return errCanceled
			}
		}
		// Insert any remaining new headers and fetch the next batch
		if len(headers) > 0 {
			p.log.Trace("Scheduling new headers", "count", len(headers), "from", from)
			select {
			case d.headerProcCh <- &headerTask{
				headers: headers,
				hashes:  hashes,
			}:
			case <-d.cancelCh:
				return errCanceled
			}
			from += uint64(len(headers))
		}
		// If we're still skeleton filling snap sync, check pivot staleness
		// before continuing to the next skeleton filling
		if skeleton && pivot > 0 {
			pivoting = true
		}
	}
}

// fillHeaderSkeleton concurrently retrieves headers from all our available peers
// and maps them to the provided skeleton header chain.
//
// Any partial results from the beginning of the skeleton is (if possible) forwarded
// immediately to the header processor to keep the rest of the pipeline full even
// in the case of header stalls.
//
// The method returns the entire filled skeleton and also the number of headers
// already forwarded for processing.
func (d *Downloader) fillHeaderSkeleton(from uint64, skeleton []*types.Header, qwh *queueWithHeaders) ([]*types.Header, []common.Hash, int, error) {
	log.Debug("Filling up skeleton", "from", from)
	qwh.ScheduleSkeleton(from, skeleton)

	hq := &headerQueue{d: d, queue: qwh}
	err := d.concurrentFetch(hq)
	if err != nil {
		log.Debug("Skeleton fill failed", "err", err)
	}
	filled, hashes, proced := qwh.RetrieveHeaders()
	if err == nil {
		log.Debug("Skeleton fill succeeded", "filled", len(filled), "processed", proced)
	}
	return filled, hashes, proced, err
}

// processHeadersPoW takes batches of retrieved headers from an input channel and
// keeps processing and scheduling them into the header chain and downloader's
// queue until the stream ends or a failure occurs.
func (d *Downloader) processHeadersPoW(origin uint64, td *big.Int) error {
	var (
		mode       = d.getMode()
		gotHeaders = false // Wait for batches of headers to process
		timer      = time.NewTimer(time.Second)
	)
	defer timer.Stop()

	for {
		select {
		case <-d.cancelCh:
			return errCanceled

		case task := <-d.headerProcCh:
			// Terminate header processing if we synced up
			if task == nil || len(task.headers) == 0 {
				// Notify everyone that headers are fully processed
				for _, ch := range []chan bool{d.queue.blockWakeCh, d.queue.receiptWakeCh} {
					select {
					case ch <- false:
					case <-d.cancelCh:
					}
				}
				// If no headers were retrieved at all, the peer violated its TD promise that it had a
				// better chain compared to ours. The only exception is if its promised blocks were
				// already imported by other means (e.g. fetcher):
				//
				// R <remote peer>, L <local node>: Both at block 10
				// R: Mine block 11, and propagate it to L
				// L: Queue block 11 for import
				// L: Notice that R's head and TD increased compared to ours, start sync
				// L: Import of block 11 finishes
				// L: Sync begins, and finds common ancestor at 11
				// L: Request new headers up from 11 (R's TD was higher, it must have something)
				// R: Nothing to give
				head := d.blockchain.CurrentBlock()
				if !gotHeaders && td.Cmp(d.blockchain.GetTd(head.Hash(), head.Number.Uint64())) > 0 {
					return errStallingPeer
				}
				// If snap or light syncing, ensure promised headers are indeed delivered. This is
				// needed to detect scenarios where an attacker feeds a bad pivot and then bails out
				// of delivering the post-pivot blocks that would flag the invalid content.
				//
				// This check cannot be executed "as is" for full imports, since blocks may still be
				// queued for processing when the header download completes. However, as long as the
				// peer gave us something useful, we're already happy/progressed (above check).
				if mode == ethconfig.SnapSync {
					head := d.blockchain.CurrentHeader()
					if td.Cmp(d.blockchain.GetTd(head.Hash(), head.Number.Uint64())) > 0 {
						return errStallingPeer
					}
				}
				return nil
			}
			// Otherwise split the chunk of headers into batches and process them
			headers, hashes, scheduled := task.headers, task.hashes, false

			gotHeaders = true
			for len(headers) > 0 {
				// Terminate if something failed in between processing chunks
				select {
				case <-d.cancelCh:
					return errCanceled
				default:
				}
				// Select the next chunk of headers to import
				limit := maxHeadersProcess
				if limit > len(headers) {
					limit = len(headers)
				}
				chunkHeaders := headers[:limit]
				chunkHashes := hashes[:limit]

				// Split the headers around the chain cutoff
				var cutoff int
				if mode == ethconfig.SnapSync && d.chainCutoffNumber != 0 {
					cutoff = sort.Search(len(chunkHeaders), func(i int) bool {
						return chunkHeaders[i].Number.Uint64() >= d.chainCutoffNumber
					})
				}
				// Insert the header chain into the ancient store (with block bodies and
				// receipts set to nil) if they fall before the cutoff.
				if mode == ethconfig.SnapSync && cutoff != 0 {
					if n, err := d.blockchain.InsertHeadersBeforeCutoff(chunkHeaders[:cutoff]); err != nil {
						log.Warn("Failed to insert ancient header chain", "number", chunkHeaders[n].Number, "hash", chunkHashes[n], "parent", chunkHeaders[n].ParentHash, "err", err)
						return fmt.Errorf("%w: %v", errInvalidChain, err)
					}
					log.Debug("Inserted headers before cutoff", "number", chunkHeaders[cutoff-1].Number, "hash", chunkHashes[cutoff-1])
				}
				// In snap sync, insert headers into the header chain so that TDs
				// are computed and persisted. Without this, headers exist in the
				// database but have no associated TD, breaking PoW sync operations
				// that rely on TD comparisons (cf. core-geth processHeaders).
				if mode == ethconfig.SnapSync {
					if n, err := d.blockchain.InsertHeaderChain(chunkHeaders); err != nil {
						log.Warn("Failed to insert header chain", "number", chunkHeaders[n].Number, "hash", chunkHashes[n], "parent", chunkHeaders[n].ParentHash, "err", err)
						return fmt.Errorf("%w: %v", errInvalidChain, err)
					}
				}
				// If we've reached the allowed number of pending headers, stall a bit
				for d.queue.PendingBodies() >= maxQueuedHeaders || d.queue.PendingReceipts() >= maxQueuedHeaders {
					timer.Reset(time.Second)
					select {
					case <-d.cancelCh:
						return errCanceled
					case <-timer.C:
					}
				}
				// Otherwise, schedule the headers for content retrieval (block bodies and
				// potentially receipts in snap sync).
				//
				// Skip the bodies/receipts retrieval scheduling before the cutoff in snap
				// sync if chain pruning is configured.
				if mode == ethconfig.SnapSync && cutoff != 0 {
					chunkHeaders = chunkHeaders[cutoff:]
					chunkHashes = chunkHashes[cutoff:]
				}
				if len(chunkHeaders) > 0 {
					scheduled = true
					if d.queue.Schedule(chunkHeaders, chunkHashes, origin+uint64(cutoff)) != len(chunkHeaders) {
						return fmt.Errorf("%w: stale headers", errBadPeer)
					}
				}
				headers = headers[limit:]
				hashes = hashes[limit:]
				origin += uint64(limit)
			}
			// Update the highest block number we know if a higher one is found.
			d.syncStatsLock.Lock()
			if d.syncStatsChainHeight < origin {
				d.syncStatsChainHeight = origin - 1
			}
			d.syncStatsLock.Unlock()

			// Signal the content downloaders of the availability of new tasks
			if scheduled {
				for _, ch := range []chan bool{d.queue.blockWakeCh, d.queue.receiptWakeCh} {
					select {
					case ch <- true:
					default:
					}
				}
			}
		}
	}
}

// fetchHeadersByNumber is a blocking version of Peer.RequestHeadersByNumber which
// handles all the cancellation, interruption and timeout mechanisms of a data
// retrieval to allow blocking API calls.
func (d *Downloader) fetchHeadersByNumber(p *peerConnection, number uint64, amount int, skip int, reverse bool) ([]*types.Header, []common.Hash, error) {
	// Create the response sink and send the network request
	start := time.Now()
	resCh := make(chan *eth.Response)

	req, err := p.peer.RequestHeadersByNumber(number, amount, skip, reverse, resCh)
	if err != nil {
		return nil, nil, err
	}
	defer req.Close()

	// Wait until the response arrives, the request is cancelled or times out
	ttl := d.peers.rates.TargetTimeout()

	timeoutTimer := time.NewTimer(ttl)
	defer timeoutTimer.Stop()

	select {
	case <-d.cancelCh:
		return nil, nil, errCanceled

	case <-timeoutTimer.C:
		// Header retrieval timed out, update the metrics
		p.log.Debug("Header request timed out", "elapsed", ttl)
		headerTimeoutMeter.Mark(1)

		return nil, nil, errTimeout

	case res := <-resCh:
		// Headers successfully retrieved, update the metrics
		headerReqTimer.Update(time.Since(start))
		headerInMeter.Mark(int64(len(*res.Res.(*eth.BlockHeadersRequest))))

		// Don't reject the packet even if it turns out to be bad, downloader will
		// disconnect the peer on its own terms. Simply delivery the headers to
		// be processed by the caller
		res.Done <- nil

		return *res.Res.(*eth.BlockHeadersRequest), res.Meta.([]common.Hash), nil
	}
}
