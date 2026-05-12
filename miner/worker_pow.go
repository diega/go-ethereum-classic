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

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// resultQueueSize is the size of channel listening to sealing result.
	resultQueueSize = 10

	// txChanSize is the size of channel listening to NewTxsEvent.
	txChanSize = 4096

	// chainHeadChanSize is the size of channel listening to ChainHeadEvent.
	chainHeadChanSize = 10

	// chainSideChanSize is the size of channel listening to side-block events.
	chainSideChanSize = 10

	// staleThreshold is the maximum depth of an acceptable stale uncle.
	staleThreshold = 7

	// sealingLogAtDepth is the number of confirmations before a sealing log is emitted.
	sealingLogAtDepth = 7

	// minRecommitInterval is the minimal time interval to recreate the sealing block with
	// any newly arrived transactions.
	minRecommitInterval = 1 * time.Second
)

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	receipts  []*types.Receipt
	block     *types.Block
	createdAt time.Time
}

// newWorkReq represents a request for new sealing work submitting with relative interrupt notifier.
type newWorkReq struct {
	interrupt *atomic.Int32
	timestamp int64
}

// powWorker is the PoW mining worker that runs the sealing loops.
// It wraps the existing Miner's block-building functions and adds
// the active mining loops removed in upstream's purge3.
type powWorker struct {
	miner  *Miner
	engine consensus.Engine
	chain  *core.BlockChain
	txpool *txpool.TxPool
	mux    *event.TypeMux

	// Channels
	newWorkCh          chan *newWorkReq
	taskCh             chan *task
	resultCh           chan *types.Block
	startCh            chan struct{}
	exitCh             chan struct{}
	resubmitIntervalCh chan time.Duration
	uncleRecommitCh    chan struct{} // signals newWorkLoop to regenerate work when a fresh uncle arrives

	// Subscriptions
	txsCh        chan core.NewTxsEvent
	txsSub       event.Subscription
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription
	chainSideCh  chan core.ChainSideEvent
	chainSideSub event.Subscription

	// Uncle candidates sourced from the fork-choice side-block feed. Accessed
	// only from mainLoop (single goroutine), so they need no extra locking.
	localUncles  map[common.Hash]*types.Header
	remoteUncles map[common.Hash]*types.Header

	// Test hooks (nil in production) that let worker_test.go drive the sealing
	// loops deterministically.
	newTaskHook  func(*task)
	skipSealHook func(*task) bool
	fullTaskHook func()

	// State
	running  atomic.Bool
	newTxs   atomic.Int32
	syncing  atomic.Bool
	coinbase common.Address
	mu       sync.RWMutex // protects coinbase

	// Pending tasks for seal result correlation
	pendingMu    sync.RWMutex
	pendingTasks map[common.Hash]*task

	// Block tracking
	unconfirmed *unconfirmedBlocks

	wg sync.WaitGroup
}

// newPowWorker creates a new PoW mining worker.
func newPowWorker(miner *Miner, chain *core.BlockChain, pool *txpool.TxPool, engine consensus.Engine, mux *event.TypeMux) *powWorker {
	w := &powWorker{
		miner:              miner,
		engine:             engine,
		chain:              chain,
		txpool:             pool,
		mux:                mux,
		newWorkCh:          make(chan *newWorkReq),
		taskCh:             make(chan *task),
		resultCh:           make(chan *types.Block, resultQueueSize),
		startCh:            make(chan struct{}, 1),
		exitCh:             make(chan struct{}),
		resubmitIntervalCh: make(chan time.Duration),
		uncleRecommitCh:    make(chan struct{}, 1),
		txsCh:              make(chan core.NewTxsEvent, txChanSize),
		chainHeadCh:        make(chan core.ChainHeadEvent, chainHeadChanSize),
		chainSideCh:        make(chan core.ChainSideEvent, chainSideChanSize),
		localUncles:        make(map[common.Hash]*types.Header),
		remoteUncles:       make(map[common.Hash]*types.Header),
		pendingTasks:       make(map[common.Hash]*task),
		unconfirmed:        newUnconfirmedBlocks(chain, sealingLogAtDepth),
	}
	// Subscribe to events
	w.txsSub = pool.SubscribeTransactions(w.txsCh, false)
	w.chainHeadSub = chain.SubscribeChainHeadEvent(w.chainHeadCh)
	w.chainSideSub = chain.SubscribeChainSideEvent(w.chainSideCh)

	// Sanitize recommit interval
	recommit := miner.config.Recommit
	if recommit < minRecommitInterval {
		recommit = minRecommitInterval
	}

	w.wg.Add(4)
	go w.mainLoop()
	go w.newWorkLoop(recommit)
	go w.taskLoop()
	go w.resultLoop()

	return w
}

// start signals the worker to begin mining.
func (w *powWorker) start() {
	w.running.Store(true)
	select {
	case w.startCh <- struct{}{}:
	default:
	}
}

// stop signals the worker to stop mining.
func (w *powWorker) stop() {
	w.running.Store(false)
}

