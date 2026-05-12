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

package params

import (
	"math/big"
	"testing"
)

// devnetPoWConfig returns a perpetual PoW config with a custom chain ID and
// every shared fork through London active from genesis — the shape of a
// from-scratch ETC-style devnet.
func devnetPoWConfig() *ChainConfig {
	return &ChainConfig{
		ChainID:             big.NewInt(9999),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ECIP1041Block:       big.NewInt(0),
		Ethash:              new(EthashConfig),
	}
}

// TestIsEIP1559PerpetualPoW verifies that the fee market is gated on the config
// shape (perpetual PoW = no fee market), not on chain IDs 61/63: a custom-chain-ID
// PoW devnet gets Mystique semantics, while the same config on a merge track
// (TTD set) gets the real EIP-1559.
func TestIsEIP1559PerpetualPoW(t *testing.T) {
	// The well-known ETC networks: London (Mystique) active, fee market off.
	for _, cfg := range []*ChainConfig{ClassicChainConfig, MordorChainConfig} {
		postMystique := new(big.Int).Add(cfg.LondonBlock, big.NewInt(1))
		if !cfg.IsLondon(postMystique) {
			t.Fatalf("chain %v: expected IsLondon true post-Mystique", cfg.ChainID)
		}
		if cfg.IsEIP1559(postMystique) {
			t.Errorf("chain %v: expected IsEIP1559 false post-Mystique", cfg.ChainID)
		}
	}

	// A devnet with a custom chain ID: same behavior, no chain ID involved.
	zero := big.NewInt(0)
	devnet := devnetPoWConfig()
	if !devnet.IsLondon(zero) {
		t.Fatal("devnet: expected IsLondon true at genesis")
	}
	if devnet.IsEIP1559(zero) {
		t.Error("devnet: expected IsEIP1559 false (perpetual PoW has no fee market)")
	}
	rules := devnet.Rules(zero, false, 0)
	if !rules.IsMystique {
		t.Error("devnet: expected Rules.IsMystique true (London opcodes without fee market)")
	}

	// The same config on a merge track (TTD set) is not perpetual PoW, so the
	// fee market follows London as usual.
	merged := devnetPoWConfig()
	merged.TerminalTotalDifficulty = big.NewInt(0)
	if !merged.IsEIP1559(zero) {
		t.Error("merge-track config: expected IsEIP1559 true post-London")
	}

	// The standard test config is post-merge and keeps the fee market.
	if !TestChainConfig.IsEIP1559(zero) {
		t.Error("TestChainConfig: expected IsEIP1559 true post-London")
	}
}

// TestIsEIP160Window verifies the EIP-160 patch window derives from the config
// splitting EIP-155 from EIP-158 (ETC's DieHard→Atlantis gap), for any chain ID,
// and stays empty when both activate together (Ethereum's EIP-607 bundling).
func TestIsEIP160Window(t *testing.T) {
	split := devnetPoWConfig()
	split.EIP155Block = big.NewInt(100)
	split.EIP158Block = big.NewInt(200)
	if split.isEIP160(big.NewInt(99)) {
		t.Error("expected isEIP160 false before EIP-155")
	}
	if !split.isEIP160(big.NewInt(150)) {
		t.Error("expected isEIP160 true inside the 155..158 window")
	}
	if split.isEIP160(big.NewInt(250)) {
		t.Error("expected isEIP160 false once EIP-158 is active")
	}

	// Ethereum mainnet activates 155 and 158 at the same block: empty window.
	for _, num := range []int64{0, 2_675_000, 3_000_000, 10_000_000} {
		if MainnetChainConfig.isEIP160(big.NewInt(num)) {
			t.Errorf("mainnet: expected isEIP160 false at block %d", num)
		}
	}

	// Classic's real window: DieHard (3M) through Atlantis (8.772M).
	if !ClassicChainConfig.isEIP160(big.NewInt(5_000_000)) {
		t.Error("Classic: expected isEIP160 true between DieHard and Atlantis")
	}
	if ClassicChainConfig.isEIP160(big.NewInt(9_000_000)) {
		t.Error("Classic: expected isEIP160 false after Atlantis")
	}
}
