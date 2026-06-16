// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package vault

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"

	"github.com/blocksecteam/web3-companion/internal/gasfee"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func (v *Vault) CheckDelegation(ctx context.Context, chainName, userID, walletID string) (*DelegationResult, error) {
	wallet, err := v.GetWallet(userID, walletID)
	if err != nil {
		return nil, err
	}

	rpcURL, ok := v.resolveRPCURL(chainName)
	if !ok {
		return nil, fmt.Errorf("no RPC URL for chain %q", chainName)
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial RPC: %w", err)
	}
	defer client.Close()

	code, err := client.CodeAt(ctx, common.HexToAddress(wallet.Address), nil)
	if err != nil {
		return nil, fmt.Errorf("get code: %w", err)
	}

	result := &DelegationResult{WalletID: walletID, Address: wallet.Address}
	var state storage.WalletDelegation
	hasState := v.db.WithContext(ctx).
		Where("wallet_uid = ? AND chain = ?", wallet.WalletUID, chainName).
		First(&state).Error == nil
	if hasState && strings.TrimSpace(state.DelegationHash) != "" {
		result.DelegationHash = state.DelegationHash
	}
	delegator := parseDelegationDesignator(code)
	if delegator != "" {
		result.Delegated = true
		result.Status = "delegated"
		result.Delegator = delegator
		if !strings.EqualFold(delegator, v.delegator) {
			_ = v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
				"delegated":       false,
				"delegator":       delegator,
				"status":          "wrong_delegator",
				"delegation_hash": result.DelegationHash,
			})
			return result, ErrWrongDelegator
		}
		now := time.Now().UTC()
		_ = v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
			"delegated":       true,
			"delegator":       delegator,
			"status":          "delegated",
			"delegation_hash": result.DelegationHash,
			"delegated_at":    now,
		})
		return result, nil
	}
	if hasState && strings.TrimSpace(state.Status) == "pending_delegation" && strings.TrimSpace(state.DelegationHash) != "" {
		result.Status = "pending_delegation"
		result.DelegationHash = state.DelegationHash
	}

	balance, err := client.BalanceAt(ctx, common.HexToAddress(wallet.Address), nil)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}

	// Align precheck cost estimate with the signer's effective gas price
	// (baseFee + tip) instead of legacy SuggestGasPrice — otherwise the
	// "do you have enough native to delegate?" check uses a different
	// strategy from what the signer actually pays.
	rpcForGas, _ := v.resolveRPCURL(chainName)
	gasPrice, gasPriceFallback := gasfee.ResolveEffectiveGasPriceOrFallback(ctx, rpcForGas)
	estimatedGas, gasLimitFallback := v.estimateDelegationGas(ctx, client, wallet.Address, chainName)
	gasCost := new(big.Int).Mul(big.NewInt(int64(estimatedGas)), gasPrice)

	result.NeedsGas = balance.Cmp(gasCost) < 0
	result.GasEstimate = gasCost.String()
	result.CurrentBalance = balance.String()
	if gasPriceFallback || gasLimitFallback {
		result.GasEstimateSource = "fallback"
	} else {
		result.GasEstimateSource = "rpc"
	}
	if result.Status == "" {
		result.Status = "not_delegated"
	}
	_ = v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
		"delegated":       false,
		"delegator":       "",
		"status":          result.Status,
		"delegation_hash": result.DelegationHash,
		"delegated_at":    nil,
	})
	return result, nil
}

func (v *Vault) EnsureDelegated(ctx context.Context, chainName, userID, walletID string) (*DelegationResult, error) {
	result, err := v.CheckDelegation(ctx, chainName, userID, walletID)
	if err != nil {
		return result, err
	}
	if result.Delegated {
		return result, nil
	}
	if result.Status == "pending_delegation" && strings.TrimSpace(result.DelegationHash) != "" {
		return v.refreshPendingDelegation(ctx, chainName, userID, walletID, result)
	}
	return result, ErrNotDelegated
}

