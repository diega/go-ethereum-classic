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

package core

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

// expectedTds builds a hash→TD map by walking a chain forward from a parent TD.
// Used to assert exact TD values after batched WriteHeaders calls.
func expectedTds(parentTd *big.Int, chain []*types.Header) map[common.Hash]*big.Int {
	out := make(map[common.Hash]*big.Int, len(chain))
	td := new(big.Int).Set(parentTd)
	for _, h := range chain {
		td = new(big.Int).Add(td, h.Difficulty)
		out[h.Hash()] = new(big.Int).Set(td)
	}
	return out
}

// assertHeaderTds checks that every header in chain has the expected TD on
// disk. wants is keyed by hash. Hashes absent from wants are ignored.
func assertHeaderTds(t *testing.T, hc *HeaderChain, chain []*types.Header, wants map[common.Hash]*big.Int) {
	t.Helper()
	for _, h := range chain {
		want, ok := wants[h.Hash()]
		if !ok {
			continue
		}
		got := hc.GetTd(h.Hash(), h.Number.Uint64())
		if got == nil {
			t.Fatalf("TD missing for block %d (%x)", h.Number, h.Hash().Bytes()[:6])
		}
		if got.Cmp(want) != 0 {
			t.Fatalf("TD mismatch for block %d (%x): have %v, want %v",
				h.Number, h.Hash().Bytes()[:6], got, want)
		}
	}
}

