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
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

// TestMESSPolynomialV tests the polynomial antigravity function.
func TestMESSPolynomialV(t *testing.T) {
	testCases := []struct {
		x        int64
		expected int64 // expected minimum value (function is monotonically increasing until xcap)
	}{
		{0, 128},     // At x=0, should return base denominator (128)
		{100, 128},   // Small x, should be slightly above 128
		{1000, 128},  // Moderate x, should be higher
		{10000, 128}, // Large x, should be significantly higher
		{25132, 128}, // At xcap, should reach maximum
		{50000, 128}, // Beyond xcap, should cap at maximum
	}

	for _, tc := range testCases {
		result := messPolynomialV(big.NewInt(tc.x))
		if result.Cmp(big.NewInt(tc.expected)) < 0 {
			t.Errorf("messPolynomialV(%d) = %s, expected >= %d", tc.x, result.String(), tc.expected)
		}
	}

	// Test that function is monotonically increasing up to xcap
	prev := messPolynomialV(big.NewInt(0))
	for x := int64(100); x <= 25132; x += 500 {
		curr := messPolynomialV(big.NewInt(x))
		if curr.Cmp(prev) < 0 {
			t.Errorf("messPolynomialV is not monotonically increasing: f(%d) < f(%d)", x, x-500)
		}
		prev = curr
	}

	// Test that values beyond xcap equal the value at xcap
	atCap := messPolynomialV(big.NewInt(25132))
	beyondCap := messPolynomialV(big.NewInt(50000))
	if atCap.Cmp(beyondCap) != 0 {
		t.Errorf("messPolynomialV should cap at xcap: f(25132)=%s != f(50000)=%s", atCap.String(), beyondCap.String())
	}
}

// TestMESSPolynomialVSpecificValues tests specific values based on the ECIP-1100 spec.
func TestMESSPolynomialVSpecificValues(t *testing.T) {
	// At x=0: should be exactly CURVE_FUNCTION_DENOMINATOR = 128
	result := messPolynomialV(big.NewInt(0))
	if result.Cmp(big.NewInt(128)) != 0 {
		t.Errorf("messPolynomialV(0) = %s, expected 128", result.String())
	}

	// At xcap (25132): should be CURVE_FUNCTION_DENOMINATOR + height = 128 + 3840 = 3968
	// height = 128 * 15 * 2 = 3840
	result = messPolynomialV(big.NewInt(25132))
	expected := big.NewInt(128 + 3840)
	if result.Cmp(expected) != 0 {
		t.Errorf("messPolynomialV(25132) = %s, expected %s", result.String(), expected.String())
	}
}

// TestEcbp1100PolynomialV tests the polynomial function with block-based inputs.
// Adapted from core-geth TestEcbp1100PolynomialV.
func TestEcbp1100PolynomialV(t *testing.T) {
	cases := []struct {
		block, ag int64
	}{
		{100, 1},
		{300, 2},
		{500, 5},
		{1000, 16},
		{2000, 31},
		{10000, 31},
		{1e9, 31},
	}
	for i, c := range cases {
		// Convert blocks to seconds (13 seconds per block)
		y := messPolynomialV(big.NewInt(c.block * 13))
		y.Div(y, messCurveFunctionDenominator)
		if c.ag != y.Int64() {
			t.Fatalf("mismatch case %d: block=%d, expected ag=%d, got=%d", i, c.block, c.ag, y.Int64())
		}
	}
}

// mockTDFunc creates a mock TD function for testing.
func mockTDFunc(tds map[string]*big.Int) func(common.Hash, uint64) *big.Int {
	return func(hash common.Hash, number uint64) *big.Int {
		key := hash.Hex()[:10]
		if td, ok := tds[key]; ok {
			return td
		}
		return nil
	}
}

