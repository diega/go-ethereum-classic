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

// Package etc implements the Ethereum Classic consensus engine.
// It wraps the standard ethash engine with ETC-specific rules:
// - ECIP-1017: Era-based monetary policy (20% reduction every era)
// - ECIP-1010/1041: Difficulty bomb handling (DieHard pause, Gotham delay, disposal)
// - ECIP-1099: Etchash (60k block epochs instead of 30k)
// - London fork without EIP-1559 (Mystique)
package etc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"golang.org/x/crypto/sha3"
)

// ETCEngine wraps the standard ethash engine with ETC-specific consensus rules.
type ETCEngine struct {
	inner  *ethash.Ethash
	config *params.ChainConfig
}

// New creates a new ETC consensus engine wrapping the provided ethash engine.
func New(config *params.ChainConfig, inner *ethash.Ethash) *ETCEngine {
	return &ETCEngine{
		inner:  inner,
		config: config,
	}
}

// Author implements consensus.Engine, returning the header's coinbase.
func (e *ETCEngine) Author(header *types.Header) (common.Address, error) {
	return e.inner.Author(header)
}

// VerifyHeader checks whether a header conforms to the ETC consensus rules.
// The key difference from ETH is that ETC's London fork (Mystique) does NOT
// include EIP-1559, so BaseFee should be nil even after LondonBlock.
func (e *ETCEngine) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header) error {
	// Short circuit if the header is known
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return e.verifyHeader(chain, header, parent)
}

// verifyHeader performs ETC-specific header verification.
func (e *ETCEngine) verifyHeader(chain consensus.ChainHeaderReader, header, parent *types.Header) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}
	// Verify the header's timestamp
	if header.Time <= parent.Time {
		return errors.New("timestamp older than parent")
	}
	// Verify the block's difficulty based on its timestamp and parent's difficulty
	expected := e.CalcDifficulty(chain, header.Time, parent)
	if expected.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
	}
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit > params.MaxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	// ETC-specific: London (Mystique) does NOT include EIP-1559
	// BaseFee should always be nil for ETC chains
	if header.BaseFee != nil {
		return fmt.Errorf("invalid baseFee: ETC does not support EIP-1559, have %d, expected nil", header.BaseFee)
	}
	// Verify gas limit follows the standard rules (no EIP-1559 gas elasticity)
	if err := misc.VerifyGaslimit(parent.GasLimit, header.GasLimit); err != nil {
		return err
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the non-existence of post-merge fields (ETC is perpetual PoW)
	if header.WithdrawalsHash != nil {
		return fmt.Errorf("invalid withdrawalsHash: have %x, expected nil", header.WithdrawalsHash)
	}
	if header.ExcessBlobGas != nil {
		return fmt.Errorf("invalid excessBlobGas: have %d, expected nil", header.ExcessBlobGas)
	}
	if header.BlobGasUsed != nil {
		return fmt.Errorf("invalid blobGasUsed: have %d, expected nil", header.BlobGasUsed)
	}
	if header.ParentBeaconRoot != nil {
		return fmt.Errorf("invalid parentBeaconRoot: have %x, expected nil", header.ParentBeaconRoot)
	}
	return nil
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers concurrently.
func (e *ETCEngine) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))

	go func() {
		for i, header := range headers {
			var parent *types.Header
			if i == 0 {
				parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
			} else if headers[i-1].Hash() == headers[i].ParentHash {
				parent = headers[i-1]
			}
			var err error
			if parent == nil {
				err = consensus.ErrUnknownAncestor
			} else {
				err = e.verifyHeader(chain, header, parent)
			}
			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// VerifyUncles verifies that the given block's uncles conform to consensus rules.
func (e *ETCEngine) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return e.inner.VerifyUncles(chain, block)
}

// Prepare initializes the consensus fields of a block header according to ETC rules.
func (e *ETCEngine) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = e.CalcDifficulty(chain, header.Time, parent)
	return nil
}

// Finalize implements consensus.Engine, accumulating block and uncle rewards
// according to ECIP-1017 era-based monetary policy.
func (e *ETCEngine) Finalize(chain consensus.ChainHeaderReader, header *types.Header, stateDB vm.StateDB, body *types.Body) {
	accumulateRewardsETC(e.config, stateDB, header, body.Uncles)
}

