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

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

type preserveTestReader struct {
	tds map[common.Hash]*big.Int
}

func (r *preserveTestReader) Config() *params.ChainConfig            { return params.ClassicChainConfig }
func (r *preserveTestReader) GetTd(h common.Hash, _ uint64) *big.Int { return r.tds[h] }

// TestForkChoicePreserveEqualTD verifies the equal-TD, same-height tie-break honours the
// preserve callback: a preserved block is kept and a preserved competitor is adopted,
// deterministically (not the nil-preserve 50/50 fallback).
func TestForkChoicePreserveEqualTD(t *testing.T) {
	current := &types.Header{Number: big.NewInt(10), Extra: []byte("current")}
	extern := &types.Header{Number: big.NewInt(10), Extra: []byte("extern")}

	reader := &preserveTestReader{tds: map[common.Hash]*big.Int{
		current.Hash(): big.NewInt(1000),
		extern.Hash():  big.NewInt(1000), // equal TD, same height -> tie-break
	}}

	// Our own block is the local one: the tie must not reorg away from it.
	keepLocal := NewForkChoice(reader, func(h *types.Header) bool { return h.Hash() == current.Hash() })
	for i := 0; i < 64; i++ { // repeat: a nil-preserve fallback would flip ~half the time
		if needed, err := keepLocal.ReorgNeeded(current, extern); err != nil || needed {
			t.Fatalf("preserved local head: expected no reorg, got needed=%v err=%v", needed, err)
		}
	}

	// The competitor is the local one: the tie must reorg toward it.
	takeExtern := NewForkChoice(reader, func(h *types.Header) bool { return h.Hash() == extern.Hash() })
	for i := 0; i < 64; i++ {
		if needed, err := takeExtern.ReorgNeeded(current, extern); err != nil || !needed {
			t.Fatalf("preserved competitor: expected reorg, got needed=%v err=%v", needed, err)
		}
	}
}
