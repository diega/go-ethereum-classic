// Copyright 2023 The go-ethereum Authors
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
	"runtime"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Start starts the miner with the given number of threads. If threads is nil,
// the number of workers used is the number of logical CPUs that are usable by
// the current process.
func (api *MinerAPI) Start(threads *int) error {
	if threads == nil {
		n := runtime.NumCPU()
		threads = &n
	}
	return api.e.StartMining(*threads)
}

// Stop terminates the miner.
func (api *MinerAPI) Stop() {
	api.e.StopMining()
}

// SetEtherbase sets the etherbase of the miner.
func (api *MinerAPI) SetEtherbase(etherbase common.Address) bool {
	api.e.SetEtherbase(etherbase)
	return true
}

// SetRecommitInterval updates the interval for miner sealing work recommitting.
func (api *MinerAPI) SetRecommitInterval(interval int) {
	api.e.Miner().SetRecommitInterval(time.Duration(interval) * time.Millisecond)
}
