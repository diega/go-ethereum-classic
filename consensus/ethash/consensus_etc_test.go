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

package ethash

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
)

// defaultEraLength is 5M blocks for ETC mainnet.
var defaultEraLength = big.NewInt(5_000_000)

// TestECIP1017GetBlockEra tests era calculation for various block numbers.
// Values verified against core-geth params/mutations/rewards_test.go.
func TestECIP1017GetBlockEra(t *testing.T) {
	tests := []struct {
		blockNum    int64
		eraLength   int64
		expectedEra int64
	}{
		// Era 0 boundaries (5M era length)
		{0, 5_000_000, 0},
		{1, 5_000_000, 0},
		{1_914_999, 5_000_000, 0},
		{1_915_000, 5_000_000, 0},
		{4_999_999, 5_000_000, 0},
		{5_000_000, 5_000_000, 0},
		// Era 1
		{5_000_001, 5_000_000, 1},
		{9_999_999, 5_000_000, 1},
		{10_000_000, 5_000_000, 1},
		// Era 2
		{10_000_001, 5_000_000, 2},
		{14_999_999, 5_000_000, 2},
		{15_000_000, 5_000_000, 2},
		// Era 3
		{15_000_001, 5_000_000, 3},
		// Large values
		{24_185_528, 5_000_000, 4},
		{100_000_001, 5_000_000, 20},
		{123_456_789, 5_000_000, 24},
		// Custom era length of 2 (from core-geth test)
		{0, 2, 0},
		{1, 2, 0},
		{2, 2, 0},
		{3, 2, 1},
		{4, 2, 1},
		{5, 2, 2},
		{6, 2, 2},
		{7, 2, 3},
		{8, 2, 3},
		{9, 2, 4},
		{10, 2, 4},
		{11, 2, 5},
		{12, 2, 5},
		// Negative block number
		{-50000, 5_000_000, 0},
	}

	for _, tt := range tests {
		got := getBlockEra(big.NewInt(tt.blockNum), big.NewInt(tt.eraLength))
		if got.Cmp(big.NewInt(tt.expectedEra)) != 0 {
			t.Errorf("getBlockEra(%d, %d) = %d, want %d", tt.blockNum, tt.eraLength, got, tt.expectedEra)
		}
	}
}

// TestECIP1017GetBlockWinnerRewardByEra tests the disinflation reward schedule.
// Values verified against core-geth params/mutations/rewards_test.go.
func TestECIP1017GetBlockWinnerRewardByEra(t *testing.T) {
	baseReward := uint256.NewInt(5e+18) // 5 ETC

	tests := []struct {
		era            int64
		expectedReward uint64
	}{
		{0, 5_000_000_000_000_000_000}, // 5.0 ETC
		{1, 4_000_000_000_000_000_000}, // 4.0 ETC
		{2, 3_200_000_000_000_000_000}, // 3.2 ETC
		{3, 2_560_000_000_000_000_000}, // 2.56 ETC
		{4, 2_048_000_000_000_000_000}, // 2.048 ETC
		{5, 1_638_400_000_000_000_000}, // 1.6384 ETC
	}

	for _, tt := range tests {
		got := getBlockWinnerRewardByEra(big.NewInt(tt.era), baseReward)
		want := uint256.NewInt(tt.expectedReward)
		if got.Cmp(want) != 0 {
			t.Errorf("getBlockWinnerRewardByEra(era=%d) = %v, want %v", tt.era, got, want)
		}
	}
}

// TestECIP1017GetBlockWinnerRewardByBlock tests reward calculation using block numbers
// to derive the era first (matching core-geth TestGetBlockWinnerRewardByEra).
func TestECIP1017GetBlockWinnerRewardByBlock(t *testing.T) {
	baseReward := uint256.NewInt(5e+18)

	tests := []struct {
		blockNum       int64
		expectedReward uint64
	}{
		{0, 5e+18},
		{1, 5e+18},
		{4_999_999, 5e+18},
		{5_000_000, 5e+18},
		{5_000_001, 4e+18},
		{9_999_999, 4e+18},
		{10_000_000, 4e+18},
		{10_000_001, 3.2e+18},
		{14_999_999, 3.2e+18},
		{15_000_000, 3.2e+18},
		{15_000_001, 2.56e+18},
	}

	for _, tt := range tests {
		era := getBlockEra(big.NewInt(tt.blockNum), defaultEraLength)
		got := getBlockWinnerRewardByEra(era, baseReward)
		want := uint256.NewInt(tt.expectedReward)
		if got.Cmp(want) != 0 {
			t.Errorf("block %d (era %v): got reward %v, want %v", tt.blockNum, era, got, want)
		}
	}
}

