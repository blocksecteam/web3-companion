---
name: web3-chain
description: "Use when: gas price, gas estimate, broadcast transaction, check tx status, transaction hash, chain info, gas cost, tx confirmed, transfer ETH, send tokens, transfer USDC, transfer USDT, send crypto, token transfer. 中文: gas费, gas估算, 广播交易, 查交易状态, 交易哈希, 链上确认, 转账, 发送代币, 转ETH, 转USDC, 转账代币."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 Chain — Gas, Broadcast & Status

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Gas price, estimation, broadcast, and transaction tracking.

## Step 0 — Re-route check

- **Same-chain token swap** — HARD BLOCK: route to `web3-swap`.
- **Cross-chain bridge transfer** — HARD BLOCK: route to `web3-bridge`.
- **Portfolio / balance / USD value** — HARD BLOCK: route to `web3-portfolio`.
- **Token search / metadata / price** — HARD BLOCK: route to `web3-token`.
- **Pre-execution security check or override** — HARD BLOCK: route to `web3-security`.
- **Sign transaction / EIP-7702 delegation / account status** — HARD BLOCK: route to `web3-ssm`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Token transfer (same-chain native or ERC-20)** → stay in this skill.
- **Broadcast signed tx / tx status / gas price** → stay in this skill.

## Token Transfer

<MUST>
At the start of every transfer conversation, run `web3 chain list`, then follow `~/.claude/skills/_shared/token-resolution.md` for symbol → address resolution and decimals handling.

Then follow `~/.claude/skills/_shared/preflight.md` for delegation check, portfolio, balance, and gas verification before building the transaction.
</MUST>

## Required Parameters

See `~/.claude/skills/_shared/preflight.md` § Required Parameters. The table below lists this skill's five parameters.

| Parameter | Rule |
|-----------|------|
| `--chain` | Must be confirmed by the user; the CLI has no default. Token addresses, decimals, and gas tokens differ per chain. |
| `--token` | Must be confirmed. Pass `native` or a contract address — never a bare symbol. |
| `--to` | Must be confirmed. Must be a 0x-prefixed 40-character hex address. ENS names (`alice.eth`), nicknames, or any non-hex format are not accepted — ask the user for the explicit 0x address. Never infer, reuse a previous address, or resolve the name yourself. |
| `--amount` | Must be confirmed. Human-readable token quantity. |
| `--wallet` | Must be confirmed. Wallet ID used to sign the transaction. |

### Native token transfer (ETH / BNB / POL)

```
web3 chain transfer --chain ethereum --token native --to 0x97d6... --amount 0.1 --wallet <wallet_id>
```

### ERC-20 token transfer

```
web3 chain transfer --chain ethereum \
  --token 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48 \
  --to 0x97d6... \
  --amount 0.1 \
  --wallet <wallet_id>
```

<NEVER>
Never construct ERC-20 calldata manually. Always use `web3 chain transfer` — the CLI handles encoding for you.
</NEVER>

### Amount Resolution

Follow `~/.claude/skills/_shared/amount-resolution.md`.

**MUST read that file before issuing the CLI command** when the user's request matches any of:

- "all" / "max" / "全部" / "the whole balance" / "drain it" / equivalents meaning the entire token balance
- A fiat amount (USD / $ / CNY / ¥ / EUR, etc.)
- A bare number with no unit on a non-stablecoin token

Each case has a specific procedure in the shared doc (full-balance trim recipe and gas reserve, fiat conversion, ambiguity handling). Skipping the shared doc is what triggers the "I thought I had enough balance" simulation reverts.

### Transfer flow

`web3 chain transfer` only creates the transaction intent. It returns `status=built` and `transaction_id` — no tx hash yet.

After that, follow `~/.claude/skills/_shared/security-handling.md` for the security check → decision → sign → broadcast flow.

<NEVER>
Never ask for or display a raw signed transaction. The user only sees `transaction_id`, `hash`, and the rendered broadcast table.
</NEVER>

### Transfer Broadcast Output

Follow `~/.claude/skills/_shared/broadcast-output.md` for rendering rules. Use this transfer-specific field table:

**Transfer broadcast — \<chain\>**

| Field    | Value                                              |
|----------|----------------------------------------------------|
| Amount   | \<amount\> \<token_symbol\>                        |
| From     | \<wallet_address\>                                 |
| To       | \<to\>                                             |
| Tx hash  | `0xabc1234…<last8>`                                |
| Explorer | [\<chain explorer name\>](https://\<base-url\>/0xabc1234...full...hash) |

Substitute `<chain explorer name>` and `<base-url>` from the chain → explorer table in `_shared/broadcast-output.md`; never hardcode etherscan.io for non-Ethereum chains. Source the other fields from the earlier `web3 chain transfer` response's `summary`:
- `summary.chain`
- `summary.token` — `native` or an ERC-20 contract address (resolve to symbol; `native` = chain's native symbol, e.g. ETH / BNB / POL)
- `summary.amount` — human-readable amount
- `summary.to` — recipient address
- `summary.wallet_address` — source wallet

Never invent token symbols or amounts.

## Intent Expiry

If any command (security check, sign, broadcast) returns HTTP 410 with `transaction_intent_expired`, read `~/.claude/skills/_shared/intent-expiry.md` and follow the chain transfer recovery path.

## Other Error Codes

For any other `error.code` returned by `web3 chain transfer` / `web3 chain broadcast` / `web3 chain tx` / `web3 chain gas` (`unsupported_chain`, `invalid_token`, `invalid_amount`, `not_delegated`, `pending_delegation`, `broadcast_failed`, `transaction_not_signed`, `signed_operation_not_found`, `decimals_error`, `rpc_error`, etc.) consult `~/.claude/skills/_shared/error-codes.md`.

## Risk Controls

When any of the conditions below triggers, **stop and wait for explicit user confirmation before continuing** — even if the user has previously said "just transfer" or similar. These are user-facing reminders, not programmatic gates.

| Situation | What to tell the user |
|---|---|
| Recipient address — always echo back before building | "Sending to `<full-address>` on `<chain>`. Confirm this is correct." Wait for explicit yes. |
| Insufficient source balance for the amount | "You have `<balance>` `<symbol>` but trying to send `<amount>`." Show the gap; wait for user instruction. |
| Operation USD value is large (>$1K notional) | "This transfer moves ~$`<X>`. Confirm the size before I build." |

## Gas Price

```
web3 chain gas ethereum
```

```json
{ "chain": "ethereum", "gas_price_gwei": "25.5" }
```

> "Current gas on `<chain>`: `<gwei>` Gwei"

## Transaction Status

```
web3 chain tx ethereum 0xabc123...
```

### Confirmed

```json
{ "status": "confirmed", "block_number": 18500000, "gas_used": 21000 }
```

> "Transaction confirmed in block `<block_number>`. Gas used: `<gas_used>`."

### Failed

```json
{ "status": "failed", "gas_used": 150000 }
```

> "Transaction failed on-chain. Gas consumed: `<gas_used>`. The state change was reverted."

### Not Found

```json
{ "status": "not_found" }
```

> "Transaction not yet indexed. Check again in a few seconds, or verify the hash."

## Display Rules

- Gas prices: Gwei (`25.5 Gwei`), never raw wei
- Transaction hashes: **full hash always inside the explorer link target**; the visible display cell may be shortened to `0xabcd…wxyz` for readability
