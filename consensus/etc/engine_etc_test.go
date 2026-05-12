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

package etc

import (
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// TestETCEngineDelegatesOperationalInterfaces verifies that *ETCEngine
// exposes the four ethash operational methods that eth.Ethereum and Miner
// detect via type assertion (APIs, SetThreads, Threads, Hashrate). Without
// these delegations, eth_getWork / eth_submitWork / ethash_submitHashrate
// are never registered and SetThreads / Hashrate become no-ops.
func TestETCEngineDelegatesOperationalInterfaces(t *testing.T) {
	var e any = NewFaker(params.ClassicChainConfig)
	defer e.(*ETCEngine).Close()

	if _, ok := e.(interface {
		APIs(consensus.ChainHeaderReader) []rpc.API
	}); !ok {
		t.Error("*ETCEngine does not satisfy APIs(consensus.ChainHeaderReader) []rpc.API")
	}
	if _, ok := e.(interface{ SetThreads(int) }); !ok {
		t.Error("*ETCEngine does not satisfy SetThreads(int)")
	}
	if _, ok := e.(interface{ Threads() int }); !ok {
		t.Error("*ETCEngine does not satisfy Threads() int")
	}
	if _, ok := e.(interface{ Hashrate() float64 }); !ok {
		t.Error("*ETCEngine does not satisfy Hashrate() float64 (must match ethash signature)")
	}
}

// TestETCEngineAPIsExposeBothNamespaces checks that APIs() returns the two
// namespaces ethash registers (eth and ethash) so eth_getWork and
// ethash_getWork both resolve once the engine is wired into eth.Ethereum.
func TestETCEngineAPIsExposeBothNamespaces(t *testing.T) {
	e := NewFaker(params.ClassicChainConfig)
	defer e.Close()

	apis := e.APIs(nil)
	if len(apis) == 0 {
		t.Fatal("ETCEngine.APIs returned empty slice; getWork/submitWork won't be registered")
	}
	got := map[string]bool{}
	for _, a := range apis {
		got[a.Namespace] = true
	}
	for _, want := range []string{"eth", "ethash"} {
		if !got[want] {
			t.Errorf("ETCEngine.APIs missing %q namespace; have %v", want, got)
		}
	}
}

// TestETCEngineForwardsNotifyToEthash verifies that the notify URL list
// passed to etc.New reaches the underlying ethash remote sealer. Without
// the wiring, --miner.notify is parsed and stored on miner.Config but the
// engine is constructed with nil and remote miners are never told about
// new work.
func TestETCEngineForwardsNotifyToEthash(t *testing.T) {
	hit := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.Copy(io.Discard, req.Body)
		select {
		case hit <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	e := New(params.ClassicChainConfig, ethash.Config{PowMode: ethash.ModeTest}, []string{server.URL}, false)
	defer e.Close()

	header := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}
	if err := e.Seal(nil, types.NewBlockWithHeader(header), nil, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	select {
	case <-hit:
	case <-time.After(3 * time.Second):
		t.Fatal("notify URL was not contacted after Seal; etc.New is not forwarding notify to ethash")
	}
}

// TestETCEngineSetThreadsRoundTrip verifies that SetThreads is not a no-op
// — calling it propagates to the inner ethash engine and Threads() reads
// back the value.
func TestETCEngineSetThreadsRoundTrip(t *testing.T) {
	e := NewFaker(params.ClassicChainConfig)
	defer e.Close()

	e.SetThreads(3)
	if got := e.Threads(); got != 3 {
		t.Fatalf("ETCEngine.Threads after SetThreads(3) = %d, want 3", got)
	}
	e.SetThreads(0)
	if got := e.Threads(); got != 0 {
		t.Fatalf("ETCEngine.Threads after SetThreads(0) = %d, want 0", got)
	}
}
