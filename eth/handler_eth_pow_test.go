// Copyright 2020 The go-ethereum Authors
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
	"testing"
	"time"

	"math/big"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/forkid"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/eth/protocols/eth"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
)

// newTestHandlerPoWWithBlocks mirrors newTestHandlerWithBlocks, but builds the
// handler as a perpetual PoW chain. IsPow has to be set at construction: it
// decides what newHandler wires up and which handshake runEthPeer runs, so it
// cannot be flipped on a handler that was already built and started.
func newTestHandlerPoWWithBlocks(blocks int) *testHandler {
	db := rawdb.NewMemoryDatabase()
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{testAddr: {Balance: big.NewInt(1000000)}},
	}
	chain, _ := core.NewBlockChain(db, gspec, ethash.NewFaker(), nil)

	_, bs, _ := core.GenerateChainWithGenesis(gspec, ethash.NewFaker(), blocks, nil)
	if _, err := chain.InsertChain(bs); err != nil {
		panic(err)
	}
	txpool := newTestTxPool()

	handler, _ := newHandler(&handlerConfig{
		Database:   db,
		Chain:      chain,
		TxPool:     txpool,
		Network:    1,
		Sync:       ethconfig.FullSync,
		BloomCache: 1,
		IsPow:      true,
	})
	handler.Start(1000)

	return &testHandler{
		db:      db,
		chain:   chain,
		txpool:  txpool,
		handler: handler,
	}
}

// testEthHandlerPoW is testEthHandler with the block broadcast feed the PoW
// path needs. Everything other than NewBlock is left to the embedded handler.
type testEthHandlerPoW struct {
	testEthHandler
	blockBroadcasts event.Feed
}

func (h *testEthHandlerPoW) Handle(peer *eth.Peer, packet eth.Packet) error {
	switch packet := packet.(type) {
	case *eth.NewBlockPacket:
		h.blockBroadcasts.Send(packet.Block)
		return nil

	default:
		return h.testEthHandler.Handle(peer, packet)
	}
}

// Tests that blocks are broadcast to a sqrt number of peers only.
func TestBroadcastBlock1Peer(t *testing.T)    { testBroadcastBlock(t, 1, 1) }
func TestBroadcastBlock2Peers(t *testing.T)   { testBroadcastBlock(t, 2, 1) }
func TestBroadcastBlock3Peers(t *testing.T)   { testBroadcastBlock(t, 3, 1) }
func TestBroadcastBlock4Peers(t *testing.T)   { testBroadcastBlock(t, 4, 2) }
func TestBroadcastBlock5Peers(t *testing.T)   { testBroadcastBlock(t, 5, 2) }
func TestBroadcastBlock8Peers(t *testing.T)   { testBroadcastBlock(t, 9, 3) }
func TestBroadcastBlock12Peers(t *testing.T)  { testBroadcastBlock(t, 12, 3) }
func TestBroadcastBlock16Peers(t *testing.T)  { testBroadcastBlock(t, 16, 4) }
func TestBroadcastBloc26Peers(t *testing.T)   { testBroadcastBlock(t, 26, 5) }
func TestBroadcastBlock100Peers(t *testing.T) { testBroadcastBlock(t, 100, 10) }

func testBroadcastBlock(t *testing.T, peers, bcasts int) {
	t.Parallel()

	// Create a source handler to broadcast blocks from and a number of sinks
	// to receive them.
	source := newTestHandlerPoWWithBlocks(1)
	defer source.close()

	sinks := make([]*testEthHandlerPoW, peers)
	for i := 0; i < len(sinks); i++ {
		sinks[i] = new(testEthHandlerPoW)
	}
	// Interconnect all the sink handlers with the source handler
	var (
		genesis = source.chain.Genesis()
		td      = source.chain.GetTd(genesis.Hash(), genesis.NumberU64())
	)
	for i, sink := range sinks {
		sourcePipe, sinkPipe := p2p.MsgPipe()
		defer sourcePipe.Close()
		defer sinkPipe.Close()

		sourcePeer := eth.NewPeer(eth.ETH68, p2p.NewPeerPipe(enode.ID{byte(i)}, "", nil, sourcePipe), sourcePipe, nil, nil)
		sinkPeer := eth.NewPeer(eth.ETH68, p2p.NewPeerPipe(enode.ID{0}, "", nil, sinkPipe), sinkPipe, nil, nil)
		defer sourcePeer.Close()
		defer sinkPeer.Close()

		go source.handler.runEthPeer(sourcePeer, func(peer *eth.Peer) error {
			return eth.Handle((*ethHandler)(source.handler), peer)
		})
		if err := sinkPeer.Handshake68PoW(1, td, genesis.Hash(), genesis.Hash(), forkid.NewIDWithChain(source.chain), forkid.NewFilter(source.chain)); err != nil {
			t.Fatalf("failed to run protocol handshake")
		}
		go eth.Handle(sink, sinkPeer)
	}
	// Subscribe to all the transaction pools
	blockChs := make([]chan *types.Block, len(sinks))
	for i := 0; i < len(sinks); i++ {
		blockChs[i] = make(chan *types.Block, 1)
		defer close(blockChs[i])

		sub := sinks[i].blockBroadcasts.Subscribe(blockChs[i])
		defer sub.Unsubscribe()
	}
	// Initiate a block propagation across the peers
	time.Sleep(100 * time.Millisecond)
	header := source.chain.CurrentBlock()
	source.handler.BroadcastBlock(source.chain.GetBlock(header.Hash(), header.Number.Uint64()), true)

	// Iterate through all the sinks and ensure the correct number got the block
	done := make(chan struct{}, peers)
	for _, ch := range blockChs {
		go func() {
			<-ch
			done <- struct{}{}
		}()
	}
	var received int
	for {
		select {
		case <-done:
			received++

		case <-time.After(100 * time.Millisecond):
			if received != bcasts {
				t.Errorf("broadcast count mismatch: have %d, want %d", received, bcasts)
			}
			return
		}
	}
}