// TestECBP1100ReorgRejection tests that deep reorgs are rejected by MESS.
func TestECBP1100ReorgRejection(t *testing.T) {
	// Create mock headers
	// For a deep reorg to be rejected, we need:
	// proposed_TD * 128 < antigravity(time_delta) * local_TD
	//
	// At xcap (25132 seconds = ~7 hours), antigravity = 3968
	// So for rejection: proposed_subchain_TD * 128 < 3968 * local_subchain_TD
	// This means: proposed_subchain_TD < 31 * local_subchain_TD
	//
	// Let's use 20000 seconds (~5.5 hours) where antigravity is still significant
	commonAncestor := &types.Header{
		Number: big.NewInt(1000),
		Time:   1000000,
	}
	commonHash := commonAncestor.Hash()

	// Current chain head: 1500 blocks ahead, 20000 seconds later (~5.5 hours)
	current := &types.Header{
		Number: big.NewInt(2500),
		Time:   1020000, // 20000 seconds after common ancestor
	}
	currentHash := current.Hash()

	// Proposed chain: same height but with lower effective TD
	proposed := &types.Header{
		Number:     big.NewInt(2500),
		Time:       1020010,
		Difficulty: big.NewInt(500), // Low difficulty
		ParentHash: common.HexToHash("0xabcd"),
	}
	proposedParentHash := proposed.ParentHash

	// Mock TD values
	// Common ancestor TD: 10,000,000
	// Current TD: 25,000,000 (local_subchain_TD = 15,000,000)
	// Proposed parent TD: 10,000,100 (proposed_subchain_TD = 600 including difficulty)
	//
	// Check: 600 * 128 = 76,800
	// antigravity(20000) * 15,000,000 should be much higher
	tds := map[string]*big.Int{
		commonHash.Hex()[:10]:         big.NewInt(10000000),
		currentHash.Hex()[:10]:        big.NewInt(25000000),
		proposedParentHash.Hex()[:10]: big.NewInt(10000100), // Very low proposed TD
	}

	err := ecbp1100(commonAncestor, current, proposed, mockTDFunc(tds))

	// Should be rejected because proposed TD is not high enough to overcome antigravity
	if err == nil {
		t.Error("Expected reorg to be rejected by MESS, but it was accepted")
	}
	if !errors.Is(err, errReorgFinality) {
		t.Errorf("Expected errReorgFinality, got: %v", err)
	}
}

// TestECBP1100ReorgAcceptance tests that legitimate reorgs are accepted.
func TestECBP1100ReorgAcceptance(t *testing.T) {
	// Create mock headers
	commonAncestor := &types.Header{
		Number: big.NewInt(1000),
		Time:   1000000,
	}
	commonHash := commonAncestor.Hash()

	// Current chain head: only 2 blocks ahead, 26 seconds later (very recent)
	current := &types.Header{
		Number: big.NewInt(1002),
		Time:   1000026, // 26 seconds after common ancestor (2 blocks * 13 seconds)
	}
	currentHash := current.Hash()

	// Proposed chain: same height with higher TD
	proposed := &types.Header{
		Number:     big.NewInt(1002),
		Time:       1000026,
		Difficulty: big.NewInt(2000000),
		ParentHash: common.HexToHash("0xabcd"),
	}
	proposedParentHash := proposed.ParentHash

	// Mock TD values
	// For a short reorg (26 seconds), antigravity is ~128 (base value)
	// So proposed_TD * 128 needs to be >= 128 * local_TD
	// This means proposed_TD >= local_TD
	tds := map[string]*big.Int{
		commonHash.Hex()[:10]:         big.NewInt(1000000),
		currentHash.Hex()[:10]:        big.NewInt(1002000),
		proposedParentHash.Hex()[:10]: big.NewInt(1004000), // Proposed has higher TD
	}

	err := ecbp1100(commonAncestor, current, proposed, mockTDFunc(tds))

	// Should be accepted because proposed TD is higher and reorg is recent
	if err != nil {
		t.Errorf("Expected reorg to be accepted, but got error: %v", err)
	}
}

