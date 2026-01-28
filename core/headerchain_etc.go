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
	"github.com/ethereum/go-ethereum/ethdb"
)

// GetTd retrieves a block's total difficulty in the canonical chain from the
// database by hash and number, caching it if found.
func (hc *HeaderChain) GetTd(hash common.Hash, number uint64) *big.Int {
	if cached, ok := hc.tdCache.Get(hash); ok {
		return cached
	}
	td := rawdb.ReadTd(hc.chainDb, hash, number)
	if td == nil {
		return nil
	}
	hc.tdCache.Add(hash, td)
	return td
}

// writeTd calculates and writes TD for a block.
// Returns the new accumulated TD for the next block.
func (hc *HeaderChain) writeTd(batch ethdb.KeyValueWriter, hash common.Hash, number uint64, difficulty, parentTd *big.Int) *big.Int {
	td := new(big.Int).Add(parentTd, difficulty)
	rawdb.WriteTd(batch, hash, number, td)
	hc.tdCache.Add(hash, new(big.Int).Set(td))
	return td
}

// deleteTd removes TD from the database.
func (hc *HeaderChain) deleteTd(batch ethdb.KeyValueWriter, hash common.Hash, number uint64) {
	rawdb.DeleteTd(batch, hash, number)
}
