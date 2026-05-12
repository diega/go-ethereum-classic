// Copyright 2015 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package eth

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/log"
)

// We regard two types of accounts as local miner account: etherbase
// and accounts specified via `txpool.locals` flag.
func (s *Ethereum) isLocalBlock(header *types.Header) bool {
	author, err := s.engine.Author(header)
	if err != nil {
		log.Warn("Failed to retrieve block author", "number", header.Number.Uint64(), "hash", header.Hash(), "err", err)
		return false
	}
	// Check whether the given address is etherbase.
	etherbaseMu.RLock()
	eb := etherbase
	etherbaseMu.RUnlock()
	if author == eb {
		return true
	}
	// Check whether the given address is specified by `txpool.local` CLI flag.
	for _, account := range s.config.TxPool.Locals {
		if account == author {
			return true
		}
	}
	return false
}

// etherbase is the mining reward address, protected by etherbaseMu.
var (
	etherbaseMu sync.RWMutex
	etherbase   common.Address
)

// StartMining starts the PoW mining process with the given number of threads.
// If threads is 0, the engine decides the thread count internally.
func (s *Ethereum) StartMining(threads int) error {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		log.Info("Updated mining threads", "threads", threads)
		if threads == 0 {
			threads = -1 // Disable the miner from within
		}
		th.SetThreads(threads)
	}
	// If the miner was not running, initialize it
	if !s.IsMining() {
		// Propagate the initial price point to the transaction pool
		s.lock.RLock()
		price := s.gasPrice
		s.lock.RUnlock()
		s.txPool.SetGasTip(price)

		// Configure the local mining address
		eb, err := s.Etherbase()
		if err != nil {
			log.Error("Cannot start mining without etherbase", "err", err)
			return fmt.Errorf("etherbase missing: %v", err)
		}
		s.miner.SetEtherbase(eb)
		s.miner.StartMining()
	}
	return nil
}

// StopMining stops the PoW mining process.
func (s *Ethereum) StopMining() {
	// Update the thread count within the consensus engine
	type threaded interface {
		SetThreads(threads int)
	}
	if th, ok := s.engine.(threaded); ok {
		th.SetThreads(-1)
	}
	s.miner.StopMining()
}

// IsMining returns whether the miner is currently mining.
func (s *Ethereum) IsMining() bool {
	return s.miner.Mining()
}

// Etherbase returns the configured etherbase address for mining rewards.
func (s *Ethereum) Etherbase() (common.Address, error) {
	etherbaseMu.RLock()
	eb := etherbase
	etherbaseMu.RUnlock()

	if eb != (common.Address{}) {
		return eb, nil
	}
	return common.Address{}, fmt.Errorf("etherbase must be explicitly specified")
}

// SetEtherbase sets the mining reward address.
func (s *Ethereum) SetEtherbase(addr common.Address) {
	etherbaseMu.Lock()
	etherbase = addr
	etherbaseMu.Unlock()

	s.miner.SetEtherbase(addr)
}

// minerUpdate keeps track of the downloader events and pauses/resumes mining accordingly.
// This is placed in the eth package (instead of miner) to avoid an import cycle
// between miner and eth/downloader.
func (s *Ethereum) minerUpdate() {
	eventCh := make(chan downloader.SyncEvent, 16)
	sub := s.handler.downloader.SubscribeSyncEvents(eventCh)
	defer sub.Unsubscribe()

	shouldStart := false
	for {
		select {
		case ev := <-eventCh:
			switch ev.Type {
			case downloader.SyncStarted:
				if s.IsMining() {
					s.miner.StopMining()
					shouldStart = true
				}
				s.miner.SetSyncing(true)

			case downloader.SyncFailed:
				if shouldStart {
					s.miner.StartMining()
				}
				s.miner.SetSyncing(false)
				// Stop reacting to downloader events (one-shot security measure)
				sub.Unsubscribe()
				eventCh = nil

			case downloader.SyncCompleted:
				if shouldStart {
					s.miner.StartMining()
				}
				s.miner.SetSyncing(false)
				// Stop reacting to downloader events (one-shot security measure)
				sub.Unsubscribe()
				eventCh = nil
			}

		case <-s.miner.ExitCh():
			return
		}
	}
}
