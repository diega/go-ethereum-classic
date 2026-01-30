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
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"
)

// PoW-specific constants for block propagation
const (
	maxKnownBlocks     = 1024 // Maximum block hashes to keep in known list
	maxQueuedBlocks    = 4    // Maximum block propagations to queue
	maxQueuedBlockAnns = 4    // Maximum block announcements to queue
)

// blockPropagation represents a block propagation event with its total difficulty.
type blockPropagation struct {
	block *types.Block
	td    *big.Int
}

// PoWPeerExtension holds PoW-specific peer state for perpetual PoW chains.
type PoWPeerExtension struct {
	lock            sync.RWMutex
	head            common.Hash
	td              *big.Int
	knownBlocks     *knownCache            // Blocks that the peer knows about
	queuedBlocks    chan *blockPropagation // Queue of blocks to propagate
	queuedBlockAnns chan *types.Block      // Queue of block announcements
}

// peerHeadInfo stores the head and TD information for a peer.
// This is used by perpetual PoW networks where TD is exchanged during handshake.
type peerHeadInfo struct {
	head common.Hash
	td   *big.Int
}

var (
	peerHeadsMu sync.RWMutex
	peerHeads   = make(map[string]*peerHeadInfo)
)

// SetPeerHead stores the head hash and TD for a peer.
// This is called after a successful PoW handshake.
func SetPeerHead(id string, head common.Hash, td *big.Int) {
	peerHeadsMu.Lock()
	defer peerHeadsMu.Unlock()
	peerHeads[id] = &peerHeadInfo{
		head: head,
		td:   td,
	}
}

// GetPeerHead retrieves the head hash and TD for a peer.
// Returns empty hash and nil TD if peer is not found.
func GetPeerHead(id string) (common.Hash, *big.Int) {
	peerHeadsMu.RLock()
	defer peerHeadsMu.RUnlock()
	if info, ok := peerHeads[id]; ok {
		return info.head, info.td
	}
	return common.Hash{}, nil
}

// DeletePeerHead removes the head/TD info for a peer.
// This is called when a peer disconnects.
func DeletePeerHead(id string) {
	peerHeadsMu.Lock()
	defer peerHeadsMu.Unlock()
	delete(peerHeads, id)
}

// InitPoWExtension initializes PoW-specific fields for the peer.
// This should be called after a successful PoW handshake.
func (p *Peer) InitPoWExtension(head common.Hash, td *big.Int) {
	p.powExt = &PoWPeerExtension{
		head:            head,
		td:              new(big.Int).Set(td),
		knownBlocks:     newKnownCache(maxKnownBlocks),
		queuedBlocks:    make(chan *blockPropagation, maxQueuedBlocks),
		queuedBlockAnns: make(chan *types.Block, maxQueuedBlockAnns),
	}
	go p.broadcastBlocksPoW()
}

// MarkBlock marks a block as known for the peer, ensuring that it
// will never be propagated to this particular peer.
func (p *Peer) MarkBlock(hash common.Hash) {
	if p.powExt != nil {
		p.powExt.knownBlocks.Add(hash)
	}
}

// KnownBlock returns whether peer is known to already have a block.
func (p *Peer) KnownBlock(hash common.Hash) bool {
	if p.powExt == nil {
		return false
	}
	return p.powExt.knownBlocks.Contains(hash)
}

// Head returns the current head hash and total difficulty of the peer.
func (p *Peer) Head() (common.Hash, *big.Int) {
	if p.powExt == nil {
		return common.Hash{}, nil
	}
	p.powExt.lock.RLock()
	defer p.powExt.lock.RUnlock()
	return p.powExt.head, new(big.Int).Set(p.powExt.td)
}

// SetHead updates the head hash and total difficulty of the peer.
func (p *Peer) SetHead(hash common.Hash, td *big.Int) {
	if p.powExt == nil {
		return
	}
	p.powExt.lock.Lock()
	defer p.powExt.lock.Unlock()
	p.powExt.head = hash
	p.powExt.td = new(big.Int).Set(td)

	// Also update the global peer head map
	SetPeerHead(p.id, hash, td)
}

// AsyncSendNewBlock queues an entire block for propagation to a remote peer.
// If the peer's broadcast queue is full, the block is silently dropped.
func (p *Peer) AsyncSendNewBlock(block *types.Block, td *big.Int) {
	if p.powExt == nil {
		return
	}
	select {
	case p.powExt.queuedBlocks <- &blockPropagation{block: block, td: td}:
		p.MarkBlock(block.Hash())
	default:
		// Queue full, drop
	}
}

// AsyncSendNewBlockHash queues a block hash for announcement to a remote peer.
// If the peer's broadcast queue is full, the hash is silently dropped.
func (p *Peer) AsyncSendNewBlockHash(block *types.Block) {
	if p.powExt == nil {
		return
	}
	select {
	case p.powExt.queuedBlockAnns <- block:
		p.MarkBlock(block.Hash())
	default:
		// Queue full, drop
	}
}

// SendNewBlock propagates an entire block to a remote peer.
func (p *Peer) SendNewBlock(block *types.Block, td *big.Int) error {
	return p2p.Send(p.rw, NewBlockMsg, &NewBlockPacket{
		Block: block,
		TD:    td,
	})
}

// SendNewBlockHashes announces the availability of a number of blocks.
func (p *Peer) SendNewBlockHashes(hashes []common.Hash, numbers []uint64) error {
	request := make(NewBlockHashesPacket, len(hashes))
	for i := 0; i < len(hashes); i++ {
		request[i].Hash = hashes[i]
		request[i].Number = numbers[i]
	}
	return p2p.Send(p.rw, NewBlockHashesMsg, request)
}

// broadcastBlocksPoW is a goroutine that broadcasts blocks and block hashes
// to the peer. It runs until the peer is closed.
func (p *Peer) broadcastBlocksPoW() {
	for {
		select {
		case prop := <-p.powExt.queuedBlocks:
			if err := p.SendNewBlock(prop.block, prop.td); err != nil {
				p.Log().Debug("Failed to send new block", "err", err)
				return
			}
		case block := <-p.powExt.queuedBlockAnns:
			if err := p.SendNewBlockHashes([]common.Hash{block.Hash()}, []uint64{block.NumberU64()}); err != nil {
				p.Log().Debug("Failed to send new block hash", "err", err)
				return
			}
		case <-p.term:
			return
		}
	}
}
