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

package core

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// SetPreserve installs the fork-choice preserve callback; no-op on non-PoW chains.
func (bc *BlockChain) SetPreserve(preserve func(header *types.Header) bool) {
	if bc.forker != nil {
		bc.forker.preserve = preserve
	}
}

// GetTd retrieves a block's total difficulty in the canonical chain from the
// database by hash and number, caching it if found.
func (bc *BlockChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return bc.hc.GetTd(hash, number)
}

// WriteTd writes the total difficulty of a block to the database.
// This is used by ETC/PoW networks during chain insertion.
func (bc *BlockChain) WriteTd(hash common.Hash, number uint64, td *big.Int) {
	rawdb.WriteTd(bc.db, hash, number, td)
}

// SubscribeChainSideEvent registers a subscription for side-block headers
// (uncle candidates) surfaced by the TD-based fork choice. On non-PoW chains,
// where no fork choice is installed, the returned subscription never fires.
func (bc *BlockChain) SubscribeChainSideEvent(ch chan<- ChainSideEvent) event.Subscription {
	if bc.forker == nil {
		return bc.scope.Track(new(event.Feed).Subscribe(ch))
	}
	return bc.scope.Track(bc.forker.SubscribeSideBlocks(ch))
}
