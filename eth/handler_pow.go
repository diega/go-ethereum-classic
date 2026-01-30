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
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/fetcher"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/log"
)

// handleBlockAnnouncesPoW handles incoming block hash announcements for PoW networks.
// This is used by perpetual PoW chains like Ethereum Classic.
func (h *handler) handleBlockAnnouncesPoW(peer *eth.Peer, hashes []common.Hash, numbers []uint64) error {
	// Mark the hashes as known for this peer
	for _, hash := range hashes {
		peer.MarkBlock(hash)
	}
	// Filter out any hashes we already have
	unknownHashes := make([]common.Hash, 0, len(hashes))
	unknownNumbers := make([]uint64, 0, len(numbers))
	for i := 0; i < len(hashes); i++ {
		if !h.chain.HasBlock(hashes[i], numbers[i]) {
			unknownHashes = append(unknownHashes, hashes[i])
			unknownNumbers = append(unknownNumbers, numbers[i])
		}
	}
	// Notify the block fetcher about the new hashes
	for i := 0; i < len(unknownHashes); i++ {
		if h.blockFetcher != nil {
			h.blockFetcher.Notify(peer.ID(), unknownHashes[i], unknownNumbers[i],
				time.Now(), nil, nil)
		}
	}
	return nil
}

// handleBlockBroadcastPoW handles an incoming block broadcast for PoW networks.
// This is used by perpetual PoW chains like Ethereum Classic.
func (h *handler) handleBlockBroadcastPoW(peer *eth.Peer, block *types.Block, td *big.Int) error {
	// Mark the block as known for this peer
	peer.MarkBlock(block.Hash())

	// Enqueue the block for import
	if h.blockFetcher != nil {
		h.blockFetcher.Enqueue(peer.ID(), block)
	}

	// Update the peer's head if the announced TD is higher
	if _, peerTD := peer.Head(); peerTD == nil || td.Cmp(peerTD) > 0 {
		peer.SetHead(block.ParentHash(), new(big.Int).Sub(td, block.Difficulty()))

		// Notify the chain syncer about the update
		h.chainSync.handlePeerEvent()
	}
	return nil
}

// BroadcastBlockPoW propagates a block to a subset of peers, or only announces it
// depending on the propagate flag. This is used by perpetual PoW networks.
func (h *handler) BroadcastBlockPoW(block *types.Block, propagate bool) {
	// Get the peers that don't know about this block
	peers := h.peers.peersWithoutBlock(block.Hash())
	if len(peers) == 0 {
		return
	}

	if propagate {
		// Calculate the TD of the block
		parent := h.chain.GetBlock(block.ParentHash(), block.NumberU64()-1)
		if parent == nil {
			log.Warn("Propagating dangling block", "number", block.Number(), "hash", block.Hash())
			return
		}
		parentTD := h.chain.GetTd(block.ParentHash(), block.NumberU64()-1)
		if parentTD == nil {
			log.Warn("Propagating block with unknown TD", "number", block.Number(), "hash", block.Hash())
			return
		}
		td := new(big.Int).Add(block.Difficulty(), parentTD)

		// Send to sqrt(peers) for full propagation
		transfer := peers[:int(math.Sqrt(float64(len(peers))))]
		for _, peer := range transfer {
			peer.AsyncSendNewBlock(block, td)
		}
		log.Trace("Propagated block", "hash", block.Hash(), "recipients", len(transfer), "duration", common.PrettyDuration(0))
		return
	}

	// Only announce to all peers
	for _, peer := range peers {
		peer.AsyncSendNewBlockHash(block)
	}
	log.Trace("Announced block", "hash", block.Hash(), "recipients", len(peers), "duration", common.PrettyDuration(0))
}

// startBlockFetcherPoW initializes and starts the block fetcher for PoW networks.
func (h *handler) startBlockFetcherPoW() {
	// Create the block fetcher callbacks
	getBlock := func(hash common.Hash) *types.Block {
		return h.chain.GetBlockByHash(hash)
	}
	verifyHeader := func(header *types.Header) error {
		return h.chain.Engine().VerifyHeader(h.chain, header)
	}
	broadcastBlock := func(block *types.Block, propagate bool) {
		h.BroadcastBlockPoW(block, propagate)
	}
	chainHeight := func() uint64 {
		return h.chain.CurrentBlock().Number.Uint64()
	}
	headBlock := func() common.Hash {
		return h.chain.CurrentBlock().Hash()
	}
	insertChain := func(blocks types.Blocks) (int, error) {
		// If snap sync is running, deny importing blocks
		if h.snapSync.Load() {
			log.Warn("Snap sync running, denying block import", "count", len(blocks))
			return 0, nil
		}
		return h.chain.InsertChain(blocks)
	}
	dropPeer := func(id string) {
		h.removePeer(id)
	}

	h.blockFetcher = fetcher.NewBlockFetcherPoW(
		getBlock,
		verifyHeader,
		broadcastBlock,
		chainHeight,
		headBlock,
		insertChain,
		dropPeer,
	)
	h.blockFetcher.Start()
}
