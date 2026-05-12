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

package eth

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
)

// TestPoWBackendSeedsEtherbaseFromConfig guards the revived etherbase seed:
// before #28623, eth.New initialized the etherbase from the miner config, so
// `geth --mine` could start mining straight from the command line. Without
// the seed, Etherbase() only ever succeeds after a miner_setEtherbase RPC
// call and --mine dies at startup with "etherbase missing".
func TestPoWBackendSeedsEtherbaseFromConfig(t *testing.T) {
	n, err := node.New(&node.Config{
		P2P: p2p.Config{
			ListenAddr:  "127.0.0.1:0",
			NoDiscovery: true,
			MaxPeers:    0,
		}})
	if err != nil {
		t.Fatal("can't create node:", err)
	}
	defer n.Close()

	want := common.HexToAddress("0x00000000000000000000000000000000deadbeef")
	minerCfg := miner.DefaultConfig
	minerCfg.PendingFeeRecipient = want
	ethashCfg := ethconfig.Defaults.Ethash
	ethashCfg.PowMode = ethash.ModeFake

	ethservice, err := New(n, &ethconfig.Config{
		Genesis:        core.DefaultMordorGenesisBlock(),
		SyncMode:       ethconfig.FullSync,
		TrieTimeout:    time.Minute,
		TrieDirtyCache: 256,
		TrieCleanCache: 256,
		Miner:          minerCfg,
		Ethash:         ethashCfg,
	})
	if err != nil {
		t.Fatal("can't create eth service:", err)
	}

	got, err := ethservice.Etherbase()
	if err != nil {
		t.Fatalf("Etherbase() = %v; miner config PendingFeeRecipient was not seeded", err)
	}
	if got != want {
		t.Fatalf("Etherbase() = %v, want %v", got, want)
	}
}
