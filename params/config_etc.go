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

package params

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ETC genesis hashes to enforce below configs on.
var (
	// ClassicGenesisHash is the same as MainnetGenesisHash - ETH and ETC share the same genesis block
	ClassicGenesisHash = common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")
	MordorGenesisHash  = common.HexToHash("0xa68ebde7932eccb177d38d55dcc6461a019dd795a681e59b5a3e4f3a7259a3f1")
)

var (
	// ClassicChainConfig is the chain parameters for Ethereum Classic mainnet.
	ClassicChainConfig = &ChainConfig{
		ChainID:             big.NewInt(61),
		HomesteadBlock:      big.NewInt(1_150_000),
		DAOForkBlock:        nil,                    // ETC did NOT have the DAO fork (nil excludes from Fork ID)
		DAOForkSupport:      false,                  // ETC rejected the DAO fork
		EIP150Block:         big.NewInt(2_500_000),  // ETC Tangerine Whistle
		EIP155Block:         big.NewInt(3_000_000),  // ETC Spurious Dragon (part 1)
		EIP158Block:         big.NewInt(8_772_000),  // ETC Atlantis (delayed EIP-158)
		ByzantiumBlock:      big.NewInt(8_772_000),  // ETC Atlantis
		ConstantinopleBlock: big.NewInt(9_573_000),  // ETC Agharta
		PetersburgBlock:     big.NewInt(9_573_000),  // ETC Agharta
		IstanbulBlock:       big.NewInt(10_500_839), // ETC Phoenix
		MuirGlacierBlock:    nil,                    // ETC doesn't have Muir Glacier
		BerlinBlock:         big.NewInt(13_189_133), // ETC Magneto
		LondonBlock:         big.NewInt(14_525_000), // ETC Mystique (WITHOUT EIP-1559)
		ArrowGlacierBlock:   nil,                    // ETC doesn't have Arrow Glacier
		GrayGlacierBlock:    nil,                    // ETC doesn't have Gray Glacier
		MergeNetsplitBlock:  nil,                    // ETC doesn't have merge
		// Timestamp-based forks - none for ETC (perpetual PoW)
		ShanghaiTime:            nil,
		CancunTime:              nil,
		PragueTime:              nil,
		OsakaTime:               nil,
		AmsterdamTime:           nil,
		UBTTime:                 nil,
		TerminalTotalDifficulty: nil, // ETC is perpetual PoW, no TTD
		// ETC-specific ECIPs
		ECIP1017Block:                big.NewInt(5_000_000),  // Gotham monetary policy
		ECIP1041Block:                big.NewInt(5_900_000),  // ECIP-1041 bomb disposal
		ECIP1099Block:                big.NewInt(11_700_000), // Etchash (60k epochs)
		SpiralBlock:                  big.NewInt(19_250_000), // Spiral (partial Shanghai)
		ECIP1017EraRounds:            big.NewInt(5_000_000),  // Era length: 5M blocks
		ECIP1010Transition:           big.NewInt(3_000_000),  // DieHard bomb pause
		ECIP1010Length:               big.NewInt(2_000_000),  // Pause duration
		ECBP1100Transition:           big.NewInt(11_380_000), // MESS activation
		ECBP1100DeactivateTransition: big.NewInt(19_250_000), // MESS deactivation (same as Spiral)
		// Consensus engine
		Ethash: new(EthashConfig),
	}

	// MordorChainConfig is the chain parameters for Mordor testnet (ETC testnet).
	MordorChainConfig = &ChainConfig{
		ChainID:             big.NewInt(63),
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        nil, // Mordor never had DAO fork
		DAOForkSupport:      false,
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(301_243),   // Mordor Agharta
		PetersburgBlock:     big.NewInt(301_243),   // Mordor Agharta
		IstanbulBlock:       big.NewInt(999_983),   // Mordor Phoenix
		MuirGlacierBlock:    nil,                   // No Muir Glacier
		BerlinBlock:         big.NewInt(3_985_893), // Mordor Magneto
		LondonBlock:         big.NewInt(5_520_000), // Mordor Mystique (WITHOUT EIP-1559)
		ArrowGlacierBlock:   nil,
		GrayGlacierBlock:    nil,
		MergeNetsplitBlock:  nil,
		// Timestamp-based forks - none for ETC
		ShanghaiTime:            nil,
		CancunTime:              nil,
		PragueTime:              nil,
		OsakaTime:               nil,
		AmsterdamTime:           nil,
		UBTTime:                 nil,
		TerminalTotalDifficulty: nil, // Perpetual PoW
		// ETC-specific ECIPs
		ECIP1017Block:      big.NewInt(0),         // Active from genesis
		ECIP1041Block:      big.NewInt(0),         // Bomb disposed from genesis
		ECIP1099Block:      big.NewInt(2_520_000), // Etchash from Magneto
		SpiralBlock:        big.NewInt(9_957_000), // Mordor Spiral
		ECIP1017EraRounds:  big.NewInt(2_000_000), // Era length: 2M blocks
		ECIP1010Transition: nil,                   // Mordor never had bomb pause
		ECIP1010Length:     nil,
		// Consensus engine
		Ethash: new(EthashConfig),
	}
)

