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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/tracker"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

func init() {
	versionHandlers[ETH68] = map[uint64]msgHandler{
		NewBlockHashesMsg:             handleNewBlockhashesPow,
		NewBlockMsg:                   handleNewBlockPow,
		TransactionsMsg:               handleTransactions,
		NewPooledTransactionHashesMsg: handleNewPooledTransactionHashes,
		GetBlockHeadersMsg:            handleGetBlockHeaders,
		BlockHeadersMsg:               handleBlockHeaders,
		GetBlockBodiesMsg:             handleGetBlockBodies,
		BlockBodiesMsg:                handleBlockBodies,
		GetReceiptsMsg:                handleGetReceipts68,
		ReceiptsMsg:                   handleReceipts68,
		GetPooledTransactionsMsg:      handleGetPooledTransactions,
		PooledTransactionsMsg:         handlePooledTransactions,
	}
}

func handleNewBlockhashesPow(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of new block announcements just arrived
	ann := new(NewBlockHashesPacket)
	if err := msg.Decode(ann); err != nil {
		return err
	}
	if len(*ann) > maxBlockAnnouncements {
		return fmt.Errorf("too many block announcements: %d > %d", len(*ann), maxBlockAnnouncements)
	}
	// Mark the hashes as present at the remote node
	for _, block := range *ann {
		peer.markBlock(block.Hash)
	}
	// Deliver them all to the backend for queuing
	return backend.Handle(peer, ann)
}

func handleNewBlockPow(backend Backend, msg Decoder, peer *Peer) error {
	// Retrieve the propagated block, leaving its body encoded for now
	ann := new(rawNewBlockPacket)
	if err := msg.Decode(ann); err != nil {
		return err
	}
	if err := ann.sanityCheck(); err != nil {
		return err
	}
	// Bound what the body may expand into. Decoding into rlp.RawList only counts
	// the items, so this runs before any transaction or uncle is materialized.
	if txs := ann.Block.Transactions.Len(); txs > maxBlockTransactions {
		return fmt.Errorf("too many transactions in propagated block: %d > %d", txs, maxBlockTransactions)
	}
	if uncles := ann.Block.Uncles.Len(); uncles > maxBlockUncles {
		return fmt.Errorf("too many uncles in propagated block: %d > %d", uncles, maxBlockUncles)
	}
	// Check the body against the header on the encoded form, so a block that is
	// not what it claims to be is dropped without being materialized either.
	header := ann.Block.Header
	roots := hashBodyParts([]BlockBody{ann.Block.body()})
	if hash := roots.UncleHashes[0]; hash != header.UncleHash {
		log.Warn("Propagated block has invalid uncles", "have", hash, "exp", header.UncleHash)
		return nil // TODO(karalabe): return error eventually, but wait a few releases
	}
	if hash := roots.TransactionRoots[0]; hash != header.TxHash {
		log.Warn("Propagated block has invalid body", "have", hash, "exp", header.TxHash)
		return nil // TODO(karalabe): return error eventually, but wait a few releases
	}
	// Bounded and self-consistent, so it is safe to materialize now.
	txs, err := ann.Block.Transactions.Items()
	if err != nil {
		return fmt.Errorf("NewBlock: %w", err)
	}
	uncles, err := ann.Block.Uncles.Items()
	if err != nil {
		return fmt.Errorf("NewBlock: %w", err)
	}
	block := types.NewBlockWithHeader(header).WithBody(types.Body{Transactions: txs, Uncles: uncles})
	block.ReceivedAt = time.Now()
	block.ReceivedFrom = peer

	// Mark the peer as owning the block
	peer.markBlock(block.Hash())

	return backend.Handle(peer, &NewBlockPacket{Block: block, TD: ann.TD})
}

func handleGetReceipts68(backend Backend, msg Decoder, peer *Peer) error {
	// Decode the block receipts retrieval message
	var query GetReceiptsPacket
	if err := msg.Decode(&query); err != nil {
		return err
	}
	response := ServiceGetReceiptsQuery68(backend.Chain(), query.GetReceiptsRequest)
	return peer.ReplyReceiptsRLP(query.RequestId, response)
}

// ServiceGetReceiptsQuery68 assembles the response to a receipt query. It is
// exposed to allow external packages to test protocol behavior.
func ServiceGetReceiptsQuery68(chain *core.BlockChain, query GetReceiptsRequest) []rlp.RawValue {
	// Gather state data until the fetch or network limits is reached
	var (
		bytes    int
		receipts []rlp.RawValue
	)
	for lookups, hash := range query {
		if bytes >= softResponseLimit || len(receipts) >= maxReceiptsServe ||
			lookups >= 2*maxReceiptsServe {
			break
		}
		// Retrieve the requested block's receipts
		results := chain.GetReceiptsRLP(hash)
		if results == nil {
			if header := chain.GetHeaderByHash(hash); header == nil || header.ReceiptHash != types.EmptyRootHash {
				continue
			}
			results = rlp.EmptyList
		} else {
			body := chain.GetBodyRLP(hash)
			if body == nil {
				continue
			}
			var err error
			results, err = blockReceiptsToNetwork68(results, body)
			if err != nil {
				log.Error("Error in block receipts conversion", "hash", hash, "err", err)
				continue
			}
		}
		receipts = append(receipts, results)
		bytes += len(results)
	}
	return receipts
}

func handleReceipts68(backend Backend, msg Decoder, peer *Peer) error {
	// A batch of receipts arrived to one of our previous requests
	res := new(ReceiptsPacket68)
	if err := msg.Decode(res); err != nil {
		return err
	}

	tresp := tracker.Response{ID: res.RequestId, MsgCode: ReceiptsMsg, Size: res.List.Len()}
	if err := peer.tracker.Fulfil(tresp); err != nil {
		return fmt.Errorf("Receipts: %w", err)
	}

	// Assign temporary hashing buffer to each list item, the same buffer is shared
	// between all receipt list instances.
	receiptLists, err := res.List.Items()
	if err != nil {
		return fmt.Errorf("Receipts: %w", err)
	}
	buffers := new(receiptListBuffers)
	for i := range receiptLists {
		receiptLists[i].setBuffers(buffers)
	}

	metadata := func() interface{} {
		hasher := trie.NewStackTrie(nil)
		hashes := make([]common.Hash, len(receiptLists))
		for i := range receiptLists {
			hashes[i] = types.DeriveSha(receiptLists[i].Derivable(), hasher)
		}
		return hashes
	}
	var enc ReceiptsRLPResponse
	for i := range receiptLists {
		encReceipts, err := receiptLists[i].EncodeForStorage()
		if err != nil {
			return fmt.Errorf("Receipts: invalid list %d: %v", i, err)
		}
		enc = append(enc, encReceipts)
	}
	return peer.dispatchResponse(&Response{
		id:   res.RequestId,
		code: ReceiptsMsg,
		Res:  &enc,
	}, metadata)
}
