# Web3 Swap — CLI Reference

## `web3 swap protocols`

List supported swap protocols.

**Return:** `{ "protocols": [{ "name", "supports_slippage" }, ...] }`.

- `name` — protocol id used in `--protocol`.
- `supports_slippage` — whether `--slippage` actually affects the quote. AMM-style aggregators declare `true`; RFQ / limit-order / Dutch-auction adapters would declare `false`.

Run the command yourself to get the current list — do not hardcode protocol names; the supported set can change between deployments.

## `web3 swap quote`

Get swap quote from single protocol or best-of-all fan-out.

| Param | Required | Default | Description |
|-------|----------|---------|-------------|
| `--chain` | Yes | — | Chain name |
| `--from-address` | Yes | — | Wallet/source address |
| `--from` | Yes | — | Source token: contract address or `native` — see `_shared/token-resolution.md`. Also accepts EIP-155 placeholders `0xEee…E` and `0x0`. |
| `--to` | Yes | — | Target token: contract address or `native` — see `_shared/token-resolution.md`. |
| `--amount` | Yes | — | Human-readable amount (e.g. `0.001`). The CLI handles decimals for you — do not pass wei. |
| `--protocol` | No | — | Specific protocol (omit for best-of-all) |
| `--slippage` | No | 0.5 | Slippage tolerance (%) |

**Success response shape:**

```json
{
  "chain": "ethereum",
  "from_token": "0xEee...E",
  "to_token": "0xA0b8...6eB48",
  "protocols": [
    { "protocol": "odos", "in_amount": "1000000000000000000", "out_amount": "2450000000" },
    { "protocol": "kyber", "in_amount": "1000000000000000000", "out_amount": "2440000000" }
  ]
}
```

Fields are minimal by design. `in_amount` and `out_amount` are raw on-chain units (wei for native, micro-units for 6-decimal stables, etc.) — format with the source/target token decimals before showing the user. Higher-level fields like `price_impact`, `gas_estimate`, or `route` are **not** returned; do not invent them.

**No-quote / error response:**

```json
{
  "error": { "code": "no_quote", "message": "No protocol returned a valid quote" },
  "all_results": [
    { "protocol": "odos", "error": "..." },
    { "protocol": "kyber", "error": "..." }
  ]
}
```

**Error codes:**

| Exit | Meaning |
|------|---------|
| 0 | Success |
| 1 | No quote available / all protocols failed / unknown protocol |

## `web3 swap build`

Quote and create a transaction intent. This command does not run security checks, sign, or broadcast.

| Param | Required | Default | Description |
|-------|----------|---------|-------------|
| `--wallet` | Yes | — | Wallet ID |
| `--chain` | Yes | — | Source chain name. No default; user must explicitly confirm it. |
| `--from` | Yes | — | Source token: contract address or `native` — see `_shared/token-resolution.md`. Also accepts EIP-155 placeholders `0xEee…E` and `0x0`. |
| `--to` | Yes | — | Target token: contract address or `native` — see `_shared/token-resolution.md`. |
| `--amount` | Yes | — | Human-readable amount (e.g. `0.001`). The CLI handles decimals for you — do not pass wei. |
| `--protocol` | No | — | Specific protocol; omit for best-of-all fan-out (same semantics as `quote`) |
| `--slippage` | No | 0.5 | Slippage tolerance (%) |

**Return:** transaction intent fields including `transaction_id`, `status`, `summary`, and `expires_at`.

After build, run `web3 security check --transaction-id <transaction_id>`. Only sign with `web3 ssm sign --transaction-id <transaction_id>` after the security check clears the intent (per `_shared/security-handling.md`).