// IsClassic returns whether this config is one of the well-known Ethereum
// Classic networks: ETC mainnet (ChainID 61) or Mordor testnet (ChainID 63).
// It identifies presets for datadir/genesis resolution and CLI purposes only;
// consensus rules never branch on it. Block-validity behavior is derived from
// the individual config keys (IsPow, IsEIP1559, the ECIP fields), so custom
// chains get ETC semantics by setting those keys, not by borrowing a chain ID.
func (c *ChainConfig) IsClassic() bool {
	if c.ChainID == nil {
		return false
	}
	id := c.ChainID.Uint64()
	return id == 61 || id == 63
}

// IsPow returns whether this chain uses perpetual Proof of Work.
// Currently, ETC is the only supported perpetual PoW chain.
// In the future, this could support other PoW chains.
func (c *ChainConfig) IsPow() bool {
	return c.Ethash != nil && c.TerminalTotalDifficulty == nil
}

// describePoWForks returns the human-readable hard-fork section for a
// perpetual PoW chain. Used by ChainConfig.Description for the --classic /
// --mordor banner, where merge/post-merge sections do not apply and forks
// carry their ETC names. Only non-nil fields are printed, so the helper
// works for both ETC mainnet and Mordor (which omits a few ECIPs).
func (c *ChainConfig) describePoWForks() string {
	s := "Hard forks (block based):\n"
	if c.HomesteadBlock != nil {
		s += fmt.Sprintf(" - Homestead:                   #%-8v\n", c.HomesteadBlock)
	}
	if c.EIP150Block != nil {
		s += fmt.Sprintf(" - Tangerine Whistle:           #%-8v\n", c.EIP150Block)
	}
	if c.ECIP1010Transition != nil {
		s += fmt.Sprintf(" - DieHard:                     #%-8v\n", c.ECIP1010Transition)
	}
	if c.EIP155Block != nil {
		s += fmt.Sprintf(" - Spurious Dragon:             #%-8v\n", c.EIP155Block)
	}
	if c.ECIP1017Block != nil {
		s += fmt.Sprintf(" - Gotham:                      #%-8v\n", c.ECIP1017Block)
	}
	if c.ECIP1041Block != nil {
		s += fmt.Sprintf(" - Bomb disposal:               #%-8v\n", c.ECIP1041Block)
	}
	if c.ByzantiumBlock != nil {
		s += fmt.Sprintf(" - Atlantis:                    #%-8v\n", c.ByzantiumBlock)
	}
	if c.ConstantinopleBlock != nil {
		s += fmt.Sprintf(" - Agharta:                     #%-8v\n", c.ConstantinopleBlock)
	}
	if c.IstanbulBlock != nil {
		s += fmt.Sprintf(" - Phoenix:                     #%-8v\n", c.IstanbulBlock)
	}
	if c.ECIP1099Block != nil {
		s += fmt.Sprintf(" - Etchash:                     #%-8v\n", c.ECIP1099Block)
	}
	if c.BerlinBlock != nil {
		s += fmt.Sprintf(" - Magneto:                     #%-8v\n", c.BerlinBlock)
	}
	if c.LondonBlock != nil {
		s += fmt.Sprintf(" - Mystique:                    #%-8v\n", c.LondonBlock)
	}
	if c.SpiralBlock != nil {
		s += fmt.Sprintf(" - Spiral:                      #%-8v\n", c.SpiralBlock)
	}
	return s
}

// IsSpiral returns whether num is either equal to the Spiral fork block or greater.
func (c *ChainConfig) IsSpiral(num *big.Int) bool {
	return isBlockForked(c.SpiralBlock, num)
}

