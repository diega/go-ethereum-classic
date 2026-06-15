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

// This file is a ONE-SHOT, on-disk MIGRATION (not a permanent compatibility
// layer) that lets getc adopt a datadir previously used by core-geth.
//
// core-geth stores the chain config under the same database key as go-ethereum
// (ethereum-config-<genesisHash>) but with a different JSON schema: it serializes
// the standard forks under its own field names (eip2FBlock, eip161FBlock, ...)
// instead of go-ethereum's (homesteadBlock, eip158Block, ...). When getc reads such
// a config, json.Unmarshal silently drops the unknown fields, leaving the standard
// fork markers nil. That partially-parsed config would then trip
// ChainConfig.CheckCompatible and rewind the chain back to the Homestead block
// (~block 1,150,000 on Classic), forcing a near-full resync.
//
// On the first startup we detect such a config and REWRITE it in place in getc's
// canonical format (the exact blob getc would have written natively in
// SetupGenesisBlock). After that the datadir is indistinguishable from one created
// by getc, and this code becomes a no-op for it forever.
//
// Because it mutates the persisted data rather than translating it on every read,
// this file is safe to DELETE once core-geth is deprecated: nodes that have started
// at least once with this code keep working without it (their on-disk config is
// already in getc format). The only capability lost on deletion is migrating a
// pristine core-geth datadir that has never been opened by getc. It introduces no
// proprietary on-disk format. To remove: delete this file and its single call site
// in eth/backend.go.

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// canonicalETCChainConfig returns getc's canonical chain config for a known ETC
// genesis hash, or nil for unknown / non-ETC networks.
func canonicalETCChainConfig(genesisHash common.Hash) *params.ChainConfig {
	switch genesisHash {
	case params.ClassicGenesisHash:
		return params.ClassicChainConfig
	case params.MordorGenesisHash:
		return params.MordorChainConfig
	}
	return nil
}

// MigrateCoreGethChainConfig detects a chain config written by core-geth and
// rewrites it, in place, in getc's canonical format for known ETC networks.
//
// It is idempotent and a no-op for empty datadirs, datadirs already in getc's
// format, and unknown / non-ETC networks. Any error reading or writing the
// database is returned to the caller so that startup can abort loudly rather
// than silently proceeding toward a chain rewind.
func MigrateCoreGethChainConfig(db ethdb.Database) error {
	// Resolve the canonical genesis hash; an empty datadir has none yet, in which
	// case SetupGenesisBlock will write the correct config later.
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return nil
	}
	canonical := canonicalETCChainConfig(genesisHash)
	if canonical == nil {
		// Not a network we have a canonical config for (e.g. ETH or a private
		// chain). Never touch a config we don't fully understand.
		return nil
	}
	stored := rawdb.ReadChainConfig(db, genesisHash)
	if stored == nil {
		// No persisted config yet; nothing to migrate.
		return nil
	}
	// core-geth names the base forks differently, so getc cannot parse them and
	// HomesteadBlock ends up nil. A config natively written by getc for a known
	// ETC network ALWAYS has HomesteadBlock set (Classic: 1,150,000; Mordor: 0).
	// Hence a nil HomesteadBlock here is an unambiguous marker of a core-geth
	// config that must be rewritten.
	if stored.HomesteadBlock != nil {
		return nil
	}
	rawdb.WriteChainConfig(db, genesisHash, canonical)
	log.Warn("Rewrote core-geth chain config into getc format",
		"genesis", genesisHash, "homestead", canonical.HomesteadBlock)
	return nil
}
