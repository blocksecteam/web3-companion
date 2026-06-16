# Web3 Bridge — CLI Reference

## `web3 bridge protocols`

List supported bridge protocols.

**Return:** `{ "protocols": [{ "name", "mode", "approve_style", "supports_slippage" }, ...] }`.

- `name` — adapter id used in `--bridge`.
- `mode` — `"approve"` (bridge pulls funds via ERC-20 approve + router call) or `"deposit"` (user sends funds to a per-quote deposit address). Affects recovery semantics on intent expiry — see `_shared/intent-expiry.md`.
- `approve_style` — present for approve-mode adapters; `"erc20"` or `"permit2"`. Agent does not need to read or act on this field.
- `supports_slippage` — whether `--slippage` actually affects the quote. Deposit-mode bridges silently drop slippage (see `web3-bridge/SKILL.md`).

Run the command yourself to get the current list — do not hardcode protocol names or capability values; the supported set and per-adapter capabilities can change between deployments.

## `web3 bridge quote`

Get one bridge quote or a smart quote across all compatible bridge adapters.

| Param | Required | Description |
|-------|----------|-------------|
| `--from-chain` | Yes | Source chain name |
| `--to-chain` | Yes | Destination chain name |
| `--from` | Yes | Source token: contract address or `native` — see `_shared/token-resolution.md`. |
| `--to` | Yes | Destination token: contract address or `native` — see `_shared/token-resolution.md`. Must be confirmed with the user. Always pass `--to` explicitly even when the source and destination token symbols look identical — USDC vs USDC.e on different chains are not interchangeable. |
| `--amount` | Yes | Human-readable amount (e.g. `500`, `0.001`). The CLI handles decimals for you — do not pass wei. |
| `--from-address` | Yes | Wallet/source address |
| `--bridge` | No | Specific bridge protocol from `web3 bridge protocols`. Omit for smart-quote fan-out. |
| `--slippage` | No | Slippage percent |

**Return:** Smart quote success (HTTP 200): `{ "from_chain", "to_chain", "from_token", "to_token", "bridges": [{ "bridge", "in_amount", "out_amount" }, ...] }` sorted by `out_amount` descending. If every adapter fails: HTTP 503 with `{ "error": { "code": "no_quote", "message": ... }, "all_results": [...] }`. For a specific `--bridge`, the direct quote object.

## `web3 bridge build`

Create the selected bridge transaction intent. This command does not run security checks, sign, or broadcast.

| Param | Required | Description |
|-------|----------|-------------|
| `--wallet` | Yes | Wallet ID |
| `--from-chain` | Yes | Source chain name |
| `--to-chain` | Yes | Destination chain name |
| `--from` | Yes | Source token: contract address or `native` — see `_shared/token-resolution.md`. |
| `--to` | Yes | Destination token: contract address or `native` — see `_shared/token-resolution.md`. Must be confirmed with the user. Always pass `--to` explicitly even when the source and destination token symbols look identical — USDC vs USDC.e on different chains are not interchangeable. |
| `--amount` | Yes | Human-readable amount (e.g. `500`, `0.001`). The CLI handles decimals for you — do not pass wei. |
| `--bridge` | Yes | Bridge protocol name |
| `--slippage` | No | Slippage percent |

**Return:** transaction intent fields including `transaction_id`, `status`, `summary`, and `expires_at`.

After build, run `web3 security check --transaction-id <transaction_id>`. Only sign with `web3 ssm sign --transaction-id <transaction_id>` after the security check clears the intent (per `_shared/security-handling.md`).
