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
		ECIP1017Block:     big.NewInt(5_000_000), // Gotham monetary policy
		ECIP1017EraRounds: big.NewInt(5_000_000), // Era length: 5M blocks
		ECIP1041Block: big.NewInt(5_900_000),  // ECIP-1041 bomb disposal
		ECIP1099Block: big.NewInt(11_700_000), // Etchash (60k epochs)
		SpiralBlock:   big.NewInt(19_250_000), // Spiral (partial Shanghai)
		// Consensus engine
		Ethash: new(EthashConfig),
	}
)

// IsECIP1017 returns whether num is either equal to the ECIP-1017 transition block or greater.
func (c *ChainConfig) IsECIP1017(num *big.Int) bool {
	return isBlockForked(c.ECIP1017Block, num)
}

// IsClassic returns whether this chain is an Ethereum Classic chain.
func (c *ChainConfig) IsClassic() bool {
	if c.ChainID == nil {
		return false
	}
	return c.ChainID.Uint64() == 61
}

func init() {
	// Register ETC networks in NetworkNames
	NetworkNames[ClassicChainConfig.ChainID.String()] = "classic"
}
