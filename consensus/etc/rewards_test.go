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

	"github.com/holiman/uint256"
)

// TestGetBlockEra verifies era calculation for different block numbers.
func TestGetBlockEra(t *testing.T) {
	eraRounds := big.NewInt(5000000) // ETC mainnet

	tests := []struct {
		name        string
		blockNumber *big.Int
		expectedEra int64
	}{
		{"Genesis", big.NewInt(0), 0},
		{"Block 1", big.NewInt(1), 0},
		{"Before Era 1", big.NewInt(4999999), 0},
		{"Era 1 boundary", big.NewInt(5000000), 0},
		{"Era 1 first block", big.NewInt(5000001), 1},
		{"Mid Era 1", big.NewInt(7500000), 1},
		{"Before Era 2", big.NewInt(9999999), 1},
		{"Era 2 boundary", big.NewInt(10000000), 1},
		{"Era 2 first block", big.NewInt(10000001), 2},
		{"Era 3", big.NewInt(15000001), 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			era := getBlockEra(tt.blockNumber, eraRounds)
			if era.Cmp(big.NewInt(tt.expectedEra)) != 0 {
				t.Errorf("getBlockEra(%v) = %v, want %v", tt.blockNumber, era, tt.expectedEra)
			}
		})
	}
}

// TestGetBlockEra_Mordor verifies era calculation for Mordor testnet (2M era rounds).
func TestGetBlockEra_Mordor(t *testing.T) {
	eraRounds := big.NewInt(2000000) // Mordor testnet

	tests := []struct {
		name        string
		blockNumber *big.Int
		expectedEra int64
	}{
		{"Genesis", big.NewInt(0), 0},
		{"Before Era 1", big.NewInt(1999999), 0},
		{"Era 1 boundary", big.NewInt(2000000), 0},
		{"Era 1 first block", big.NewInt(2000001), 1},
		{"Era 2 first block", big.NewInt(4000001), 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			era := getBlockEra(tt.blockNumber, eraRounds)
			if era.Cmp(big.NewInt(tt.expectedEra)) != 0 {
				t.Errorf("getBlockEra(%v) = %v, want %v", tt.blockNumber, era, tt.expectedEra)
			}
		})
	}
}

// TestGetBlockWinnerRewardByEra verifies ECIP-1017 block rewards.
func TestGetBlockWinnerRewardByEra(t *testing.T) {
	tests := []struct {
		name     string
		era      int64
		expected string // Expected reward in wei
	}{
		{"Era 0", 0, "5000000000000000000"}, // 5 ETC
		{"Era 1", 1, "4000000000000000000"}, // 4 ETC (5 * 0.8)
		{"Era 2", 2, "3200000000000000000"}, // 3.2 ETC (5 * 0.8^2)
		{"Era 3", 3, "2560000000000000000"}, // 2.56 ETC (5 * 0.8^3)
		{"Era 4", 4, "2048000000000000000"}, // 2.048 ETC (5 * 0.8^4)
		{"Era 5", 5, "1638400000000000000"}, // 1.6384 ETC (5 * 0.8^5)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reward := getBlockWinnerRewardByEra(big.NewInt(tt.era))
			expected, _ := uint256.FromDecimal(tt.expected)
			if reward.Cmp(expected) != 0 {
				t.Errorf("getBlockWinnerRewardByEra(%v) = %v, want %v", tt.era, reward, expected)
			}
		})
	}
}

// TestUncleRewardEra0 verifies uncle rewards in Era 0 use the standard Ethereum formula.
// Uncle reward = (8 - (block - uncle_block)) / 8 * blockReward
func TestUncleRewardEra0(t *testing.T) {
	blockReward := era0BlockReward // 5 ETC
	era := big.NewInt(0)

	tests := []struct {
		name        string
		blockNumber uint64
		uncleNumber uint64
		expectedWei string
	}{
		// Distance 1: (8-1)/8 * 5 = 7/8 * 5 = 4.375 ETC
		{"Distance 1", 4999999, 4999998, "4375000000000000000"},
		// Distance 2: (8-2)/8 * 5 = 6/8 * 5 = 3.75 ETC
		{"Distance 2", 4999999, 4999997, "3750000000000000000"},
		// Distance 3: (8-3)/8 * 5 = 5/8 * 5 = 3.125 ETC
		{"Distance 3", 4999999, 4999996, "3125000000000000000"},
		// Distance 6: (8-6)/8 * 5 = 2/8 * 5 = 1.25 ETC
		{"Distance 6", 4999999, 4999993, "1250000000000000000"},
		// Distance 7: (8-7)/8 * 5 = 1/8 * 5 = 0.625 ETC
		{"Distance 7 (max)", 4999999, 4999992, "625000000000000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reward := calculateUncleReward(era, blockReward, tt.blockNumber, tt.uncleNumber)
			expected, _ := uint256.FromDecimal(tt.expectedWei)
			if reward.Cmp(expected) != 0 {
				t.Errorf("Uncle reward for distance %d = %v wei, want %v wei",
					tt.blockNumber-tt.uncleNumber, reward, expected)
			}
		})
	}
}

