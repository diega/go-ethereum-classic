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
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// headerChunk represents a chunk of headers to be fetched.
type headerChunk struct {
	from       uint64            // Starting block number
	count      int               // Number of headers in this chunk
	peer       string            // Peer ID fetching this chunk
	started    time.Time         // When the fetch started
	failedWith map[string]bool   // Peers that failed to deliver this chunk
}

// headerTaskManager manages header download tasks for PoW sync.
// It divides the header range into chunks that can be fetched concurrently
// by multiple peers, then reassembles them in order.
type headerTaskManager struct {
	from      uint64 // Start of range to fetch (inclusive)
	to        uint64 // End of range to fetch (inclusive)
	chunkSize int    // Headers per chunk (default: MaxHeaderFetch)

	pending   map[uint64]*headerChunk   // Chunks waiting to be assigned (key: from block)
	inflight  map[string]*headerChunk   // Chunks being fetched (key: peer ID)
	completed map[uint64][]*types.Header // Completed chunks with their headers (key: from block)
	hashes    map[uint64][]common.Hash  // Hashes for completed headers (key: from block)

	// Tracking for ordered delivery
	nextExpected uint64 // Next block number expected for ordered delivery

	wakeCh chan bool // Signal when more work is available

	lock sync.RWMutex
}

// newHeaderTaskManager creates a new header task manager for the given range.
func newHeaderTaskManager(from, to uint64) *headerTaskManager {
	mgr := &headerTaskManager{
		from:         from,
		to:           to,
		chunkSize:    MaxHeaderFetch,
		pending:      make(map[uint64]*headerChunk),
		inflight:     make(map[string]*headerChunk),
		completed:    make(map[uint64][]*types.Header),
		hashes:       make(map[uint64][]common.Hash),
		nextExpected: from,
		wakeCh:       make(chan bool, 1),
	}

	// Divide the range into chunks
	for start := from; start <= to; {
		count := mgr.chunkSize
		if start+uint64(count)-1 > to {
			count = int(to - start + 1)
		}
		mgr.pending[start] = &headerChunk{
			from:  start,
			count: count,
		}
		start += uint64(count)
	}

	log.Debug("Created header task manager", "from", from, "to", to, "chunks", len(mgr.pending))
	return mgr
}

// Pending returns the number of headers still pending (not yet fetched).
func (m *headerTaskManager) Pending() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	var total int
	for _, chunk := range m.pending {
		total += chunk.count
	}
	for _, chunk := range m.inflight {
		total += chunk.count
	}
	return total
}

// Reserve assigns a chunk of headers to the given peer.
// Returns (request, progress, throttle).
func (m *headerTaskManager) Reserve(peer *peerConnection, count int) (*fetchRequest, bool, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	// Check if peer already has a chunk assigned
	if _, ok := m.inflight[peer.id]; ok {
		return nil, false, false
	}

	// No pending chunks
	if len(m.pending) == 0 {
		return nil, false, true
	}

	// Find the lowest pending chunk that this peer hasn't failed with
	var selected *headerChunk
	var selectedFrom uint64
	for from, chunk := range m.pending {
		// Skip chunks this peer already failed to deliver
		if chunk.failedWith != nil && chunk.failedWith[peer.id] {
			continue
		}
		if selected == nil || from < selectedFrom {
			selected = chunk
			selectedFrom = from
		}
	}

	if selected == nil {
		return nil, false, true
	}

	// Assign to peer
	delete(m.pending, selectedFrom)
	selected.peer = peer.id
	selected.started = time.Now()
	m.inflight[peer.id] = selected

	log.Trace("Reserved header chunk", "peer", peer.id[:8], "from", selected.from, "count", selected.count)

	// Create the fetch request
	// Note: For headers, we use From field instead of Headers slice
	req := &fetchRequest{
		Peer: peer,
		From: selected.from,
		Time: selected.started,
	}

	return req, true, false
}

// Unreserve returns the chunk assigned to a peer back to the pending pool.
// This is called on timeout or peer disconnect.
func (m *headerTaskManager) Unreserve(peerID string) int {
	m.lock.Lock()
	defer m.lock.Unlock()

	chunk, ok := m.inflight[peerID]
	if !ok {
		return 0
	}

	delete(m.inflight, peerID)
	chunk.peer = ""
	chunk.started = time.Time{}
	m.pending[chunk.from] = chunk

	// Mark peer as failed (timeout) for IdlePeer cooldown
	MarkPeerFailed(peerID)

	log.Trace("Unreserved header chunk", "peer", peerID[:8], "from", chunk.from, "count", chunk.count)

	// Signal that work is available
	select {
	case m.wakeCh <- true:
	default:
	}

	return chunk.count
}

