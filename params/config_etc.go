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
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// ETC genesis hashes to enforce below configs on.
var (
	// ClassicGenesisHash is the same as MainnetGenesisHash - ETH and ETC share the same genesis block
	ClassicGenesisHash = common.HexToHash("0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3")
	MordorGenesisHash  = common.HexToHash("0xa68ebde7932f0e8c1f5e49ab8f9da6b0e0c0c68c7a4d5c5e7e0b6c3c4c3c2c1c0")
)

var (
	// ClassicChainConfig is the chain parameters for Ethereum Classic mainnet.
	ClassicChainConfig = &ChainConfig{
		ChainID:             big.NewInt(61),
		HomesteadBlock:      big.NewInt(1_150_000),
		DAOForkBlock:        nil,   // ETC did NOT have the DAO fork (nil excludes from Fork ID)
		DAOForkSupport:      false, // ETC rejected the DAO fork
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
		VerkleTime:              nil,
		TerminalTotalDifficulty: nil, // ETC is perpetual PoW, no TTD
		// ETC-specific ECIPs
		ECIP1017Block:      big.NewInt(5_000_000),  // Gotham monetary policy
		ECIP1041Block:      big.NewInt(5_900_000),  // ECIP-1041 bomb disposal
		ECIP1099Block:      big.NewInt(11_700_000), // Etchash (60k epochs)
		SpiralBlock:        big.NewInt(19_250_000), // Spiral (partial Shanghai)
		ECIP1017EraRounds:  big.NewInt(5_000_000),  // Era length: 5M blocks
		ECIP1010Transition: big.NewInt(3_000_000),  // DieHard bomb pause
		ECIP1010Length:     big.NewInt(2_000_000),  // Pause duration
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
		BerlinBlock:         big.NewInt(2_520_000), // Mordor Magneto
		LondonBlock:         big.NewInt(3_985_893), // Mordor Mystique (WITHOUT EIP-1559)
		ArrowGlacierBlock:   nil,
		GrayGlacierBlock:    nil,
		MergeNetsplitBlock:  nil,
		// Timestamp-based forks - none for ETC
		ShanghaiTime:            nil,
		CancunTime:              nil,
		PragueTime:              nil,
		OsakaTime:               nil,
		VerkleTime:              nil,
		TerminalTotalDifficulty: nil, // Perpetual PoW
		// ETC-specific ECIPs
		ECIP1017Block:      nil,                   // From genesis (not a fork point)
		ECIP1041Block:      nil,                   // Mordor never had bomb
		ECIP1099Block:      big.NewInt(2_520_000), // Etchash from Magneto
		SpiralBlock:        big.NewInt(9_957_000), // Mordor Spiral
		ECIP1017EraRounds:  big.NewInt(2_000_000), // Era length: 2M blocks
		ECIP1010Transition: nil,                   // Mordor never had bomb pause
		ECIP1010Length:     nil,
		// Consensus engine
		Ethash: new(EthashConfig),
	}
)

// IsClassic returns whether this chain is an Ethereum Classic chain.
// ETC mainnet has ChainID 61, Mordor testnet has ChainID 63.
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
	return c.IsClassic()
}

func init() {
	// Register ETC networks in NetworkNames
	NetworkNames[ClassicChainConfig.ChainID.String()] = "classic"
	NetworkNames[MordorChainConfig.ChainID.String()] = "mordor"
}