// FinalizeAndAssemble implements consensus.Engine, running post-transaction
// state modifications and assembling the final block.
func (e *ETCEngine) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, stateDB *state.StateDB, body *types.Body, receipts []*types.Receipt) (*types.Block, error) {
	if len(body.Withdrawals) > 0 {
		return nil, errors.New("ETC does not support withdrawals")
	}
	// Finalize block
	e.Finalize(chain, header, stateDB, body)

	// Assign the final state root to header
	header.Root = stateDB.IntermediateRoot(e.config.IsEIP158(header.Number))

	// Assemble and return the final block
	return types.NewBlock(header, &types.Body{Transactions: body.Transactions, Uncles: body.Uncles}, receipts, trie.NewStackTrie(nil)), nil
}

// Seal generates a new sealing request for the given input block.
// For ETC, this panics as real PoW sealing is not supported in this implementation.
func (e *ETCEngine) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	return e.inner.Seal(chain, block, results, stop)
}

// SealHash returns the hash of a block prior to it being sealed.
func (e *ETCEngine) SealHash(header *types.Header) common.Hash {
	hasher := sha3.NewLegacyKeccak256()

	enc := []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra,
	}
	// ETC does not have BaseFee (no EIP-1559)
	rlp.Encode(hasher, enc)
	var hash common.Hash
	hasher.Sum(hash[:0])
	return hash
}

// CalcDifficulty implements the ETC difficulty adjustment algorithm.
// This handles ECIP-1010 (DieHard pause), Gotham delay, and ECIP-1041 (bomb disposal).
func (e *ETCEngine) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return CalcDifficultyETC(e.config, time, parent)
}

// Close terminates any background threads maintained by the consensus engine.
func (e *ETCEngine) Close() error {
	return e.inner.Close()
}

// Constants for difficulty calculation
var (
	big1       = big.NewInt(1)
	big2       = big.NewInt(2)
	big9       = big.NewInt(9)
	big10      = big.NewInt(10)
	bigMinus99 = big.NewInt(-99)

	expDiffPeriod = big.NewInt(100000)
)

// CalcDifficultyETC is the ETC difficulty adjustment algorithm.
// It handles the various ETC-specific bomb modifications:
// - ECIP-1010: DieHard bomb pause at block 3M
// - Gotham: Bomb delay after pause
// - ECIP-1041: Bomb disposal at block 5.9M
func CalcDifficultyETC(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
	next := new(big.Int).Add(parent.Number, big1)

	// Select the appropriate difficulty calculator
	if config.IsConstantinople(next) {
		return calcDifficultyByzantiumETC(config, time, parent)
	}
	if config.IsByzantium(next) {
		return calcDifficultyByzantiumETC(config, time, parent)
	}
	if config.IsHomestead(next) {
		return calcDifficultyHomesteadETC(config, time, parent)
	}
	return calcDifficultyFrontierETC(time, parent)
}

// calcDifficultyByzantiumETC is the Byzantium+ difficulty for ETC with bomb handling.
func calcDifficultyByzantiumETC(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
	bigTime := new(big.Int).SetUint64(time)
	bigParentTime := new(big.Int).SetUint64(parent.Time)

	x := new(big.Int)
	y := new(big.Int)

	// (2 if len(parent_uncles) else 1) - (block_timestamp - parent_timestamp) // 9
	x.Sub(bigTime, bigParentTime)
	x.Div(x, big9)
	if parent.UncleHash == types.EmptyUncleHash {
		x.Sub(big1, x)
	} else {
		x.Sub(big2, x)
	}
	// max(..., -99)
	if x.Cmp(bigMinus99) < 0 {
		x.Set(bigMinus99)
	}
	// parent_diff + (parent_diff / 2048 * max(...))
	y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
	x.Mul(y, x)
	x.Add(parent.Difficulty, x)

	// minimum difficulty
	if x.Cmp(params.MinimumDifficulty) < 0 {
		x.Set(params.MinimumDifficulty)
	}

	// Add bomb component (ETC-specific handling)
	bombComponent := calcBombComponentETC(config, parent.Number)
	x.Add(x, bombComponent)

	return x
}

