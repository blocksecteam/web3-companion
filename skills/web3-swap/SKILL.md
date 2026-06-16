---
name: web3-swap
description: "Use when: swap tokens, trade, exchange, buy tokens, sell tokens, convert crypto, get swap quote, best swap route, DEX aggregation (same-chain only). NOT for cross-chain or bridge transfers. 中文: 兑换, 交易, 换币, swap, 买卖, 币币兑换, 最优路径, 报价（仅限同链，不含跨链）."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 Swap — Aggregated DEX

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Supported protocols are dynamic — run `web3 swap protocols` to get the current list. Live quote and execute support depends on each adapter; unimplemented adapters return explicit errors and are excluded from best-route selection.

## Step 0 — Re-route check

- **Cross-chain bridge transfer** — HARD BLOCK: route to `web3-bridge`.
- **Token transfer (same-chain native or ERC-20)** — HARD BLOCK: route to `web3-chain`.
- **Portfolio / balance / USD value** — HARD BLOCK: route to `web3-portfolio`.
- **Token search / metadata / price** — HARD BLOCK: route to `web3-token`.
- **Pre-execution security check or override** — HARD BLOCK: route to `web3-security`.
- **Sign transaction / EIP-7702 delegation / account status** — HARD BLOCK: route to `web3-ssm`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Same-chain token swap (DEX)** → stay in this skill.

## Pre-flight Checks

