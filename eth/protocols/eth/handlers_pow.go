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
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
)

// handleNewBlockhashesPow handles block hash announcements for PoW networks.
// This is called when peer.powExt != nil, indicating a PoW peer.
func handleNewBlockhashesPow(backend Backend, msg Decoder, peer *Peer) error {
	ann := new(NewBlockHashesPacket)
	if err := msg.Decode(ann); err != nil {
		return err
	}
	// Mark the hashes as known for this peer
	for _, block := range *ann {
		peer.MarkBlock(block.Hash)
	}
	// Delegate to the backend for processing
	return backend.Handle(peer, ann)
}

// handleNewBlockPow handles block broadcasts for PoW networks.
// This is called when peer.powExt != nil, indicating a PoW peer.
func handleNewBlockPow(backend Backend, msg Decoder, peer *Peer) error {
	ann := new(NewBlockPacket)
	if err := msg.Decode(ann); err != nil {
		return err
	}
	// Validate the block integrity
	if hash := types.CalcUncleHash(ann.Block.Uncles()); hash != ann.Block.UncleHash() {
		log.Warn("Propagated block has invalid uncles", "have", hash, "exp", ann.Block.UncleHash())
		return nil // Don't disconnect, just ignore the block
	}
	if hash := types.DeriveSha(ann.Block.Transactions(), trie.NewStackTrie(nil)); hash != ann.Block.TxHash() {
		log.Warn("Propagated block has invalid body", "have", hash, "exp", ann.Block.TxHash())
		return nil // Don't disconnect, just ignore the block
	}
	// Set metadata for the block
	ann.Block.ReceivedAt = msg.Time()
	ann.Block.ReceivedFrom = peer
	// Mark the block as known for this peer
	peer.MarkBlock(ann.Block.Hash())
	// Delegate to the backend for processing
	return backend.Handle(peer, ann)
}