// calcDifficultyHomesteadETC is the Homestead difficulty for ETC.
func calcDifficultyHomesteadETC(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
	bigTime := new(big.Int).SetUint64(time)
	bigParentTime := new(big.Int).SetUint64(parent.Time)

	x := new(big.Int)
	y := new(big.Int)

	// 1 - (block_timestamp - parent_timestamp) // 10
	x.Sub(bigTime, bigParentTime)
	x.Div(x, big10)
	x.Sub(big1, x)

	// max(1 - ..., -99)
	if x.Cmp(bigMinus99) < 0 {
		x.Set(bigMinus99)
	}
	// parent_diff + parent_diff // 2048 * max(...)
	y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
	x.Mul(y, x)
	x.Add(parent.Difficulty, x)

	// minimum difficulty
	if x.Cmp(params.MinimumDifficulty) < 0 {
		x.Set(params.MinimumDifficulty)
	}

	// Add bomb component
	bombComponent := calcBombComponentETC(config, parent.Number)
	x.Add(x, bombComponent)

	return x
}

// calcDifficultyFrontierETC is the Frontier difficulty for ETC.
func calcDifficultyFrontierETC(time uint64, parent *types.Header) *big.Int {
	diff := new(big.Int)
	adjust := new(big.Int).Div(parent.Difficulty, params.DifficultyBoundDivisor)
	bigTime := new(big.Int).SetUint64(time)
	bigParentTime := new(big.Int).SetUint64(parent.Time)

	if bigTime.Sub(bigTime, bigParentTime).Cmp(params.DurationLimit) < 0 {
		diff.Add(parent.Difficulty, adjust)
	} else {
		diff.Sub(parent.Difficulty, adjust)
	}
	if diff.Cmp(params.MinimumDifficulty) < 0 {
		diff.Set(params.MinimumDifficulty)
	}

	// Frontier bomb (no ETC modifications needed pre-DieHard)
	periodCount := new(big.Int).Add(parent.Number, big1)
	periodCount.Div(periodCount, expDiffPeriod)
	if periodCount.Cmp(big1) > 0 {
		y := new(big.Int).Sub(periodCount, big2)
		y.Exp(big2, y, nil)
		diff.Add(diff, y)
	}
	return diff
}

// calcBombComponentETC calculates the difficulty bomb component for ETC.
// ECIP-1010: Pause bomb at block 3M (DieHard)
// ECIP-1041: Remove bomb at block 5.9M
func calcBombComponentETC(config *params.ChainConfig, parentNumber *big.Int) *big.Int {
	// next is the block number we're calculating difficulty for
	next := new(big.Int).Add(parentNumber, big1)

	// ECIP-1041: Bomb completely removed
	if config.ECIP1041Block != nil && next.Cmp(config.ECIP1041Block) >= 0 {
		return big.NewInt(0)
	}

	// Calculate the fake block number for bomb calculation
	fakeBlockNumber := new(big.Int).Set(next)

	// ECIP-1010: DieHard bomb pause
	// The pause applies starting AT the transition block, not after it
	if config.ECIP1010Transition != nil && next.Cmp(config.ECIP1010Transition) >= 0 {
		if config.ECIP1010Length != nil {
			// Pause the bomb: use block number 3M for all blocks between 3M and 3M+pause_length
			pauseEnd := new(big.Int).Add(config.ECIP1010Transition, config.ECIP1010Length)
			if next.Cmp(pauseEnd) < 0 {
				// During pause: freeze at DieHard block
				fakeBlockNumber.Set(config.ECIP1010Transition)
			} else {
				// After pause: resume with delay
				delay := new(big.Int).Sub(next, pauseEnd)
				fakeBlockNumber.Add(config.ECIP1010Transition, delay)
			}
		}
	}

	// Calculate exponential bomb component
	periodCount := new(big.Int).Div(fakeBlockNumber, expDiffPeriod)
	if periodCount.Cmp(big1) > 0 {
		y := new(big.Int).Sub(periodCount, big2)
		return y.Exp(big2, y, nil)
	}
	return big.NewInt(0)
}

