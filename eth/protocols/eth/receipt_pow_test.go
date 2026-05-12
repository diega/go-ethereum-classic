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
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

func TestReceiptList68(t *testing.T) {
	for i, test := range receiptsTests {
		// encode receipts from types.ReceiptForStorage object.
		canonDB, _ := rlp.EncodeToBytes(test.input)

		// encode block body from types object.
		blockBody := types.Body{Transactions: test.txs}
		canonBody, _ := rlp.EncodeToBytes(blockBody)

		// convert from storage encoding to network encoding
		network, err := blockReceiptsToNetwork68(canonDB, canonBody)
		if err != nil {
			t.Fatalf("test[%d]: blockReceiptsToNetwork68 error: %v", i, err)
		}

		// parse as Receipts response list from network encoding
		var rl ReceiptList68
		if err := rlp.DecodeBytes(network, &rl); err != nil {
			t.Fatalf("test[%d]: can't decode network receipts: %v", i, err)
		}
		rlStorageEnc, err := rl.EncodeForStorage()
		if err != nil {
			t.Fatalf("test[%d]: error from EncodeForStorage: %v", i, err)
		}
		if !bytes.Equal(rlStorageEnc, canonDB) {
			t.Fatalf("test[%d]: re-encoded receipts not equal\nhave: %x\nwant: %x", i, rlStorageEnc, canonDB)
		}
		rlNetworkEnc, _ := rlp.EncodeToBytes(&rl)
		if !bytes.Equal(rlNetworkEnc, network) {
			t.Fatalf("test[%d]: re-encoded network receipt list not equal\nhave: %x\nwant: %x", i, rlNetworkEnc, network)
		}

		// compute root hash from ReceiptList68 and compare.
		responseHash := types.DeriveSha(rl.Derivable(), trie.NewStackTrie(nil))
		if responseHash != test.root {
			t.Fatalf("test[%d]: wrong root hash from ReceiptList68\nhave: %v\nwant: %v", i, responseHash, test.root)
		}
	}
}
