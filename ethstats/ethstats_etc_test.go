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

package ethstats

import "github.com/ethereum/go-ethereum/eth"

// The mining/hashrate report relies on a silent interface assertion: if
// *eth.EthAPIBackend ever stops satisfying miningNodeBackend, reportStats
// degrades to mining=false / hashrate=0 with no compile or runtime error.
// This pin turns that failure mode into a build break. See
// miner/miner_pow_etc_test.go for the equivalent guard one level down
// (miner → engine).
var _ miningNodeBackend = (*eth.EthAPIBackend)(nil)