// ECIP-1017 block rewards
var (
	// Era 0 (blocks 0 - 4,999,999): 5 ETC
	era0BlockReward = uint256.NewInt(5e+18)
	// Reduction factor: 0.8 (20% reduction per era)
	// Era 1: 4 ETC, Era 2: 3.2 ETC, Era 3: 2.56 ETC, etc.
)

// accumulateRewardsETC credits the coinbase with ECIP-1017 era-based rewards.
func accumulateRewardsETC(config *params.ChainConfig, stateDB vm.StateDB, header *types.Header, uncles []*types.Header) {
	// Get current era
	era := getBlockEra(header.Number, config.ECIP1017EraRounds)

	// Calculate block reward for this era
	blockReward := getBlockWinnerRewardByEra(era)

	// Accumulate the rewards for the miner
	reward := new(uint256.Int).Set(blockReward)

	// Uncle rewards
	r := new(uint256.Int)
	hNum, _ := uint256.FromBig(header.Number)
	for _, uncle := range uncles {
		uNum, _ := uint256.FromBig(uncle.Number)
		// Uncle reward: (8 - (block - uncle_block)) / 8 * block_reward
		r.AddUint64(uNum, 8)
		r.Sub(r, hNum)
		r.Mul(r, blockReward)
		r.Rsh(r, 3) // Divide by 8
		stateDB.AddBalance(uncle.Coinbase, r, tracing.BalanceIncreaseRewardMineUncle)

		// Miner gets 1/32 of block reward per uncle
		r.Rsh(blockReward, 5) // Divide by 32
		reward.Add(reward, r)
	}
	stateDB.AddBalance(header.Coinbase, reward, tracing.BalanceIncreaseRewardMineBlock)
}

// getBlockEra returns the era number for a given block number.
// Era 0: blocks 0 to eraRounds-1
// Era 1: blocks eraRounds to 2*eraRounds-1
// etc.
func getBlockEra(blockNum *big.Int, eraRounds *big.Int) *big.Int {
	if eraRounds == nil || eraRounds.Sign() <= 0 {
		return big.NewInt(0)
	}
	if blockNum.Sign() <= 0 {
		return big.NewInt(0)
	}
	// era = (blockNum - 1) / eraRounds
	// We subtract 1 so that era changes happen AT the boundary, not after
	era := new(big.Int).Sub(blockNum, big1)
	era.Div(era, eraRounds)
	return era
}

// getBlockWinnerRewardByEra calculates the block reward for a given era.
// ECIP-1017: reward = 5 ETC * (4/5)^era = 5 ETC * 0.8^era
func getBlockWinnerRewardByEra(era *big.Int) *uint256.Int {
	if era.Sign() <= 0 {
		return new(uint256.Int).Set(era0BlockReward)
	}

	// For efficiency, handle small era numbers directly
	eraU64 := era.Uint64()
	if era.IsUint64() && eraU64 < 50 { // Reasonable upper bound
		reward := new(uint256.Int).Set(era0BlockReward)
		for i := uint64(0); i < eraU64; i++ {
			// Multiply by 4, divide by 5 (= multiply by 0.8)
			reward.Mul(reward, uint256.NewInt(4))
			reward.Div(reward, uint256.NewInt(5))
		}
		return reward
	}

	// For very large era numbers (theoretical), use big.Int arithmetic
	// This is unlikely to be reached in practice
	reward := new(big.Int).Set(era0BlockReward.ToBig())
	four := big.NewInt(4)
	five := big.NewInt(5)
	for i := big.NewInt(0); i.Cmp(era) < 0; i.Add(i, big1) {
		reward.Mul(reward, four)
		reward.Div(reward, five)
	}
	result, _ := uint256.FromBig(reward)
	return result
}

// GetEpochLength returns the DAG epoch length for a given block number.
// ECIP-1099: After the transition, epochs are 60,000 blocks instead of 30,000.
func GetEpochLength(config *params.ChainConfig, blockNumber uint64) uint64 {
	if config.ECIP1099Block != nil && blockNumber >= config.ECIP1099Block.Uint64() {
		return 60000 // Etchash
	}
	return 30000 // Ethash
}