// isRunning returns whether the worker is currently running.
func (w *powWorker) isRunning() bool {
	return w.running.Load()
}

// close terminates all goroutines and waits for them to finish.
func (w *powWorker) close() {
	w.running.Store(false)
	close(w.exitCh)
	w.wg.Wait()
}

// setEtherbase sets the mining coinbase address.
func (w *powWorker) setEtherbase(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.coinbase = addr
}

// etherbase returns the current mining coinbase address.
func (w *powWorker) etherbase() common.Address {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.coinbase
}

// setRecommitInterval updates the interval for miner sealing work recommitting.
func (w *powWorker) setRecommitInterval(interval time.Duration) {
	select {
	case w.resubmitIntervalCh <- interval:
	case <-w.exitCh:
	}
}

// newWorkLoop is a standalone goroutine to submit new sealing work upon received events.
func (w *powWorker) newWorkLoop(recommit time.Duration) {
	defer w.wg.Done()

	var (
		interrupt *atomic.Int32
		timestamp int64 // timestamp for each round of sealing.
	)

	timer := time.NewTimer(0)
	defer timer.Stop()
	<-timer.C // discard the initial tick

	// commit aborts in-flight transaction execution with given signal and resubmits a new one.
	commit := func(reason int32) {
		if interrupt != nil {
			interrupt.Store(reason)
		}
		interrupt = new(atomic.Int32)
		select {
		case w.newWorkCh <- &newWorkReq{interrupt: interrupt, timestamp: timestamp}:
		case <-w.exitCh:
			return
		}
		timer.Reset(recommit)
	}
	// clearPending drops pending tasks whose blocks are now buried deeper than
	// staleThreshold below the given height, so the map does not grow unbounded.
	clearPending := func(number uint64) {
		w.pendingMu.Lock()
		for h, t := range w.pendingTasks {
			if t.block.NumberU64()+staleThreshold <= number {
				delete(w.pendingTasks, h)
			}
		}
		w.pendingMu.Unlock()
	}

	for {
		select {
		case <-w.startCh:
			clearPending(w.chain.CurrentBlock().Number.Uint64())
			timestamp = time.Now().Unix()
			commit(commitInterruptNewHead)

		case <-w.chainHeadCh:
			clearPending(w.chain.CurrentBlock().Number.Uint64())
			timestamp = time.Now().Unix()
			commit(commitInterruptNewHead)

		case <-timer.C:
			// If sealing is running, resubmit a new work cycle periodically to pull in
			// higher priced transactions. Disable this overhead for pending blocks.
			if w.isRunning() && (w.newTxs.Load() > 0) {
				timestamp = time.Now().Unix()
				commit(commitInterruptResubmit)
				w.newTxs.Store(0)
			}

		case <-w.uncleRecommitCh:
			// A fresh uncle arrived; regenerate work now so it can be folded into the
			// block being sealed (higher uncle reward), mirroring core-geth's
			// recommit-on-side-block instead of waiting for the next resubmit.
			if w.isRunning() {
				timestamp = time.Now().Unix()
				commit(commitInterruptResubmit)
			}

		case interval := <-w.resubmitIntervalCh:
			// Adjust resubmit interval explicitly by the user.
			if interval < minRecommitInterval {
				log.Warn("Sanitizing miner recommit interval", "provided", interval, "updated", minRecommitInterval)
				interval = minRecommitInterval
			}
			log.Info("Miner recommit interval update", "from", recommit, "to", interval)
			recommit = interval

		case <-w.exitCh:
			return
		}
	}
}

// mainLoop is a standalone goroutine to regenerate the sealing task based on the received event.
func (w *powWorker) mainLoop() {
	defer w.wg.Done()
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()
	defer w.chainSideSub.Unsubscribe()

	cleanTicker := time.NewTicker(time.Second * 10)
	defer cleanTicker.Stop()

	for {
		select {
		case req := <-w.newWorkCh:
			w.commitWork(req.interrupt, req.timestamp)

		case ev := <-w.chainSideCh:
			// A block lost the fork choice; keep its header as a possible uncle. If it's
			// new and we're sealing, ask newWorkLoop to regenerate work so the uncle is
			// folded in immediately (like core-geth) rather than at the next resubmit.
			if w.collectUncle(ev.Header) && w.isRunning() {
				select {
				case w.uncleRecommitCh <- struct{}{}:
				default: // a recommit is already pending
				}
			}

		case <-cleanTicker.C:
			// Purge uncle candidates that have grown too old to be included.
			w.pruneStaleUncles()

		case <-w.txsCh:
			// New transactions arrived, mark them for the next sealing round.
			if w.isRunning() {
				w.newTxs.Add(1)
			}

		// System stopped
		case <-w.exitCh:
			return
		case <-w.txsSub.Err():
			return
		case <-w.chainHeadSub.Err():
			return
		case <-w.chainSideSub.Err():
			return
		}
	}
}

