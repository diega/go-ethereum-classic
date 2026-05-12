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

// ETC-specific tests for the Mystique and Spiral EVM instruction sets that
// commit 90db69123 introduced. They guard the consensus-critical opcode
// surface that distinguishes ETC from ETH: ETC Mystique does NOT include
// EIP-3198 (BASEFEE), and ETC Spiral adopts EIP-3855 (PUSH0) on top of
// Mystique without adding BASEFEE.
//
// File suffix _etc_test.go isolates this from upstream rebases.

package vm

import (
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestMystiqueOmitsBaseFee verifies that the ETC Mystique instruction set
// leaves opcode 0x48 (BASEFEE) undefined. Mystique is "London minus
// EIP-1559" — including BASEFEE would silently return baseFee=0 (the value
// set in the EVM block context for Mystique), creating a consensus
// divergence from any ETC client that correctly leaves the opcode invalid.
func TestMystiqueOmitsBaseFee(t *testing.T) {
	op := mystiqueInstructionSet[BASEFEE]
	if op == nil {
		t.Fatal("mystiqueInstructionSet[BASEFEE] is nil; expected an opUndefined entry")
	}
	if !op.undefined {
		t.Fatal("BASEFEE must be undefined in ETC Mystique instruction set")
	}
	if op.HasCost() {
		t.Fatal("BASEFEE in ETC Mystique must not have a cost assigned")
	}
}

// TestMystiqueOmitsPush0 verifies that PUSH0 (0x5F) is absent from Mystique
// — PUSH0 is part of Shanghai/Spiral, not London. Catching its accidental
// inclusion in Mystique prevents accepting contracts on ETC pre-Spiral that
// would be invalid on a correctly-configured node.
func TestMystiqueOmitsPush0(t *testing.T) {
	op := mystiqueInstructionSet[PUSH0]
	if op == nil {
		t.Fatal("mystiqueInstructionSet[PUSH0] is nil; expected an opUndefined entry")
	}
	if !op.undefined {
		t.Fatal("PUSH0 must be undefined in ETC Mystique instruction set")
	}
}

// TestSpiralOmitsBaseFee verifies that ETC Spiral keeps BASEFEE undefined.
// Spiral = Mystique + PUSH0 + EIP-3860, deliberately NOT adopting EIP-3198.
func TestSpiralOmitsBaseFee(t *testing.T) {
	op := spiralInstructionSet[BASEFEE]
	if op == nil {
		t.Fatal("spiralInstructionSet[BASEFEE] is nil; expected an opUndefined entry")
	}
	if !op.undefined {
		t.Fatal("BASEFEE must remain undefined in ETC Spiral instruction set")
	}
}

// TestSpiralEnablesPush0 verifies that ETC Spiral adopts EIP-3855 (PUSH0).
// This is the partial-Shanghai semantics: PUSH0 yes, BASEFEE/blob ops no.
func TestSpiralEnablesPush0(t *testing.T) {
	op := spiralInstructionSet[PUSH0]
	if op == nil {
		t.Fatal("spiralInstructionSet[PUSH0] is nil; expected a defined entry")
	}
	if op.undefined {
		t.Fatal("PUSH0 must be defined in ETC Spiral instruction set")
	}
	if !op.HasCost() {
		t.Fatal("PUSH0 in ETC Spiral must have a non-zero cost")
	}
}

// TestLondonHasBaseFee is a control case: BASEFEE must be defined in the
// stock London instruction set. If this fails the comparison above loses
// meaning (we wouldn't be testing a real divergence).
func TestLondonHasBaseFee(t *testing.T) {
	op := londonInstructionSet[BASEFEE]
	if op == nil || op.undefined {
		t.Fatal("BASEFEE must be defined in upstream London instruction set; test fixture is broken")
	}
}

// TestMystiqueRetainsEIP3529 verifies that the EIP-3529 refund reduction
// (the part of London that Mystique adopted) is applied: SELFDESTRUCT no
// longer carries a refund. Without EIP-3529 ETC would be open to the same
// state-bloat refund abuse London fixed.
func TestMystiqueRetainsEIP3529(t *testing.T) {
	mystique := mystiqueInstructionSet[SELFDESTRUCT]
	berlin := berlinInstructionSet[SELFDESTRUCT]
	if mystique == nil || berlin == nil {
		t.Fatal("SELFDESTRUCT operation missing from Berlin or Mystique table")
	}
	// EIP-3529 swaps Berlin's gasSelfdestructEIP2929 for gasSelfdestructEIP3529.
	// The function pointers differ; comparing identities catches a regression
	// where enable3529 silently stops being applied to the Mystique table.
	if sameFn(mystique.dynamicGas, berlin.dynamicGas) {
		t.Fatal("Mystique SELFDESTRUCT dynamic gas matches Berlin; EIP-3529 not applied")
	}
}

// sameFn reports whether two gas functions point at the same underlying
// implementation. Go forbids direct == on funcs, so we compare their
// reflect-resolved code pointers.
func sameFn(a, b gasFunc) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// TestLookupInstructionSetMystique verifies that LookupInstructionSet picks
// the Mystique table for an ETC chain past Mystique but before Spiral.
// Without an explicit IsMystique case the switch would fall through to
// IsLondon and return a table containing BASEFEE — a silent divergence
// from the EVM execution path, which correctly selects Mystique.
func TestLookupInstructionSetMystique(t *testing.T) {
	postMystique := new(big.Int).Add(params.ClassicChainConfig.LondonBlock, big.NewInt(1))
	rules := params.ClassicChainConfig.Rules(postMystique, false, 0)

	table, err := LookupInstructionSet(rules)
	if err != nil {
		t.Fatalf("LookupInstructionSet: %v", err)
	}
	if op := table[BASEFEE]; op == nil || !op.undefined {
		t.Fatal("ETC Mystique lookup returned a table where BASEFEE is defined")
	}
	if op := table[PUSH0]; op == nil || !op.undefined {
		t.Fatal("ETC Mystique lookup returned a table where PUSH0 is defined")
	}
}

// TestLookupInstructionSetSpiral verifies the Spiral case: PUSH0 defined,
// BASEFEE still undefined.
func TestLookupInstructionSetSpiral(t *testing.T) {
	postSpiral := new(big.Int).Add(params.ClassicChainConfig.SpiralBlock, big.NewInt(1))
	rules := params.ClassicChainConfig.Rules(postSpiral, false, 0)

	table, err := LookupInstructionSet(rules)
	if err != nil {
		t.Fatalf("LookupInstructionSet: %v", err)
	}
	if op := table[BASEFEE]; op == nil || !op.undefined {
		t.Fatal("ETC Spiral lookup returned a table where BASEFEE is defined")
	}
	if op := table[PUSH0]; op == nil || op.undefined {
		t.Fatal("ETC Spiral lookup returned a table where PUSH0 is undefined")
	}
}