func (v *Vault) refreshPendingDelegation(ctx context.Context, chainName, userID, walletID string, result *DelegationResult) (*DelegationResult, error) {
	wallet, walletErr := v.GetWallet(userID, walletID)
	if walletErr != nil {
		return result, walletErr
	}
	rpcURL, ok := v.resolveRPCURL(chainName)
	if !ok {
		return result, fmt.Errorf("no RPC URL for chain %q", chainName)
	}
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return result, fmt.Errorf("dial RPC: %w", err)
	}
	defer client.Close()

	txHash := common.HexToHash(result.DelegationHash)
	receipt, err := v.waitForDelegationReceipt(ctx, client, txHash)
	if err != nil {
		if errors.Is(err, ErrDelegationPending) {
			return result, ErrDelegationPending
		}
		_ = v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
			"delegated": false,
			"status":    "failed",
		})
		return result, err
	}

	confirmedAt := time.Now()
	if header, hErr := client.HeaderByNumber(ctx, receipt.BlockNumber); hErr == nil {
		confirmedAt = time.Unix(int64(header.Time), 0)
	}
	_ = v.db.Model(&storage.Transaction{}).
		Where("user_id = ? AND wallet_id = ? AND tx_type = ? AND status = ? AND chain = ?", userID, walletID, "delegate", "pending", chainName).
		Updates(map[string]interface{}{
			"status":       "confirmed",
			"confirmed_at": &confirmedAt,
		})
	if err := v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
		"delegated":       true,
		"delegator":       v.delegator,
		"status":          "delegated",
		"delegation_hash": result.DelegationHash,
		"delegated_at":    confirmedAt,
	}); err != nil {
		return result, err
	}
	result.Delegated = true
	result.Status = "delegated"
	result.Delegator = v.delegator
	return result, nil
}

