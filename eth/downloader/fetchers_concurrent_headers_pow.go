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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

// headerQueuePoW implements typedQueue for PoW header fetching.
// It uses headerTaskManager to manage chunks and enable parallel fetching.
type headerQueuePoW struct {
	d       *Downloader
	taskMgr *headerTaskManager
}

// waker returns a notification channel that gets pinged in case more header
// fetches have been queued up, so the fetcher might assign it to idle peers.
func (q *headerQueuePoW) waker() chan bool {
	return q.taskMgr.wakeCh
}

// pending returns the number of headers that are currently queued for fetching
// by the concurrent downloader.
func (q *headerQueuePoW) pending() int {
	return q.taskMgr.Pending()
}

// capacity is responsible for calculating how many headers a particular peer is
// estimated to be able to retrieve within the allotted round trip time.
func (q *headerQueuePoW) capacity(peer *peerConnection, rtt time.Duration) int {
	return peer.HeaderCapacity(rtt)
}

// updateCapacity is responsible for updating how many headers a particular peer
// is estimated to be able to retrieve in a unit time.
func (q *headerQueuePoW) updateCapacity(peer *peerConnection, items int, span time.Duration) {
	peer.UpdateHeaderRate(items, span)
}

// reserve is responsible for allocating a requested number of pending headers
// from the download queue to the specified peer.
func (q *headerQueuePoW) reserve(peer *peerConnection, items int) (*fetchRequest, bool, bool) {
	return q.taskMgr.Reserve(peer, items)
}

// unreserve is responsible for removing the current header retrieval allocation
// assigned to a specific peer and placing it back into the pool to allow
// reassigning to some other peer.
func (q *headerQueuePoW) unreserve(peerID string) int {
	fails := q.taskMgr.Unreserve(peerID)
	if fails > 2 {
		log.Trace("Header delivery timed out", "peer", peerID)
	} else if fails > 0 {
		log.Debug("Header delivery stalling", "peer", peerID)
	}
	return fails
}

// request is responsible for converting a generic fetch request into a header
// one and sending it to the remote peer for fulfillment.
func (q *headerQueuePoW) request(peer *peerConnection, req *fetchRequest, resCh chan *eth.Response) (*eth.Request, error) {
	// For header requests, we use the From field to specify the starting block number
	count := q.taskMgr.chunkSize
	if req.From+uint64(count)-1 > q.taskMgr.to {
		count = int(q.taskMgr.to - req.From + 1)
	}

	peer.log.Trace("Requesting new batch of headers", "from", req.From, "count", count)
	return peer.peer.RequestHeadersByNumber(req.From, count, 0, false, resCh)
}

// deliver is responsible for taking a generic response packet from the concurrent
// fetcher, unpacking the header data and delivering it to the task manager.
func (q *headerQueuePoW) deliver(peer *peerConnection, packet *eth.Response) (int, error) {
	headers := *packet.Res.(*eth.BlockHeadersRequest)
	hashes := packet.Meta.([]common.Hash)

	accepted, err := q.taskMgr.Deliver(peer.id, headers, hashes)

	switch {
	case err == nil && len(headers) == 0:
		peer.log.Trace("Requested headers delivered (empty)")
	case err == nil:
		peer.log.Trace("Delivered new batch of headers", "count", len(headers), "accepted", accepted)
	default:
		peer.log.Debug("Failed to deliver retrieved headers", "err", err)
	}

	// Process completed headers in order and schedule for body download
	if err == nil {
		q.processCompletedHeaders()
	}

	return accepted, err
}

// processCompletedHeaders takes completed headers from the task manager
// and schedules them for body download in order.
func (q *headerQueuePoW) processCompletedHeaders() {
	headers, hashes := q.taskMgr.PopCompleted()
	if len(headers) == 0 {
		return
	}

	// Headers were already validated in queue_pow.go:Deliver()
	// Schedule headers for body download
	scheduled := q.d.queue.Schedule(headers, hashes, headers[0].Number.Uint64())
	if scheduled != len(headers) {
		log.Warn("Not all headers scheduled", "scheduled", scheduled, "received", len(headers))
	}

	log.Debug("Scheduled headers for body download",
		"from", headers[0].Number.Uint64(),
		"to", headers[len(headers)-1].Number.Uint64(),
		"count", len(headers))

	// Wake up the body fetcher to process the newly scheduled headers.
	// The body fetcher is blocked on select waiting for this signal.
	select {
	case q.d.queue.blockWakeCh <- true:
	default:
		// Channel already has a pending signal, no need to send another
	}
}
