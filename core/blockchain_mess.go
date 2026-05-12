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
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// errReorgFinality represents an error caused by MESS artificial finality.
var errReorgFinality = errors.New("MESS: finality-enforced reorg rejected")

// MESS (ECBP-1100) polynomial constants
// Reference: https://github.com/ethereumclassic/ECIPs/blob/master/_specs/ecip-1100.md
var (
	big2 = big.NewInt(2)
	big3 = big.NewInt(3)

	// messCurveFunctionDenominator = 128
	messCurveFunctionDenominator = big.NewInt(128)

	// messXCap = 25132 (floor(8000*pi))
	messXCap = big.NewInt(25132)

	// messAmpl = 15
	messAmpl = big.NewInt(15)

	// messHeight = CURVE_FUNCTION_DENOMINATOR * (ampl * 2) = 128 * 30 = 3840
	messHeight = new(big.Int).Mul(new(big.Int).Mul(messCurveFunctionDenominator, messAmpl), big2)
)

// EnableMESS enables or disables MESS artificial finality for this blockchain.
// This is controlled dynamically by the sync process based on network conditions.
// The state is per-BlockChain (bc.messEnabled), so separate chains/tests in the same
// process don't affect each other. The method is idempotent.
func (bc *BlockChain) EnableMESS(enable bool, reason string) {
	var statusLog string
	if enable {
		statusLog = "enabled"
		bc.messEnabled.Store(1)
	} else {
		statusLog = "disabled"
		bc.messEnabled.Store(0)
	}
	log.Info("MESS artificial finality "+statusLog, "reason", reason)
}

// IsMESSEnabled returns the current MESS activation status for this blockchain.
// This status is independent of chain config activation - it reflects
// the dynamic runtime state controlled by the sync process.
func (bc *BlockChain) IsMESSEnabled() bool {
	return bc.messEnabled.Load() == 1
}

// ecbp1100 implements the "MESS" artificial finality mechanism.
// "Modified Exponential Subjective Scoring" is used to prefer known chain segments
// over later-to-come counterparts, especially proposed segments stretching far into the past.
//
// Algorithm: proposed_TD * 128 >= antigravity(time_delta) * local_TD
//
// Where antigravity is a polynomial function that increases with time, making it
// progressively harder for an attacker to reorganize older blocks.
func ecbp1100(commonAncestor, current, proposed *types.Header, getTDFunc func(common.Hash, uint64) *big.Int) error {
	// Get total difficulties
	commonAncestorTD := getTDFunc(commonAncestor.Hash(), commonAncestor.Number.Uint64())
	if commonAncestorTD == nil {
		return fmt.Errorf("MESS: cannot get TD for common ancestor block %d", commonAncestor.Number.Uint64())
	}

	proposedParentTD := getTDFunc(proposed.ParentHash, proposed.Number.Uint64()-1)
	if proposedParentTD == nil {
		return fmt.Errorf("MESS: cannot get TD for proposed parent block %d", proposed.Number.Uint64()-1)
	}
	proposedTD := new(big.Int).Add(proposed.Difficulty, proposedParentTD)

	localTD := getTDFunc(current.Hash(), current.Number.Uint64())
	if localTD == nil {
		return fmt.Errorf("MESS: cannot get TD for current block %d", current.Number.Uint64())
	}

	// Calculate subchain TDs (segment TDs from common ancestor)
	proposedSubchainTD := new(big.Int).Sub(proposedTD, commonAncestorTD)
	localSubchainTD := new(big.Int).Sub(localTD, commonAncestorTD)

	// Time delta from common ancestor to current head
	xBig := big.NewInt(int64(current.Time - commonAncestor.Time))

	// Calculate antigravity using polynomial function
	antigravity := messPolynomialV(xBig)

	// want = antigravity * localSubchainTD
	want := new(big.Int).Mul(antigravity, localSubchainTD)

	// got = proposedSubchainTD * CURVE_FUNCTION_DENOMINATOR
	got := new(big.Int).Mul(proposedSubchainTD, messCurveFunctionDenominator)

	// Reject if proposed chain doesn't have enough TD to overcome antigravity
	if got.Cmp(want) < 0 {
		// Calculate ratio for logging
		prettyRatio, _ := new(big.Float).Quo(
			new(big.Float).SetInt(got),
			new(big.Float).SetInt(want),
		).Float64()

		return fmt.Errorf(`%w: status=rejected age=%v current.span=%v proposed.span=%v tdr/gravity=%0.6f common.block=%d common.hash=%s current.block=%d current.hash=%s proposed.block=%d proposed.hash=%s`,
			errReorgFinality,
			common.PrettyAge(time.Unix(int64(commonAncestor.Time), 0)),
			common.PrettyDuration(time.Duration(current.Time-commonAncestor.Time)*time.Second),
			common.PrettyDuration(time.Duration(proposed.Time-commonAncestor.Time)*time.Second),
			prettyRatio,
			commonAncestor.Number.Uint64(), commonAncestor.Hash().Hex()[:10],
			current.Number.Uint64(), current.Hash().Hex()[:10],
			proposed.Number.Uint64(), proposed.Hash().Hex()[:10],
		)
	}
	return nil
}

