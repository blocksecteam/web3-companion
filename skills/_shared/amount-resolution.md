# Shared Amount Resolution

Used by web3-swap, web3-bridge, web3-chain.

`--amount` always takes a human-readable **input** token quantity (e.g. `0.5` = 0.5 ETH). Never pass raw on-chain units.

## Fiat amount in USD / $

Always convert fiat to the **input** token quantity:

1. Get input token price from portfolio output (`balances[].price`). If not in portfolio: native token → `web3 token info --chain <chain> native`; known ERC-20 address → `web3 token info --chain <chain> <address>`; unknown symbol → `web3 token search --query <symbol> --chain <chain>`.
2. `input_amount = usd_amount / input_token_price`, rounded to `min(decimals, 8)` decimal places
3. Confirm with the user before proceeding. Include the calculated input amount and clarify that the actual outcome depends on the operation type (slippage for swaps, bridge fees for bridges, network fees for transfers). Respond in the user's language.

Example: "Current `<token>` ~$`<price>`; $5 ≈ `<amount>` `<token>`. Proceed with `<amount>` `<token>`?"

## Fiat amount in non-USD currency (CNY, EUR, GBP, etc.)

Use web search to look up the current exchange rate (e.g. search "USD CNY exchange rate"), convert to USD, then follow the USD flow above. If web search is unavailable, ask the user to provide the current rate or the USD equivalent directly.

## Ambiguous amount

Bare number with no unit and non-stablecoin token: ask the user whether it means token units or a fiat value.

## "All / max" balance operations

When the user asks to operate on "all", "max", "全部", "全部余额", "the whole balance", etc., do NOT copy the portfolio balance string verbatim into `--amount`. The rendered balance is sometimes rounded UP from the wallet's actual on-chain balance, so the verbatim value fails simulation (insufficient balance). Affects swap, bridge, and chain transfer.

Conservative recipe:

1. Take the portfolio balance string.
2. Trim trailing zeros after the decimal point.
3. Subtract 1 from the last digit. (If the last digit is `1`, it becomes `0`.)
4. Trim trailing zeros again.

Examples:

  - `0.9973662430219585` → `0.9973662430219584`
  - `1.234567890123`     → `1.234567890122`
  - `0.123451`           → `0.12345`

Tell the user the computed amount so they can correct it: "Using `<amount>` — your full balance minus a tiny margin."

Pre-conditions; if any fails, ask the user for an explicit amount instead of applying the recipe:

  - Balance has at least 6 significant digits after step 2's trim (`0.5`, `1.5`, `2.001` do not qualify).
  - Balance is greater than zero.

### Extra step for NATIVE token full-balance operations

If the token being operated on is native (ETH on Ethereum/Base, BNB on BSC, POL on Polygon, etc.), the same wallet balance pays both the operation amount AND the transaction's gas. Without a gas reserve the simulation reverts "insufficient balance for gas". Apply this step AFTER the trim recipe above:

1. Run `web3 chain gas <chain>` and read `gas_price_gwei`.
2. Estimate the gas limit for the intended operation. The backend runs `eth_estimateGas` on the RPC node during transaction building and applies a 1.2× safety buffer automatically. For the pre-flight reserve calculation here, use a conservative upper-bound estimate:
   - If a previous build attempt returned a `gas_limit` in the intent, use that value × 1.5.
   - Otherwise, fall back to `500_000` as a safe default for most operations.
3. Compute reserve in native units:

   `gas_reserve = gas_price_gwei × estimated_gas_limit × 10⁻⁹`

4. Subtract `gas_reserve` from the trimmed amount.

Example (BSC): `gas_price_gwei = 0.1`, estimated gas = `500_000` → `gas_reserve = 0.1 × 500_000 × 10⁻⁹ = 0.00005 BNB`.

ERC-20 full operations (token ≠ native) do NOT deduct gas from the amount — gas is paid in native and is unrelated to the ERC-20 balance. Only verify the wallet has a non-zero native balance before proceeding (already covered by `~/.claude/skills/_shared/preflight.md`).

When telling the user, include both deductions: "Using `<amount>` `<native_symbol>` (full balance minus a tiny margin and ~`<reserve>` `<native_symbol>` reserved for gas)."

## NEVER

<NEVER>
Never silently treat a fiat-denominated amount as a token quantity.
Never copy the portfolio balance string verbatim into `--amount` for "all / 全部" operations — the rendered balance can exceed the on-chain raw balance by a sub-unit rounding margin and will cause the build's simulation to revert.
Never reverse-calculate gas_limit from a broadcast error message. The error `"insufficient funds for gas * price + value"` reports `tx cost = gas_limit × gas_price + value` but dividing tx_cost by gas_price to "recover" gas_limit produces wrong results because of rounding. The authoritative gas_limit is always the value in the build response's `gas_limit` field. If you need to diagnose a broadcast failure, read the intent's `gas_limit` from the build response, not from arithmetic on error messages.
Never tell the user the gas limit is "unstable" or "fluctuating" based on error message arithmetic. Transfer gas limits are fixed values set by the backend (70k for native, 120k for ERC-20). If a broadcast fails with "insufficient funds", the problem is that balance < gas_fee + value at current gas prices, not that the gas limit changed.
</NEVER>