// TestECIP1017GetBlockUncleRewardByEra tests uncle reward calculation.
// Era 0: standard sliding scale (uncle_number + 8 - header_number) / 8 * blockReward.
// Era 1+: flat 1/32 of era-adjusted winner reward.
func TestECIP1017GetBlockUncleRewardByEra(t *testing.T) {
	baseReward := uint256.NewInt(5e+18)

	// Era 0: uncle at depth 1 (uncle.Number = header.Number - 1)
	// reward = (uncle + 8 - header) / 8 * 5e18 = 7/8 * 5e18 = 4.375e18
	header := &types.Header{Number: big.NewInt(2_534_999)}
	uncle := &types.Header{Number: big.NewInt(2_534_998)}
	era := getBlockEra(header.Number, defaultEraLength)
	got := getBlockUncleRewardByEra(era, header, uncle, baseReward)
	// (2534998 + 8 - 2534999) = 7; 7 * 5e18 / 8 = 4.375e18
	want := uint256.NewInt(4_375_000_000_000_000_000)
	if got.Cmp(want) != 0 {
		t.Errorf("era 0 uncle reward: got %v, want %v", got, want)
	}

	// Era 1: uncle gets 1/32 of era-1 winner reward (4e18)
	header2 := &types.Header{Number: big.NewInt(5_000_001)}
	uncle2 := &types.Header{Number: big.NewInt(5_000_000)}
	era2 := getBlockEra(header2.Number, defaultEraLength)
	got2 := getBlockUncleRewardByEra(era2, header2, uncle2, baseReward)
	// 4e18 / 32 = 125000000000000000
	want2 := uint256.NewInt(125_000_000_000_000_000)
	if got2.Cmp(want2) != 0 {
		t.Errorf("era 1 uncle reward: got %v, want %v", got2, want2)
	}

	// Era 2: uncle gets 1/32 of era-2 winner reward (3.2e18)
	header3 := &types.Header{Number: big.NewInt(10_000_001)}
	uncle3 := &types.Header{Number: big.NewInt(10_000_000)}
	era3 := getBlockEra(header3.Number, defaultEraLength)
	got3 := getBlockUncleRewardByEra(era3, header3, uncle3, baseReward)
	// 3.2e18 / 32 = 100000000000000000
	want3 := uint256.NewInt(100_000_000_000_000_000)
	if got3.Cmp(want3) != 0 {
		t.Errorf("era 2 uncle reward: got %v, want %v", got3, want3)
	}
}

// mockStateDB is a minimal implementation of vm.StateDB for testing accumulateRewards.
type mockStateDB struct {
	balances map[common.Address]*uint256.Int
}

func newMockStateDB() *mockStateDB {
	return &mockStateDB{balances: make(map[common.Address]*uint256.Int)}
}

func (m *mockStateDB) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	if _, ok := m.balances[addr]; !ok {
		m.balances[addr] = new(uint256.Int)
	}
	m.balances[addr].Add(m.balances[addr], amount)
	return *m.balances[addr]
}

func (m *mockStateDB) GetBalance(addr common.Address) *uint256.Int {
	if b, ok := m.balances[addr]; ok {
		return new(uint256.Int).Set(b)
	}
	return uint256.NewInt(0)
}

