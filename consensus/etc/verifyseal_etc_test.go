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

package etc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestVerifyUncleHeaderVerifiesSeal checks that verifyUncleHeader verifies the uncle's
// PoW seal. NewFakeFailer fails the seal at exactly one block number (no DAG), so the
// test asserts the routing through VerifySeal independently of the ethash algorithm.
func TestVerifyUncleHeaderVerifiesSeal(t *testing.T) {
	config := params.ClassicChainConfig

	// Pre-Homestead frontier parent; block 2 stays on the frontier difficulty rule.
	parent := &types.Header{
		Number:     big.NewInt(1),
		Time:       1000,
		Difficulty: big.NewInt(131072),
		GasLimit:   5000,
		UncleHash:  types.EmptyUncleHash,
	}
	uncle := &types.Header{
		Number:   big.NewInt(2),
		Time:     parent.Time + 13,
		GasLimit: parent.GasLimit,
	}
	// Difficulty must equal the engine's expectation so validation reaches the
	// seal check rather than short-circuiting on an invalid-difficulty error.
	uncle.Difficulty = CalcDifficultyETC(config, uncle.Time, parent)

	// verifyUncleHeader never dereferences the chain reader (CalcDifficulty keys off
	// the engine's own config), so nil is a valid argument here.

	// Seal verifier rigged to fail at the uncle's number: only surfaces if
	// verifyUncleHeader actually verifies the seal.
	failing := &ETCEngine{inner: ethash.NewFakeFailer(uncle.Number.Uint64()), config: config}
	if err := failing.verifyUncleHeader(nil, uncle, parent); err == nil {
		t.Fatal("verifyUncleHeader accepted an uncle with an invalid PoW seal; the seal is not being verified")
	}

	// Control: identical uncle, seal verifier that fails at a different number. It
	// must validate, proving the rejection above came from the seal check and not
	// from another field (difficulty, gas limit, timestamp, number, ...).
	passing := &ETCEngine{inner: ethash.NewFakeFailer(uncle.Number.Uint64() + 1000), config: config}
	if err := passing.verifyUncleHeader(nil, uncle, parent); err != nil {
		t.Fatalf("verifyUncleHeader rejected an otherwise-valid uncle: %v", err)
	}
}
