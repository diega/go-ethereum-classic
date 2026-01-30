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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
)

// peerWithHighestTD returns the peer with the highest Total Difficulty.
// This is used by perpetual PoW networks where TD is exchanged during the handshake.
// Returns nil if no peers with TD are available.
func (ps *peerSet) peerWithHighestTD() *ethPeer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	var (
		bestPeer *ethPeer
		bestTD   *big.Int
	)
	for _, p := range ps.peers {
		_, td := eth.GetPeerHead(p.ID())
		if td == nil {
			continue
		}
		if bestTD == nil || td.Cmp(bestTD) > 0 {
			bestPeer = p
			bestTD = td
		}
	}
	return bestPeer
}

// peersWithoutBlock returns a list of peers that don't know about the given block.
// This is used by perpetual PoW networks for block propagation.
func (ps *peerSet) peersWithoutBlock(hash common.Hash) []*ethPeer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	list := make([]*ethPeer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.KnownBlock(hash) {
			list = append(list, p)
		}
	}
	return list
}