// Deliver processes a header delivery from a peer.
// Returns the number of accepted headers and any error.
func (m *headerTaskManager) Deliver(peerID string, headers []*types.Header, headerHashes []common.Hash) (int, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	chunk, ok := m.inflight[peerID]
	if !ok {
		return 0, errStaleDelivery
	}

	delete(m.inflight, peerID)

	// Validate the delivery
	if len(headers) == 0 {
		// Empty response - mark this peer as having failed for this chunk and try another peer
		log.Debug("Empty header delivery", "peer", peerID[:8], "from", chunk.from, "count", chunk.count)
		MarkPeerFailed(peerID) // Track failure for IdlePeer cooldown
		if chunk.failedWith == nil {
			chunk.failedWith = make(map[string]bool)
		}
		chunk.failedWith[peerID] = true
		chunk.peer = ""
		chunk.started = time.Time{}
		m.pending[chunk.from] = chunk
		return 0, nil
	}

	// Verify headers start at expected block
	if headers[0].Number.Uint64() != chunk.from {
		log.Warn("Header delivery has wrong start", "peer", peerID[:8],
			"expected", chunk.from, "got", headers[0].Number.Uint64())
		MarkPeerFailed(peerID) // Track failure for IdlePeer cooldown
		chunk.peer = ""
		chunk.started = time.Time{}
		m.pending[chunk.from] = chunk
		return 0, fmt.Errorf("header delivery mismatch: expected %d, got %d", chunk.from, headers[0].Number.Uint64())
	}

	// Verify chain continuity within delivery
	for i := 1; i < len(headers); i++ {
		if headers[i].Number.Uint64() != headers[i-1].Number.Uint64()+1 {
			log.Warn("Header chain discontinuous", "peer", peerID[:8],
				"prev", headers[i-1].Number.Uint64(), "cur", headers[i].Number.Uint64())
			MarkPeerFailed(peerID)
			chunk.peer = ""
			chunk.started = time.Time{}
			m.pending[chunk.from] = chunk
			return 0, errInvalidChain
		}
		if headers[i].ParentHash != headerHashes[i-1] {
			log.Warn("Header parent hash mismatch", "peer", peerID[:8],
				"number", headers[i].Number.Uint64())
			MarkPeerFailed(peerID)
			chunk.peer = ""
			chunk.started = time.Time{}
			m.pending[chunk.from] = chunk
			return 0, errInvalidChain
		}
	}

	// If we received fewer headers than expected, we need to put the remaining back
	if len(headers) < chunk.count {
		remaining := chunk.count - len(headers)
		nextFrom := chunk.from + uint64(len(headers))

		// Only add remaining if it's still within range
		if nextFrom <= m.to {
			m.pending[nextFrom] = &headerChunk{
				from:  nextFrom,
				count: remaining,
			}
		}
	}

	// Store completed chunk
	m.completed[chunk.from] = headers
	m.hashes[chunk.from] = headerHashes

	log.Trace("Delivered header chunk", "peer", peerID[:8], "from", chunk.from, "count", len(headers))

	// Signal that work might be available (for ordered processing)
	select {
	case m.wakeCh <- true:
	default:
	}

	return len(headers), nil
}

// PopCompleted returns completed headers in order, starting from nextExpected.
// Returns headers, hashes, and updates nextExpected.
func (m *headerTaskManager) PopCompleted() ([]*types.Header, []common.Hash) {
	m.lock.Lock()
	defer m.lock.Unlock()

	var allHeaders []*types.Header
	var allHashes []common.Hash

	for {
		headers, ok := m.completed[m.nextExpected]
		if !ok {
			// Log if we're blocked waiting for a specific chunk while we have completed chunks waiting
			if len(m.completed) > 0 {
				log.Trace("Waiting for header chunk", "nextExpected", m.nextExpected, "completedChunks", len(m.completed), "inflight", len(m.inflight), "pending", len(m.pending))
			}
			break
		}
		hashes := m.hashes[m.nextExpected]

		delete(m.completed, m.nextExpected)
		delete(m.hashes, m.nextExpected)

		allHeaders = append(allHeaders, headers...)
		allHashes = append(allHashes, hashes...)

		m.nextExpected += uint64(len(headers))
	}

	return allHeaders, allHashes
}