// TestECBP1100ShortReorg tests that very short reorgs are always accepted.
func TestECBP1100ShortReorg(t *testing.T) {
	// Create mock headers for a 1-block reorg
	commonAncestor := &types.Header{
		Number: big.NewInt(1000),
		Time:   1000000,
	}
	commonHash := commonAncestor.Hash()

	// Current: 1 block ahead
	current := &types.Header{
		Number: big.NewInt(1001),
		Time:   1000013, // 13 seconds (1 block)
	}
	currentHash := current.Hash()

	// Proposed: same height, slightly higher TD
	proposed := &types.Header{
		Number:     big.NewInt(1001),
		Time:       1000013,
		Difficulty: big.NewInt(1000001),
		ParentHash: common.HexToHash("0xabcd"),
	}
	proposedParentHash := proposed.ParentHash

	// Equal subchain TDs should pass for short timeframes (antigravity ≈ 128)
	tds := map[string]*big.Int{
		commonHash.Hex()[:10]:         big.NewInt(1000000),
		currentHash.Hex()[:10]:        big.NewInt(1001000),
		proposedParentHash.Hex()[:10]: big.NewInt(1001001), // Slightly higher
	}

	err := ecbp1100(commonAncestor, current, proposed, mockTDFunc(tds))

	if err != nil {
		t.Errorf("Expected short reorg to be accepted, but got error: %v", err)
	}
}

// messTestGenesis returns a genesis configuration suitable for MESS tests.
// Uses a timestamp far enough in the past to allow generating many blocks without hitting "block in the future".
func messTestGenesis() *Genesis {
	// Create a config with ECBP1100 enabled from block 11
	config := &params.ChainConfig{
		ChainID:             big.NewInt(61), // ETC mainnet chain ID for IsClassic()
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
		Ethash:              new(params.EthashConfig),
		// MESS/ECBP1100 activation
		ECBP1100Transition: big.NewInt(11),
	}
	// Use a timestamp far enough in the past (like core-geth's 1598650845 = Aug 2020)
	// This allows generating thousands of blocks (~10 sec each) without hitting "block in the future"
	return &Genesis{
		Config:     config,
		Timestamp:  1598650845,                 // Aug 28, 2020 - same as core-geth MessNet
		Difficulty: big.NewInt(37103392657464), // High difficulty like core-geth
		GasLimit:   10485760,
		Alloc: map[common.Address]types.Account{
			common.BytesToAddress([]byte{1}): {Balance: big.NewInt(1)}, // ECRecover
			common.BytesToAddress([]byte{2}): {Balance: big.NewInt(1)}, // SHA256
			common.BytesToAddress([]byte{3}): {Balance: big.NewInt(1)}, // RIPEMD
			common.BytesToAddress([]byte{4}): {Balance: big.NewInt(1)}, // Identity
			common.BytesToAddress([]byte{5}): {Balance: big.NewInt(1)}, // ModExp
			common.BytesToAddress([]byte{6}): {Balance: big.NewInt(1)}, // ECAdd
			common.BytesToAddress([]byte{7}): {Balance: big.NewInt(1)}, // ECScalarMul
			common.BytesToAddress([]byte{8}): {Balance: big.NewInt(1)}, // ECPairing
		},
	}
}

