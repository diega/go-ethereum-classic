// Copyright 2021 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package ethtest

import (
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/internal/utesting"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
)

func TestEthSuite(t *testing.T) {
	geth, err := runGeth("./testdata")
	if err != nil {
		t.Fatalf("could not run geth: %v", err)
	}
	defer geth.Close()

	suite, err := NewSuite(geth.Server().Self(), "./testdata", "", "")
	if err != nil {
		t.Fatalf("could not create new test suite: %v", err)
	}
	for _, test := range suite.EthTests() {
		t.Run(test.Name, func(t *testing.T) {
			if test.Slow && testing.Short() {
				t.Skipf("%s: skipping in -short mode", test.Name)
			}
			result := utesting.RunTests([]utesting.Test{{Name: test.Name, Fn: test.Fn}}, os.Stdout)
			if result[0].Failed {
				t.Fatal()
			}
		})
	}
}

func TestSnapSuite(t *testing.T) {
	geth, err := runGeth("./testdata")
	if err != nil {
		t.Fatalf("could not run geth: %v", err)
	}
	defer geth.Close()

	suite, err := NewSuite(geth.Server().Self(), "./testdata", "", "")
	if err != nil {
		t.Fatalf("could not create new test suite: %v", err)
	}
	for _, test := range suite.SnapTests() {
		t.Run(test.Name, func(t *testing.T) {
			result := utesting.RunTests([]utesting.Test{{Name: test.Name, Fn: test.Fn}}, os.Stdout)
			if result[0].Failed {
				t.Fatal()
			}
		})
	}
}

// runGeth creates and starts a geth node configured as PoW (ETH/68).
func runGeth(dir string) (*node.Node, error) {
	stack, err := node.New(&node.Config{
		P2P: p2p.Config{
			ListenAddr:  "127.0.0.1:0",
			NoDiscovery: true,
			MaxPeers:    10,
			NoDial:      true,
		},
	})
	if err != nil {
		return nil, err
	}
	if err := setupGeth(stack, dir); err != nil {
		stack.Close()
		return nil, err
	}
	if err = stack.Start(); err != nil {
		stack.Close()
		return nil, err
	}
	return stack, nil
}

func setupGeth(stack *node.Node, dir string) error {
	chain, err := NewChain(dir)
	if err != nil {
		return err
	}
	// Configure as PoW with ModeFake (cf. core-geth setupGeth):
	// - Ethash config present so engine uses PoW path (ETH/68 with TD)
	// - ModeFake skips seal verification (testdata blocks have zero mixhash)
	// - TTD cleared so node runs perpetual PoW, not beacon sync
	// Configure as PoW with ModeFake (cf. core-geth setupGeth).
	// Ethash config present so engine uses PoW path (ETH/68 with TD).
	// ModeFake skips seal verification (testdata blocks have zero mixhash).
	chain.genesis.Config.Ethash = new(params.EthashConfig)

	backend, err := eth.New(stack, &ethconfig.Config{
		Genesis:        &chain.genesis,
		Ethash:         ethash.Config{PowMode: ethash.ModeFake},
		NetworkId:      chain.genesis.Config.ChainID.Uint64(),
		DatabaseCache:  10,
		TrieCleanCache: 10,
		TrieDirtyCache: 16,
		TrieTimeout:    60 * time.Minute,
		SnapshotCache:  10,
	})
	if err != nil {
		return err
	}
	_, err = backend.BlockChain().InsertChain(chain.blocks[1:])
	return err
}
