// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ApproveCall is the canonical shape for a single approve / state-changing
// ERC-20 (or Permit2) calldata payload used across swap and bridge. Bridge
// converts to its own Call (Value as string) at the HTTP boundary;
// swap aliases this type directly via `type ApproveCall = token.ApproveCall`.
type ApproveCall struct {
	To    string
	Data  string
	Value *big.Int
}

// EncodeERC20Approve encodes the standard ERC-20 approve(spender, amount)
// calldata (selector 0x095ea7b3 + 32-byte padded spender + 32-byte amount).
// A nil amount is treated as zero; negative amounts will panic via FillBytes.
func EncodeERC20Approve(spender string, amount *big.Int) string {
	paddedAmount := make([]byte, 32)
	if amount != nil {
		amount.FillBytes(paddedAmount)
	}
	return "0x095ea7b3" + padHexAddress(spender) + hex.EncodeToString(paddedAmount)
}

// BuildERC20ApproveSequence returns the calldata sequence needed to bring
// the owner's ERC-20 allowance to `amount` for `spender`. Three cases:
//
//	current >= amount  → nothing to do (returns nil)
//	current == 0       → single approve(spender, amount)
//	current > 0        → two-step approve(spender, 0) then approve(spender, amount)
//	                     (USDT and other quirky ERC-20s refuse to update a
//	                     non-zero allowance directly; the zero-reset is the
//	                     defensive path)
//
// The returned ApproveCall.Value is always zero; the slice is freshly
// allocated. Single source of truth for both swap and bridge.
func BuildERC20ApproveSequence(tokenAddr, spender string, amount, current *big.Int) []ApproveCall {
	if current.Cmp(amount) >= 0 {
		return nil
	}
	zero := big.NewInt(0)
	if current.Sign() > 0 {
		return []ApproveCall{
			{To: tokenAddr, Data: EncodeERC20Approve(spender, zero), Value: zero},
			{To: tokenAddr, Data: EncodeERC20Approve(spender, amount), Value: zero},
		}
	}
	return []ApproveCall{
		{To: tokenAddr, Data: EncodeERC20Approve(spender, amount), Value: zero},
	}
}

// FetchAllowance reads the ERC-20 allowance(owner, spender) value for an
// ERC-20 token on a given RPC endpoint. Co-located with the approve helpers
// because allowance is the read side of approve — together they form the
// complete approve-flow surface.
//
// Returns the current allowance as a *big.Int, or an error when the RPC call
// fails or the response cannot be decoded. Callers that want to fall back to
// zero on transient failure (e.g. "assume nothing approved, emit full approve
// sequence") should do so explicitly at the call site.
//
// Single source of truth for both swap and bridge. Uses ethclient + Call-
// Contract: errors propagate as Go errors instead of being collapsed into a
// bool, and for https:// RPC URLs the underlying http.DefaultTransport pool
// is shared across calls (no per-call TLS handshake cost in steady state).
func FetchAllowance(ctx context.Context, rpcURL, tokenAddr, owner, spender string) (*big.Int, error) {
	if strings.TrimSpace(rpcURL) == "" {
		return nil, fmt.Errorf("token allowance: rpc url is empty")
	}
	if !common.IsHexAddress(tokenAddr) || !common.IsHexAddress(owner) || !common.IsHexAddress(spender) {
		return nil, fmt.Errorf("token allowance: invalid address (token=%q owner=%q spender=%q)", tokenAddr, owner, spender)
	}
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("token allowance: dial rpc: %w", err)
	}
	defer client.Close()

	// allowance(address,address) selector = 0xdd62ed3e
	data := make([]byte, 4+32+32)
	copy(data[0:4], []byte{0xdd, 0x62, 0xed, 0x3e})
	copy(data[4+12:4+32], common.HexToAddress(owner).Bytes())
	copy(data[4+32+12:], common.HexToAddress(spender).Bytes())

	to := common.HexToAddress(tokenAddr)
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("token allowance: eth_call: %w", err)
	}
	if len(out) == 0 {
		// No returndata — could be a non-ERC-20 contract or an EOA. Treat as
		// zero rather than fabricating a value; caller decides what to do.
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(out), nil
}
