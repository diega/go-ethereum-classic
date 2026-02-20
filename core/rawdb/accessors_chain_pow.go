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

package rawdb

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

// ETC: PoW versions of ancient write functions that properly accumulate and write total difficulty.

// WriteAncientBlocksPoW writes entire block data into ancient store and returns the total written size.
// td is the total difficulty of the FIRST block in the slice (including its own difficulty).
func WriteAncientBlocksPoW(db ethdb.AncientWriter, blocks []*types.Block, receipts []rlp.RawValue, td *big.Int) (int64, error) {
	tdSum := new(big.Int).Sub(td, blocks[0].Difficulty())
	return db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for i, block := range blocks {
			header := block.Header()
			tdSum.Add(tdSum, header.Difficulty)
			if err := writeAncientBlockPoW(op, block, header, receipts[i], tdSum); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeAncientBlockPoW(op ethdb.AncientWriteOp, block *types.Block, header *types.Header, receipts rlp.RawValue, td *big.Int) error {
	num := block.NumberU64()
	if err := op.AppendRaw(ChainFreezerHashTable, num, block.Hash().Bytes()); err != nil {
		return fmt.Errorf("can't add block %d hash: %v", num, err)
	}
	if err := op.Append(ChainFreezerHeaderTable, num, header); err != nil {
		return fmt.Errorf("can't append block header %d: %v", num, err)
	}
	if err := op.Append(ChainFreezerBodiesTable, num, block.Body()); err != nil {
		return fmt.Errorf("can't append block body %d: %v", num, err)
	}
	if err := op.Append(ChainFreezerReceiptTable, num, receipts); err != nil {
		return fmt.Errorf("can't append block %d receipts: %v", num, err)
	}
	if err := op.Append(ChainFreezerDifficultyTable, num, td); err != nil {
		return fmt.Errorf("can't append block %d total difficulty: %v", num, err)
	}
	return nil
}

// WriteAncientHeaderChainPoW writes headers with proper TD into ancient store.
// td is the total difficulty of the FIRST header (including its own difficulty).
func WriteAncientHeaderChainPoW(db ethdb.AncientWriter, headers []*types.Header, td *big.Int) (int64, error) {
	tdSum := new(big.Int).Sub(td, headers[0].Difficulty)
	return db.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for _, header := range headers {
			num := header.Number.Uint64()
			tdSum.Add(tdSum, header.Difficulty)
			if err := op.AppendRaw(ChainFreezerHashTable, num, header.Hash().Bytes()); err != nil {
				return fmt.Errorf("can't add block %d hash: %v", num, err)
			}
			if err := op.Append(ChainFreezerHeaderTable, num, header); err != nil {
				return fmt.Errorf("can't append block header %d: %v", num, err)
			}
			if err := op.AppendRaw(ChainFreezerBodiesTable, num, nil); err != nil {
				return fmt.Errorf("can't append block body %d: %v", num, err)
			}
			if err := op.AppendRaw(ChainFreezerReceiptTable, num, nil); err != nil {
				return fmt.Errorf("can't append block %d receipts: %v", num, err)
			}
			if err := op.Append(ChainFreezerDifficultyTable, num, new(big.Int).Set(tdSum)); err != nil {
				return fmt.Errorf("can't append block %d total difficulty: %v", num, err)
			}
		}
		return nil
	})
}
