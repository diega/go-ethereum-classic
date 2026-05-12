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
	"github.com/ethereum/go-ethereum/p2p"
)

// Handshake68PoW executes the eth/68 protocol handshake for perpetual PoW networks.
// Unlike the standard ETH68 handshake, this version sends and receives TD (Total Difficulty)
// which is required for proper chain synchronization in PoW networks.
// After a successful handshake, it initializes the PoW extension with the peer's head info.
func (p *Peer) Handshake68PoW(networkID uint64, td *big.Int, head common.Hash, genesis common.Hash, forkID forkid.ID, forkFilter forkid.Filter) error {
	errc := make(chan error, 2)
	go func() {
		pkt := &StatusPacket68{
			ProtocolVersion: uint32(p.version),
			NetworkID:       networkID,
			TD:              td,
			Head:            head,
			Genesis:         genesis,
			ForkID:          forkID,
		}
		errc <- p2p.Send(p.rw, StatusMsg, pkt)
	}()

	var status StatusPacket68 // safe to read after two values have been received from errc
	go func() {
		errc <- p.readStatus68(networkID, &status, genesis, forkFilter)
	}()

	if err := waitForHandshake(errc, p); err != nil {
		return err
	}
	// Initialize PoW extension with peer's head info directly
	p.InitPoWExtension(status.Head, status.TD)
	return nil
}

// readStatus68 reads and validates an ETH/68 status message from the peer.
func (p *Peer) readStatus68(networkID uint64, status *StatusPacket68, genesis common.Hash, forkFilter forkid.Filter) error {
	if err := p.readStatusMsg(status); err != nil {
		return err
	}
	if status.NetworkID != networkID {
		return fmt.Errorf("%w: %d (!= %d)", errNetworkIDMismatch, status.NetworkID, networkID)
	}
	if uint(status.ProtocolVersion) != p.version {
		return fmt.Errorf("%w: %d (!= %d)", errProtocolVersionMismatch, status.ProtocolVersion, p.version)
	}
	if status.Genesis != genesis {
		return fmt.Errorf("%w: %x (!= %x)", errGenesisMismatch, status.Genesis, genesis)
	}
	if err := forkFilter(status.ForkID); err != nil {
		return fmt.Errorf("%w: %v", errForkIDRejected, err)
	}
	// TD at mainnet block #7753254 is 76 bits. If it becomes 100 million times
	// larger, it will still fit within 100 bits
	if tdlen := status.TD.BitLen(); tdlen > 100 {
		return fmt.Errorf("too large total difficulty: bitlen %d", tdlen)
	}
	return nil
}
