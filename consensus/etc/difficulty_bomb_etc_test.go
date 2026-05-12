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

	"github.com/ethereum/go-ethereum/params"
)

// TestMordorHasNoDifficultyBomb checks that calcBombComponentETC returns 0 at every
// height on Mordor, which disposed of the difficulty bomb from genesis (ECIP1041Block=0).
func TestMordorHasNoDifficultyBomb(t *testing.T) {
	for _, parent := range []int64{0, 99_999, 199_999, 200_000, 300_000, 1_000_000, 5_000_000, 10_000_000} {
		if got := calcBombComponentETC(params.MordorChainConfig, big.NewInt(parent)); got.Sign() != 0 {
			t.Errorf("Mordor bomb component at parent %d: got %v, want 0", parent, got)
		}
	}
}

// TestClassicDifficultyBombDisposal is the control: Classic keeps the bomb until
// ECIP-1041 disposal at 5,900,000, then drops to 0.
func TestClassicDifficultyBombDisposal(t *testing.T) {
	// During the DieHard pause / resume window the bomb is present (non-zero).
	if got := calcBombComponentETC(params.ClassicChainConfig, big.NewInt(4_000_000)); got.Sign() == 0 {
		t.Error("Classic bomb before ECIP-1041 disposal should be non-zero")
	}
	// From ECIP-1041 (5,900,000) onward it is fully removed.
	if got := calcBombComponentETC(params.ClassicChainConfig, big.NewInt(6_000_000)); got.Sign() != 0 {
		t.Errorf("Classic bomb after ECIP-1041 disposal: got %v, want 0", got)
	}
}