// IsEIP1559 returns whether EIP-1559 (fee market) is active at the given block.
// To date no perpetual PoW chain (ethash with no terminal total difficulty)
// has activated a fee market: they run London as Mystique, i.e. the London
// opcodes WITHOUT the fee market, so this defaults to false when IsPow. For
// every other chain, IsEIP1559 is equivalent to IsLondon.
//
// This is a default, not a law of nature: a future fee-market activation on
// ETC (e.g. ECIP-1120, which keeps the EIP-1559 mechanics and changes only
// the base-fee disposition) would ship with its own activation key, checked
// here ahead of the PoW default — everything downstream (base fee calc,
// header validation, txpool, signer, RPC) already dispatches on this method.
func (c *ChainConfig) IsEIP1559(num *big.Int) bool {
	if c.IsPow() {
		return false // no perpetual PoW chain has activated a fee market so far
	}
	return c.IsLondon(num)
}

// IsECBP1100 returns true if MESS (ECBP-1100) is configured to be active at the given block.
// - If ECBP1100Transition == nil, returns false (not configured)
// - If ECBP1100Transition <= num < ECBP1100DeactivateTransition, returns true
func (c *ChainConfig) IsECBP1100(num *big.Int) bool {
	// If no config for blocks, MESS is not activated by default
	if c.ECBP1100Transition == nil {
		return false
	}
	// If configured, verify the block has passed activation and not yet deactivated
	return isBlockForked(c.ECBP1100Transition, num) &&
		!isBlockForked(c.ECBP1100DeactivateTransition, num)
}

// IsECIP1017 returns whether num is either equal to the ECIP-1017 transition block or greater.
func (c *ChainConfig) IsECIP1017(num *big.Int) bool {
	return isBlockForked(c.ECIP1017Block, num)
}

// IsECIP1010 returns whether num is either equal to the ECIP-1010 (DieHard) transition block or greater.
func (c *ChainConfig) IsECIP1010(num *big.Int) bool {
	return isBlockForked(c.ECIP1010Transition, num)
}

// IsECIP1041 returns whether num is either equal to the ECIP-1041 (bomb disposal) transition block or greater.
func (c *ChainConfig) IsECIP1041(num *big.Int) bool {
	return isBlockForked(c.ECIP1041Block, num)
}

// IsECIP1099 returns whether num is either equal to the ECIP-1099 (Etchash) transition block or greater.
func (c *ChainConfig) IsECIP1099(num *big.Int) bool {
	return isBlockForked(c.ECIP1099Block, num)
}

// isEIP160 returns true when EIP-160 (EXP gas cost increase) needs manual activation.
// The window only exists on configs that split Spurious Dragon: ETC's Die Hard
// (ECIP-1010) activated EIP-155/160 at block 3M but delayed EIP-161/170 to Atlantis
// (ECIP-1054, block 8.77M), so the jump table needs patching in between. Ethereum's
// EIP-607 activated EIP-155 and EIP-158 at the same block, making the window empty
// there by construction — no chain identity check is needed.
func (c *ChainConfig) isEIP160(num *big.Int) bool {
	return c.IsEIP155(num) && !c.IsEIP158(num)
}

