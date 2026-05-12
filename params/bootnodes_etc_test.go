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

package params

import (
	"strings"
	"testing"
)

// TestETCDNSNetworkChainIDDispatch covers the ETC mainnet and Mordor cases.
// Guards against a regression where the lookup keyed off genesis hash (which
// ETC shares with ETH mainnet) and returned the mainnet tree by mistake.
func TestETCDNSNetworkChainIDDispatch(t *testing.T) {
	tests := []struct {
		name      string
		chainID   uint64
		protocol  string
		wantHost  string
		wantEmpty bool
	}{
		{name: "ETC mainnet", chainID: 61, protocol: "all", wantHost: "all.classic.etcdisco.net"},
		{name: "Mordor", chainID: 63, protocol: "all", wantHost: "all.mordor.etcdisco.net"},
		{name: "ETH mainnet must not match", chainID: 1, protocol: "all", wantEmpty: true},
		{name: "unknown chain", chainID: 999, protocol: "all", wantEmpty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ETCDNSNetwork(tt.chainID, tt.protocol)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("ETCDNSNetwork(%d, %q) = %q, want \"\"", tt.chainID, tt.protocol, got)
				}
				return
			}
			if !strings.HasPrefix(got, etcDNSPrefix) {
				t.Errorf("ETCDNSNetwork(%d, %q) = %q, missing etcDNSPrefix", tt.chainID, tt.protocol, got)
			}
			if !strings.HasSuffix(got, tt.wantHost) {
				t.Errorf("ETCDNSNetwork(%d, %q) = %q, want suffix %q", tt.chainID, tt.protocol, got, tt.wantHost)
			}
		})
	}
}

// TestETCDNSNetworkDoesNotCollideWithEthMainnet documents the central
// invariant: even though ClassicGenesisHash == MainnetGenesisHash, asking
// for the ETC network must never produce the upstream mainnet tree URL.
func TestETCDNSNetworkDoesNotCollideWithEthMainnet(t *testing.T) {
	etcURL := ETCDNSNetwork(61, "all")
	mainnetURL := KnownDNSNetwork(MainnetGenesisHash, "all")
	if etcURL == mainnetURL {
		t.Fatalf("ETCDNSNetwork(61) == KnownDNSNetwork(MainnetGenesisHash); ETC peers would discover ETH mainnet nodes (%s)", etcURL)
	}
}
