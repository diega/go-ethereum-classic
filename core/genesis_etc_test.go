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

package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestChainConfigOrDefaultClassic verifies that a stored Classic/Mordor config wins over
// the genesis hash that ETC shares with ETH mainnet. Without this guard, opening a Classic
// datadir with no explicit genesis (embedded use, or running without --classic) would be
// misresolved to Ethereum mainnet.
func TestChainConfigOrDefaultClassic(t *testing.T) {
	var g *Genesis // no explicit genesis provided

	// A stored Classic config must win over the shared ETH/ETC genesis hash.
	if got := g.chainConfigOrDefault(params.MainnetGenesisHash, params.ClassicChainConfig); got != params.ClassicChainConfig {
		t.Errorf("Classic datadir misresolved over shared genesis hash: got ChainID %v, want Classic (61)", got.ChainID)
	}
	// Mordor is preferred too (it shares no hash with mainnet, but the guard is harmless).
	if got := g.chainConfigOrDefault(params.MordorGenesisHash, params.MordorChainConfig); got != params.MordorChainConfig {
		t.Errorf("Mordor datadir misresolved: got ChainID %v, want Mordor (63)", got.ChainID)
	}
	// With no stored config, the shared genesis hash still resolves to ETH mainnet (unchanged).
	if got := g.chainConfigOrDefault(params.MainnetGenesisHash, nil); got != params.MainnetChainConfig {
		t.Error("with no stored config, shared genesis hash should resolve to ETH mainnet")
	}
}
