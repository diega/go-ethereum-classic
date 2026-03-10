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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

type queueWithHeaders struct {
	*queue // embedding - allows access to q.lock, q.mode, etc.

	// Headers are "special", they download in batches, supported by a skeleton chain
	headerHead      common.Hash                    // Hash of the last queued header to verify order
	headerTaskPool  map[uint64]*types.Header       // Pending header retrieval tasks, mapping starting indexes to skeleton headers
	headerTaskQueue *prque.Prque[int64, uint64]    // Priority queue of the skeleton indexes to fetch the filling headers for
	headerPeerMiss  map[string]map[uint64]struct{} // Set of per-peer header batches known to be unavailable
	headerPendPool  map[string]*fetchRequest       // Currently pending header retrieval operations
	headerResults   []*types.Header                // Result cache accumulating the completed headers
	headerHashes    []common.Hash                  // Result cache accumulating the completed header hashes
	headerProced    int                            // Number of headers already processed from the results
	headerOffset    uint64                         // Number of the first header in the result cache
	headerContCh    chan bool                      // Channel to notify when header download finishes
}

// newQueueWithHeaders creates a new queueWithHeaders wrapping the given queue.
// It installs a revokeHook so that queue.Revoke also revokes pending header requests.
func newQueueWithHeaders(q *queue) *queueWithHeaders {
	qwh := &queueWithHeaders{
		queue:        q,
		headerContCh: make(chan bool, 1),
	}
	q.revokeHook = func(peerID string) {
		if request, ok := qwh.headerPendPool[peerID]; ok {
			qwh.headerTaskQueue.Push(request.From, -int64(request.From))
			delete(qwh.headerPendPool, peerID)
		}
	}
	return qwh
}

func (q *queueWithHeaders) ResetHeaders() {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	q.headerHead = common.Hash{}
	q.headerPendPool = make(map[string]*fetchRequest)
}

// PendingHeaders retrieves the number of header requests pending for retrieval.
func (q *queueWithHeaders) PendingHeaders() int {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	return q.headerTaskQueue.Size()
}

// ScheduleSkeleton adds a batch of header retrieval tasks to the queue to fill
// up an already retrieved header skeleton.
func (q *queueWithHeaders) ScheduleSkeleton(from uint64, skeleton []*types.Header) {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	// No skeleton retrieval can be in progress, fail hard if so (huge implementation bug)
	if q.headerResults != nil {
		panic("skeleton assembly already in progress")
	}
	// Schedule all the header retrieval tasks for the skeleton assembly
	q.headerTaskPool = make(map[uint64]*types.Header)
	q.headerTaskQueue = prque.New[int64, uint64](nil)
	q.headerPeerMiss = make(map[string]map[uint64]struct{}) // Reset availability to correct invalid chains
	q.headerResults = make([]*types.Header, len(skeleton)*MaxHeaderFetch)
	q.headerHashes = make([]common.Hash, len(skeleton)*MaxHeaderFetch)
	q.headerProced = 0
	q.headerOffset = from
	q.headerContCh = make(chan bool, 1)

	for i, header := range skeleton {
		index := from + uint64(i*MaxHeaderFetch)

		q.headerTaskPool[index] = header
		q.headerTaskQueue.Push(index, -int64(index))
	}
}

// RetrieveHeaders retrieves the header chain assemble based on the scheduled
// skeleton.
func (q *queueWithHeaders) RetrieveHeaders() ([]*types.Header, []common.Hash, int) {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	headers, hashes, proced := q.headerResults, q.headerHashes, q.headerProced
	q.headerResults, q.headerHashes, q.headerProced = nil, nil, 0

	return headers, hashes, proced
}

// ReserveHeaders reserves a set of headers for the given peer, skipping any
// previously failed batches.
func (q *queueWithHeaders) ReserveHeaders(p *peerConnection, count int) *fetchRequest {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	// Short circuit if the peer's already downloading something (sanity check to
	// not corrupt state)
	if _, ok := q.headerPendPool[p.id]; ok {
		return nil
	}
	// Retrieve a batch of hashes, skipping previously failed ones
	send, skip := uint64(0), []uint64{}
	for send == 0 && !q.headerTaskQueue.Empty() {
		from, _ := q.headerTaskQueue.Pop()
		if q.headerPeerMiss[p.id] != nil {
			if _, ok := q.headerPeerMiss[p.id][from]; ok {
				skip = append(skip, from)
				continue
			}
		}
		send = from
	}
	// Merge all the skipped batches back
	for _, from := range skip {
		q.headerTaskQueue.Push(from, -int64(from))
	}
	// Assemble and return the block download request
	if send == 0 {
		return nil
	}
	request := &fetchRequest{
		Peer: p,
		From: send,
		Time: time.Now(),
	}
	q.headerPendPool[p.id] = request
	return request
}

