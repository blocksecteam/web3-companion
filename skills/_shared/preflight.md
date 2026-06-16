# Shared Pre-flight: Required Parameters, Delegation, Portfolio & Balance

Used by web3-swap, web3-bridge, web3-chain, web3-ssm. Apply the parameter discipline below for every write request; run the checks below before any write operation.

## 0. Required Parameters

Every parameter listed in the calling skill's "Required Parameters" table must be **explicitly stated by the user in the current request**. Do NOT infer, assume, default, or carry over from earlier conversation context — ask the user first if any value has not been clearly stated in this message.

This applies regardless of how many parameters the skill defines (two for delegate, five for chain transfer, six for swap, eight for bridge) and is the source of the per-skill rule "Must be confirmed" entries.

## 1. Account readiness

Run `web3 context status` and check the top-level `readiness` field **before** looking at any wallet's `chains[]`:

- `ready`: continue to step 2.
- `needs_wallet` (or `wallets[]` empty): tell the user no wallet exists yet and direct them to the Wallets page to create one. Stop the write flow.
- `pending_delegation`: a delegation transaction is still confirming for one of the wallets. Tell the user to retry in 15–60 seconds. Stop the write flow.
- `status_unavailable`: surface the `blocking_reason` field to the user and stop. Do not attempt the write.

## 2. Delegation Check

Once `readiness=ready`, find the wallet being used and check `chains[]` for the relevant chain's `delegation_status`:

- Missing from `chains[]` or `not_delegated`: stop the write flow and follow the explicit delegation/insufficient-gas flow in `web3-ssm` before continuing.
- `pending_delegation`: tell the user to wait for on-chain confirmation (~15–60 seconds), then retry.
- `delegated`: continue.
- Explicit delegation returns `delegation_confirmed`: delegation confirmed on-chain. Ask the user before retrying the write flow from the beginning.

## 3. Portfolio & Price Lookup

Run portfolio before any price query:

```
web3 portfolio --wallet <wallet-id> --chain <chain>
```

Price lookup priority:
1. Portfolio `balances[].price`
2. Native token not in portfolio → `web3 token info --chain <chain> native`
3. ERC-20 with known address not in portfolio → `web3 token info --chain <chain> <address>`
4. Unknown symbol → `web3 token search --query <symbol> --chain <chain>`; use the search result price directly

## 4. Balance & Gas Check

Before comparing balance:
- If the operation amount has no decimal point and would exceed the user's balance for that token, ask the user first: "Did you mean X in base units (= Y tokens)?" Get decimals via `web3 token info --chain <chain> <address|native>`, compute `human_amount = raw_amount / 10^decimals`, and confirm before proceeding. Do not report insufficient balance until the amount is confirmed as human-readable.

Then verify:
- Source token balance ≥ operation amount
- Source chain native token balance > 0 (for gas)

### Dust balance feasibility check (native token full-balance transfers)

When the user wants to transfer their entire native token balance, check whether the transfer is feasible **before** building:

1. Read `gas_price_gwei` from `web3 chain gas <chain>`.
2. Compute minimum gas fee: `min_gas_fee = gas_price_gwei × 70_000 × 10⁻⁹` (native transfer gas limit is 70k).
3. If `balance ≤ min_gas_fee`, the transfer is **not feasible** at current gas prices. Tell the user: "Your balance of `<balance>` `<symbol>` is not enough to cover gas fees (~`<min_gas_fee>` `<symbol>` at current `<gas_price>` Gwei). You'd need to wait for lower gas prices or receive more funds." **Stop the flow** — do not build, sign, or broadcast.
4. If `balance > min_gas_fee`, proceed with the amount-resolution recipe to calculate the transferable amount.
