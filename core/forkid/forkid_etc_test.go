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

package forkid

import (
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestCreationETC verifies the fork ID for Classic and Mordor against the canonical
// ETC values, pinning every ETC fork block and the genesis hash (the FORK_HASH base).
func TestCreationETC(t *testing.T) {
	type testcase struct {
		head uint64
		time uint64
		want ID
	}
	tests := []struct {
		name    string
		config  *params.ChainConfig
		genesis *types.Block
		cases   []testcase
	}{
		{
			"classic",
			params.ClassicChainConfig,
			core.DefaultClassicGenesisBlock().ToBlock(),
			[]testcase{
				{0, 0, ID{Hash: checksumToBytes(0xfc64ec04), Next: 1150000}},
				{1149999, 0, ID{Hash: checksumToBytes(0xfc64ec04), Next: 1150000}},
				{1150000, 0, ID{Hash: checksumToBytes(0x97c2c34c), Next: 2500000}},
				{2499999, 0, ID{Hash: checksumToBytes(0x97c2c34c), Next: 2500000}},
				{2500000, 0, ID{Hash: checksumToBytes(0xdb06803f), Next: 3000000}},
				{2999999, 0, ID{Hash: checksumToBytes(0xdb06803f), Next: 3000000}},
				{3000000, 0, ID{Hash: checksumToBytes(0xaff4bed4), Next: 5000000}},
				{4999999, 0, ID{Hash: checksumToBytes(0xaff4bed4), Next: 5000000}},
				{5000000, 0, ID{Hash: checksumToBytes(0xf79a63c0), Next: 5900000}},
				{5899999, 0, ID{Hash: checksumToBytes(0xf79a63c0), Next: 5900000}},
				{5900000, 0, ID{Hash: checksumToBytes(0x744899d6), Next: 8772000}},
				{8771999, 0, ID{Hash: checksumToBytes(0x744899d6), Next: 8772000}},
				{8772000, 0, ID{Hash: checksumToBytes(0x518b59c6), Next: 9573000}},
				{9572999, 0, ID{Hash: checksumToBytes(0x518b59c6), Next: 9573000}},
				{9573000, 0, ID{Hash: checksumToBytes(0x7ba22882), Next: 10500839}},
				{10500838, 0, ID{Hash: checksumToBytes(0x7ba22882), Next: 10500839}},
				{10500839, 0, ID{Hash: checksumToBytes(0x9007bfcc), Next: 11700000}},
				{11699999, 0, ID{Hash: checksumToBytes(0x9007bfcc), Next: 11700000}},
				{11700000, 0, ID{Hash: checksumToBytes(0xdb63a1ca), Next: 13189133}},
				{13189132, 0, ID{Hash: checksumToBytes(0xdb63a1ca), Next: 13189133}},
				{13189133, 0, ID{Hash: checksumToBytes(0x0f6bf187), Next: 14525000}},
				{14524999, 0, ID{Hash: checksumToBytes(0x0f6bf187), Next: 14525000}},
				{14525000, 0, ID{Hash: checksumToBytes(0x7fd1bb25), Next: 19250000}},
				{19249999, 0, ID{Hash: checksumToBytes(0x7fd1bb25), Next: 19250000}},
				{19250000, 0, ID{Hash: checksumToBytes(0xbe46d57c), Next: 0}},
				{19250001, 0, ID{Hash: checksumToBytes(0xbe46d57c), Next: 0}},
			},
		},
		{
			"mordor",
			params.MordorChainConfig,
			core.DefaultMordorGenesisBlock().ToBlock(),
			[]testcase{
				{0, 0, ID{Hash: checksumToBytes(0x175782aa), Next: 301243}},
				{301242, 0, ID{Hash: checksumToBytes(0x175782aa), Next: 301243}},
				{301243, 0, ID{Hash: checksumToBytes(0x604f6ee1), Next: 999983}},
				{999982, 0, ID{Hash: checksumToBytes(0x604f6ee1), Next: 999983}},
				{999983, 0, ID{Hash: checksumToBytes(0xf42f5539), Next: 2520000}},
				{2519999, 0, ID{Hash: checksumToBytes(0xf42f5539), Next: 2520000}},
				{2520000, 0, ID{Hash: checksumToBytes(0x66b5c286), Next: 3985893}},
				{3985892, 0, ID{Hash: checksumToBytes(0x66b5c286), Next: 3985893}},
				{3985893, 0, ID{Hash: checksumToBytes(0x92b323e0), Next: 5520000}},
				{5519999, 0, ID{Hash: checksumToBytes(0x92b323e0), Next: 5520000}},
				{5520000, 0, ID{Hash: checksumToBytes(0x8c9b1797), Next: 9957000}},
				{9956999, 0, ID{Hash: checksumToBytes(0x8c9b1797), Next: 9957000}},
				{9957000, 0, ID{Hash: checksumToBytes(0x3a6b00d7), Next: 0}},
				{9957001, 0, ID{Hash: checksumToBytes(0x3a6b00d7), Next: 0}},
			},
		},
	}
	for _, tt := range tests {
		for i, ttt := range tt.cases {
			if have := NewID(tt.config, tt.genesis, ttt.head, ttt.time); have != ttt.want {
				t.Errorf("%s case %d (head %d): fork ID mismatch: have %x, want %x", tt.name, i, ttt.head, have, ttt.want)
			}
		}
	}
}