// ExpireHeaders cancels a request that timed out and moves the pending fetch
// task back into the queue for rescheduling.
func (q *queueWithHeaders) ExpireHeaders(peer string) int {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	headerTimeoutMeter.Mark(1)
	return q.expire(peer, q.headerPendPool, q.headerTaskQueue)
}

// DeliverHeaders injects a header retrieval response into the header results
// cache. This method either accepts all headers it received, or none of them
// if they do not map correctly to the skeleton.
//
// If the headers are accepted, the method makes an attempt to deliver the set
// of ready headers to the processor to keep the pipeline full. However, it will
// not block to prevent stalling other pending deliveries.
func (q *queueWithHeaders) DeliverHeaders(id string, headers []*types.Header, hashes []common.Hash, headerProcCh chan *headerTask) (int, error) {
	q.queue.lock.Lock()
	defer q.queue.lock.Unlock()

	var logger log.Logger
	if len(id) < 16 {
		// Tests use short IDs, don't choke on them
		logger = log.New("peer", id)
	} else {
		logger = log.New("peer", id[:16])
	}
	// Short circuit if the data was never requested
	request := q.headerPendPool[id]
	if request == nil {
		headerDropMeter.Mark(int64(len(headers)))
		return 0, errNoFetchesPending
	}
	delete(q.headerPendPool, id)

	headerReqTimer.UpdateSince(request.Time)
	headerInMeter.Mark(int64(len(headers)))

	// Ensure headers can be mapped onto the skeleton chain
	target := q.headerTaskPool[request.From].Hash()

	accepted := len(headers) == MaxHeaderFetch
	if accepted {
		if headers[0].Number.Uint64() != request.From {
			logger.Trace("First header broke chain ordering", "number", headers[0].Number, "hash", hashes[0], "expected", request.From)
			accepted = false
		} else if hashes[len(headers)-1] != target {
			logger.Trace("Last header broke skeleton structure ", "number", headers[len(headers)-1].Number, "hash", hashes[len(headers)-1], "expected", target)
			accepted = false
		}
	}
	if accepted {
		parentHash := hashes[0]
		for i, header := range headers[1:] {
			hash := hashes[i+1]
			if want := request.From + 1 + uint64(i); header.Number.Uint64() != want {
				logger.Warn("Header broke chain ordering", "number", header.Number, "hash", hash, "expected", want)
				accepted = false
				break
			}
			if parentHash != header.ParentHash {
				logger.Warn("Header broke chain ancestry", "number", header.Number, "hash", hash)
				accepted = false
				break
			}
			// Set-up parent hash for next round
			parentHash = hash
		}
	}
	// If the batch of headers wasn't accepted, mark as unavailable
	if !accepted {
		logger.Trace("Skeleton filling not accepted", "from", request.From)
		headerDropMeter.Mark(int64(len(headers)))

		miss := q.headerPeerMiss[id]
		if miss == nil {
			q.headerPeerMiss[id] = make(map[uint64]struct{})
			miss = q.headerPeerMiss[id]
		}
		miss[request.From] = struct{}{}

		q.headerTaskQueue.Push(request.From, -int64(request.From))
		return 0, errors.New("delivery not accepted")
	}
	// Clean up a successful fetch and try to deliver any sub-results
	copy(q.headerResults[request.From-q.headerOffset:], headers)
	copy(q.headerHashes[request.From-q.headerOffset:], hashes)

	delete(q.headerTaskPool, request.From)

	ready := 0
	for q.headerProced+ready < len(q.headerResults) && q.headerResults[q.headerProced+ready] != nil {
		ready += MaxHeaderFetch
	}
	if ready > 0 {
		// Headers are ready for delivery, gather them and push forward (non blocking)
		processHeaders := make([]*types.Header, ready)
		copy(processHeaders, q.headerResults[q.headerProced:q.headerProced+ready])

		processHashes := make([]common.Hash, ready)
		copy(processHashes, q.headerHashes[q.headerProced:q.headerProced+ready])

		select {
		case headerProcCh <- &headerTask{
			headers: processHeaders,
			hashes:  processHashes,
		}:
			logger.Trace("Pre-scheduled new headers", "count", len(processHeaders), "from", processHeaders[0].Number)
			q.headerProced += len(processHeaders)
		default:
		}
	}
	// Check for termination and return
	if len(q.headerTaskPool) == 0 {
		q.headerContCh <- false
	}
	return len(headers), nil
}
