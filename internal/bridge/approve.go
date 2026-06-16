// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package bridge

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/token"
)

// ComputeApproveSequence builds the approve calls needed before invoking an
// approve-mode bridge router. Routing is controlled by `style`:
//
//	"permit2" — token.approve(Permit2, amount) (when current Permit2 allowance
//	            is insufficient, with USDT-safe zero-reset) followed by
//	            Permit2.approve(token, router, amount, deadline).
//	anything else ("erc20" / "") — plain ERC-20 approve(spender, amount),
//	            with the same zero-reset when needed.
//
// The allowance lookup uses rpcURL when non-empty. When the RPC is unavailable
// the function assumes zero allowance and emits the full approve sequence,
// trading an extra on-chain call for safety.
//
//	tokenAddr — ERC-20 the user is approving (post-wrap if handler wrapped first)
//	spender   — direct spender (router for "erc20"; Permit2 address for "permit2")
//	router    — final consumer pulling token via Permit2 (only used for "permit2")
func ComputeApproveSequence(ctx context.Context, rpcURL, owner, tokenAddr, spender, router, style string, amount *big.Int) []Call {
	if amount == nil || amount.Sign() <= 0 || !common.IsHexAddress(tokenAddr) || !common.IsHexAddress(owner) || !common.IsHexAddress(spender) {
		return nil
	}
	current, err := token.FetchAllowance(ctx, rpcURL, tokenAddr, owner, spender)
	if err != nil {
		// RPC unavailable: assume zero allowance and emit the full sequence,
		// trading an extra on-chain approve for safety. Matches prior bridge
		// behaviour (which used a (value, bool) signature for the same effect).
		current = big.NewInt(0)
	}

	if style == "permit2" {
		return bridgeCallsFrom(token.BuildPermit2ApproveSequence(spender, tokenAddr, router, amount, current))
	}
	return bridgeCallsFrom(token.BuildERC20ApproveSequence(tokenAddr, spender, amount, current))
}

// bridgeCallsFrom converts the canonical token.ApproveCall slice (Value as
// *big.Int) to Call (Value as decimal string), the JSON-tagged shape
// the bridge HTTP surface exposes. Nil/zero values become "0".
func bridgeCallsFrom(raw []token.ApproveCall) []Call {
	if len(raw) == 0 {
		return nil
	}
	out := make([]Call, len(raw))
	for i, c := range raw {
		v := "0"
		if c.Value != nil && c.Value.Sign() > 0 {
			v = c.Value.String()
		}
		out[i] = Call{To: c.To, Value: v, Data: c.Data}
	}
	return out
}
