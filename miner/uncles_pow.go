// Copyright 2015 The go-ethereum Authors
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

// Uncle (ommer) handling for the PoW miner: it collects the headers of blocks
// that lost the fork choice as uncle candidates, validates them against the
// block being sealed, and selects up to two for inclusion. ETC keeps the
// pre-merge uncle reward schedule (ECIP-1017), so the miner includes uncles.

import (
	"errors"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

// uncleEnv holds the uncle bookkeeping for a block being sealed: the header of
// that block, the accepted uncle set, and the ancestor and family sets used to
// validate candidates.
type uncleEnv struct {
	header    *types.Header
	uncles    map[common.Hash]*types.Header
	ancestors mapset.Set[common.Hash]
	family    mapset.Set[common.Hash]
}

// newUncleEnv seeds the ancestor and family sets for a block being sealed on
// top of the given header.
func (w *powWorker) newUncleEnv(header *types.Header) *uncleEnv {
	env := &uncleEnv{
		header:    header,
		uncles:    make(map[common.Hash]*types.Header),
		ancestors: mapset.NewSet[common.Hash](),
		family:    mapset.NewSet[common.Hash](),
	}
	// when 08 is processed ancestors contain 07 (quick block)
	for _, ancestor := range w.chain.GetBlocksFromHash(header.ParentHash, 7) {
		for _, uncle := range ancestor.Uncles() {
			env.family.Add(uncle.Hash())
		}
		env.family.Add(ancestor.Hash())
		env.ancestors.Add(ancestor.Hash())
	}
	return env
}

// commitUncle validates an uncle header against the block being sealed and adds
// it to the uncle set, returning an error if it is not acceptable.
func (env *uncleEnv) commitUncle(uncle *types.Header) error {
	hash := uncle.Hash()
	if _, exist := env.uncles[hash]; exist {
		return errors.New("uncle not unique")
	}
	if env.header.ParentHash == uncle.ParentHash {
		return errors.New("uncle is sibling")
	}
	if !env.ancestors.Contains(uncle.ParentHash) {
		return errors.New("uncle's parent unknown")
	}
	if env.family.Contains(hash) {
		return errors.New("uncle already included")
	}
	env.uncles[hash] = uncle
	return nil
}

// unclelist returns the accepted uncles as a slice.
func (env *uncleEnv) unclelist() []*types.Header {
	var uncles []*types.Header
	for _, uncle := range env.uncles {
		uncles = append(uncles, uncle)
	}
	return uncles
}

// gatherUncles selects up to two valid uncles for the block whose header is
// being sealed, preferring locally mined ones.
func (w *powWorker) gatherUncles(header *types.Header) []*types.Header {
	env := w.newUncleEnv(header)
	commitUncles := func(blocks map[common.Hash]*types.Header) {
		for hash, uncle := range blocks {
			if len(env.uncles) == 2 {
				break
			}
			if err := env.commitUncle(uncle); err != nil {
				log.Trace("Possible uncle rejected", "hash", hash, "reason", err)
			} else {
				log.Debug("Committing new uncle to block", "hash", hash)
			}
		}
	}
	// Prefer to locally generated uncle
	commitUncles(w.localUncles)
	commitUncles(w.remoteUncles)
	return env.unclelist()
}

// collectUncle records a side-block header as a possible uncle, classifying it
// as locally or remotely mined by comparing against the configured etherbase.
// It returns true if the header was newly added (not a duplicate).
func (w *powWorker) collectUncle(header *types.Header) bool {
	if header == nil {
		return false
	}
	hash := header.Hash()
	if _, exist := w.localUncles[hash]; exist {
		return false
	}
	if _, exist := w.remoteUncles[hash]; exist {
		return false
	}
	if header.Coinbase == w.etherbase() {
		w.localUncles[hash] = header
	} else {
		w.remoteUncles[hash] = header
	}
	return true
}

// pruneStaleUncles drops uncle candidates that have fallen too far behind the
// chain head to still be includable.
func (w *powWorker) pruneStaleUncles() {
	chainHead := w.chain.CurrentBlock()
	for hash, uncle := range w.localUncles {
		if uncle.Number.Uint64()+staleThreshold <= chainHead.Number.Uint64() {
			delete(w.localUncles, hash)
		}
	}
	for hash, uncle := range w.remoteUncles {
		if uncle.Number.Uint64()+staleThreshold <= chainHead.Number.Uint64() {
			delete(w.remoteUncles, hash)
		}
	}
}
