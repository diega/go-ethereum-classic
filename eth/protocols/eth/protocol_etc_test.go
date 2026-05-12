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

package eth

import (
	"slices"
	"testing"
)

// TestGetProtocolVersionsNonPowExcludesETH68 guards against the regression
// where init() mutated the global ProtocolVersions to include ETH68,
// causing GetProtocolVersions(false) to advertise ETH68 on non-PoW chains.
// Non-PoW handshakes have no TD plumbing for ETH68 and crash the handlers.
func TestGetProtocolVersionsNonPowExcludesETH68(t *testing.T) {
	got := GetProtocolVersions(false)
	if slices.Contains(got, uint(ETH68)) {
		t.Fatalf("GetProtocolVersions(false) advertises ETH68: %v", got)
	}
}

// TestGetProtocolVersionsPowIsETH68Only confirms PoW chains advertise only
// ETH68 (the last version that carries TD in the handshake, required by
// the PoW sync path).
func TestGetProtocolVersionsPowIsETH68Only(t *testing.T) {
	got := GetProtocolVersions(true)
	if len(got) != 1 || got[0] != ETH68 {
		t.Fatalf("GetProtocolVersions(true) = %v, want [%d]", got, ETH68)
	}
}

// TestAllProtocolVersionsIncludesETH68 verifies the membership helper used
// by peerset capability checks covers every version this binary speaks,
// regardless of chain type.
func TestAllProtocolVersionsIncludesETH68(t *testing.T) {
	got := AllProtocolVersions()
	if !slices.Contains(got, uint(ETH68)) {
		t.Fatalf("AllProtocolVersions missing ETH68: %v", got)
	}
	for _, v := range ProtocolVersions {
		if !slices.Contains(got, v) {
			t.Errorf("AllProtocolVersions missing %d: %v", v, got)
		}
	}
}
