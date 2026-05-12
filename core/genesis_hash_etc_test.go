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
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestETCGenesisHashes pins the Classic and Mordor genesis blocks to the hashes
// declared in params (nothing else asserts the declared constants actually match).
func TestETCGenesisHashes(t *testing.T) {
	for _, tt := range []struct {
		name    string
		genesis *Genesis
		want    string
	}{
		{"classic", DefaultClassicGenesisBlock(), params.ClassicGenesisHash.Hex()},
		{"mordor", DefaultMordorGenesisBlock(), params.MordorGenesisHash.Hex()},
	} {
		if got := tt.genesis.ToBlock().Hash().Hex(); got != tt.want {
			t.Errorf("%s genesis hash mismatch: got %s, want %s", tt.name, got, tt.want)
		}
	}
}