<MUST>
At the start of every swap conversation, run:
```
web3 chain list
web3 swap protocols
```
The output goes into your context for later token / protocol resolution — do not dump it to the user. Only surface it when relevant: if the user picks a chain not in the list, tell them "Supported chains are: [list]." (Respond in the user's language.) If the user asks what swap protocols are available, list them then.

Then follow `~/.claude/skills/_shared/preflight.md` for delegation check, portfolio, balance, and gas verification before executing.
</MUST>

## Required Parameters

See `~/.claude/skills/_shared/preflight.md` § Required Parameters. The table below lists this skill's six parameters.

| Parameter | Rule |
|-----------|------|
| `--chain` | Must be confirmed by the user; the CLI has no default. |
| `--from` | Must be confirmed. Contract address, or `native` for the chain's native token. Never a bare symbol — see Token Resolution below. |
| `--to` | Must be confirmed. Contract address, or `native` for the chain's native token. Never a bare symbol — see Token Resolution below. |
| `--amount` | Must be confirmed. Human-readable token quantity. |
| `--from-address` | Required for `quote` only. `build` does not accept this flag — the wallet's address is resolved from `--wallet`. |
| `--wallet` | Must be confirmed for `build`. Wallet ID used to sign the transaction. |

## Token Resolution

Follow `~/.claude/skills/_shared/token-resolution.md` for symbol → address resolution, decimals handling, and `native` usage.

## Amount Resolution

Follow `~/.claude/skills/_shared/amount-resolution.md`.

**MUST read that file before issuing the CLI command** when the user's request matches any of:

- "all" / "max" / "全部" / "the whole balance" / "drain it" / equivalents meaning the entire token balance
- A fiat amount (USD / $ / CNY / ¥ / EUR, etc.)
- A bare number with no unit on a non-stablecoin token

Each case has a specific procedure in the shared doc (full-balance trim recipe and gas reserve, fiat conversion, ambiguity handling). Skipping the shared doc is what triggers the "I thought I had enough balance" simulation reverts.

**Only exact-amount-in is supported.** There is no exact-amount-out. When the user expresses a desired output amount (e.g. "get 10 AAVE") or an output fiat value (e.g. "receive $5 of AAVE"), convert to the equivalent input token quantity and confirm with the user.

<NEVER>
Never pass an output token quantity as --amount (exact-amount-out is not supported).
</NEVER>

## Getting a Quote

```
web3 swap quote --chain ethereum --from-address 0xabc... --from native --to 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48 --amount 1
```

If the user doesn't specify a protocol, the system fans out to all compatible adapters and returns the successful quotes in `protocols[]` (sorted best first). If every adapter fails, the response is HTTP 503 with `error.code = "no_quote"` and the per-adapter errors under `all_results` — there is no `all_results` field on the success path. Unimplemented or failing adapters never appear in `protocols[]` and must not be presented as usable routes.

`--from-address` is required for `quote`. `build` does not accept this flag — the wallet's address is resolved from `--wallet`. `--amount` is human-readable (see `_shared/token-resolution.md`).

### Slippage

Follow `~/.claude/skills/_shared/slippage.md` for the default-announcement script, `supports_slippage` decision tree, and suggested ranges.

### Displaying the Quote

<MUST>
Present the quote with only the fields the response actually returns. Format `in_amount` and `out_amount` to human units using the source/target token decimals. Do not invent price impact, gas estimate, or route — the quote response does not include them.

> **Best route: `<protocol>`**
> 
> | Detail | Value |
> |--------|-------|
> | Swap | `<from_amount>` `<from_symbol>` → ~`<to_amount>` `<to_symbol>` |
> | Slippage | `<slippage>`% |
> 
> Would you like to proceed?
</MUST>

### When multiple protocols return quotes

<SHOULD>
Give the user context: name the winning protocol + which others also returned a quote and which had no route. Example shape (translate to user's language): "`<best_protocol>` has the best rate. `<other_returning_protocol>` also returned a quote. The others had no route or are not implemented for this pair."
</SHOULD>

## Build, Security Review, Sign, Broadcast

After the user confirms the quote, create the transaction intent with:
```
web3 swap build --wallet <wallet_id> --chain <chain> --from <from_address> --to <to_address> --amount <amount> --protocol <chosen_protocol> [--slippage <percent>]
```
`--slippage` defaults to 0.5%. Override per `_shared/slippage.md`.

`web3 swap build` only creates the transaction intent. It does not run security checks, sign, or broadcast.

Follow `~/.claude/skills/_shared/security-handling.md` for the security check → decision → sign → broadcast flow. After broadcast returns `submitted`, also follow `~/.claude/skills/_shared/broadcast-output.md` for the rendering rules, then apply the swap-specific field table in **Execution Output** below.

### Swap-specific build summary fields

When you pull fields for the broadcast output, source them from the earlier `web3 swap build` response's `summary`:

- **From / To**: format `summary.input_amount` and `summary.output_amount` (raw integer wei) into human units using the source/target token decimals, then label with the token symbol. Resolve `summary.source_asset` and `summary.target_asset` addresses; for `"native"`, look up the chain's `native_symbol` from `web3 chain list`.
- **Protocol**: `summary.protocol` verbatim.
- **Tx hash / Explorer**: from the broadcast response.

Show only the fields you actually have. Never invent amounts or token symbols.

<NEVER>
Never manually reconstruct swap transactions from `to/value/data`. `web3 swap build` returns a `transaction_id` and everything downstream (sign, broadcast) keys off that id.
</NEVER>

### Execution Output

Follow `~/.claude/skills/_shared/broadcast-output.md` for rendering rules (no fenced block, hash truncation, explorer table, status wording). Use this swap-specific field table:

**Swap broadcast — \<chain\>**

| Field    | Value                                              |
|----------|----------------------------------------------------|
| From     | \<from_amount\> \<from_symbol\>                    |
| To       | ~\<to_amount\> \<to_symbol\>                       |
| Protocol | \<protocol\>                                       |
| Tx hash  | `0xabc1234…<last8>`                                |
| Explorer | [\<chain explorer name\>](https://\<base-url\>/0xabc1234...full...hash) |

Substitute `<chain explorer name>` and `<base-url>` from the chain → explorer table in `_shared/broadcast-output.md`; never hardcode etherscan.io for non-Ethereum chains.

<NEVER>
- Never auto-confirm without showing the quote first
- Never execute without explicit user confirmation
</NEVER>

## Intent Expiry

If any command (security check, sign, broadcast) returns HTTP 410 with `transaction_intent_expired`, read `~/.claude/skills/_shared/intent-expiry.md` and follow the swap recovery path.

## Other Error Codes

For any other `error.code` returned by quote/build/sign/broadcast (`unsupported_chain`, `unknown_protocol`, `quote_failed`, `build_failed`, `not_delegated`, `pending_delegation`, `policy_rejected`, `invalid_amount`, `decimals_unresolvable`, etc.) consult `~/.claude/skills/_shared/error-codes.md`. `no_quote` is handled inline in the Getting a Quote section.

## Risk Controls

When any of the conditions below triggers, **stop and wait for explicit user confirmation before continuing** — even if the user has previously said "just quote and build" or similar. These are user-facing reminders, not programmatic gates.

| Situation | What to tell the user |
|---|---|
| Every adapter returned no quote (`all_results` all error) | "No route available for this pair right now. Want to try a smaller amount or a different pair?" Do not retry build automatically. |
| Insufficient source balance for the amount, or zero native for gas | "You have `<balance>` `<symbol>` but trying to swap `<amount>`." Show the gap explicitly; wait for user instruction. |
| Operation USD value is large (>$1K notional) | "This swap moves ~$`<X>`. Confirm the size before I quote." |

Slippage ranges live in `~/.claude/skills/_shared/slippage.md`.

## Post-Swap Suggestions

After a swap, suggest:
- Check new balance: "Want to verify your new balance?"
- Track the tx: "I can check when the transaction confirms."
- Another swap: "Need to swap anything else?"
