---
name: web3-bridge
description: "Use when: bridge tokens, cross-chain swap, move assets between chains, cross-chain transfer, bridge quote. NOT for same-chain swaps. 中文: 跨链, 桥接, 跨链桥, 跨链转账, 资产跨链（仅限跨链，不含同链兑换）."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 Bridge — Cross-Chain Swap

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Available bridge adapters are dynamic — run `web3 bridge protocols` to get the current list. Quotes fan out to compatible adapters unless a specific `--bridge` is selected.

**Cross-chain transfers are NOT atomic.** Funds may be in transit for seconds to minutes.

## Step 0 — Re-route check

- **Same-chain token swap** — HARD BLOCK: route to `web3-swap`.
- **Token transfer (same-chain native or ERC-20)** — HARD BLOCK: route to `web3-chain`.
- **Portfolio / balance / USD value** — HARD BLOCK: route to `web3-portfolio`.
- **Token search / metadata / price** — HARD BLOCK: route to `web3-token`.
- **Pre-execution security check or override** — HARD BLOCK: route to `web3-security`.
- **Sign transaction / EIP-7702 delegation / account status** — HARD BLOCK: route to `web3-ssm`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Cross-chain bridge transfer** → stay in this skill.

## Pre-flight Checks

<MUST>
At the start of every bridge conversation, run:
```
web3 chain list
web3 bridge protocols
```
The output goes into your context for later token / bridge resolution — do not dump it to the user. Only surface it when relevant: if `from_chain` or `to_chain` is not in the list, tell them "Supported chains are: [list]. Bridge is only available between these chains." (Respond in the user's language.)

Then follow `~/.claude/skills/_shared/preflight.md` for delegation check, portfolio, balance, and gas verification before requesting a quote.
</MUST>

## Bridge Modes — explain to the user before they pick

`web3 bridge protocols` returns one entry per supported bridge with these fields:

| Field               | Meaning                                                                                  |
|---------------------|------------------------------------------------------------------------------------------|
| `name`              | bridge identifier (e.g. `bungee`, `relay`, `lifi`)                                        |
| `mode`              | how the bridge collects user funds (see below)                                            |
| `approve_style`     | only present when `mode == "approve"`; `erc20` or `permit2`                              |
| `supports_slippage` | whether `--slippage` actually affects what the user receives                              |

Two values for `mode`, with very different UX:

- **`mode: "approve"`** — the bridge runs an on-chain swap on the source chain. You authorise (approve) the bridge router to take your tokens, and the router performs a swap in the same transaction. If `supports_slippage: true`, the minimum output is enforced on-chain — too-large a price move makes the tx revert without losing funds. Explain to the user: *"This bridge will run a swap on your source chain. You can specify a slippage percent; if the price moves more than that, the transaction reverts and you keep your funds."*
- **`mode: "deposit"`** — the bridge gives you a one-time deposit address. You transfer your tokens to it; the bridge service determines the destination amount at execution time. Usually `supports_slippage: false`. Explain to the user: *"This bridge takes your tokens and the service decides the final exchange rate when they fulfil the cross-chain delivery. The slippage parameter has no effect here — you accept whatever rate the bridge quotes."*

`supports_slippage` is the **authoritative** field, not `mode`. Some `mode: "approve"` bridges (e.g. canonical L2 bridges that are 1:1 lock-and-mint) still have `supports_slippage: false`, and the agent should follow the field, not infer from mode.

## Slippage

Follow `~/.claude/skills/_shared/slippage.md` for the default-announcement script, `supports_slippage` decision tree, and suggested ranges. `supports_slippage` here is the authoritative field — do not infer from bridge `mode`.

## Required Parameters

See `~/.claude/skills/_shared/preflight.md` § Required Parameters. The table below lists this skill's eight parameters.

| Parameter | Rule |
|-----------|------|
| `--from-chain` | Must be confirmed by the user; the CLI has no default. |
| `--to-chain` | Must be confirmed. No default. |
| `--from` | Must be confirmed. Contract address, or `native` for the source chain's native token. Never a bare symbol — see Token Resolution below. |
| `--to` | Must be confirmed. Contract address, or `native` for the destination chain's native token. Do not assume it matches the source token — ETH on Ethereum and ETH/WETH/BNB on BSC are different choices. Never a bare symbol — see Token Resolution below. |
| `--amount` | Must be confirmed. Human-readable token quantity. |
| `--from-address` | Required for `quote` only. `build` does not accept this flag — the wallet's address is resolved from `--wallet`. |
| `--bridge` | Must be confirmed for `build`. Bridge protocol to use (e.g. `relay`). |
| `--wallet` | Must be confirmed for `build`. Wallet ID used to sign the transaction. |

## Token Resolution

Follow `~/.claude/skills/_shared/token-resolution.md` for symbol → address resolution, decimals handling, and `native` usage. Bridge is cross-chain so resolve `--from` and `--to` independently per their respective chains.

## Amount Resolution

Follow `~/.claude/skills/_shared/amount-resolution.md`.

**MUST read that file before issuing the CLI command** when the user's request matches any of:

- "all" / "max" / "全部" / "the whole balance" / "drain it" / equivalents meaning the entire token balance
- A fiat amount (USD / $ / CNY / ¥ / EUR, etc.)
- A bare number with no unit on a non-stablecoin token

Each case has a specific procedure in the shared doc (full-balance trim recipe and gas reserve, fiat conversion, ambiguity handling). Skipping the shared doc is what triggers the "I thought I had enough balance" simulation reverts.

**Only exact-amount-in is supported.** There is no exact-amount-out. When the user expresses a desired output amount or output fiat value (e.g. "bridge $5 worth of USDC to Base"), convert to the equivalent input token quantity and confirm with the user.

<NEVER>
Never pass an output token quantity as --amount (exact-amount-out is not supported).
</NEVER>

## Getting a Quote

Run the quote command — `--from-address` is required:

```
web3 bridge quote --from-chain ethereum --to-chain base --from 0xA0b8... --to 0x8335... --amount 500 --from-address 0xabc...
```

`--from-address` is required for `quote`. `build` does not accept this flag — the wallet's address is resolved from `--wallet`. `--bridge` is optional for quote; omit to fan out to all adapters. `--amount` is human-readable (see `_shared/token-resolution.md`).

The system queries compatible bridge adapters and returns only the successful quotes in `bridges[]`. If every adapter fails, the response is HTTP 503 with `error.code = "no_quote"` and the per-adapter errors under `all_results` — there is no `all_results` field on the success path.

<MUST>
Present the route comparison:

> **Bridge routes: `<from_chain>` → `<to_chain>`**
> 
> | # | Bridge | Receive |
> |---|--------|---------|
> | 1 | `<bridge_a>` | `<out_amount_a>` `<to_symbol>` |
> | 2 | `<bridge_b>` | `<out_amount_b>` `<to_symbol>` |
> 
> Recommended: `<bridge_a>` (best rate). Which route would you prefer?

Each entry in `bridges[]` has only `bridge`, `in_amount`, `out_amount` — do not invent fee or ETA columns.

Only auto-select when exactly one route exists. With multiple routes, let the user choose.

If the response is HTTP 503 with `error.code = "no_quote"`, say the bridge route is not available through this service right now and do not imply funds can be bridged.
</MUST>

## Execution

After user selects a route and confirms.

### Broadcast Output

Follow `~/.claude/skills/_shared/broadcast-output.md` for rendering rules. Use this bridge-specific field table:

**Bridge broadcast — \<from_chain\> → \<to_chain\>**

| Field    | Value                                              |
|----------|----------------------------------------------------|
| From     | \<amount\> \<from_token_symbol\> on \<from_chain\> |
| Bridge   | \<bridge\>                                         |
| Tx hash  | `0xabc1234…<last8>`                                |
| Explorer | [\<chain explorer name\>](https://\<base-url\>/0xabc1234...full...hash) |

Substitute `<chain explorer name>` and `<base-url>` from the chain → explorer table in `_shared/broadcast-output.md` (use the source chain for bridges); never hardcode etherscan.io for non-Ethereum chains. Source the other fields from the earlier `web3 bridge build` response's `summary`:
- `summary.bridge` → bridge name
- `summary.from_chain`, `summary.to_chain` → header / labels
- `summary.from_token`, `summary.to_token` → token addresses (resolve to symbol; `native` = chain's native symbol)
- `summary.amount` → human-readable source amount

Never invent output amounts, fees, or ETAs (the build/broadcast responses do not include them). `summary.deposit_address` is an internal field — do not surface it to the user.

Funds always arrive at the source wallet on the destination chain — bridges do not accept a third-party recipient. If the user wants to send to someone else on the destination chain, bridge to self first, then run `web3 chain transfer` on the destination chain.

```
web3 bridge build --wallet <wallet_id> --bridge relay --from-chain ethereum --to-chain base --from 0xA0b8... --to 0x8335... --amount 500
```

`web3 bridge build` only creates the transaction intent. It does not run security checks, sign, or broadcast.

**Policy note**: the recipient whitelist policy applies only to `chain transfer`. Bridge sign decisions skip the recipient whitelist rule; other policy rules (per-tx limit, daily limit, etc.) still apply.

Follow `~/.claude/skills/_shared/security-handling.md` for the security check → decision → sign → broadcast flow. The bridge build uses the source chain in the final broadcast step:

```
web3 chain broadcast <from-chain> --transaction-id <transaction_id>
```

## Intent Expiry

If any command (security check, sign, broadcast) returns HTTP 410 with `transaction_intent_expired`, read `~/.claude/skills/_shared/intent-expiry.md` and follow the bridge recovery path. Deposit-mode bridges (those with `mode: deposit` in `web3 bridge protocols`) have a stronger reason than swaps to honour the window — sending to a stale deposit address risks refund or stuck funds.

## Other Error Codes

For any other `error.code` returned by quote/build/sign/broadcast (`unsupported_chain`, `unknown_bridge`, `unsupported_output`, `quote_failed`, `build_failed`, `wrap_failed`, `not_delegated`, `pending_delegation`, `policy_rejected`, `invalid_amount`, `decimals_unresolvable`, etc.) consult `~/.claude/skills/_shared/error-codes.md`. `no_quote` is handled inline in the Getting a Quote section.

## Risk Controls

When any of the conditions below triggers, **stop and wait for explicit user confirmation before continuing** — even if the user has previously said "just quote and build" or similar. These are user-facing reminders, not programmatic gates.

| Situation | What to tell the user |
|---|---|
| Every adapter returned no quote (`all_results` all error / `no_quote`) | "No bridge route available right now. Want to try a transit token (USDC / native), a smaller amount, or retry later?" Do not retry build automatically. |
| Insufficient source balance for the amount or for native gas | "You have `<balance>` `<symbol>` but need `<required>`." Show the gap explicitly and wait for user instruction. |

<NEVER>
- Never say "bridge complete" / "funds arrived" before the destination-chain balance change is observed. The source-chain broadcast only proves the deposit transaction was submitted. To confirm arrival, run `web3 portfolio --wallet <id> --chain <to-chain>` and look for the expected balance increase.
- Never skip the pre-flight balance check.
</NEVER>

## Post-Bridge Suggestions

A bridge completes in two stages: the source-chain tx confirms, then funds arrive on the destination chain (seconds to minutes later, depending on the bridge). After a bridge broadcast, suggest (in the user's language):

- Source-chain confirmation: "I can verify the source-chain submission confirmed" → `web3 chain tx <from-chain> <hash>`.
- Destination-chain arrival: "After a short wait, I can confirm funds arrived" → `web3 portfolio --wallet <wallet_id> --chain <to-chain>`.
- Next action: "Anything else on `<destination chain>` once funds arrive?"
