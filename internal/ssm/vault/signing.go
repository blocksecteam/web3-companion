// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/blocksecteam/web3-companion/internal/gasfee"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (v *Vault) SignTransaction(ctx context.Context, userID, walletID string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error) {
	privateKey, err := v.loadAndDecrypt(ctx, userID, walletID)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	defer WipePrivateKey(privateKey)

	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign tx: %w", err)
	}
	return signedTx, nil
}

func (v *Vault) PendingNonce(ctx context.Context, chainName, userID, walletID string) (uint64, error) {
	wallet, err := v.GetWallet(userID, walletID)
	if err != nil {
		return 0, err
	}

	rpcURL, ok := v.resolveRPCURL(chainName)
	if !ok {
		return 0, fmt.Errorf("no RPC URL for chain %q", chainName)
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return 0, fmt.Errorf("dial RPC: %w", err)
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, common.HexToAddress(wallet.Address))
	if err != nil {
		return 0, fmt.Errorf("get pending nonce: %w", err)
	}
	return nonce, nil
}

type FeeParams struct {
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
}

// FetchFeeParams returns the EIP-1559 fee params signed into the transaction:
//
//	MaxFeePerGas         = 4 * baseFee + tip   (extra room for baseFee growth between sign and broadcast)
//	MaxPriorityFeePerGas = tip
//
// The 4x multiplier governs how much baseFee can grow between sign time and
// broadcast time before the broadcast is rejected with "max fee per gas less
// than block base fee". EIP-1559 charges `baseFee + min(tip, maxFee -
// baseFee) * gasUsed`, so as long as baseFee at broadcast time is below
// maxFee the user pays the same effective price regardless of the
// multiplier — the multiplier only affects RPC acceptance and the wallet
// balance reserve the node enforces. With baseFee on Ethereum capped at
// +12.5%/block and Base/L2s far faster, 2x was insufficient: a 90-second
// pause on Ethereum or a 30-second pause on Base could exceed it. 4x
// covers Ethereum up to ~2 minutes and Base up to ~20 seconds, which
// matches realistic Passkey-confirm timing in the chat UX.
//
// Falls back to the package-level constants in internal/gasfee when the
// RPC is unreachable, so the signer never blocks on transient RPC failure.
// build-phase simulation uses the same gasfee package to stay strategy-aligned.
func (v *Vault) FetchFeeParams(ctx context.Context, chainName string) FeeParams {
	rpcURL, ok := v.resolveRPCURL(chainName)
	var (
		baseFee, tip *big.Int
		err          error
	)
	if ok {
		baseFee, tip, err = gasfee.FetchBaseFeeAndTip(ctx, rpcURL)
	}
	if !ok || err != nil {
		baseFee = new(big.Int).Set(gasfee.FallbackBaseFee)
		tip = new(big.Int).Set(gasfee.FallbackTip)
	}
	maxFee := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(4)), tip)
	return FeeParams{MaxFeePerGas: maxFee, MaxPriorityFeePerGas: tip}
}

func (v *Vault) ExportPrivateKey(ctx context.Context, userID, walletID string) (address string, privateKeyHex string, err error) {
	privateKey, err := v.loadAndDecrypt(ctx, userID, walletID)
	if err != nil {
		return "", "", fmt.Errorf("export key: %w", err)
	}
	defer WipePrivateKey(privateKey)

	wallet, err := v.GetWallet(userID, walletID)
	if err != nil {
		return "", "", err
	}
	privateKeyBytes := crypto.FromECDSA(privateKey)
	defer ZeroBytes(privateKeyBytes)
	return wallet.Address, "0x" + common.Bytes2Hex(privateKeyBytes), nil
}

func (v *Vault) loadAndDecrypt(ctx context.Context, userID, walletID string) (*ecdsa.PrivateKey, error) {
	var wallet storage.Wallet
	if err := v.db.Where("user_id = ? AND display_wallet_id = ? AND deleted_at IS NULL AND key_status = ?", userID, walletID, "active").First(&wallet).Error; err != nil {
		return nil, ErrWalletNotFound
	}
	kf, err := v.readKeyFile(wallet.KeyFile)
	if err != nil {
		return nil, err
	}
	dataKey, err := v.kms.Decrypt(ctx, kf.DataKeyEnc)
	if err != nil {
		return nil, fmt.Errorf("kms decrypt: %w", err)
	}
	defer ZeroBytes(dataKey)

	privateKeyBytes, err := Decrypt(dataKey, kf.Encrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt private key: %w", err)
	}
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	ZeroBytes(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return privateKey, nil
}

func (v *Vault) DefaultWalletKey(ctx context.Context, userID string) (*ecdsa.PrivateKey, string, error) {
	var wallet storage.Wallet
	if err := v.db.Where("user_id = ? AND is_default = ? AND deleted_at IS NULL AND key_status = ?", userID, true, "active").First(&wallet).Error; err != nil {
		if err2 := v.db.Where("user_id = ? AND deleted_at IS NULL AND key_status = ?", userID, "active").First(&wallet).Error; err2 != nil {
			return nil, "", ErrWalletNotFound
		}
	}
	key, err := v.loadAndDecrypt(ctx, userID, wallet.DisplayWalletID)
	if err != nil {
		return nil, "", err
	}
	return key, wallet.Address, nil
}

func WipePrivateKey(key *ecdsa.PrivateKey) {
	if key == nil || key.D == nil {
		return
	}
	words := key.D.Bits()
	for i := range words {
		words[i] = 0
	}
	key.D.SetInt64(0)
}