// TestHeaderInsertionStrictTd mirrors the canon-status scenarios of
// upstream TestHeaderInsertion (chainA/chainB interleaved inserts with
// known/new overlaps and reorgs) but adds strict TD-value assertions
// after every batch. This catches regressions like the [known, new] TD
// accumulation bug where the pre-purge `verifyUnbrokenCanonchain` check
// would not have fired (it asserted TD existence only, not correctness).
func TestHeaderInsertionStrictTd(t *testing.T) {
	var (
		db    = rawdb.NewMemoryDatabase()
		gspec = &Genesis{BaseFee: big.NewInt(params.InitialBaseFee), Config: params.AllEthashProtocolChanges}
	)
	gblock, err := gspec.Commit(db, triedb.NewDatabase(db, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	hc, err := NewHeaderChain(db, gspec.Config, ethash.NewFaker(), func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	// chain A: G->A1->A2...A128
	genDb, chainA := makeHeaderChainWithGenesis(gspec, 128, ethash.NewFaker(), 10)
	// chain B: G->A1->B1...B128
	chainB := makeHeaderChain(gspec.Config, chainA[0], 128, ethash.NewFaker(), genDb, 10)

	genesisTd := hc.GetTd(gblock.Hash(), 0)
	if genesisTd == nil {
		t.Fatal("genesis TD missing after Commit")
	}
	wantsA := expectedTds(genesisTd, chainA)
	// chainB branches off chainA[0]: chainB[0] is the first child of A1, so
	// the parent TD for chainB as a whole is the TD recorded at A1.
	wantsB := expectedTds(wantsA[chainA[0].Hash()], chainB)
	wantsB[chainA[0].Hash()] = wantsA[chainA[0].Hash()]

	mustInsert := func(name string, chain []*types.Header) {
		t.Helper()
		if _, err := hc.InsertHeaderChain(chain, time.Now(), nil); err != nil {
			t.Fatalf("%s: insert: %v", name, err)
		}
	}

	// Scenario mirrors TestHeaderInsertion's six canon-status inserts. The
	// third (chainA[32:96]) is the [known, new] pattern that demonstrated
	// the bug; the rest exercise sidechain → canon reorgs that must also
	// preserve TDs.
	mustInsert("chainA[:64]", chainA[:64])
	assertHeaderTds(t, hc, chainA[:64], wantsA)

	mustInsert("chainA[:64] re-insert", chainA[:64])
	assertHeaderTds(t, hc, chainA[:64], wantsA)

	mustInsert("chainA[32:96]", chainA[32:96])
	assertHeaderTds(t, hc, chainA[32:96], wantsA)

	mustInsert("chainB[0:32]", chainB[0:32])
	assertHeaderTds(t, hc, chainB[0:32], wantsB)

	mustInsert("chainB[32:97]", chainB[32:97])
	assertHeaderTds(t, hc, chainB[32:97], wantsB)

	mustInsert("chainA[90:100]", chainA[90:100])
	assertHeaderTds(t, hc, chainA[90:100], wantsA)

	mustInsert("chainB[97:107]", chainB[97:107])
	assertHeaderTds(t, hc, chainB[97:107], wantsB)

	mustInsert("chainB[107:128]", chainB[107:128])
	assertHeaderTds(t, hc, chainB[107:128], wantsB)
}

// TestWriteHeadersIsPowGateAllowsPosWithEthashConfig verifies that
// HeaderChain.WriteHeaders accepts a missing parent TD on a config that
// retains an EthashConfig but has TerminalTotalDifficulty set (the upstream
// post-merge shape). Pre-fix, the gate was `hc.config.Ethash != nil`, which
// errored out for such configs because no genesis TD was written. The fix
// switches to `IsPow()` (Ethash != nil AND TTD == nil).
func TestWriteHeadersIsPowGateAllowsPosWithEthashConfig(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// AllEthashProtocolChanges keeps Ethash != nil but sets TerminalTotalDifficulty,
	// matching how upstream merge configs were shaped.
	gspec := &Genesis{
		BaseFee: big.NewInt(params.InitialBaseFee),
		Config:  params.AllEthashProtocolChanges,
	}
	gblock, err := gspec.Commit(db, triedb.NewDatabase(db, nil), nil)
	if err != nil {
		t.Fatal(err)
	}

	hc, err := NewHeaderChain(db, gspec.Config, ethash.NewFaker(), func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	_, chain := makeHeaderChainWithGenesis(gspec, 4, ethash.NewFaker(), 10)

	// Purge the genesis TD to simulate a state where TDs aren't tracked.
	rawdb.DeleteTd(db, gblock.Hash(), 0)
	hc.tdCache.Purge()

	// WriteHeaders must succeed despite the missing parent TD because
	// IsPow() is false for this config.
	if _, err := hc.WriteHeaders(chain); err != nil {
		t.Fatalf("WriteHeaders should accept missing TD on non-PoW config, got %v", err)
	}
}

type headerChainTDReader struct {
	hc     *HeaderChain
	config *params.ChainConfig
}

func (r *headerChainTDReader) Config() *params.ChainConfig            { return r.config }
func (r *headerChainTDReader) GetTd(h common.Hash, n uint64) *big.Int { return r.hc.GetTd(h, n) }

// TestHeaderInsertionForkChoiceTD checks that InsertHeaderChain applies the TD fork
// choice at the header level: a lower-total-difficulty competing header chain is not
// adopted as the head. Without threading the fork choicer into writeHeadersAndSetHead
// the header head would blindly follow the last inserted chain.
func TestHeaderInsertionForkChoiceTD(t *testing.T) {
	var (
		db    = rawdb.NewMemoryDatabase()
		gspec = &Genesis{BaseFee: big.NewInt(params.InitialBaseFee), Config: params.AllEthashProtocolChanges}
	)
	if _, err := gspec.Commit(db, triedb.NewDatabase(db, nil), nil); err != nil {
		t.Fatal(err)
	}
	hc, err := NewHeaderChain(db, gspec.Config, ethash.NewFaker(), func() bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	genDb, chainA := makeHeaderChainWithGenesis(gspec, 64, ethash.NewFaker(), 10)
	// chainB branches off chainA[0] and is shorter -> strictly lower total difficulty.
	chainB := makeHeaderChain(gspec.Config, chainA[0], 16, ethash.NewFaker(), genDb, 11)

	forker := NewForkChoice(&headerChainTDReader{hc: hc, config: gspec.Config}, nil)

	if _, err := hc.InsertHeaderChain(chainA, time.Now(), forker); err != nil {
		t.Fatalf("insert chainA: %v", err)
	}
	headA := chainA[len(chainA)-1].Hash()
	if hc.CurrentHeader().Hash() != headA {
		t.Fatal("chainA was not adopted as the head")
	}

	if _, err := hc.InsertHeaderChain(chainB, time.Now(), forker); err != nil {
		t.Fatalf("insert chainB: %v", err)
	}
	if hc.CurrentHeader().Hash() != headA {
		t.Fatal("lower-TD chainB was adopted; fork choice not applied at header level")
	}
}
