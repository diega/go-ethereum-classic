// Copyright 2026 The go-ethereum Authors
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
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestInsertReceiptChainPoWRejectsMissingParentTd guards against a silent
// fallback in writeAncient where, if the parent TD is missing on a PoW
// chain, the ancient writer downgraded to the no-TD upstream variant and
// left the freezer with nil-difficulty ancients. The fix returns
// ErrUnknownAncestor instead, matching the WriteHeaders semantics.
func TestInsertReceiptChainPoWRejectsMissingParentTd(t *testing.T) {
	// PoW config: Ethash != nil AND TerminalTotalDifficulty == nil,
	// so chainConfig.IsPow() returns true.
	cfg := *params.AllEthashProtocolChanges
	cfg.TerminalTotalDifficulty = nil

	gspec := &Genesis{
		BaseFee: big.NewInt(params.InitialBaseFee),
		Config:  &cfg,
	}
	genDb, blocks, receipts := GenerateChainWithGenesis(gspec, ethash.NewFaker(), 8, nil)
	_ = genDb

	db, err := rawdb.Open(rawdb.NewMemoryDatabase(), rawdb.OpenOptions{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	bc, err := NewBlockChain(db, gspec, ethash.NewFaker(), DefaultConfig())
	if err != nil {
		t.Fatalf("new blockchain: %v", err)
	}
	defer bc.Stop()

	// Populate TDs for blocks 1..4 by importing them normally.
	if _, err := bc.InsertChain(blocks[:4]); err != nil {
		t.Fatalf("InsertChain prefix: %v", err)
	}

	// Simulate TD-store corruption: drop the TD for block #4 (blocks[3]).
	// Now blocks[4]'s parent TD lookup returns nil, which is exactly the
	// condition the buggy fallback masked by writing ancients without TD.
	rawdb.DeleteTd(db, blocks[3].Hash(), 4)
	bc.hc.tdCache.Purge()

	// ancientLimit covers blocks[4:], forcing them through writeAncient.
	_, err = bc.InsertReceiptChain(blocks[4:], types.EncodeBlockReceiptLists(receipts[4:]), 8)
	if !errors.Is(err, consensus.ErrUnknownAncestor) {
		t.Fatalf("InsertReceiptChain on PoW with missing parent TD: want ErrUnknownAncestor, got %v", err)
	}
}
