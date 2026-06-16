# Web3 Chain — CLI Reference

## `web3 chain list`

List supported chains and canonical token addresses. This is a public metadata call.

**Return:** Array of chain objects with `name`, `chain_id`, `native_symbol`, and `canonical_tokens`.

## `web3 chain gas <chain>`

Current gas price.

**Return fields:**

| Field | Type | Description |
|-------|------|-------------|
| `chain` | String | Chain name |
| `chain_id` | Integer | Chain ID |
| `gas_price_wei` | String | Raw wei price |
| `gas_price_gwei` | String | Gwei (human-readable) |

## `web3 chain broadcast <chain> --transaction-id <id>`

Broadcast a transaction intent that has already passed security checks and been signed. Only `--transaction-id` is accepted.

| Param | Required | Description |
|-------|----------|-------------|
| `chain` (arg) | Yes | Chain name |
| `--transaction-id` | Yes | Transaction intent ID |

**Return fields:**

| Field | Type | Description |
|-------|------|-------------|
| `status` | String | `submitted` |
| `transaction_id` | String | Transaction intent ID |
| `chain` | String | Chain name |
| `hash` | String | Submitted transaction hash |

## `web3 chain tx <chain> <hash>`

Transaction status.

**Return fields:**

| Field | Type | Description |
|-------|------|-------------|
| `chain` | String | Chain name |
| `hash` | String | Transaction hash |
| `status` | String | `confirmed`, `failed`, or `not_found` |
| `block_number` | Integer | Block number (when confirmed) |
| `gas_used` | Integer | Gas consumed |

## `web3 chain transfer`

Build a native or ERC-20 transfer transaction intent. This command does not run security checks, sign, or broadcast.

| Param | Required | Description |
|-------|----------|-------------|
| `--chain` | Yes | Chain name. No default; user must explicitly confirm the target chain. |
| `--token` | Yes | `native` or ERC-20 contract address — see `_shared/token-resolution.md`. |
| `--to` | Yes | Recipient address |
| `--amount` | Yes | Human-readable amount (decimals are resolved for you). |
| `--wallet` | Yes | Wallet ID used for signing |

**Return:** `status=built`, `transaction_id`, `summary`, and `expires_at`.

After build, run `web3 security check --transaction-id <transaction_id>`. Only sign with `web3 ssm sign --transaction-id <transaction_id>` after the security check clears the intent (per `_shared/security-handling.md`). Broadcast with `web3 chain broadcast <chain> --transaction-id <transaction_id>` after signing.