// messPolynomialV is a cubic polynomial function that calculates the "antigravity" value.
// This function looks similar to a sinusoidal curve but uses integer arithmetic.
//
// The function:
//
//	CURVE_FUNCTION_DENOMINATOR + (3 * x**2 - 2 * x**3 // xcap) * height // xcap ** 2
//
// Where:
//   - xcap = 25132 (floor(8000*pi))
//   - height = 128 * 15 * 2 = 3840
//   - CURVE_FUNCTION_DENOMINATOR = 128
//
// Reference: https://github.com/ethereumclassic/ECIPs/issues/374#issuecomment-694156719
func messPolynomialV(x *big.Int) *big.Int {
	// Cap x to xcap if larger
	xA := new(big.Int).Set(x)
	if xA.Cmp(messXCap) > 0 {
		xA.Set(messXCap)
	}

	xB := new(big.Int).Set(x)
	if xB.Cmp(messXCap) > 0 {
		xB.Set(messXCap)
	}

	out := big.NewInt(0)

	// 3 * x**2
	xA.Exp(xA, big2, nil)
	xA.Mul(xA, big3)

	// 2 * x**3 // xcap
	xB.Exp(xB, big3, nil)
	xB.Mul(xB, big2)
	xB.Div(xB, messXCap)

	// (3 * x**2 - 2 * x**3 // xcap)
	out.Sub(xA, xB)

	// (3 * x**2 - 2 * x**3 // xcap) * height
	out.Mul(out, messHeight)

	// xcap ** 2
	xcap2 := new(big.Int).Exp(messXCap, big2, nil)

	// (3 * x**2 - 2 * x**3 // xcap) * height // xcap ** 2
	out.Div(out, xcap2)

	// CURVE_FUNCTION_DENOMINATOR + (3 * x**2 - 2 * x**3 // xcap) * height // xcap ** 2
	out.Add(out, messCurveFunctionDenominator)
	return out
}

// ecbp1100AGSinusoidalA is an alternative antigravity function using a sinusoidal curve.
// It provides a different growth characteristic for the antigravity value.
// Reference: https://github.com/ethereumclassic/ECIPs/issues/374
func ecbp1100AGSinusoidalA(x float64) float64 {
	ampl := float64(15)
	xcap := 25132.0
	if x > xcap {
		x = xcap
	}
	return 1 + ampl*(1-math.Cos(x*math.Pi/xcap))
}

// findCommonAncestor finds the common ancestor between two headers.
func (bc *BlockChain) findCommonAncestor(oldH, newH *types.Header) (*types.Header, error) {
	// Make copies to avoid modifying originals
	oldH = bc.GetHeader(oldH.Hash(), oldH.Number.Uint64())
	newH = bc.GetHeader(newH.Hash(), newH.Number.Uint64())

	if oldH == nil || newH == nil {
		return nil, fmt.Errorf("header not found")
	}

	// Reduce to same height
	for oldH.Number.Uint64() > newH.Number.Uint64() {
		oldH = bc.GetHeader(oldH.ParentHash, oldH.Number.Uint64()-1)
		if oldH == nil {
			return nil, fmt.Errorf("invalid old chain")
		}
	}
	for newH.Number.Uint64() > oldH.Number.Uint64() {
		newH = bc.GetHeader(newH.ParentHash, newH.Number.Uint64()-1)
		if newH == nil {
			return nil, fmt.Errorf("invalid new chain")
		}
	}

	// Find common ancestor
	for oldH.Hash() != newH.Hash() {
		oldH = bc.GetHeader(oldH.ParentHash, oldH.Number.Uint64()-1)
		newH = bc.GetHeader(newH.ParentHash, newH.Number.Uint64()-1)
		if oldH == nil || newH == nil {
			return nil, fmt.Errorf("no common ancestor found")
		}
	}

	return oldH, nil
}

// NewForkChoiceWithMESS creates a ForkChoice configured for ETC chains.
// It wires the MESS (ECBP-1100) artificial finality check as a reorg filter
// callback, so that MESS validation is evaluated per-block inside ReorgNeeded.
func NewForkChoiceWithMESS(bc *BlockChain, preserve func(header *types.Header) bool) *ForkChoice {
	f := NewForkChoice(bc, preserve)
	f.reorgFilter = func(current, extern *types.Header) error {
		if !bc.IsMESSEnabled() || !bc.chainConfig.IsECBP1100(current.Number) {
			return nil
		}
		commonAncestor, err := bc.findCommonAncestor(current, extern)
		if err != nil || commonAncestor == nil {
			// Fail closed: MESS is an artificial-finality guard, so a reorg we cannot
			// verify (no common ancestor, or a missing header) must be rejected, not
			// silently allowed. core-geth propagates this error in the equivalent path
			// (core/forkchoice.go); allowing the reorg here would bypass MESS exactly
			// in the anomalous case where it should act.
			if err == nil {
				err = errors.New("no common ancestor")
			}
			log.Warn("MESS: rejecting unverifiable reorg", "current", current.Number, "proposed", extern.Number, "err", err)
			return fmt.Errorf("MESS: cannot verify reorg: %w", err)
		}
		if err := ecbp1100(commonAncestor, current, extern, bc.GetTd); err != nil {
			log.Warn("MESS: reorg disallowed", "common", commonAncestor.Number, "current", current.Number, "proposed", extern.Number, "err", err)
			return err
		}
		return nil
	}
	return f
}
