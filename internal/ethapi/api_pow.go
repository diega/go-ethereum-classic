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

package ethapi

import (
	"context"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// rpcMarshalHeader uses the generalized output filler, then adds the total difficulty field, which requires
// a BlockchainAPI.
func (api *BlockChainAPI) rpcMarshalHeader(ctx context.Context, header *types.Header) map[string]interface{} {
	fields := RPCMarshalHeader(header)
	fields["totalDifficulty"] = (*hexutil.Big)(api.b.GetTd(ctx, header.Hash()))
	return fields
}

// rpcMarshalBlock uses the generalized output filler, then adds the total difficulty field, which requires
// a BlockchainAPI.
func (api *BlockChainAPI) rpcMarshalBlock(ctx context.Context, b *types.Block, inclTx bool, fullTx bool) (map[string]interface{}, error) {
	fields := RPCMarshalBlock(b, inclTx, fullTx, api.b.ChainConfig())
	if inclTx {
		fields["totalDifficulty"] = (*hexutil.Big)(api.b.GetTd(ctx, b.Hash()))
	}
	return fields, nil
}
