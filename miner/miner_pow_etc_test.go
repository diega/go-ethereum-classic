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

package miner

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/ethash"
)

// TestMinerHashrateMatchesEthashSignature guards against a regression where
// Miner.Hashrate's internal type assertion requires `Hashrate() uint64`
// while ethash exposes `Hashrate() float64`. With the wrong signature the
// assertion silently fails and Miner.Hashrate returns 0 even when the
// engine is reporting a positive rate.
func TestMinerHashrateMatchesEthashSignature(t *testing.T) {
	eng := ethash.NewTester(nil, false)
	defer eng.Close()

	// Submit a known remote hashrate through the engine's RPC API. Use the
	// exposed APIs() to obtain an API service, since the underlying *API
	// type's ethash field is unexported.
	apis := eng.APIs(nil)
	if len(apis) == 0 {
		t.Fatal("ethash.APIs returned empty; cannot reach SubmitHashrate")
	}
	sub, ok := apis[0].Service.(interface {
		SubmitHashrate(hexutil.Uint64, common.Hash) bool
	})
	if !ok {
		t.Fatalf("API service %T does not expose SubmitHashrate", apis[0].Service)
	}
	if ok := sub.SubmitHashrate(hexutil.Uint64(1234), common.HexToHash("0x1")); !ok {
		t.Fatal("SubmitHashrate returned false")
	}

	m := &Miner{engine: eng}
	if got := m.Hashrate(); got != 1234 {
		t.Fatalf("Miner.Hashrate = %d, want 1234 (interface assertion likely missed Hashrate() float64)", got)
	}
}
