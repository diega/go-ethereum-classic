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

package params

import (
	"math/big"
	"reflect"
	"strings"
	"testing"
)

// TestETCForkCompatibilityCoverage enforces the fork-ID ↔ compatibility invariant: every
// *big.Int field whose name ends in "Block" enters the fork ID via gatherForks' reflection,
// so CheckCompatible must reject moving it into the past. core-geth gets this symmetry for
// free from its reflective confp system; go-ethereum keeps checkCompatible enumerated, so we
// assert coverage here instead of importing reflection into production. Adding a new ETC
// *Block fork without wiring it into checkCompatibleETC makes this test fail.
func TestETCForkCompatibilityCoverage(t *testing.T) {
	bigIntPtr := reflect.TypeOf((*big.Int)(nil))
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"classic", ClassicChainConfig},
		{"mordor", MordorChainConfig},
	} {
		v := reflect.ValueOf(tc.cfg).Elem()
		typ := v.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Type != bigIntPtr || !strings.HasSuffix(field.Name, "Block") {
				continue // only block-number forks enter the fork ID
			}
			orig, _ := v.Field(i).Interface().(*big.Int)
			if orig == nil || orig.Sign() == 0 {
				continue // unconfigured, or a genesis fork that can't move earlier
			}
			// Copy the config and move this single fork one block earlier — a change in the
			// past, which must be reported as incompatible.
			moved := *tc.cfg
			earlier := new(big.Int).Sub(orig, big.NewInt(1))
			reflect.ValueOf(&moved).Elem().Field(i).Set(reflect.ValueOf(earlier))

			head := new(big.Int).Add(orig, big.NewInt(10)).Uint64()
			if err := tc.cfg.CheckCompatible(&moved, head, 0); err == nil {
				t.Errorf("%s: fork field %q enters the fork ID but CheckCompatible does not "+
					"guard it (moving %v→%v at head %d was accepted) — add it to checkCompatibleETC",
					tc.name, field.Name, orig, earlier, head)
			}
		}
	}
}

// TestETCForkOrder checks that the canonical ETC configs pass the fork-order check and that
// out-of-order ETC forks are rejected.
func TestETCForkOrder(t *testing.T) {
	// The real presets must be in order (a wrong canonical list would fail here).
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"classic", ClassicChainConfig},
		{"mordor", MordorChainConfig},
	} {
		if err := tc.cfg.CheckConfigForkOrder(); err != nil {
			t.Errorf("%s: canonical config rejected by CheckConfigForkOrder: %v", tc.name, err)
		}
	}

	// Spiral before Mystique (London) must be rejected.
	bad := *ClassicChainConfig
	bad.SpiralBlock = new(big.Int).Sub(ClassicChainConfig.LondonBlock, big.NewInt(1))
	if err := bad.checkConfigForkOrderETC(); err == nil {
		t.Error("checkConfigForkOrderETC accepted Spiral before Mystique (London)")
	}

	// Etchash before Phoenix (Istanbul) must be rejected.
	bad2 := *ClassicChainConfig
	bad2.ECIP1099Block = new(big.Int).Sub(ClassicChainConfig.IstanbulBlock, big.NewInt(1))
	if err := bad2.checkConfigForkOrderETC(); err == nil {
		t.Error("checkConfigForkOrderETC accepted Etchash before Phoenix (Istanbul)")
	}
}
