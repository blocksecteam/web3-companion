// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"math/big"

	"github.com/blocksecteam/web3-companion/internal/token"
)

// ApproveCall is the swap-side alias for the shared token.ApproveCall shape.
// Aliased (not a fresh type) so existing literals like
// `ApproveCall{To: ..., Data: ..., Value: ...}` and the swap-side helpers
// remain interchangeable with token.BuildPermit2ApproveSequence output.
type ApproveCall = token.ApproveCall

// BuildApproveSequence is a thin re-export of token.BuildERC20ApproveSequence.
// Kept for backward compatibility with existing call sites; the canonical
// implementation lives in internal/token alongside the related Permit2
// builder so future ERC-20 write helpers have one obvious home.
func BuildApproveSequence(tokenAddr, spender string, amount, current *big.Int) []ApproveCall {
	return token.BuildERC20ApproveSequence(tokenAddr, spender, amount, current)
}

// FetchAllowance is a thin re-export of token.FetchAllowance. Kept so
// existing call sites (allowance_handler.go, integration tests) keep
// working without churn.
func FetchAllowance(ctx context.Context, rpcURL, tokenAddr, owner, spender string) (*big.Int, error) {
	return token.FetchAllowance(ctx, rpcURL, tokenAddr, owner, spender)
}