// taskLoop is a standalone goroutine to fetch sealing task from the generator and
// push them to consensus engine.
func (w *powWorker) taskLoop() {
	defer w.wg.Done()

	var (
		stopCh chan struct{}
		prev   common.Hash
	)

	// interrupt aborts the in-flight sealing task.
	interrupt := func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	for {
		select {
		case t := <-w.taskCh:
			if w.newTaskHook != nil {
				w.newTaskHook(t)
			}
			// Reject duplicate sealing work due to resubmitting.
			sealHash := w.engine.SealHash(t.block.Header())
			if sealHash == prev {
				continue
			}
			// Interrupt previous sealing operation
			interrupt()
			stopCh = make(chan struct{})
			prev = sealHash

			// Allow tests to skip the (slow) sealing step.
			if w.skipSealHook != nil && w.skipSealHook(t) {
				continue
			}

			w.pendingMu.Lock()
			w.pendingTasks[sealHash] = t
			w.pendingMu.Unlock()

			if err := w.engine.Seal(w.chain, t.block, w.resultCh, stopCh); err != nil {
				log.Warn("Block sealing failed", "err", err)
				w.pendingMu.Lock()
				delete(w.pendingTasks, sealHash)
				w.pendingMu.Unlock()
			}

		case <-w.exitCh:
			interrupt()
			return
		}
	}
}

// resultLoop is a standalone goroutine to handle sealing result submitting
// and flush relative data to the database.
func (w *powWorker) resultLoop() {
	defer w.wg.Done()

	for {
		select {
		case block := <-w.resultCh:
			// Short circuit when receiving empty result.
			if block == nil {
				continue
			}
			// Short circuit when receiving duplicate result caused by resubmitting.
			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
				continue
			}

			var (
				sealhash = w.engine.SealHash(block.Header())
			)
			w.pendingMu.RLock()
			t, exist := w.pendingTasks[sealhash]
			w.pendingMu.RUnlock()
			if !exist {
				log.Error("Block found but no relative pending task", "number", block.Number(), "sealhash", sealhash, "hash", block.Hash())
				continue
			}

			// Commit block to blockchain.
			if _, err := w.chain.InsertChain(types.Blocks{block}); err != nil {
				log.Error("Failed to insert mined block", "err", err)
				continue
			}
			log.Info("Successfully sealed new block", "number", block.Number(), "sealhash", sealhash, "hash", block.Hash(),
				"elapsed", common.PrettyDuration(time.Since(t.createdAt)))

			// Broadcast the block and announce chain insertion event
			w.mux.Post(core.NewMinedBlockEvent{Block: block})

			// Insert into unconfirmed block set
			w.unconfirmed.Insert(block.NumberU64(), block.Hash())

		case <-w.exitCh:
			return
		}
	}
}

// commitWork generates a new sealing work based on the parent block and submits it to the sealer.
func (w *powWorker) commitWork(interrupt *atomic.Int32, timestamp int64) {
	// Abort if we're syncing
	if w.syncing.Load() {
		return
	}
	coinbase := w.etherbase()
	if coinbase == (common.Address{}) {
		if w.isRunning() {
			log.Error("Refusing to mine without etherbase")
		}
		return
	}

	// Construct the sealing task using the existing Miner methods.
	work, err := w.miner.prepareWork(context.Background(), &generateParams{
		timestamp: uint64(timestamp),
		coinbase:  coinbase,
	}, false)
	if err != nil {
		log.Error("Failed to prepare work for sealing", "err", err)
		return
	}

	// Fill transactions.
	err = w.miner.fillTransactions(context.Background(), interrupt, work)
	if err != nil {
		log.Warn("Block building interrupted", "err", err)
	}

	// Gather up to two valid uncles for the block being sealed.
	uncles := w.gatherUncles(work.header)

	// Assemble the block.
	body := types.Body{
		Transactions: work.txs,
		Uncles:       uncles,
	}
	block := core.AssembleBlock(w.miner.engine, w.chain, work.header, work.state, &body, work.receipts)

	// Allow tests to observe the full sealing task before it is pushed.
	if w.fullTaskHook != nil {
		w.fullTaskHook()
	}

	// Calculate fees for logging.
	fees := totalFees(block, work.receipts)
	if w.isRunning() {
		log.Info("Commit new sealing work", "number", block.Number(), "sealhash", w.engine.SealHash(block.Header()),
			"uncles", len(uncles), "txs", work.tcount, "gas", block.GasUsed(), "fees", ethToFloat(fees), "elapsed", common.PrettyDuration(time.Since(time.Unix(timestamp, 0))))
	}

	select {
	case w.taskCh <- &task{receipts: work.receipts, block: block, createdAt: time.Now()}:
	case <-w.exitCh:
	}
}

// ethToFloat converts a wei amount to an ETH float for logging purposes.
func ethToFloat(amount *big.Int) float64 {
	if amount == nil {
		return 0
	}
	f := new(big.Float).SetInt(amount)
	f.Quo(f, new(big.Float).SetInt(big.NewInt(1e18)))
	result, _ := f.Float64()
	return result
}
