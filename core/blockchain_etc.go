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
)

// GetTd retrieves a block's total difficulty from the database.
// This is used by ETC/PoW networks where TD is required for chain sync decisions.
func (bc *BlockChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return rawdb.ReadTd(bc.db, hash, number)
}

// WriteTd writes the total difficulty of a block to the database.
// This is used by ETC/PoW networks during chain insertion.
func (bc *BlockChain) WriteTd(hash common.Hash, number uint64, td *big.Int) {
	rawdb.WriteTd(bc.db, hash, number, td)
}