func (v *Vault) Delegate(ctx context.Context, chainName, userID, walletID string) (*DelegationResult, error) {
	wallet, err := v.GetWallet(userID, walletID)
	if err != nil {
		return nil, err
	}

	rpcURL, ok := v.resolveRPCURL(chainName)
	if !ok {
		return nil, fmt.Errorf("no RPC URL for chain %q", chainName)
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial RPC: %w", err)
	}
	defer client.Close()

	privateKey, err := v.loadAndDecrypt(ctx, userID, walletID)
	if err != nil {
		return nil, fmt.Errorf("load key: %w", err)
	}
	defer WipePrivateKey(privateKey)

	nonce, err := client.PendingNonceAt(ctx, common.HexToAddress(wallet.Address))
	if err != nil {
		return nil, fmt.Errorf("get nonce: %w", err)
	}

	// Pull baseFee + tip from the same helper the signer's FetchFeeParams
	// uses, so the EIP-7702 delegation tx is priced on the same strategy
	// as every other signed tx in the project.
	rpcForFee, _ := v.resolveRPCURL(chainName)
	baseFee, tip, ferr := gasfee.FetchBaseFeeAndTip(ctx, rpcForFee)
	if ferr != nil {
		baseFee = new(big.Int).Set(gasfee.FallbackBaseFee)
		tip = new(big.Int).Set(gasfee.FallbackTip)
	}
	// Effective gas price = baseFee + tip; recorded in the result for
	// downstream display only (the actual fee paid is determined on-chain).
	effectiveGasPrice := new(big.Int).Add(baseFee, tip)

	// Balance precheck so the caller gets `insufficient_gas` with a
	// concrete `gas_estimate` instead of an opaque RPC error wrapped in
	// `delegation_failed`. Use the same live estimateGas helper as
	// CheckDelegation so the precheck verdict surfaced by /delegate
	// matches what /ssm/delegate/estimate previewed to the user.
	balance, err := client.BalanceAt(ctx, common.HexToAddress(wallet.Address), nil)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	estimatedGas, gasLimitFallback := v.estimateDelegationGas(ctx, client, wallet.Address, chainName)
	gasCost := new(big.Int).Mul(big.NewInt(int64(estimatedGas)), effectiveGasPrice)
	if balance.Cmp(gasCost) < 0 {
		source := "rpc"
		if ferr != nil || gasLimitFallback {
			source = "fallback"
		}
		return &DelegationResult{
			WalletID:          walletID,
			Address:           wallet.Address,
			Delegated:         false,
			Status:            "not_delegated",
			NeedsGas:          true,
			GasEstimate:       gasCost.String(),
			CurrentBalance:    balance.String(),
			GasEstimateSource: source,
		}, ErrInsufficientGas
	}

	signedHex, err := v.buildAndSignSetCode(ctx, privateKey, chainName, nonce, baseFee, tip)
	if err != nil {
		return nil, fmt.Errorf("build set-code: %w", err)
	}

	var result string
	err = client.Client().CallContext(ctx, &result, "eth_sendRawTransaction", signedHex)
	if err != nil {
		return nil, fmt.Errorf("broadcast delegation: %w", err)
	}

	txHash := common.HexToHash(result)
	now := time.Now()
	if err := v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
		"delegated":       false,
		"delegator":       v.delegator,
		"status":          "pending_delegation",
		"delegation_hash": txHash.Hex(),
		"delegated_at":    nil,
	}); err != nil {
		return nil, err
	}
	_ = v.db.Create(&storage.Transaction{
		Hash:        txHash.Hex(),
		Chain:       chainName,
		UserID:      wallet.UserID,
		WalletID:    walletDisplayID(*wallet),
		WalletUID:   wallet.WalletUID,
		Status:      "pending",
		TxType:      "delegate",
		SubmittedAt: &now,
	})
	receipt, err := v.waitForDelegationReceipt(ctx, client, txHash)
	if err != nil {
		if errors.Is(err, ErrDelegationPending) {
			return &DelegationResult{
				WalletID: walletID, Address: wallet.Address, Delegated: false,
				Status: "pending_delegation", Delegator: v.delegator,
				DelegationHash: txHash.Hex(), GasEstimate: effectiveGasPrice.String(),
			}, ErrDelegationPending
		}
		_ = v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
			"delegated": false,
			"status":    "failed",
		})
		_ = v.db.Model(&storage.Transaction{}).
			Where("hash = ? AND chain = ?", txHash.Hex(), chainName).
			Updates(map[string]interface{}{"status": "failed"})
		return nil, err
	}

	confirmedAt := now
	if header, hErr := client.HeaderByNumber(ctx, receipt.BlockNumber); hErr == nil {
		confirmedAt = time.Unix(int64(header.Time), 0)
	}

	if err := v.upsertDelegationState(ctx, *wallet, chainName, map[string]interface{}{
		"delegated":       true,
		"delegator":       v.delegator,
		"status":          "delegated",
		"delegation_hash": txHash.Hex(),
		"delegated_at":    confirmedAt,
	}); err != nil {
		return nil, err
	}
	_ = v.db.Model(&storage.Transaction{}).
		Where("hash = ? AND chain = ?", txHash.Hex(), chainName).
		Updates(map[string]interface{}{
			"status":       "confirmed",
			"confirmed_at": &confirmedAt,
		})

	return &DelegationResult{
		WalletID: walletID, Address: wallet.Address, Delegated: true,
		Status: "delegated", Delegator: v.delegator, DelegationHash: txHash.Hex(), GasEstimate: effectiveGasPrice.String(),
	}, nil
}

func (v *Vault) waitForDelegationReceipt(ctx context.Context, client *ethclient.Client, txHash common.Hash) (*types.Receipt, error) {
	waitCtx, cancel := context.WithTimeout(ctx, DelegationReceiptWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		receipt, err := client.TransactionReceipt(waitCtx, txHash)
		if err == nil {
			if receipt.Status != types.ReceiptStatusSuccessful {
				return nil, fmt.Errorf("delegation transaction failed: %s", txHash.Hex())
			}
			return receipt, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrDelegationPending
		}
		if !errors.Is(err, ethereum.NotFound) {
			return nil, fmt.Errorf("get delegation receipt: %w", err)
		}
		select {
		case <-waitCtx.Done():
			return nil, ErrDelegationPending
		case <-ticker.C:
		}
	}
}