// Implement the rest of vm.StateDB interface as no-ops for compilation.
func (m *mockStateDB) CreateAccount(common.Address)  {}
func (m *mockStateDB) CreateContract(common.Address) {}
func (m *mockStateDB) SubBalance(common.Address, *uint256.Int, tracing.BalanceChangeReason) uint256.Int {
	return uint256.Int{}
}
func (m *mockStateDB) SetNonce(common.Address, uint64, tracing.NonceChangeReason)      {}
func (m *mockStateDB) GetNonce(common.Address) uint64                                  { return 0 }
func (m *mockStateDB) GetCodeHash(common.Address) common.Hash                          { return common.Hash{} }
func (m *mockStateDB) GetCode(common.Address) []byte                                   { return nil }
func (m *mockStateDB) SetCode(common.Address, []byte, tracing.CodeChangeReason) []byte { return nil }
func (m *mockStateDB) GetCodeSize(common.Address) int                                  { return 0 }
func (m *mockStateDB) AddRefund(uint64)                                                {}
func (m *mockStateDB) SubRefund(uint64)                                                {}
func (m *mockStateDB) GetRefund() uint64                                               { return 0 }
func (m *mockStateDB) GetStateAndCommittedState(common.Address, common.Hash) (common.Hash, common.Hash) {
	return common.Hash{}, common.Hash{}
}
func (m *mockStateDB) GetState(common.Address, common.Hash) common.Hash { return common.Hash{} }
func (m *mockStateDB) SetState(common.Address, common.Hash, common.Hash) common.Hash {
	return common.Hash{}
}
func (m *mockStateDB) GetStorageRoot(common.Address) common.Hash { return common.Hash{} }
func (m *mockStateDB) GetTransientState(common.Address, common.Hash) common.Hash {
	return common.Hash{}
}
func (m *mockStateDB) SetTransientState(common.Address, common.Hash, common.Hash) {}
func (m *mockStateDB) SelfDestruct(common.Address) uint256.Int                    { return uint256.Int{} }
func (m *mockStateDB) HasSelfDestructed(common.Address) bool                      { return false }
func (m *mockStateDB) SelfDestruct6780(common.Address) (uint256.Int, bool) {
	return uint256.Int{}, false
}
func (m *mockStateDB) Exist(common.Address) bool                                 { return false }
func (m *mockStateDB) Empty(common.Address) bool                                 { return true }
func (m *mockStateDB) AddressInAccessList(common.Address) bool                   { return false }
func (m *mockStateDB) SlotInAccessList(common.Address, common.Hash) (bool, bool) { return false, false }
func (m *mockStateDB) AddAddressToAccessList(common.Address)                     {}
func (m *mockStateDB) AddSlotToAccessList(common.Address, common.Hash)           {}
func (m *mockStateDB) RevertToSnapshot(int)                                      {}
func (m *mockStateDB) Snapshot() int                                             { return 0 }
func (m *mockStateDB) AddLog(*types.Log)                                         {}
func (m *mockStateDB) AddPreimage(common.Hash, []byte)                           {}
func (m *mockStateDB) PointCache() *utils.PointCache                             { return nil }
func (m *mockStateDB) Witness() *stateless.Witness                               { return nil }
func (m *mockStateDB) AccessEvents() *state.AccessEvents                         { return nil }
func (m *mockStateDB) Finalise(bool)                                             {}
func (m *mockStateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
}

// TestECIP1017AccumulateRewards tests the full accumulateRewards function
// with ETC config to verify correct balance after mining rewards.
func TestECIP1017AccumulateRewards(t *testing.T) {
	coinbase := common.HexToAddress("0x0000000000000000000000000000000000000001")

	tests := []struct {
		name           string
		blockNum       int64
		expectedReward *uint256.Int
	}{
		{"era0_block_1", 1, uint256.NewInt(5e+18)},
		{"era0_block_4999999", 4_999_999, uint256.NewInt(5e+18)},
		{"era0_block_5000000", 5_000_000, uint256.NewInt(5e+18)},
		{"era1_block_5000001", 5_000_001, uint256.NewInt(4e+18)},
		{"era1_block_9999999", 9_999_999, uint256.NewInt(4e+18)},
		{"era2_block_10000001", 10_000_001, uint256.NewInt(3.2e+18)},
		{"era3_block_15000001", 15_000_001, uint256.NewInt(2.56e+18)},
		{"era4_block_20000001", 20_000_001, uint256.NewInt(2.048e+18)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDB := newMockStateDB()
			header := &types.Header{
				Number:   big.NewInt(tt.blockNum),
				Coinbase: coinbase,
			}
			accumulateRewards(params.ClassicChainConfig, stateDB, header, nil)

			got := stateDB.GetBalance(coinbase)
			if got.Cmp(tt.expectedReward) != 0 {
				t.Errorf("block %d: balance = %v, want %v", tt.blockNum, got, tt.expectedReward)
			}
		})
	}
}

