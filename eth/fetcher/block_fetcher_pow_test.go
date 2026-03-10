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

package fetcher

import (
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

var (
	testdb      = rawdb.NewMemoryDatabase()
	testKey, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddress = crypto.PubkeyToAddress(testKey.PublicKey)
	gspec       = &core.Genesis{
		Config:  params.TestChainConfig,
		Alloc:   types.GenesisAlloc{testAddress: {Balance: big.NewInt(1000000000000000)}},
		BaseFee: big.NewInt(params.InitialBaseFee),
	}
	genesis      = gspec.MustCommit(testdb, triedb.NewDatabase(testdb, triedb.HashDefaults))
	unknownBlock = types.NewBlockWithHeader(&types.Header{Root: types.EmptyRootHash, GasLimit: params.GenesisGasLimit, BaseFee: big.NewInt(params.InitialBaseFee)})
)

// makeChain creates a chain of n blocks starting at and including parent.
// the returned hash chain is ordered head->parent. In addition, every 3rd block
// contains a transaction and every 5th an uncle to allow testing correct block
// reassembly.
func makeChain(n int, seed byte, parent *types.Block) ([]common.Hash, map[common.Hash]*types.Block) {
	blocks, _ := core.GenerateChain(gspec.Config, parent, ethash.NewFaker(), testdb, n, func(i int, block *core.BlockGen) {
		block.SetCoinbase(common.Address{seed})

		// If the block number is multiple of 3, send a bonus transaction to the miner
		if parent == genesis && i%3 == 0 {
			signer := types.MakeSigner(params.TestChainConfig, block.Number(), block.Timestamp())
			tx, err := types.SignTx(types.NewTransaction(block.TxNonce(testAddress), common.Address{seed}, big.NewInt(1000), params.TxGas, block.BaseFee(), nil), signer, testKey)
			if err != nil {
				panic(err)
			}
			block.AddTx(tx)
		}
		// If the block number is a multiple of 5, add a bonus uncle to the block
		if i > 0 && i%5 == 0 {
			block.AddUncle(&types.Header{ParentHash: block.PrevBlock(i - 2).Hash(), Number: big.NewInt(int64(i - 1))})
		}
	})
	hashes := make([]common.Hash, n+1)
	hashes[len(hashes)-1] = parent.Hash()
	blockm := make(map[common.Hash]*types.Block, n+1)
	blockm[parent.Hash()] = parent
	for i, b := range blocks {
		hashes[len(hashes)-i-2] = b.Hash()
		blockm[b.Hash()] = b
	}
	return hashes, blockm
}

// fetcherTesterPoW is a test simulator for mocking out local block chain.
type fetcherTesterPoW struct {
	fetcher *BlockFetcher

	hashes  []common.Hash                 // Hash chain belonging to the tester
	headers map[common.Hash]*types.Header // Headers belonging to the tester
	blocks  map[common.Hash]*types.Block  // Blocks belonging to the tester
	drops   map[string]bool               // Map of peers dropped by the fetcher

	lock sync.RWMutex
}

// newTesterPoW creates a new fetcher test mocker.
func newTesterPoW() *fetcherTesterPoW {
	tester := &fetcherTesterPoW{
		hashes:  []common.Hash{genesis.Hash()},
		headers: map[common.Hash]*types.Header{genesis.Hash(): genesis.Header()},
		blocks:  map[common.Hash]*types.Block{genesis.Hash(): genesis},
		drops:   make(map[string]bool),
	}
	tester.fetcher = NewBlockFetcher(false, tester.getHeader, tester.getBlock, tester.verifyHeader, tester.broadcastBlock, tester.chainHeight, tester.insertHeaders, tester.insertChain, tester.dropPeer)
	tester.fetcher.Start()

	return tester
}

// getHeader retrieves a header from the tester's block chain.
func (f *fetcherTesterPoW) getHeader(hash common.Hash) *types.Header {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.headers[hash]
}

// getBlock retrieves a block from the tester's block chain.
func (f *fetcherTesterPoW) getBlock(hash common.Hash) *types.Block {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.blocks[hash]
}

// verifyHeader is a nop placeholder for the block header verification.
func (f *fetcherTesterPoW) verifyHeader(header *types.Header) error {
	return nil
}

// broadcastBlock is a nop placeholder for the block broadcasting.
func (f *fetcherTesterPoW) broadcastBlock(block *types.Block, propagate bool) {
}

// chainHeight retrieves the current height (block number) of the chain.
func (f *fetcherTesterPoW) chainHeight() uint64 {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return f.blocks[f.hashes[len(f.hashes)-1]].NumberU64()
}

// insertHeaders injects new headers into the simulated chain.
func (f *fetcherTesterPoW) insertHeaders(headers []*types.Header) (int, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for i, header := range headers {
		// Make sure the parent in known
		if _, ok := f.headers[header.ParentHash]; !ok {
			return i, errors.New("unknown parent")
		}
		// Discard any new blocks if the same height already exists
		if header.Number.Uint64() <= f.headers[f.hashes[len(f.hashes)-1]].Number.Uint64() {
			return i, nil
		}
		// Otherwise build our current chain
		f.hashes = append(f.hashes, header.Hash())
		f.headers[header.Hash()] = header
	}
	return 0, nil
}

// insertChain injects a new blocks into the simulated chain.
func (f *fetcherTesterPoW) insertChain(blocks types.Blocks) (int, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for i, block := range blocks {
		// Make sure the parent in known
		if _, ok := f.blocks[block.ParentHash()]; !ok {
			return i, errors.New("unknown parent")
		}
		// Discard any new blocks if the same height already exists
		if block.NumberU64() <= f.blocks[f.hashes[len(f.hashes)-1]].NumberU64() {
			return i, nil
		}
		// Otherwise build our current chain
		f.hashes = append(f.hashes, block.Hash())
		f.blocks[block.Hash()] = block
	}
	return 0, nil
}

// dropPeer is an emulator for the peer removal, simply accumulating the various
// peers dropped by the fetcher.
func (f *fetcherTesterPoW) dropPeer(peer string) {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.drops[peer] = true
}

// makeHeaderFetcher retrieves a block header fetcher associated with a simulated peer.
func (f *fetcherTesterPoW) makeHeaderFetcher(peer string, blocks map[common.Hash]*types.Block, drift time.Duration) headerRequesterFn {
	closure := make(map[common.Hash]*types.Block)
	for hash, block := range blocks {
		closure[hash] = block
	}
	// Create a function that return a header from the closure
	return func(hash common.Hash, sink chan *eth.Response) (*eth.Request, error) {
		// Gather the blocks to return
		headers := make([]*types.Header, 0, 1)
		if block, ok := closure[hash]; ok {
			headers = append(headers, block.Header())
		}
		// Return on a new thread
		req := &eth.Request{
			Peer: peer,
		}
		res := &eth.Response{
			Req:  req,
			Res:  (*eth.BlockHeadersRequest)(&headers),
			Time: drift,
			Done: make(chan error, 1), // Ignore the returned status
		}
		go func() {
			sink <- res
		}()
		return req, nil
	}
}

// makeBodyFetcher retrieves a block body fetcher associated with a simulated peer.
func (f *fetcherTesterPoW) makeBodyFetcher(peer string, blocks map[common.Hash]*types.Block, drift time.Duration) bodyRequesterFn {
	closure := make(map[common.Hash]*types.Block)
	for hash, block := range blocks {
		closure[hash] = block
	}
	// Create a function that returns blocks from the closure
	return func(hashes []common.Hash, sink chan *eth.Response) (*eth.Request, error) {
		// Gather the block bodies to return
		transactions := make([][]*types.Transaction, 0, len(hashes))
		uncles := make([][]*types.Header, 0, len(hashes))

		for _, hash := range hashes {
			if block, ok := closure[hash]; ok {
				transactions = append(transactions, block.Transactions())
				uncles = append(uncles, block.Uncles())
			}
		}
		// Return on a new thread
		bodies := make([]eth.BlockBody, len(transactions))
		for i, txs := range transactions {
			for _, tx := range txs {
				bodies[i].Transactions.Append(tx)
			}
			for _, uncle := range uncles[i] {
				bodies[i].Uncles.Append(uncle)
			}
		}
		req := &eth.Request{
			Peer: peer,
		}
		resp := eth.BlockBodiesResponse(bodies)
		res := &eth.Response{
			Req:  req,
			Res:  &resp,
			Time: drift,
			Done: make(chan error, 1), // Ignore the returned status
		}
		go func() {
			sink <- res
		}()
		return req, nil
	}
}

// verifyImportEvent verifies that one single event arrive on an import channel.
func verifyImportEventPoW(t *testing.T, imported chan interface{}, arrive bool) {
	t.Helper()

	if arrive {
		select {
		case <-imported:
		case <-time.After(time.Second):
			t.Fatalf("import timeout")
		}
	} else {
		select {
		case <-imported:
			t.Fatalf("import invoked")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// verifyImportCount verifies that exactly count number of events arrive on an
// import hook channel.
func verifyImportCountPoW(t *testing.T, imported chan interface{}, count int) {
	t.Helper()

	for i := 0; i < count; i++ {
		select {
		case <-imported:
		case <-time.After(time.Second):
			t.Fatalf("block %d: import timeout", i+1)
		}
	}
	verifyImportDonePoW(t, imported)
}

// verifyImportDone verifies that no more events are arriving on an import channel.
func verifyImportDonePoW(t *testing.T, imported chan interface{}) {
	t.Helper()

	select {
	case <-imported:
		t.Fatalf("extra block imported")
	case <-time.After(50 * time.Millisecond):
	}
}

// verifyChainHeight verifies the chain height is as expected.
func verifyChainHeightPoW(t *testing.T, fetcher *fetcherTesterPoW, height uint64) {
	t.Helper()

	if fetcher.chainHeight() != height {
		t.Fatalf("chain height mismatch, got %d, want %d", fetcher.chainHeight(), height)
	}
}

// Tests that a fetcher accepts block announcements and initiates retrievals
// for them, successfully importing into the local chain.
func TestPoWSequentialAnnouncements(t *testing.T) {
	// Create a chain of blocks to import
	targetBlocks := 4 * hashLimit
	hashes, blocks := makeChain(targetBlocks, 0, genesis)

	tester := newTesterPoW()
	defer tester.fetcher.Stop()
	headerFetcher := tester.makeHeaderFetcher("valid", blocks, -gatherSlack)
	bodyFetcher := tester.makeBodyFetcher("valid", blocks, 0)

	// Iteratively announce blocks until all are imported
	imported := make(chan interface{})
	tester.fetcher.importedHook = func(header *types.Header, block *types.Block) {
		if block == nil {
			t.Fatalf("Fetcher try to import empty block")
		}
		imported <- block
	}
	for i := len(hashes) - 2; i >= 0; i-- {
		tester.fetcher.Notify("valid", hashes[i], uint64(len(hashes)-i-1), time.Now().Add(-arriveTimeout), headerFetcher, bodyFetcher)
		verifyImportEventPoW(t, imported, true)
	}
	verifyImportDonePoW(t, imported)
	verifyChainHeightPoW(t, tester, uint64(len(hashes)-1))
}

// Tests that if blocks are announced by multiple peers (or even the same buggy
// peer), they will only get downloaded at most once.
func TestPoWConcurrentAnnouncements(t *testing.T) {
	// Create a chain of blocks to import
	targetBlocks := 4 * hashLimit
	hashes, blocks := makeChain(targetBlocks, 0, genesis)

	// Assemble a tester with a built in counter for the requests
	tester := newTesterPoW()
	defer tester.fetcher.Stop()
	firstHeaderFetcher := tester.makeHeaderFetcher("first", blocks, -gatherSlack)
	firstBodyFetcher := tester.makeBodyFetcher("first", blocks, 0)
	secondHeaderFetcher := tester.makeHeaderFetcher("second", blocks, -gatherSlack)
	secondBodyFetcher := tester.makeBodyFetcher("second", blocks, 0)

	var counter atomic.Uint32
	firstHeaderWrapper := func(hash common.Hash, sink chan *eth.Response) (*eth.Request, error) {
		counter.Add(1)
		return firstHeaderFetcher(hash, sink)
	}
	secondHeaderWrapper := func(hash common.Hash, sink chan *eth.Response) (*eth.Request, error) {
		counter.Add(1)
		return secondHeaderFetcher(hash, sink)
	}
	// Iteratively announce blocks until all are imported
	imported := make(chan interface{})
	tester.fetcher.importedHook = func(header *types.Header, block *types.Block) {
		if block == nil {
			t.Fatalf("Fetcher try to import empty block")
		}
		imported <- block
	}
	for i := len(hashes) - 2; i >= 0; i-- {
		tester.fetcher.Notify("first", hashes[i], uint64(len(hashes)-i-1), time.Now().Add(-arriveTimeout), firstHeaderWrapper, firstBodyFetcher)
		tester.fetcher.Notify("second", hashes[i], uint64(len(hashes)-i-1), time.Now().Add(-arriveTimeout+time.Millisecond), secondHeaderWrapper, secondBodyFetcher)
		tester.fetcher.Notify("second", hashes[i], uint64(len(hashes)-i-1), time.Now().Add(-arriveTimeout-time.Millisecond), secondHeaderWrapper, secondBodyFetcher)
		verifyImportEventPoW(t, imported, true)
	}
	verifyImportDonePoW(t, imported)

	// Make sure no blocks were retrieved twice
	if c := int(counter.Load()); c != targetBlocks {
		t.Fatalf("retrieval count mismatch: have %v, want %v", c, targetBlocks)
	}
	verifyChainHeightPoW(t, tester, uint64(len(hashes)-1))
}

// Tests that blocks arriving from various sources (multiple propagations, hash
// announces, etc) do not get scheduled for import multiple times.
func TestPoWImportDeduplication(t *testing.T) {
	// Create two blocks to import (one for duplication, the other for stalling)
	hashes, blocks := makeChain(2, 0, genesis)

	// Create the tester and wrap the importer with a counter
	tester := newTesterPoW()
	defer tester.fetcher.Stop()
	headerFetcher := tester.makeHeaderFetcher("valid", blocks, -gatherSlack)
	bodyFetcher := tester.makeBodyFetcher("valid", blocks, 0)

	var counter atomic.Uint32
	tester.fetcher.insertChain = func(blocks types.Blocks) (int, error) {
		counter.Add(uint32(len(blocks)))
		return tester.insertChain(blocks)
	}
	// Instrument the fetching and imported events
	fetching := make(chan []common.Hash)
	imported := make(chan interface{}, len(hashes)-1)
	tester.fetcher.fetchingHook = func(hashes []common.Hash) { fetching <- hashes }
	tester.fetcher.importedHook = func(header *types.Header, block *types.Block) { imported <- block }

	// Announce the duplicating block, wait for retrieval, and also propagate directly
	tester.fetcher.Notify("valid", hashes[0], 1, time.Now().Add(-arriveTimeout), headerFetcher, bodyFetcher)
	<-fetching

	tester.fetcher.Enqueue("valid", blocks[hashes[0]])
	tester.fetcher.Enqueue("valid", blocks[hashes[0]])
	tester.fetcher.Enqueue("valid", blocks[hashes[0]])

	// Fill the missing block directly as if propagated, and check import uniqueness
	tester.fetcher.Enqueue("valid", blocks[hashes[1]])
	verifyImportCountPoW(t, imported, 2)

	if c := counter.Load(); c != 2 {
		t.Fatalf("import invocation count mismatch: have %v, want %v", c, 2)
	}
}

// Tests that blocks with numbers much lower or higher than out current head get
// discarded to prevent wasting resources on useless blocks from faulty peers.
func TestPoWDistantPropagationDiscarding(t *testing.T) {
	// Create a long chain to import and define the discard boundaries
	hashes, blocks := makeChain(3*maxQueueDist, 0, genesis)
	head := hashes[len(hashes)/2]

	low, high := len(hashes)/2+maxUncleDist+1, len(hashes)/2-maxQueueDist-1

	// Create a tester and simulate a head block being the middle of the above chain
	tester := newTesterPoW()
	defer tester.fetcher.Stop()

	tester.lock.Lock()
	tester.hashes = []common.Hash{head}
	tester.blocks = map[common.Hash]*types.Block{head: blocks[head]}
	tester.lock.Unlock()

	// Ensure that a block with a lower number than the threshold is discarded
	tester.fetcher.Enqueue("lower", blocks[hashes[low]])
	time.Sleep(10 * time.Millisecond)
	if !tester.fetcher.queue.Empty() {
		t.Fatalf("fetcher queued stale block")
	}
	// Ensure that a block with a higher number than the threshold is discarded
	tester.fetcher.Enqueue("higher", blocks[hashes[high]])
	time.Sleep(10 * time.Millisecond)
	if !tester.fetcher.queue.Empty() {
		t.Fatalf("fetcher queued future block")
	}
}

// Tests that a peer is unable to use unbounded memory with sending infinite
// block announcements to a node, but that even in the face of such an attack,
// the fetcher remains operational.
func TestPoWHashMemoryExhaustionAttack(t *testing.T) {
	// Create a tester with instrumented import hooks
	tester := newTesterPoW()
	defer tester.fetcher.Stop()

	imported, announces := make(chan interface{}), atomic.Int32{}
	tester.fetcher.importedHook = func(header *types.Header, block *types.Block) { imported <- block }
	tester.fetcher.announceChangeHook = func(hash common.Hash, added bool) {
		if added {
			announces.Add(1)
		} else {
			announces.Add(-1)
		}
	}
	// Create a valid chain and an infinite junk chain
	targetBlocks := hashLimit + 2*maxQueueDist
	hashes, blocks := makeChain(targetBlocks, 0, genesis)
	validHeaderFetcher := tester.makeHeaderFetcher("valid", blocks, -gatherSlack)
	validBodyFetcher := tester.makeBodyFetcher("valid", blocks, 0)

	attack, _ := makeChain(targetBlocks, 0, unknownBlock)
	attackerHeaderFetcher := tester.makeHeaderFetcher("attacker", nil, -gatherSlack)
	attackerBodyFetcher := tester.makeBodyFetcher("attacker", nil, 0)

	// Feed the tester a huge hashset from the attacker, and a limited from the valid peer
	for i := 0; i < len(attack); i++ {
		if i < maxQueueDist {
			tester.fetcher.Notify("valid", hashes[len(hashes)-2-i], uint64(i+1), time.Now(), validHeaderFetcher, validBodyFetcher)
		}
		tester.fetcher.Notify("attacker", attack[i], 1 /* don't distance drop */, time.Now(), attackerHeaderFetcher, attackerBodyFetcher)
	}
	if count := announces.Load(); count != hashLimit+maxQueueDist {
		t.Fatalf("queued announce count mismatch: have %d, want %d", count, hashLimit+maxQueueDist)
	}
	// Wait for fetches to complete
	verifyImportCountPoW(t, imported, maxQueueDist)

	// Feed the remaining valid hashes to ensure DOS protection state remains clean
	for i := len(hashes) - maxQueueDist - 2; i >= 0; i-- {
		tester.fetcher.Notify("valid", hashes[i], uint64(len(hashes)-i-1), time.Now().Add(-arriveTimeout), validHeaderFetcher, validBodyFetcher)
		verifyImportEventPoW(t, imported, true)
	}
	verifyImportDonePoW(t, imported)
}

// Tests that blocks sent to the fetcher (either through propagation or via hash
// announces and retrievals) don't pile up indefinitely, exhausting available
// system memory.
func TestPoWBlockMemoryExhaustionAttack(t *testing.T) {
	// Create a tester with instrumented import hooks
	tester := newTesterPoW()
	defer tester.fetcher.Stop()

	imported, enqueued := make(chan interface{}), atomic.Int32{}
	tester.fetcher.importedHook = func(header *types.Header, block *types.Block) { imported <- block }
	tester.fetcher.queueChangeHook = func(hash common.Hash, added bool) {
		if added {
			enqueued.Add(1)
		} else {
			enqueued.Add(-1)
		}
	}
	// Create a valid chain and a batch of dangling (but in range) blocks
	targetBlocks := hashLimit + 2*maxQueueDist
	hashes, blocks := makeChain(targetBlocks, 0, genesis)
	attack := make(map[common.Hash]*types.Block)
	for i := byte(0); len(attack) < blockLimit+2*maxQueueDist; i++ {
		hashes, blocks := makeChain(maxQueueDist-1, i, unknownBlock)
		for _, hash := range hashes[:maxQueueDist-2] {
			attack[hash] = blocks[hash]
		}
	}
	// Try to feed all the attacker blocks make sure only a limited batch is accepted
	for _, block := range attack {
		tester.fetcher.Enqueue("attacker", block)
	}
	time.Sleep(200 * time.Millisecond)
	if queued := enqueued.Load(); queued != blockLimit {
		t.Fatalf("queued block count mismatch: have %d, want %d", queued, blockLimit)
	}
	// Queue up a batch of valid blocks, and check that a new peer is allowed to do so
	for i := 0; i < maxQueueDist-1; i++ {
		tester.fetcher.Enqueue("valid", blocks[hashes[len(hashes)-3-i]])
	}
	time.Sleep(100 * time.Millisecond)
	if queued := enqueued.Load(); queued != blockLimit+maxQueueDist-1 {
		t.Fatalf("queued block count mismatch: have %d, want %d", queued, blockLimit+maxQueueDist-1)
	}
	// Insert the missing piece (and sanity check the import)
	tester.fetcher.Enqueue("valid", blocks[hashes[len(hashes)-2]])
	verifyImportCountPoW(t, imported, maxQueueDist)

	// Insert the remaining blocks in chunks to ensure clean DOS protection
	for i := maxQueueDist; i < len(hashes)-1; i++ {
		tester.fetcher.Enqueue("valid", blocks[hashes[len(hashes)-2-i]])
		verifyImportEventPoW(t, imported, true)
	}
	verifyImportDonePoW(t, imported)
}

// Tests that direct block enqueues (due to block propagation vs. hash announce)
// are correctly scheduled, filling and import queue gaps.
func TestPoWQueueGapFill(t *testing.T) {
	// Create a chain of blocks to import, and choose one to not announce at all
	targetBlocks := maxQueueDist
	hashes, blocks := makeChain(targetBlocks, 0, genesis)
	skip := targetBlocks / 2

	tester := newTesterPoW()
	defer tester.fetcher.Stop()
	headerFetcher := tester.makeHeaderFetcher("valid", blocks, -gatherSlack)
	bodyFetcher := tester.makeBodyFetcher("valid", blocks, 0)

	// Iteratively announce blocks, skipping one entry
	imported := make(chan interface{}, len(hashes)-1)
	tester.fetcher.importedHook = func(header *types.Header, block *types.Block) { imported <- block }

	for i := len(hashes) - 1; i >= 0; i-- {
		if i != skip {
			tester.fetcher.Notify("valid", hashes[i], uint64(len(hashes)-i-1), time.Now().Add(-arriveTimeout), headerFetcher, bodyFetcher)
			time.Sleep(time.Millisecond)
		}
	}
	// Fill the missing block directly as if propagated
	tester.fetcher.Enqueue("valid", blocks[hashes[skip]])
	verifyImportCountPoW(t, imported, len(hashes)-1)
	verifyChainHeightPoW(t, tester, uint64(len(hashes)-1))
}
