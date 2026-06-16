---
name: web3-token
description: "Use when: token search, token info, token price, token metadata, find token address, search by symbol, get decimals. 中文: 代币搜索, 代币信息, 代币价格, 查代币, 搜索代币, 代币地址."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 Token — Search & Info

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Token search, metadata, and price across blockchains. Used to resolve symbols to addresses before swaps.

## Step 0 — Re-route check

- **Same-chain token swap** — HARD BLOCK: route to `web3-swap`.
- **Cross-chain bridge transfer** — HARD BLOCK: route to `web3-bridge`.
- **Token transfer (same-chain native or ERC-20)** — HARD BLOCK: route to `web3-chain`.
- **Portfolio / balance / USD value** — HARD BLOCK: route to `web3-portfolio`.
- **Pre-execution security check or override** — HARD BLOCK: route to `web3-security`.
- **Sign transaction / EIP-7702 delegation / account status** — HARD BLOCK: route to `web3-ssm`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Token search / metadata / price** → stay in this skill.

## Required Parameters

All parameters below must be explicitly stated by the user. Do NOT infer, assume, or default any of them — ask the user first if not clearly specified.

| Parameter | Rule |
|-----------|------|
| `--chain` | Must be confirmed by the user; the CLI has no default. Token addresses, decimals, and prices differ per chain. |
| `--query` / token address | Must be confirmed. The specific token or search term the user wants. |

## Token Search

```
web3 token search --query USDC --chain ethereum
```

`token search` searches by symbol/name text. It does **not** support native tokens (ETH/BNB/POL have no contract address to search). For native token price or info, use `web3 token info --chain <chain> native`.

<NEVER>
Never use `--networks` — the flag is `--chain` (consistent with all other commands).
Never use `token search` to look up native token price — use `token info --chain <chain> native`.
</NEVER>

### Displaying Results

<MUST>
Show results as a list with key details:

> **Search results for "`<query>`":**
> 
> 1. `<symbol>` — `<chain>` — `<address_short>` — $`<price>`
> 2. `<symbol>` — `<chain>` — `<address_short>` — $`<price>`
> 3. `<symbol>` — `<chain>` — `<address_short>` — $`<price>`
> 
> Which one would you like details on?

Multiple results → let user pick. Single result → show details and confirm.
Never assume the first result is the one the user wants.
</MUST>

### When No Results

> "No tokens found matching 'X'. Try a different search term or provide the contract address directly."

## Token Info

For ERC-20 tokens:
```
web3 token info --chain ethereum 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48
```

For native tokens:
```
web3 token info --chain ethereum native
```

<MUST>
Present token info:

> **`<symbol>` — `<name>`**
> 
> | Detail | Value |
> |--------|-------|
> | Chain | `<chain>` |
> | Address | `<address_short>` |
> | Decimals | `<decimals>` |
> | Price | $`<price>` |
> | Market Cap | $`<market_cap>` |
> | Liquidity | $`<liquidity>` |

Then suggest next steps: "Would you like to check your balance, see a price chart, or swap this token?"
</MUST>

## Safety Rules

<MUST>
1. **Contract address is the only reliable identifier** — always show it alongside symbol
2. **Community recognition is informational** — does NOT guarantee safety. State it as context only
3. **Unverified tokens**: if token is not widely recognized, warn: "This token is not widely recognized. Verify the contract independently before trading."
4. **Low liquidity <$10K**: warn about high slippage risk
5. **Very low liquidity <$1K**: strongly warn — "Trading this token may result in significant losses"
</MUST>

## Display Rules

- Token amounts: UI units with appropriate precision
- Market cap / liquidity: shorthand (`$1.2B`, `$45M`)
- Contract addresses: abbreviated `0x1234...abcd`
- Native tokens: `(native)` tag instead of address
- Always show full address on first mention; abbreviate on subsequent mentions

## Edge Cases

| Scenario | Handling |
|----------|----------|
| Token not found | "Token not found. Try verifying the address — symbols can collide across chains." |
| Same symbol, multiple chains | Show ALL matches, let user pick |
| Network error | Retry once |

## Error Codes

If the command returns an `error.code` (e.g. `portals_error` when the Portals upstream is down, `unsupported_chain` for an invalid `--chain`), consult `~/.claude/skills/_shared/error-codes.md`.
