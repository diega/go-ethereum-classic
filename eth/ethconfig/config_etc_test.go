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

package ethconfig

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/etc"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/params"
)

// TestPoWEngineExposesHashrate guards against the consensus engine for PoW
// chains being hidden behind a wrapper that drops the Hashrate method, the
// way upstream wraps every engine in beacon.Beacon. Both miner.Hashrate and
// the ethstats mining report discover the hashrate through a silent type
// assertion on interface{ Hashrate() float64 }, so a wrapped engine reports
// 0 forever without failing loudly (core-geth ships exactly this bug: its
// eth_hashrate is always 0 because beacon.Beacon has no Hashrate method).
// customPoWConfig returns a perpetual PoW config with a custom chain ID —
// the shape of a from-scratch devnet. With ecip set, it carries an ECIP key
// (bomb disposal at genesis); without, it is a plain pre-merge ethash chain
// like the cmd/devp2p ethtest testdata one.
func customPoWConfig(ecip bool) *params.ChainConfig {
	config := &params.ChainConfig{
		ChainID:        big.NewInt(9999),
		HomesteadBlock: big.NewInt(0),
		EIP150Block:    big.NewInt(0),
		EIP155Block:    big.NewInt(0),
		EIP158Block:    big.NewInt(0),
		Ethash:         new(params.EthashConfig),
	}
	if ecip {
		config.ECIP1041Block = big.NewInt(0)
	}
	return config
}

// TestEngineSelectionByConfigShape guards that the consensus engine is picked
// from the config alone: any perpetual PoW config (ethash section, no TTD) gets
// the ETCEngine regardless of chain ID, and merge-track configs get the beacon
// wrapper. Chain IDs 61/63 must play no role in the selection.
func TestEngineSelectionByConfigShape(t *testing.T) {
	for _, tt := range []struct {
		name    string
		config  *params.ChainConfig
		wantETC bool
	}{
		{"classic", params.ClassicChainConfig, true},
		{"mordor", params.MordorChainConfig, true},
		{"devnet_with_ecip_keys", customPoWConfig(true), true},
		{"pow_without_ecip_keys", customPoWConfig(false), true},
		{"merged", params.MergedTestChainConfig, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CreateConsensusEngine(tt.config, rawdb.NewMemoryDatabase())
			if err != nil {
				t.Fatalf("CreateConsensusEngine: %v", err)
			}
			defer engine.Close()
			if _, isETC := engine.(*etc.ETCEngine); isETC != tt.wantETC {
				t.Fatalf("engine = %T, want ETCEngine: %v", engine, tt.wantETC)
			}
			if _, isBeacon := engine.(*beacon.Beacon); isBeacon == tt.wantETC {
				t.Fatalf("engine = %T, want beacon: %v", engine, !tt.wantETC)
			}
		})
	}

	// A non-PoW config without TTD has no valid engine: this remains an error.
	noEngine := customPoWConfig(false)
	noEngine.Ethash = nil
	if _, err := CreateConsensusEngine(noEngine, rawdb.NewMemoryDatabase()); err == nil {
		t.Fatal("expected error for a config with neither PoW engine nor TTD")
	}
}

func TestPoWEngineExposesHashrate(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config *params.ChainConfig
	}{
		{"classic", params.ClassicChainConfig},
		{"mordor", params.MordorChainConfig},
		{"devnet", customPoWConfig(true)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := CreateConsensusEngine(tt.config, rawdb.NewMemoryDatabase())
			if err != nil {
				t.Fatalf("CreateConsensusEngine: %v", err)
			}
			defer engine.Close()
			if _, ok := engine.(interface{ Hashrate() float64 }); !ok {
				t.Fatalf("engine %T does not expose Hashrate() float64; miner and ethstats would report 0", engine)
			}
		})
	}
}
