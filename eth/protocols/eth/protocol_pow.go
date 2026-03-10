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
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/types"
)

const ETH68 = 68

func init() {
	ProtocolVersions = append(ProtocolVersions, ETH68)
	protocolLengths[ETH68] = 17
}

// GetProtocolVersions returns supported protocol versions based on chain type.
// For perpetual PoW chains, only ETH68 is supported because it includes
// TotalDifficulty (TD) in the handshake, which is required for PoW sync.
// ETH69 removed TD from the handshake as it was designed for post-merge (PoS) chains.
func GetProtocolVersions(isPow bool) []uint {
	if isPow {
		return []uint{ETH68} // Only ETH68 for PoW (has TD in handshake)
	}
	return ProtocolVersions // Normal: both versions
}

// StatusPacket68 is the network packet for the ETH/68 status message.
// Unlike ETH/69, it includes TD (total difficulty) which is required for PoW sync.
type StatusPacket68 struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            common.Hash
	Genesis         common.Hash
	ForkID          forkid.ID
}

func (*StatusPacket68) Name() string { return "Status" }
func (*StatusPacket68) Kind() byte   { return StatusMsg }

// NewBlockHashesPacket is the network packet for the block announcement message.
type NewBlockHashesPacket []struct {
	Hash   common.Hash // Hash of one particular block being announced
	Number uint64      // Number of one particular block being announced
}

// Unpack retrieves the individual hashes and numbers from the packet.
func (p *NewBlockHashesPacket) Unpack() ([]common.Hash, []uint64) {
	var (
		hashes  = make([]common.Hash, len(*p))
		numbers = make([]uint64, len(*p))
	)
	for i, body := range *p {
		hashes[i], numbers[i] = body.Hash, body.Number
	}
	return hashes, numbers
}

func (*NewBlockHashesPacket) Name() string { return "NewBlockHashes" }
func (*NewBlockHashesPacket) Kind() byte   { return NewBlockHashesMsg }

// NewBlockPacket is the network packet for the block propagation message.
type NewBlockPacket struct {
	Block *types.Block
	TD    *big.Int
}

// sanityCheck verifies that the values are reasonable, as a DoS protection
func (request *NewBlockPacket) sanityCheck() error {
	if err := request.Block.SanityCheck(); err != nil {
		return err
	}
	//TD at mainnet block #7753254 is 76 bits. If it becomes 100 million times
	// larger, it will still fit within 100 bits
	if tdlen := request.TD.BitLen(); tdlen > 100 {
		return fmt.Errorf("too large block TD: bitlen %d", tdlen)
	}
	return nil
}

func (*NewBlockPacket) Name() string { return "NewBlock" }
func (*NewBlockPacket) Kind() byte   { return NewBlockMsg }

// Unpack retrieves the transactions and uncles from each block body in the response.
// This is needed for PoW block fetcher which processes bodies differently.
func (p *BlockBodiesResponse) Unpack() ([][]*types.Transaction, [][]*types.Header) {
	txset := make([][]*types.Transaction, len(*p))
	uncleset := make([][]*types.Header, len(*p))
	for i, body := range *p {
		txset[i], _ = body.Transactions.Items()
		uncleset[i], _ = body.Uncles.Items()
	}
	return txset, uncleset
}
