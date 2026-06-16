---
name: web3-portfolio
description: "Use when: check balance, wallet holdings, portfolio value, what tokens do I have, show my assets, account overview, total value. 中文: 余额, 持仓, 投资组合, 资产, 账户余额, 钱包余额."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 Portfolio — Account Balance & Value

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Read-only balance check with USD valuation. Any address can be queried — no risk to funds.

## Step 0 — Re-route check

- **Same-chain token swap** — HARD BLOCK: route to `web3-swap`.
- **Cross-chain bridge transfer** — HARD BLOCK: route to `web3-bridge`.
- **Token transfer (same-chain native or ERC-20)** — HARD BLOCK: route to `web3-chain`.
- **Token search / metadata / price** — HARD BLOCK: route to `web3-token`.
- **Pre-execution security check or override** — HARD BLOCK: route to `web3-security`.
- **Sign transaction / EIP-7702 delegation / account status** — HARD BLOCK: route to `web3-ssm`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Portfolio / balance / USD value** → stay in this skill.

## Querying

```
web3 portfolio <chain> <address>
```

For the user's own wallets, use `--wallet <wallet_id>` (ask the user which wallet if they have multiple) and let them specify the chain:

```
web3 portfolio --wallet <wallet_id> --chain <chain>
```

`--chain` also accepts a comma-separated list to fetch multiple chains in a single call:

```
web3 portfolio --wallet <wallet_id> --chain ethereum,base,bsc,polygon
```

See **Multi-chain queries** below for when to use this form.

<NEVER>
Never call `web3 portfolio` without both a chain AND an address (or `--wallet` flag).
Never guess the chain when the request is single-chain. Ask the user if their request mentions assets / balance without naming a chain. (Multi-chain queries below are the explicit exception — those are not guessing.)
Never guess a wallet address — use `--wallet <id>` or get the address from `web3 context status`.
</NEVER>

### Multi-chain queries

Triggers (use the CSV form only when the user's request matches one of these):

- Asks for "all chains" / "every chain" / "across chains" / "cross-chain total" / "all my assets" / 中文："所有链" / "跨链" / "全部资产" / "总资产"
- Names multiple specific chains: "my Ethereum and Base balance", "show me BSC and Polygon"

Procedure:

1. Run `web3 chain list` to get the deployment's supported set. Pick the subset the user asked for (all chains, or the named ones).
2. Pass the chains as CSV to `--chain`. **One call, regardless of chain count.**
3. The response merges balances across chains; each balance entry has a `network` field identifying which chain it came from.
4. Render grouped by chain — see Display Format below.

Do NOT issue multiple `web3 portfolio` calls in parallel for the multi-chain case — one CSV call is more efficient and returns the same data.

### Response

```json
{
  "chain": "ethereum",
  "address": "0xd8dA...6045",
  "total_usd": 125000.50,
  "balances": [
    { "symbol": "ETH", "name": "Ether", "balance": 50.0, "balanceUSD": 100000.00, "price": 2000.00, "address": "0x0000000000000000000000000000000000000000", "decimals": 18, "network": "ethereum" },
    { "symbol": "USDC", "name": "USD Coin", "balance": 25000, "balanceUSD": 25000.00, "price": 1.00, "address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "decimals": 6, "network": "ethereum" }
  ]
}
```

For multi-chain queries, the top-level `chain` field echoes the CSV input (e.g. `"ethereum,base"`) and `balances[]` interleaves entries from all queried chains, each tagged with its `network`. `total_usd` is the sum across all chains.

### Display Format

<MUST>
Present cleanly, sorted by USD value descending:

> **Portfolio — `<chain>` — `<address_short>`**
> 
> Total Value: **$`<total_usd>`**
> 
> | Token | Balance | Value |
> |-------|---------|-------|
> | `<native_symbol>` (native) | `<balance>` | $`<value_usd>` |
> | `<symbol>` `<address_short>` | `<balance>` | $`<value_usd>` |
> | `<symbol>` `<address_short>` | `<balance>` | $`<value_usd>` |

- Sort by USD value, highest first
- Token amounts in UI units
- USD values with 2 decimal places
- Abbreviated contract addresses alongside symbols
- Native tokens tagged `(native)`
</MUST>

For multi-chain queries, group balances by `network` and show a per-chain subtotal plus a cross-chain total:

> **Portfolio across chains — `<address_short>`**
> 
> Total Value: **$`<total_usd>`**
> 
> **`<chain_a>` — $`<subtotal_a_usd>`**
> 
> | Token | Balance | Value |
> |-------|---------|-------|
> | `<native_symbol_a>` (native) | `<balance>` | $`<value_usd>` |
> | `<symbol>` `<address_short>` | `<balance>` | $`<value_usd>` |
> 
> **`<chain_b>` — $`<subtotal_b_usd>`**
> 
> | Token | Balance | Value |
> |-------|---------|-------|
> | `<symbol>` `<address_short>` | `<balance>` | $`<value_usd>` |
> | `<symbol>` `<address_short>` | `<balance>` | $`<value_usd>` |

Chains with no balances are omitted. Same per-token rules apply within each group.

## Integration Patterns

### Pre-swap Balance Check

Before a swap, verify source token balance and native gas balance:
> "You have `<source_balance>` `<source_symbol>` and `<native_balance>` `<native_symbol>` for gas. Swapping `<amount>` `<source_symbol>` leaves `<remaining>` `<source_symbol>`. Proceed?"

### Pre-bridge Balance Check

Check both source token and gas token on source chain:
> "You have `<source_balance>` `<source_symbol>` on `<from_chain>` and `<native_balance>` `<native_symbol>` for gas. Hit confirm to proceed."

Portfolio responses contain only token balances and USD values — never invent a gas cost estimate or bridge fee figure in these prompts.

### Post-transaction Verification

After a swap, offer to verify:
> "Want me to check your updated balance?"

## Edge Cases

| Scenario | Display |
|----------|---------|
| Empty wallet | "No tokens found on `<chain>` — `<address>`" |
| Many tokens (>10) | Show top 10, note "+X more tokens" |
| Token without price | Show balance, `--` for USD value |
| Network error | Retry once |

## Error Codes

If the command returns an `error.code` (e.g. `portals_error` when the Portals upstream is down, `unsupported_chain` for an invalid `--chain`), consult `~/.claude/skills/_shared/error-codes.md`.
