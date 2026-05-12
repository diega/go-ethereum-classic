// Copyright 2020 The go-ethereum Authors
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
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// Tests that block headers can be retrieved from a remote chain based on user queries.
func TestGetBlockHeaders68(t *testing.T) { testGetBlockHeaders(t, ETH68) }

// Tests that block contents can be retrieved from a remote chain based on their hashes.
func TestGetBlockBodies68(t *testing.T) { testGetBlockBodies(t, ETH68) }

// Tests that the transaction receipts can be retrieved based on hashes.
func TestGetBlockReceipts68(t *testing.T) { testGetBlockReceipts68(t, ETH68) }

func testGetBlockReceipts68(t *testing.T, protocol uint) {
	t.Parallel()

	// Define three accounts to simulate transactions with
	acc1Key, _ := crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	acc2Key, _ := crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
	acc1Addr := crypto.PubkeyToAddress(acc1Key.PublicKey)
	acc2Addr := crypto.PubkeyToAddress(acc2Key.PublicKey)

	signer := types.HomesteadSigner{}
	// Create a chain generator with some simple transactions (blatantly stolen from @fjl/chain_markets_test)
	generator := func(i int, block *core.BlockGen) {
		switch i {
		case 0:
			// In block 1, the test bank sends account #1 some ether.
			tx, _ := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), acc1Addr, big.NewInt(10_000_000_000_000_000), params.TxGas, block.BaseFee(), nil), signer, testKey)
			block.AddTx(tx)
		case 1:
			// In block 2, the test bank sends some more ether to account #1.
			// acc1Addr passes it on to account #2.
			tx1, _ := types.SignTx(types.NewTransaction(block.TxNonce(testAddr), acc1Addr, big.NewInt(1_000_000_000_000_000), params.TxGas, block.BaseFee(), nil), signer, testKey)
			tx2, _ := types.SignTx(types.NewTransaction(block.TxNonce(acc1Addr), acc2Addr, big.NewInt(1_000_000_000_000_000), params.TxGas, block.BaseFee(), nil), signer, acc1Key)
			block.AddTx(tx1)
			block.AddTx(tx2)
		case 2:
			// Block 3 is empty but was mined by account #2.
			block.SetCoinbase(acc2Addr)
			block.SetExtra([]byte("yeehaw"))
		case 3:
			// Block 4 includes blocks 2 and 3 as uncle headers (with modified extra data).
			b2 := block.PrevBlock(1).Header()
			b2.Extra = []byte("foo")
			block.AddUncle(b2)
			b3 := block.PrevBlock(2).Header()
			b3.Extra = []byte("foo")
			block.AddUncle(b3)
		}
	}
	// Assemble the test environment
	backend := newTestBackendWithGenerator(4, false, false, generator)
	defer backend.close()

	peer, _ := newTestPeer("peer", protocol, backend)
	defer peer.close()

	// Collect the hashes to request, and the response to expect
	var (
		hashes   []common.Hash
		receipts rlp.RawList[*ReceiptList68]
	)
	for i := uint64(0); i <= backend.chain.CurrentBlock().Number.Uint64(); i++ {
		block := backend.chain.GetBlockByNumber(i)
		hashes = append(hashes, block.Hash())
		trs := backend.chain.GetReceiptsByHash(block.Hash())
		receipts.Append(NewReceiptList68(trs))
	}

	// Send the hash request and verify the response
	p2p.Send(peer.app, GetReceiptsMsg, &GetReceiptsPacket{
		RequestId:          123,
		GetReceiptsRequest: hashes,
	})
	if err := p2p.ExpectMsg(peer.app, ReceiptsMsg, &ReceiptsPacket68{
		RequestId: 123,
		List:      receipts,
	}); err != nil {
		t.Errorf("receipts mismatch: %v", err)
	}
}

// Tests that a propagated block claiming more items than a block can hold is
// rejected before its body is decoded.
func TestPropagatedBlockLimits68(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		build   func(*rawBlock)
		wantErr string
	}{
		{
			name: "uncles",
			build: func(b *rawBlock) {
				for i := 0; i <= maxBlockUncles; i++ {
					b.Uncles.Append(&types.Header{Number: big.NewInt(int64(i))})
				}
			},
			wantErr: "too many uncles in propagated block",
		},
		{
			name: "transactions",
			build: func(b *rawBlock) {
				for i := 0; i <= maxBlockTransactions; i++ {
					b.Transactions.Append(types.NewTx(&types.LegacyTx{Nonce: uint64(i)}))
				}
			},
			wantErr: "too many transactions in propagated block",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newTestBackend(1)
			defer backend.close()

			peer, errc := newTestPeer("peer", ETH68, backend)
			defer peer.close()

			ann := rawNewBlockPacket{
				Block: rawBlock{Header: &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1)}},
				TD:    big.NewInt(1),
			}
			tt.build(&ann.Block)

			if err := p2p.Send(peer.app, NewBlockMsg, &ann); err != nil {
				t.Fatalf("failed to send block: %v", err)
			}
			select {
			case err := <-errc:
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("wrong error: got %v, want one containing %q", err, tt.wantErr)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("propagated block was not rejected within 2 seconds")
			}
		})
	}
}

// Tests that an oversized batch of block announcements is rejected.
func TestBlockAnnouncementLimit68(t *testing.T) {
	t.Parallel()

	backend := newTestBackend(1)
	defer backend.close()

	peer, errc := newTestPeer("peer", ETH68, backend)
	defer peer.close()

	ann := make(NewBlockHashesPacket, maxBlockAnnouncements+1)
	for i := range ann {
		ann[i].Number = uint64(i)
	}
	if err := p2p.Send(peer.app, NewBlockHashesMsg, ann); err != nil {
		t.Fatalf("failed to send announcements: %v", err)
	}
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "too many block announcements") {
			t.Fatalf("wrong error: got %v, want one containing %q", err, "too many block announcements")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("announcement batch was not rejected within 2 seconds")
	}
}