// buildAndSignSetCode prices the EIP-7702 delegation tx using the same
// formula as vault.FetchFeeParams: MaxFeePerGas = 4*baseFee + tip, TipCap = tip.
// Caller pulls baseFee/tip from internal/gasfee (live RPC or fallback).
func (v *Vault) buildAndSignSetCode(ctx context.Context, key *ecdsa.PrivateKey, chainName string, nonce uint64, baseFee, tip *big.Int) (string, error) {
	_ = ctx
	delegatorAddr := common.HexToAddress(v.delegator)
	chainID, ok := v.chainIDForName(chainName)
	if !ok {
		return "", fmt.Errorf("unsupported chain %q", chainName)
	}
	chainIDBig := big.NewInt(int64(chainID))

	auth, err := types.SignSetCode(key, types.SetCodeAuthorization{
		ChainID: *uint256.MustFromBig(chainIDBig),
		Address: delegatorAddr,
		Nonce:   nonce + 1,
	})
	if err != nil {
		return "", fmt.Errorf("sign set-code authorization: %w", err)
	}

	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(4)), tip)
	gasTipCap := tip
	if gasTipCap.Sign() == 0 {
		gasTipCap = big.NewInt(1_000_000_000)
	}
	tx := types.NewTx(&types.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainIDBig),
		Nonce:     nonce,
		GasTipCap: uint256.MustFromBig(gasTipCap),
		GasFeeCap: uint256.MustFromBig(gasFeeCap),
		Gas:       100000,
		To:        delegatorAddr,
		Value:     uint256.NewInt(0),
		AuthList:  []types.SetCodeAuthorization{auth},
	})

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainIDBig), key)
	if err != nil {
		return "", fmt.Errorf("sign set-code tx: %w", err)
	}
	raw, err := signedTx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("marshal set-code tx: %w", err)
	}
	return "0x" + common.Bytes2Hex(raw), nil
}

func parseDelegationDesignator(code []byte) string {
	if len(code) != 23 {
		return ""
	}
	if code[0] != 0xef || code[1] != 0x01 || code[2] != 0x00 {
		return ""
	}
	return "0x" + common.Bytes2Hex(code[3:])
}

// estimateDelegationGas asks the chain's RPC for a live gas limit estimate
// of the EIP-7702 set-code delegation tx. Uses raw eth_estimateGas with a
// dummy authorizationList signature — accepted by the four currently
// supported chains (ethereum / base / polygon / bsc via QuickNode). Strict
// RPCs that reject the dummy signature fall back to a 100000-gas constant.
// Returns the chosen gas limit plus a flag indicating whether the fallback
// path was used.
func (v *Vault) estimateDelegationGas(ctx context.Context, client *ethclient.Client, walletAddr, chainName string) (uint64, bool) {
	const fallback = uint64(100000)
	chainID, ok := v.chainIDForName(chainName)
	if !ok {
		return fallback, true
	}
	chainHex := fmt.Sprintf("0x%x", chainID)
	call := map[string]interface{}{
		"type":    "0x4",
		"from":    walletAddr,
		"to":      v.delegator,
		"value":   "0x0",
		"chainId": chainHex,
		"authorizationList": []map[string]interface{}{{
			"chainId": chainHex,
			"address": v.delegator,
			"nonce":   "0x0",
			"yParity": "0x0",
			// Minimal-but-valid r/s pair: leading zeros stripped so strict
			// uint256-decoding RPCs accept the request. The signature does
			// not need to verify — estimateGas runs prior to validation.
			"r": "0x1",
			"s": "0x1",
		}},
	}
	var result string
	if err := client.Client().CallContext(ctx, &result, "eth_estimateGas", call); err != nil {
		return fallback, true
	}
	if !strings.HasPrefix(result, "0x") {
		return fallback, true
	}
	parsed, ok := new(big.Int).SetString(strings.TrimPrefix(result, "0x"), 16)
	if !ok || parsed.Sign() <= 0 {
		return fallback, true
	}
	return parsed.Uint64(), false
}

// chainIDForName reads the chain ID from the vault's per-chain registry
// (populated from config.Chains at construction time). Returns ok=false
// when the chain is unknown or its configured chain_id is non-positive,
// which surfaces as a clear "unsupported chain" error to callers rather
// than signing with chain_id=0.
func (v *Vault) chainIDForName(name string) (int, bool) {
	id, ok := v.chainIDs[name]
	if (!ok || id <= 0) && v.cfg != nil {
		if ch := v.cfg.ChainByName(name); ch != nil && ch.ChainID > 0 {
			v.chainIDs[name] = ch.ChainID
			return ch.ChainID, true
		}
	}
	if !ok || id <= 0 {
		return 0, false
	}
	return id, true
}