// TestECIP1017AccumulateRewardsWithUncles tests reward accumulation with uncle blocks.
func TestECIP1017AccumulateRewardsWithUncles(t *testing.T) {
	coinbase := common.HexToAddress("0x0000000000000000000000000000000000000001")
	uncleCoinbase := common.HexToAddress("0x0000000000000000000000000000000000000002")

	// Era 0, block 100, uncle at depth 1 (uncle.Number = 99)
	t.Run("era0_with_uncle", func(t *testing.T) {
		stateDB := newMockStateDB()
		header := &types.Header{
			Number:   big.NewInt(100),
			Coinbase: coinbase,
		}
		uncles := []*types.Header{
			{Number: big.NewInt(99), Coinbase: uncleCoinbase},
		}
		accumulateRewards(params.ClassicChainConfig, stateDB, header, uncles)

		// Winner: 5e18 (block reward) + 5e18/32 (uncle inclusion) = 5.15625e18
		wantWinner := new(uint256.Int).Add(
			uint256.NewInt(5e+18),
			new(uint256.Int).Div(uint256.NewInt(5e+18), uint256.NewInt(32)),
		)
		gotWinner := stateDB.GetBalance(coinbase)
		if gotWinner.Cmp(wantWinner) != 0 {
			t.Errorf("winner balance = %v, want %v", gotWinner, wantWinner)
		}

		// Uncle at depth 1: (99 + 8 - 100) / 8 * 5e18 = 7/8 * 5e18 = 4.375e18
		wantUncle := uint256.NewInt(4_375_000_000_000_000_000)
		gotUncle := stateDB.GetBalance(uncleCoinbase)
		if gotUncle.Cmp(wantUncle) != 0 {
			t.Errorf("uncle balance = %v, want %v", gotUncle, wantUncle)
		}
	})

	// Era 1 (block 5000001), uncle at depth 1
	t.Run("era1_with_uncle", func(t *testing.T) {
		stateDB := newMockStateDB()
		header := &types.Header{
			Number:   big.NewInt(5_000_001),
			Coinbase: coinbase,
		}
		uncles := []*types.Header{
			{Number: big.NewInt(5_000_000), Coinbase: uncleCoinbase},
		}
		accumulateRewards(params.ClassicChainConfig, stateDB, header, uncles)

		// Winner: 4e18 (era1 reward) + 4e18/32 (uncle inclusion) = 4.125e18
		wantWinner := new(uint256.Int).Add(
			uint256.NewInt(4e+18),
			new(uint256.Int).Div(uint256.NewInt(4e+18), uint256.NewInt(32)),
		)
		gotWinner := stateDB.GetBalance(coinbase)
		if gotWinner.Cmp(wantWinner) != 0 {
			t.Errorf("winner balance = %v, want %v", gotWinner, wantWinner)
		}

		// Uncle in era 1+: flat 1/32 of era-adjusted reward = 4e18/32 = 125000000000000000
		wantUncle := uint256.NewInt(125_000_000_000_000_000)
		gotUncle := stateDB.GetBalance(uncleCoinbase)
		if gotUncle.Cmp(wantUncle) != 0 {
			t.Errorf("uncle balance = %v, want %v", gotUncle, wantUncle)
		}
	})
}

// TestETHRewardScheduleUnchanged verifies that non-ETC chains (ETH config)
// still use the standard Frontier -> Byzantium -> Constantinople reward schedule.
func TestETHRewardScheduleUnchanged(t *testing.T) {
	coinbase := common.HexToAddress("0x0000000000000000000000000000000000000001")

	tests := []struct {
		name           string
		blockNum       int64
		expectedReward *uint256.Int
	}{
		// Pre-Byzantium: 5 ETH
		{"frontier", 1, uint256.NewInt(5e+18)},
		// Byzantium (block 4370000): 3 ETH
		{"byzantium", 4_370_001, uint256.NewInt(3e+18)},
		// Constantinople (block 7280000): 2 ETH
		{"constantinople", 7_280_001, uint256.NewInt(2e+18)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDB := newMockStateDB()
			header := &types.Header{
				Number:   big.NewInt(tt.blockNum),
				Coinbase: coinbase,
			}
			accumulateRewards(params.MainnetChainConfig, stateDB, header, nil)

			got := stateDB.GetBalance(coinbase)
			if got.Cmp(tt.expectedReward) != 0 {
				t.Errorf("block %d: balance = %v, want %v", tt.blockNum, got, tt.expectedReward)
			}
		})
	}
}
