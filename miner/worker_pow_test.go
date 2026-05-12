// Copyright 2015 The go-ethereum Authors
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

package miner

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// testCode is the testing contract binary code which initialises some
	// variables in its constructor.
	testCode = "0x60806040527fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff0060005534801561003457600080fd5b5060fc806100436000396000f3fe6080604052348015600f57600080fd5b506004361060325760003560e01c80630c4dae8814603757806398a213cf146053575b600080fd5b603d607e565b6040518082815260200191505060405180910390f35b607c60048036036020811015606757600080fd5b81019080803590602001909291905050506084565b005b60005481565b806000819055507fe9e44f9f7da8c559de847a3232b57364adc0354f15a2cd8dc636d54396f9587a6000546040518082815260200191505060405180910390a15056fea265627a7a723058208ae31d9424f2d0bc2a3da1a5dd659db2d71ec322a17db8f87e19e209e3a1ff4a64736f6c634300050a0032"

	// testGas is the gas required for the contract deployment above.
	testGas = 144109
)

// newTestPowWorker builds a PoW worker on top of the shared test backend,
// returning the worker (the Miner's powWorker) and the backend.
func newTestPowWorker(t *testing.T, chainConfig *params.ChainConfig, engine consensus.Engine, db ethdb.Database, blocks int) (*powWorker, *testWorkerBackend) {
	backend := newTestWorkerBackend(t, chainConfig, engine, db, blocks)
	backend.txPool.Add(pendingTxs, true)
	m := NewPoW(backend, testConfig, engine, new(event.TypeMux))
	w := m.powWorker
	w.setEtherbase(testBankAddress)
	return w, backend
}

// newRandomTx returns a signed transaction from the test bank account, either a
// contract creation or a plain value transfer.
func (b *testWorkerBackend) newRandomTx(creation bool) *types.Transaction {
	gasPrice := big.NewInt(10 * params.InitialBaseFee)
	if creation {
		tx, _ := types.SignTx(types.NewContractCreation(b.txPool.Nonce(testBankAddress), big.NewInt(0), testGas, gasPrice, common.FromHex(testCode)), types.HomesteadSigner{}, testBankKey)
		return tx
	}
	tx, _ := types.SignTx(types.NewTransaction(b.txPool.Nonce(testBankAddress), testUserAddress, big.NewInt(1000), params.TxGas, gasPrice, nil), types.HomesteadSigner{}, testBankKey)
	return tx
}

func TestGenerateBlockAndImportEthash(t *testing.T) {
	var (
		engine = ethash.NewFaker()
		db     = rawdb.NewMemoryDatabase()
	)
	w, b := newTestPowWorker(t, ethashChainConfig, engine, db, 0)
	defer w.close()

	// This test chain imports the mined blocks.
	chain, _ := core.NewBlockChain(rawdb.NewMemoryDatabase(), b.genesis, engine, &core.BlockChainConfig{ArchiveMode: true})
	defer chain.Stop()

	// Ignore empty commits here for less noise.
	w.skipSealHook = func(task *task) bool {
		return len(task.receipts) == 0
	}

	// Wait for mined blocks.
	sub := w.mux.Subscribe(core.NewMinedBlockEvent{})
	defer sub.Unsubscribe()

	// Start mining!
	w.start()

	for i := 0; i < 5; i++ {
		b.txPool.Add([]*types.Transaction{b.newRandomTx(true)}, true)
		b.txPool.Add([]*types.Transaction{b.newRandomTx(false)}, true)

		select {
		case ev := <-sub.Chan():
			block := ev.Data.(core.NewMinedBlockEvent).Block
			if _, err := chain.InsertChain([]*types.Block{block}); err != nil {
				t.Fatalf("failed to insert new mined block %d: %v", block.NumberU64(), err)
			}
		case <-time.After(3 * time.Second): // Worker needs 1s to include new changes.
			t.Fatalf("timeout")
		}
	}
}

// TestGatherUnclesEthash checks that a side-block header fed to the worker is
// validated and selected as an uncle for a block sealed on top of the chain.
func TestGatherUnclesEthash(t *testing.T) {
	var (
		engine = ethash.NewFaker()
		db     = rawdb.NewMemoryDatabase()
	)
	w, b := newTestPowWorker(t, ethashChainConfig, engine, db, 0)
	defer w.close()

	// Build a short canonical chain on the worker's backend: genesis <- b1 <- b2.
	genDb, blocks, _ := core.GenerateChainWithGenesis(b.genesis, engine, 2, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(testBankAddress)
	})
	if _, err := b.chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert canonical chain: %v", err)
	}
	b1, b2 := blocks[0], blocks[1]

	// A valid uncle for a block sealed on top of b2 is a sibling of b2 (another
	// child of b1): its parent (b1) is an ancestor of the sealing block, but not
	// the sealing block's direct parent (b2).
	siblings, _ := core.GenerateChain(b.chain.Config(), b1, engine, genDb, 1, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(testUserAddress)
	})
	uncle := siblings[0].Header()

	// Feed the uncle candidate, as the fork-choice side-block feed would.
	w.collectUncle(uncle)

	// The block sealed on top of b2 should pick up the uncle.
	sealing := &types.Header{
		ParentHash: b2.Hash(),
		Number:     new(big.Int).Add(b2.Number(), common.Big1),
	}
	got := w.gatherUncles(sealing)
	if len(got) != 1 || got[0].Hash() != uncle.Hash() {
		t.Fatalf("expected uncle %x, got %d uncles", uncle.Hash(), len(got))
	}
}

// TestUncleRecommitEthash verifies that a fresh side block fed while sealing triggers a
// work regeneration (recommit), so the uncle is folded in immediately — matching core-geth —
// rather than waiting for the next resubmit timer.
func TestUncleRecommitEthash(t *testing.T) {
	var (
		engine = ethash.NewFaker()
		db     = rawdb.NewMemoryDatabase()
	)
	w, b := newTestPowWorker(t, ethashChainConfig, engine, db, 0)
	defer w.close()

	// Build genesis <- b1 <- b2 on the worker's backend.
	genDb, blocks, _ := core.GenerateChainWithGenesis(b.genesis, engine, 2, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(testBankAddress)
	})
	if _, err := b.chain.InsertChain(blocks); err != nil {
		t.Fatalf("insert canonical chain: %v", err)
	}
	// A valid uncle: a sibling of b2 (another child of b1).
	siblings, _ := core.GenerateChain(b.chain.Config(), blocks[0], engine, genDb, 1, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(testUserAddress)
	})
	uncle := siblings[0].Header()

	// Count sealing tasks (one per commitWork); don't actually seal.
	taskCh := make(chan struct{}, 16)
	w.newTaskHook = func(*task) {
		select {
		case taskCh <- struct{}{}:
		default:
		}
	}
	w.skipSealHook = func(*task) bool { return true }

	w.start()

	// Drain the task(s) from the initial commit until quiescent.
	for quiet := false; !quiet; {
		select {
		case <-taskCh:
		case <-time.After(time.Second):
			quiet = true
		}
	}

	// Feed a fresh uncle, as the fork-choice side-block feed would.
	w.chainSideCh <- core.ChainSideEvent{Header: uncle}

	// The worker must regenerate work (a new sealing task) to fold the uncle in.
	select {
	case <-taskCh:
		// recommit observed
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not regenerate work after a fresh uncle (no recommit-on-side-block)")
	}
}
