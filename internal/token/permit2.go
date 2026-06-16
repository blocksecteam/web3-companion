// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"encoding/hex"
	"math/big"
	"time"
)

// Permit2Address is the canonical Uniswap Permit2 contract address, deployed at
// the same address on every EVM chain it ships to.
const Permit2Address = "0x000000000022D473030F116dDEE9F6B43aC78BA3"

// DefaultPermit2Deadline returns now + 30 minutes (unix seconds), the standard
// expiry attached to Permit2.approve calls when a caller does not supply one.
func DefaultPermit2Deadline() int64 {
	return time.Now().Unix() + 1800
}

// EncodePermit2Approve encodes the 4-parameter Permit2.approve(token, spender,
// uint160 amount, uint48 expiration) calldata (selector 0x87517c45 + 4×32-byte
// padded args). `spender` is the contract being whitelisted to pull `token`
// via Permit2 — the swap/bridge router that will execute the trade.
//
// Notes on Permit2 spec:
//   - `amount` is uint160 on-chain (max 2^160-1 ≈ 1.46e48). Larger values will
//     cause the Permit2 contract to revert. Negative *big.Int will panic here
//     before any tx is built (via big.Int.FillBytes, which rejects negatives).
//   - `expiration` is uint48 on-chain (max 2^48-1 ≈ 2.81e14, year 8921 in unix
//     seconds). Practically unreachable; the assertion is here in case a caller
//     passes a malformed/large value.
func EncodePermit2Approve(tokenAddr, spender string, amount *big.Int, expiration int64) string {
	if amount == nil {
		amount = new(big.Int)
	}
	mustFitUint160(amount)
	mustFitUint48(expiration)

	amountBytes := make([]byte, 32)
	amount.FillBytes(amountBytes)
	expirationBytes := make([]byte, 32)
	new(big.Int).SetInt64(expiration).FillBytes(expirationBytes)

	return "0x87517c45" +
		padHexAddress(tokenAddr) +
		padHexAddress(spender) +
		hex.EncodeToString(amountBytes) +
		hex.EncodeToString(expirationBytes)
}

// BuildPermit2ApproveSequence returns the calls needed to set up a Permit2-style
// approve: an ERC-20 approve from `tokenAddr` to the Permit2 contract (USDT-safe
// zero-reset when the current Permit2 allowance is partial), followed by a
// Permit2.approve directing Permit2 to allow `router` to pull the token.
//
//	permit2Addr             — Permit2 contract address (usually Permit2Address)
//	tokenAddr               — ERC-20 the user is approving
//	router                  — final consumer that Permit2 will let pull token
//	amount                  — amount to approve (must fit uint160; see EncodePermit2Approve)
//	currentPermit2Allowance — ERC-20 allowance owner → Permit2; when it already
//	                          covers amount, the leading ERC-20 approve is skipped
func BuildPermit2ApproveSequence(permit2Addr, tokenAddr, router string, amount, currentPermit2Allowance *big.Int) []ApproveCall {
	zero := big.NewInt(0)
	deadline := DefaultPermit2Deadline()

	var calls []ApproveCall
	if currentPermit2Allowance.Cmp(amount) < 0 {
		if currentPermit2Allowance.Cmp(zero) > 0 {
			calls = append(calls, ApproveCall{To: tokenAddr, Data: EncodeERC20Approve(permit2Addr, zero), Value: zero})
		}
		calls = append(calls, ApproveCall{To: tokenAddr, Data: EncodeERC20Approve(permit2Addr, amount), Value: zero})
	}
	calls = append(calls, ApproveCall{
		To:    permit2Addr,
		Data:  EncodePermit2Approve(tokenAddr, router, amount, deadline),
		Value: zero,
	})
	return calls
}

var maxUint160 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 160), big.NewInt(1))

func mustFitUint160(v *big.Int) {
	if v.Sign() < 0 || v.Cmp(maxUint160) > 0 {
		panic("token: Permit2 amount must fit uint160 (0..2^160-1)")
	}
}

func mustFitUint48(v int64) {
	if v < 0 || v >= 1<<48 {
		panic("token: Permit2 expiration must fit uint48 (0..2^48-1)")
	}
}
