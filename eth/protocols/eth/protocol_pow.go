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
	"github.com/ethereum/go-ethereum/rlp"
)

const ETH68 = 68

func init() {
	// Register ETH68 in the message-length table so the protocol handler
	// knows the message count when it negotiates ETH68 with a PoW peer.
	// Do NOT extend the global ProtocolVersions: that would advertise
	// ETH68 on non-PoW chains too, where it has no TD plumbing.
	protocolLengths[ETH68] = 17
}

// GetProtocolVersions returns the eth protocol versions to advertise based
// on chain type. Perpetual PoW chains only speak ETH68 (the last version
// that still carries TD in the handshake); other chains speak the upstream
// set.
func GetProtocolVersions(isPow bool) []uint {
	if isPow {
		return []uint{ETH68} // Only ETH68 for PoW (has TD in handshake)
	}
	return ProtocolVersions // Normal: ETH70, ETH69
}

// AllProtocolVersions returns every eth protocol version this binary
// understands, regardless of chain type. Use this for membership checks
// (e.g. peerset capability matching) — not for advertising.
func AllProtocolVersions() []uint {
	return append([]uint{ETH68}, ProtocolVersions...)
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

// maxBlockAnnouncements bounds the hashes a single announcement message may
// carry. Past maxKnownBlocks the message defeats itself: every hash is fed into
// the peer's known-blocks cache, which evicts at that size, so the tail of an
// oversized message pushes out its own head. It also sits well above the block
// fetcher's hashLimit, the number of announcements from one peer it tracks at a
// time, in the same arrangement upstream uses for transactions, where the
// protocol-level maxTransactionAnnouncements sits above the fetcher-level
// maxTxAnnounces. For scale, our own side sends one hash per message.
const maxBlockAnnouncements = maxKnownBlocks

// maxBlockTransactions bounds the number of transactions a propagated block may
// carry before it is rejected without being decoded. A block cannot hold more
// transactions than its gas limit admits at the 21000 gas floor of a plain
// transfer, which puts ETC's current 8M limit at a few hundred; this is set far
// above that so it keeps holding for any plausible future gas limit, and equals
// the figure upstream uses for the analogous cap on transaction broadcasts
// (maxTransactionAnnouncements). Computing it from the announced gas limit
// instead would be self-defeating: on this message that field is attacker
// controlled.
const maxBlockTransactions = 5000

// maxBlockUncles is the maximum number of uncles a block can contain, per the
// ethash/etchash consensus rules. Blocks announcing more cannot be valid.
const maxBlockUncles = 2

// NewBlockPacket is the network packet for the block propagation message. It is
// what the sending side encodes and what the backend consumes; inbound messages
// are decoded into rawNewBlockPacket first and assembled into this afterwards.
type NewBlockPacket struct {
	Block *types.Block
	TD    *big.Int
}

func (*NewBlockPacket) Name() string { return "NewBlock" }
func (*NewBlockPacket) Kind() byte   { return NewBlockMsg }

// rawNewBlockPacket is the receiving side's view of NewBlockMsg, holding the
// block body encoded so that its item counts can be checked, and the body
// verified against the header, before any of it is materialized. A broadcast
// carries no request id, so there is nothing to match it against: the counts
// are all that stands between the message size limit and the heap.
type rawNewBlockPacket struct {
	Block rawBlock
	TD    *big.Int
}

// rawBlock mirrors the block encoding, [header, txs, uncles], keeping the two
// lists encoded in the manner of BlockBody. Withdrawals are deliberately absent:
// a PoW block carries none, so a message that includes them is rejected as
// having too many elements.
type rawBlock struct {
	Header       *types.Header
	Transactions rlp.RawList[*types.Transaction]
	Uncles       rlp.RawList[*types.Header]
}

// sanityCheck verifies that the values are reasonable, as a DoS protection
func (request *rawNewBlockPacket) sanityCheck() error {
	if err := request.Block.Header.SanityCheck(); err != nil {
		return err
	}
	//TD at mainnet block #7753254 is 76 bits. If it becomes 100 million times
	// larger, it will still fit within 100 bits
	if tdlen := request.TD.BitLen(); tdlen > 100 {
		return fmt.Errorf("too large block TD: bitlen %d", tdlen)
	}
	return nil
}

// body returns the encoded body parts in the shape the shared hashing helpers
// consume.
func (b *rawBlock) body() BlockBody {
	return BlockBody{Transactions: b.Transactions, Uncles: b.Uncles}
}

// GetReceiptsPacket represents a block receipts query with request ID wrapping.
type GetReceiptsPacket struct {
	RequestId uint64
	GetReceiptsRequest
}

// ReceiptsPacket68 is the network packet for block receipts distribution with
// request ID wrapping.
type ReceiptsPacket68 struct {
	RequestId uint64
	List      rlp.RawList[*ReceiptList68]
}

// ReceiptsRLPPacket is ReceiptsRLPResponse with request ID wrapping.
type ReceiptsRLPPacket struct {
	RequestId uint64
	ReceiptsRLPResponse
}

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
