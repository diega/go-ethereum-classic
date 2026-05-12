// Copyright 2024 The go-ethereum Authors
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

package main

import (
	"time"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/urfave/cli/v2"
)

// startPoWFeatures starts the standalone mining and exit-when-synced handlers
// that upstream go-ethereum removed in PR #28623 (commit d8e0807da) as part of
// the post-Merge miner refactor. The body below is a char-for-char transplant
// of the block that used to live inline in startNode(); only the surrounding
// function header/footer are new.
func startPoWFeatures(ctx *cli.Context, stack *node.Node, ethBackend *eth.Ethereum) {
	// Start mining if requested
	if ctx.Bool(utils.MiningEnabledFlag.Name) && ethBackend != nil {
		threads := ctx.Int(utils.MinerThreadsFlag.Name)
		if err := ethBackend.StartMining(threads); err != nil {
			utils.Fatalf("Failed to start mining: %v", err)
		}
	}

	// Spawn a standalone goroutine for status synchronization monitoring,
	// close the node when synchronization is complete if user required.
	if ctx.Bool(utils.ExitWhenSyncedFlag.Name) && ethBackend != nil {
		go func() {
			eventCh := make(chan downloader.SyncEvent, 16)
			sub := ethBackend.Downloader().SubscribeSyncEvents(eventCh)
			defer sub.Unsubscribe()
			for ev := range eventCh {
				if ev.Type != downloader.SyncCompleted || ev.Latest == nil {
					continue
				}
				if timestamp := time.Unix(int64(ev.Latest.Time), 0); time.Since(timestamp) < 10*time.Minute {
					log.Info("Synchronisation completed", "latestnum", ev.Latest.Number, "latesthash", ev.Latest.Hash(),
						"age", common.PrettyAge(timestamp))
					stack.Close()
				}
			}
		}()
	}
}
