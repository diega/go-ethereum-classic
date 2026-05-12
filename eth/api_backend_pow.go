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
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/miner"
)

func (b *EthAPIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	if header := b.eth.blockchain.GetHeaderByHash(hash); header != nil {
		return b.eth.blockchain.GetTd(hash, header.Number.Uint64())
	}
	return nil
}

func (b *EthAPIBackend) Miner() *miner.Miner {
	return b.eth.Miner()
}