// runMESSTest2 runs a MESS test with the given parameters.
// Adapted from core-geth runMESSTest2.
func runMESSTest2(t *testing.T, enableMess bool, easyL, hardL, caN int, easyT, hardT int64) (hardHead bool, err error, hard, easy []*types.Block) {
	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	genesis := messTestGenesis()
	genesisB := genesis.MustCommit(db, triedb.NewDatabase(db, nil))

	chain, err := NewBlockChain(db, genesis, engine, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	// Enable or disable MESS
	chain.EnableMESS(enableMess, "test")

	easy, _ = GenerateChain(genesis.Config, genesisB, engine, db, easyL, func(i int, b *BlockGen) {
		b.SetNonce(types.EncodeNonce(uint64(rand.Int63n(1 << 62))))
		b.OffsetTime(easyT)
	})
	commonAncestor := easy[caN-1]
	hard, _ = GenerateChain(genesis.Config, commonAncestor, engine, db, hardL, func(i int, b *BlockGen) {
		b.SetNonce(types.EncodeNonce(uint64(rand.Int63n(1 << 62))))
		b.OffsetTime(hardT)
	})

	if _, err := chain.InsertChain(easy); err != nil {
		t.Fatal(err)
	}
	_, err = chain.InsertChain(hard)
	if err != nil {
		t.Logf("insert hard chain error = %v", err)
	}
	hardHead = chain.CurrentBlock().Hash() == hard[len(hard)-1].Hash()
	return
}

// TestBlockChain_MESS_ECBP1100_Scenarios tests various reorg scenarios with MESS enabled.
// Adapted from core-geth TestBlockChain_AF_ECBP1100_2.
func TestBlockChain_MESS_ECBP1100_Scenarios(t *testing.T) {
	offsetGreaterDifficulty := int64(-2) // 1..8 = -9..-2
	offsetSameDifficulty := int64(0)     // 9..17 = -1..8
	offsetWorseDifficulty := int64(8)    // 18..

	cases := []struct {
		easyLen, hardLen, commonAncestorN int
		easyOffset, hardOffset            int64
		hardGetsHead, accepted            bool
	}{
		// NOTE: Random coin tosses involved for equivalent difficulty.
		// Short trials for those are skipped.

		{
			1000, 30, 970,
			0, offsetSameDifficulty, // same difficulty
			false, true,
		},

		{
			1000, 1, 999,
			0, offsetWorseDifficulty, // worse! difficulty
			false, true,
		},
		{
			1000, 1, 999,
			0, offsetGreaterDifficulty, // better difficulty
			true, true,
		},
		{
			1000, 5, 995,
			0, offsetGreaterDifficulty,
			true, true,
		},
		{
			1000, 25, 975,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 30, 970,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 50, 950,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 50, 950,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 1000, 900,
			0, offsetGreaterDifficulty,
			true, true,
		},
		{
			1000, 2000, 800,
			0, offsetGreaterDifficulty,
			true, true,
		},
		{
			1000, 2000, 700,
			0, offsetGreaterDifficulty,
			true, true,
		},
		{
			1000, 2000, 700,
			0, offsetGreaterDifficulty,
			true, true,
		},
		{
			1000, 999, 1,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 999, 1,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 500, 500,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 500, 500,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 300, 700,
			0, offsetGreaterDifficulty,
			false, true,
		},
		{
			1000, 600, 700,
			0, offsetGreaterDifficulty,
			true, true,
		},
	}

	for i, c := range cases {
		hardHead, err, hard, easy := runMESSTest2(t, true, c.easyLen, c.hardLen, c.commonAncestorN, c.easyOffset, c.hardOffset)

		ee, hh := easy[len(easy)-1], hard[len(hard)-1]
		rat, _ := new(big.Float).Quo(
			new(big.Float).SetInt(hh.Difficulty()),
			new(big.Float).SetInt(ee.Difficulty()),
		).Float64()

		logf := fmt.Sprintf("case=%d [easy=%d hard=%d ca=%d eo=%d ho=%d] drat=%0.6f span=%v hardHead(w|g)=%v|%v err=%v",
			i,
			c.easyLen, c.hardLen, c.commonAncestorN, c.easyOffset, c.hardOffset,
			rat,
			common.PrettyDuration(time.Second*time.Duration(10*(c.easyLen-c.commonAncestorN))),
			c.hardGetsHead, hardHead, err)

		if (err != nil && c.accepted) || (err == nil && !c.accepted) || (hardHead != c.hardGetsHead) {
			t.Error("FAIL", logf)
		} else {
			t.Log("PASS", logf)
		}
	}
}

// TestMESSKnownBlock tests that MESS functionality works for chain re-insertions.
// Chain re-insertions use BlockChain.writeKnownBlockAsHead, where first-pass insertions
// will hit writeBlockWithState.
// MESS needs to be implemented at both sites to prevent re-proposed chains from sidestepping
// the MESS criteria.
// Adapted from core-geth TestAFKnownBlock.
func TestMESSKnownBlock(t *testing.T) {
	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	genesis := messTestGenesis()
	genesisB := genesis.MustCommit(db, triedb.NewDatabase(db, nil))

	chain, err := NewBlockChain(db, genesis, engine, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()
	chain.EnableMESS(true, "test")

	easy, _ := GenerateChain(genesis.Config, genesisB, engine, db, 1000, func(i int, gen *BlockGen) {
		gen.OffsetTime(0)
	})
	if _, err := chain.InsertChain(easy); err != nil {
		t.Fatal(err)
	}
	// Use the same pattern as core-geth TestAFKnownBlock:
	// easy[easyN-300] = easy[700] = block #701 (since easyN=1000)
	// This matches core-geth's approach exactly
	commonAncestor := easy[700] // Block #701, same as core-geth's easy[easyN-300]
	hard, _ := GenerateChain(genesis.Config, commonAncestor, engine, db, 300, func(i int, gen *BlockGen) {
		// Offset -7 gives extra difficulty but MESS should still reject.
		// This matches core-geth TestAFKnownBlock exactly.
		gen.OffsetTime(-7)
	})
	// First insertion: writeBlockWithState path
	if _, err := chain.InsertChain(hard); err != nil {
		t.Error("hard 1 not inserted (should be side)")
	}
	// Second insertion: writeKnownBlockAsHead path
	if _, err := chain.InsertChain(hard); err != nil {
		t.Error("hard 2 inserted (will have 'ignored' known blocks, and never tried a reorg)")
	}
	hardHeadHash := hard[len(hard)-1].Hash()
	if chain.CurrentBlock().Hash() == hardHeadHash {
		t.Fatal("hard block got chain head, should be side")
	}
	if h := chain.GetHeaderByHash(hardHeadHash); h == nil {
		t.Fatal("missing hard block (should be imported as side, but still available)")
	}
}

// TestEcbp1100ConfigActivation tests the IsECBP1100 config method.
func TestEcbp1100ConfigActivation(t *testing.T) {
	tests := []struct {
		name       string
		transition *big.Int
		deactivate *big.Int
		block      *big.Int
		expected   bool
	}{
		{
			name:       "nil transition - not active",
			transition: nil,
			deactivate: nil,
			block:      big.NewInt(100),
			expected:   false,
		},
		{
			name:       "before activation",
			transition: big.NewInt(100),
			deactivate: nil,
			block:      big.NewInt(50),
			expected:   false,
		},
		{
			name:       "at activation",
			transition: big.NewInt(100),
			deactivate: nil,
			block:      big.NewInt(100),
			expected:   true,
		},
		{
			name:       "after activation, no deactivation",
			transition: big.NewInt(100),
			deactivate: nil,
			block:      big.NewInt(200),
			expected:   true,
		},
		{
			name:       "before deactivation",
			transition: big.NewInt(100),
			deactivate: big.NewInt(200),
			block:      big.NewInt(150),
			expected:   true,
		},
		{
			name:       "at deactivation",
			transition: big.NewInt(100),
			deactivate: big.NewInt(200),
			block:      big.NewInt(200),
			expected:   false,
		},
		{
			name:       "after deactivation",
			transition: big.NewInt(100),
			deactivate: big.NewInt(200),
			block:      big.NewInt(250),
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &params.ChainConfig{
				ChainID:                      big.NewInt(61),
				ECBP1100Transition:           tt.transition,
				ECBP1100DeactivateTransition: tt.deactivate,
			}
			result := config.IsECBP1100(tt.block)
			if result != tt.expected {
				t.Errorf("IsECBP1100(%v) = %v, expected %v", tt.block, result, tt.expected)
			}
		})
	}
}

// TestEcbp1100AGSinusoidalA tests the sinusoidal antigravity function.
// Adapted from core-geth TestEcbp1100AGSinusoidalA.
func TestEcbp1100AGSinusoidalA(t *testing.T) {
	cases := []struct {
		in, out float64
	}{
		{0, 1},
		{25132, 31},
	}
	tolerance := 0.0000001
	for i, c := range cases {
		if got := ecbp1100AGSinusoidalA(c.in); got < c.out-tolerance || got > c.out+tolerance {
			t.Fatalf("%d: in: %0.6f want: %0.6f got: %0.6f", i, c.in, c.out, got)
		}
	}
}

// Ensure unused imports don't cause errors
var _ = vm.Config{}
