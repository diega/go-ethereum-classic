// Copyright 2014 The go-ethereum Authors
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

package miner

import (
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/event"
)

// NewPoW creates a new miner with PoW mining support.
// The downloader event loop (update) is managed by the eth backend
// to avoid an import cycle with eth/downloader.
func NewPoW(eth Backend, config Config, engine consensus.Engine, mux *event.TypeMux) *Miner {
	m := &Miner{
		config:      &config,
		chainConfig: eth.BlockChain().Config(),
		engine:      engine,
		txpool:      eth.TxPool(),
		chain:       eth.BlockChain(),
		pending:     &pending{},
		mux:         mux,
		exitCh:      make(chan struct{}),
		startCh:     make(chan struct{}),
		stopCh:      make(chan struct{}),
	}
	m.powWorker = newPowWorker(m, eth.BlockChain(), eth.TxPool(), engine, mux)
	return m
}

// StartMining signals the miner to start producing blocks.
func (m *Miner) StartMining() {
	if m.powWorker != nil {
		m.powWorker.start()
	}
}

// StopMining signals the miner to stop producing blocks.
func (m *Miner) StopMining() {
	if m.powWorker != nil {
		m.powWorker.stop()
	}
}

// CloseMining terminates the PoW mining loops and waits for them to finish.
func (m *Miner) CloseMining() {
	if m.powWorker != nil {
		close(m.exitCh)
		m.powWorker.close()
		m.wg.Wait()
	}
}

// Mining returns whether the miner is currently mining.
func (m *Miner) Mining() bool {
	if m.powWorker != nil {
		return m.powWorker.isRunning()
	}
	return false
}

// Hashrate returns the current mining hashrate. The internal interface
// matches ethash.Hashrate (float64); the value is truncated to uint64 for
// the JSON-RPC boundary in eth/api_pow.go.
func (m *Miner) Hashrate() uint64 {
	type hashRater interface {
		Hashrate() float64
	}
	if hr, ok := m.engine.(hashRater); ok {
		return uint64(hr.Hashrate())
	}
	return 0
}

// SetEtherbase sets the etherbase (coinbase) address for mining rewards.
func (m *Miner) SetEtherbase(addr common.Address) {
	if m.powWorker != nil {
		m.powWorker.setEtherbase(addr)
	}
}

// SetRecommitInterval sets the interval for miner sealing work recommitting.
func (m *Miner) SetRecommitInterval(interval time.Duration) {
	if m.powWorker != nil {
		m.powWorker.setRecommitInterval(interval)
	}
}

// SetSyncing sets the syncing state of the PoW worker.
// Called by the eth backend when the downloader starts/stops syncing.
func (m *Miner) SetSyncing(syncing bool) {
	if m.powWorker != nil {
		m.powWorker.syncing.Store(syncing)
	}
}

// ExitCh returns the exit channel for the PoW miner. Returns nil for PoS miners.
func (m *Miner) ExitCh() <-chan struct{} {
	return m.exitCh
}
