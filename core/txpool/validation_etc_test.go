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

// ETC-specific tests for EIP-1559 gating in the txpool validator and the
// transaction signer factory. These tests guard against a regression where
// ETC Mystique (London-without-EIP-1559) inadvertently accepts DynamicFeeTxs
// or recovers their senders, which would be consensus-critical.
//
// File suffix _etc_test.go isolates this from upstream rebases.

package txpool

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// TestValidateTransactionRejectsDynamicFeeOnETCMystique verifies that the
// txpool's basic validator rejects EIP-1559 (type 0x02) transactions on an
// ETC chain whose head sits past the Mystique fork block. ETC never enabled
// EIP-1559, so type-0x02 transactions must be unsupported regardless of
// London activation.
func TestValidateTransactionRejectsDynamicFeeOnETCMystique(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	// Head far past the Mystique (London) activation block on ETC mainnet.
	head := &types.Header{
		Number:     new(big.Int).Add(params.ClassicChainConfig.LondonBlock, big.NewInt(1)),
		GasLimit:   8_000_000,
		Time:       1,
		Difficulty: big.NewInt(1),
	}

	signer := types.LatestSignerForChainID(params.ClassicChainConfig.ChainID)
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   params.ClassicChainConfig.ChainID,
		Nonce:     0,
		To:        &to,
		Value:     big.NewInt(1),
		Gas:       21_000,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(2),
	})
	signedTx, err := types.SignTx(tx, signer, key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}

	opts := &ValidationOptions{
		Config:       params.ClassicChainConfig,
		Accept:       1<<types.LegacyTxType | 1<<types.AccessListTxType | 1<<types.DynamicFeeTxType,
		MaxSize:      32 * 1024,
		MaxBlobCount: 0,
		MinTip:       big.NewInt(0),
	}

	err = ValidateTransaction(signedTx, head, signer, opts)
	if err == nil {
		t.Fatal("expected DynamicFeeTx to be rejected on ETC Mystique, got nil error")
	}
	if !errors.Is(err, core.ErrTxTypeNotSupported) {
		t.Fatalf("expected ErrTxTypeNotSupported, got %v", err)
	}
}

// TestValidateTransactionAcceptsLegacyOnETCMystique sanity-checks that
// rejecting EIP-1559 doesn't accidentally also reject legacy or access-list
// transactions, both of which ETC supports post-Mystique.
func TestValidateTransactionAcceptsLegacyOnETCMystique(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	head := &types.Header{
		Number:     new(big.Int).Add(params.ClassicChainConfig.LondonBlock, big.NewInt(1)),
		GasLimit:   8_000_000,
		Time:       1,
		Difficulty: big.NewInt(1),
	}

	signer := types.LatestSignerForChainID(params.ClassicChainConfig.ChainID)
	to := common.HexToAddress("0x0000000000000000000000000000000000000001")
	tx, err := types.SignTx(types.NewTx(&types.AccessListTx{
		ChainID:  params.ClassicChainConfig.ChainID,
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(1),
		Gas:      21_000,
		GasPrice: big.NewInt(1),
	}), signer, key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}

	opts := &ValidationOptions{
		Config:       params.ClassicChainConfig,
		Accept:       1<<types.LegacyTxType | 1<<types.AccessListTxType | 1<<types.DynamicFeeTxType,
		MaxSize:      32 * 1024,
		MaxBlobCount: 0,
		MinTip:       big.NewInt(0),
	}

	if err := ValidateTransaction(tx, head, signer, opts); err != nil {
		t.Fatalf("expected AccessListTx to pass validation, got %v", err)
	}
}

// TestMakeSignerSkipsLondonOnETC verifies that types.MakeSigner does not
// pick the London signer (which understands DynamicFeeTx) for a perpetual
// PoW chain post-Mystique, instead falling back to the EIP-2930 (Berlin)
// signer. This prevents an attacker from getting a valid sender recovery
// for a type-0x02 transaction on ETC-style chains.
func TestMakeSignerSkipsLondonOnETC(t *testing.T) {
	postMystique := new(big.Int).Add(params.ClassicChainConfig.LondonBlock, big.NewInt(1))
	got := types.MakeSigner(params.ClassicChainConfig, postMystique, 0)

	wantBerlin := types.NewEIP2930Signer(params.ClassicChainConfig.ChainID)
	if !got.Equal(wantBerlin) {
		t.Fatalf("MakeSigner on ETC post-Mystique = %T, want EIP2930 (Berlin) signer", got)
	}
	if got.Equal(types.NewLondonSigner(params.ClassicChainConfig.ChainID)) {
		t.Fatal("MakeSigner on ETC post-Mystique unexpectedly equals London signer")
	}

	// A devnet-style chain — the same perpetual PoW config under a custom
	// chain ID — keeps the Berlin signer too: the gate derives from the
	// config shape, not from chain IDs 61/63.
	devnetCfg := *params.ClassicChainConfig
	devnetCfg.ChainID = big.NewInt(9999)
	gotDevnet := types.MakeSigner(&devnetCfg, postMystique, 0)
	if !gotDevnet.Equal(types.NewEIP2930Signer(devnetCfg.ChainID)) {
		t.Fatalf("MakeSigner on custom-chain-ID PoW post-Mystique = %T, want EIP2930 (Berlin) signer", gotDevnet)
	}

	// And confirm that a merge-track chain (TTD set) that activates London
	// DOES select the London signer.
	ethCfg := *params.ClassicChainConfig
	ethCfg.ChainID = big.NewInt(1)
	ethCfg.TerminalTotalDifficulty = big.NewInt(0)
	postLondon := new(big.Int).Add(ethCfg.LondonBlock, big.NewInt(1))
	gotETH := types.MakeSigner(&ethCfg, postLondon, 0)
	if !gotETH.Equal(types.NewLondonSigner(ethCfg.ChainID)) {
		t.Fatalf("MakeSigner on merge-track post-London = %T, want London signer", gotETH)
	}
}

// TestRulesIsEIP1559OnETC verifies that the Rules helper exposes IsEIP1559
// as false for an ETC chain past Mystique, while IsLondon stays true. This
// is the field both signer selection and the txpool validator rely on.
func TestRulesIsEIP1559OnETC(t *testing.T) {
	postMystique := new(big.Int).Add(params.ClassicChainConfig.LondonBlock, big.NewInt(1))
	rules := params.ClassicChainConfig.Rules(postMystique, false, 0)
	if !rules.IsLondon {
		t.Fatal("expected IsLondon true on ETC post-Mystique")
	}
	if rules.IsEIP1559 {
		t.Fatal("expected IsEIP1559 false on ETC post-Mystique")
	}
	if !rules.IsMystique {
		t.Fatal("expected IsMystique true on ETC post-Mystique")
	}
}
