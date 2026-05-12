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

// ClassicBootnodes are the enode URLs of the P2P bootstrap nodes running on
// Ethereum Classic mainnet.
var ClassicBootnodes = []string{
	// ETC Cooperative bootnodes
	"enode://6b6ea53a498f0895c10269a3a74b777286bd467de6425c3b512740fcc7fbc8cd281dca4ab041dd97d62b38f3d0b5b05e71f48d28a3a2f4b5de40fe1f6bf05531@157.245.77.211:30303", // AMS
	"enode://16264d48df59c3492972d96bf8a39dd38bab165809a3a4bb161859a337de38b2959cc98efea94355c7a7177cd020867c683aed934dbd6bc937d9e6b61d94d8d9@64.225.0.245:30303",   // NYC
	"enode://55bbc7f0ffa2af2ceca997ec195a98768144a163d389ae87b808dff8a861618405c2582451bbb6022e429e4bcd6b0e895e86160db6e93cdadbcfd80faacf6f06@164.90.144.106:30303", // SFO
}

// MordorBootnodes are the enode URLs of the P2P bootstrap nodes running on
// Mordor testnet (ETC testnet).
var MordorBootnodes = []string{
	// ETC Cooperative Mordor bootnodes
	"enode://642cf9650dd8869d42525dbf6858012e3b4d64f475e733847ab6f7742341a4397414865d953874e8f5ed91b0e4e1c533dee14ad1d6bb276a5459b2471460ff0d@157.230.152.87:30303",
	"enode://651b484b652c07c72adebfaaf8bc2bd95b420b16952ef3de76a9c00ef63f07cca02a20bd2f7f9a04e697fc873e8c1ed1f25925d0523deb23192af2d4bf3a8a04@157.230.152.87:30303",
}

// etcDNSPrefix is the DNS enrtree key prefix for ETC networks.
const etcDNSPrefix = "enrtree://AJE62Q4DUX4QMMXEHCSSCSC65TDHZYSMONSD64P3WULVLSF6MRQ3K@"

// ETCDNSNetwork returns the address of the public DNS-based node list for an
// ETC network. ETC chains share the genesis hash with Ethereum mainnet, so
// dispatch keys off the chain ID instead — 61 is Classic mainnet, 63 is
// Mordor testnet. Returns "" for any other chain ID.
func ETCDNSNetwork(chainID uint64, protocol string) string {
	switch chainID {
	case 61:
		return etcDNSPrefix + protocol + ".classic.etcdisco.net"
	case 63:
		return etcDNSPrefix + protocol + ".mordor.etcdisco.net"
	default:
		return ""
	}
}