// checkCompatibleETC guards the ETC-specific fork blocks against incompatible changes.
// Upstream's checkCompatible enumerates its own forks but knows nothing about the forks
// getc adds. Those *Block fields enter the fork ID via gatherForks' reflection, so a
// changed Etchash/Spiral/rewards block on an already-advanced DB would otherwise be
// accepted silently (SetupGenesisBlock rewrites the stored config when CheckCompatible
// returns no error).
//
// Enumerated on purpose: go-ethereum has always kept checkCompatible enumerated (only the
// fork ID was moved to reflection), so this mirrors upstream's style and stays rebase-
// friendly. TestETCForkCompatibilityCoverage enforces that every fork-ID *Block is guarded
// here, giving us the fork-ID↔compatibility symmetry that a reflective config system would
// provide — without importing reflection into production code.
//
// MESS (ECBP1100Transition / ECBP1100DeactivateTransition) is intentionally excluded, just
// like it is excluded from the fork ID: it is a fork-choice/reorg policy, not a block-
// validity rule, so changing it cannot retroactively split consensus.
func (c *ChainConfig) checkCompatibleETC(newcfg *ChainConfig, headNumber *big.Int) *ConfigCompatError {
	if isForkBlockIncompatible(c.ECIP1017Block, newcfg.ECIP1017Block, headNumber) {
		return newBlockCompatError("ECIP-1017 (Gotham) fork block", c.ECIP1017Block, newcfg.ECIP1017Block)
	}
	if isForkBlockIncompatible(c.ECIP1041Block, newcfg.ECIP1041Block, headNumber) {
		return newBlockCompatError("ECIP-1041 (bomb disposal) fork block", c.ECIP1041Block, newcfg.ECIP1041Block)
	}
	if isForkBlockIncompatible(c.ECIP1099Block, newcfg.ECIP1099Block, headNumber) {
		return newBlockCompatError("ECIP-1099 (Etchash) fork block", c.ECIP1099Block, newcfg.ECIP1099Block)
	}
	if isForkBlockIncompatible(c.SpiralBlock, newcfg.SpiralBlock, headNumber) {
		return newBlockCompatError("Spiral fork block", c.SpiralBlock, newcfg.SpiralBlock)
	}
	if isForkBlockIncompatible(c.ECIP1010Transition, newcfg.ECIP1010Transition, headNumber) {
		return newBlockCompatError("ECIP-1010 (DieHard) transition", c.ECIP1010Transition, newcfg.ECIP1010Transition)
	}
	// ECIP1017EraRounds / ECIP1010Length are parameters (era length, pause duration), not
	// activation blocks: changing them rewrites the reward schedule / bomb pause retroactively.
	// They don't enter the fork ID, so require equality while their fork is active.
	if c.IsECIP1017(headNumber) && !configBlockEqual(c.ECIP1017EraRounds, newcfg.ECIP1017EraRounds) {
		return newBlockCompatError("ECIP-1017 era rounds", c.ECIP1017EraRounds, newcfg.ECIP1017EraRounds)
	}
	if c.IsECIP1010(headNumber) && !configBlockEqual(c.ECIP1010Length, newcfg.ECIP1010Length) {
		return newBlockCompatError("ECIP-1010 pause length", c.ECIP1010Length, newcfg.ECIP1010Length)
	}
	return nil
}

// checkConfigForkOrderETC checks the ordering of the ETC-specific fork blocks. Upstream's
// CheckConfigForkOrder validates the Ethereum ordering of the shared forks, but knows nothing
// about the ETC forks getc adds (Gotham/DieHard/Etchash/Spiral), nor about where they sit
// relative to the shared ones. This validates the canonical ETC sequence so an out-of-order
// config (e.g. Spiral before Mystique, or Etchash before Phoenix) is rejected.
//
// A few shared forks are included as anchors so the ETC forks are ordered relative to them.
// Every entry is optional (skipped if nil): only ordering is enforced, not presence — Classic
// and Mordor enable different subsets and place some forks at the same block (e.g. Mordor has
// Magneto == Etchash), which equality tolerates.
func (c *ChainConfig) checkConfigForkOrderETC() error {
	type fork struct {
		name  string
		block *big.Int
	}
	var last fork
	for _, cur := range []fork{
		{"eip155Block", c.EIP155Block},
		{"ecip1010Transition (DieHard)", c.ECIP1010Transition},
		{"ecip1017Block (Gotham)", c.ECIP1017Block},
		{"ecip1041Block (bomb disposal)", c.ECIP1041Block},
		// ETC delayed EIP-158 to Atlantis (8.772M on Classic), unlike Ethereum's block 3M.
		{"eip158Block (Atlantis)", c.EIP158Block},
		{"byzantiumBlock (Atlantis)", c.ByzantiumBlock},
		{"istanbulBlock (Phoenix)", c.IstanbulBlock},
		{"ecip1099Block (Etchash)", c.ECIP1099Block},
		{"berlinBlock (Magneto)", c.BerlinBlock},
		{"londonBlock (Mystique)", c.LondonBlock},
		{"spiralBlock (Spiral)", c.SpiralBlock},
	} {
		if cur.block == nil {
			continue // optional: presence not required, only ordering
		}
		if last.block != nil && last.block.Cmp(cur.block) > 0 {
			return fmt.Errorf("unsupported ETC fork ordering: %s enabled at block %v, but %s enabled at block %v",
				last.name, last.block, cur.name, cur.block)
		}
		last = cur
	}
	return nil
}

func init() {
	// Register ETC networks in NetworkNames
	NetworkNames[ClassicChainConfig.ChainID.String()] = "classic"
	NetworkNames[MordorChainConfig.ChainID.String()] = "mordor"
}