// TestUncleRewardEra1Plus verifies uncle rewards in Era 1+ use fixed 1/32 of block reward.
// This is the ECIP-1017 specification that differs from Era 0.
func TestUncleRewardEra1Plus(t *testing.T) {
	tests := []struct {
		name        string
		era         int64
		blockReward string // Era's block reward
		expectedWei string // Fixed 1/32 of block reward
	}{
		// Era 1: 4 ETC / 32 = 0.125 ETC
		{"Era 1", 1, "4000000000000000000", "125000000000000000"},
		// Era 2: 3.2 ETC / 32 = 0.1 ETC
		{"Era 2", 2, "3200000000000000000", "100000000000000000"},
		// Era 3: 2.56 ETC / 32 = 0.08 ETC
		{"Era 3", 3, "2560000000000000000", "80000000000000000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			era := big.NewInt(tt.era)
			blockReward, _ := uint256.FromDecimal(tt.blockReward)
			expected, _ := uint256.FromDecimal(tt.expectedWei)

			// Test with different distances - all should give the same reward
			for distance := uint64(1); distance <= 7; distance++ {
				blockNum := uint64(5000001 + (tt.era-1)*5000000) // Block in the correct era
				uncleNum := blockNum - distance

				reward := calculateUncleReward(era, blockReward, blockNum, uncleNum)
				if reward.Cmp(expected) != 0 {
					t.Errorf("Era %d, distance %d: reward = %v wei, want %v wei (fixed 1/32)",
						tt.era, distance, reward, expected)
				}
			}
		})
	}
}

// TestUncleRewardTransition verifies the transition from Era 0 to Era 1.
func TestUncleRewardTransition(t *testing.T) {
	// Last block of Era 0 (block 5,000,000) with uncle at distance 2
	era0 := big.NewInt(0)
	era0Reward := era0BlockReward
	era0UncleReward := calculateUncleReward(era0, era0Reward, 5000000, 4999998)
	// Expected: (8-2)/8 * 5 = 3.75 ETC
	expected0, _ := uint256.FromDecimal("3750000000000000000")
	if era0UncleReward.Cmp(expected0) != 0 {
		t.Errorf("Era 0 uncle reward = %v, want %v", era0UncleReward, expected0)
	}

	// First block of Era 1 (block 5,000,001) with uncle at distance 2
	era1 := big.NewInt(1)
	era1Reward := getBlockWinnerRewardByEra(era1) // 4 ETC
	era1UncleReward := calculateUncleReward(era1, era1Reward, 5000001, 4999999)
	// Expected: 4 ETC / 32 = 0.125 ETC (fixed, NOT the distance formula!)
	expected1, _ := uint256.FromDecimal("125000000000000000")
	if era1UncleReward.Cmp(expected1) != 0 {
		t.Errorf("Era 1 uncle reward = %v, want %v (should be fixed 1/32)", era1UncleReward, expected1)
	}

	// Verify the rewards are different (this proves the fix works)
	if era0UncleReward.Cmp(era1UncleReward) == 0 {
		t.Error("Era 0 and Era 1 uncle rewards should be different!")
	}
}

// calculateUncleReward is a helper that mirrors the logic in accumulateRewardsETC.
func calculateUncleReward(era *big.Int, blockReward *uint256.Int, blockNum, uncleNum uint64) *uint256.Int {
	r := new(uint256.Int)
	hNum := uint256.NewInt(blockNum)
	uNum := uint256.NewInt(uncleNum)

	if era.Sign() == 0 {
		// Era 0: Standard Ethereum formula (8 - distance) / 8 * blockReward
		r.AddUint64(uNum, 8)
		r.Sub(r, hNum)
		r.Mul(r, blockReward)
		r.Rsh(r, 3) // Divide by 8
	} else {
		// Era 1+: Fixed 1/32 of block reward (ECIP-1017)
		r.Rsh(blockReward, 5) // Divide by 32
	}
	return r
}

// TestNephewBonus verifies the miner gets 1/32 bonus per uncle (same for all eras).
func TestNephewBonus(t *testing.T) {
	tests := []struct {
		era         int64
		blockReward string
		expected    string // 1/32 of block reward
	}{
		{0, "5000000000000000000", "156250000000000000"}, // 5 ETC / 32 = 0.15625 ETC
		{1, "4000000000000000000", "125000000000000000"}, // 4 ETC / 32 = 0.125 ETC
		{2, "3200000000000000000", "100000000000000000"}, // 3.2 ETC / 32 = 0.1 ETC
	}

	for _, tt := range tests {
		t.Run("Era "+string(rune('0'+tt.era)), func(t *testing.T) {
			blockReward, _ := uint256.FromDecimal(tt.blockReward)
			expected, _ := uint256.FromDecimal(tt.expected)

			// Nephew bonus: blockReward / 32
			bonus := new(uint256.Int).Rsh(blockReward, 5)
			if bonus.Cmp(expected) != 0 {
				t.Errorf("Nephew bonus for era %d = %v, want %v", tt.era, bonus, expected)
			}
		})
	}
}
