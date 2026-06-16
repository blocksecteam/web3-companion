# Web3 Security — CLI Reference

## `web3 security check`

Run simulation and risk checks on a transaction intent.

| Param | Required | Description |
|-------|----------|-------------|
| `--transaction-id` | Yes | Transaction intent ID |

Example:

```bash
web3 security check --transaction-id txi_123
```

## Return Fields

| Field | Type | Description |
|-------|------|-------------|
| `transaction_id` | String | Transaction intent ID |
| `status` | String | Intent status after this check |
| `tx_type` | String | Transaction type: `transfer`, `swap`, or `bridge` |
| `tx_summary` | Object | Per-tx-type summary. Only `wallet_address` (the originating wallet) is universal. Concrete fields by `tx_type` below. `amount_usd` is **not** present here; it appears later, in the `web3 ssm sign` `pending_user_auth` response. |
| `verdict` | String | `pass`, `warn`, or `fail` |
| `summary` | String | Compact verdict summary |
| `checks` | Array | Individual check results |
| `effect` | Object | Owner spending/receiving/gas/net summary from simulation |
| `simulation` | Object | Full simulation output |

Each check:

| Field | Type | Description |
|-------|------|-------------|
| `rule` | String | Check name |
| `status` | String | `pass`, `warn`, or `fail` |
| `detail` | String | Human-readable explanation |
| `observed` | Object/Array | Optional structured evidence |

Verdict priority: `fail` > `warn` > `pass`.

### `tx_summary` shape by `tx_type`

**`tx_type = "transfer"`**
| Field | Description |
|-------|-------------|
| `type` | Literal string `"transfer"` |
| `chain` | Source chain name (e.g. `"ethereum"`) |
| `wallet_address` | Originating wallet address |
| `token` | `"native"` for the chain's native token, otherwise the ERC-20 contract address |
| `amount` | Human-readable amount the user typed (e.g. `"100"`) |
| `to` | Recipient address. Equal to `wallet_address` on self-transfer |

**`tx_type = "swap"`**
| Field | Description |
|-------|-------------|
| `type` | Literal string `"swap"` |
| `chain` | Chain name |
| `wallet_address` | Originating wallet (also the funds destination on the same chain) |
| `protocol` | Swap protocol used (e.g. `"odos"`, `"kyber"`) |
| `source_asset` | Input token: `"native"` or ERC-20 address |
| `target_asset` | Output token: `"native"` or ERC-20 address |
| `input_amount` | Input amount in raw integer wei (decimals apply per `source_asset`) |
| `output_amount` | Quoted output amount in raw integer wei (per `target_asset`) |
| `approve_target` | Expected spender address for any ERC-20 `approve()` calldata; empty when source is native |

No `amount`, no `to` — display "from / to" using `source_asset` / `target_asset`.

**`tx_type = "bridge"`**
| Field | Description |
|-------|-------------|
| `type` | Literal string `"bridge"` |
| `wallet_address` | Originating wallet (also the funds destination on the destination chain — bridges always return to the sender) |
| `bridge` | Bridge protocol used (e.g. `"relay"`, `"bungee"`) |
| `from_chain` | Source chain name |
| `to_chain` | Destination chain name |
| `from_token` | Source token: `"native"` or ERC-20 address on `from_chain` |
| `to_token` | Destination token: `"native"` or ERC-20 address on `to_chain` |
| `amount` | Human-readable source amount |
| `deposit_address` | Per-quote deposit address for deposit-mode bridges. Present only when the adapter uses deposit mode. Internal field — **do not display to the user**. |
| `approve_target` | ERC-20 spender for approve-mode bridges. Present only when source is ERC-20 and mode is approve |

No top-level `chain` (use `from_chain`/`to_chain`), no `to` (use `from_token`/`to_token` + chain pair).

## Write Integration Contract

Write flows run this check after a transaction intent exists and before signing:

```bash
web3 security check --transaction-id <transaction_id>
```

A `warn` or `fail` verdict blocks signing until cleared by a higher-level review or an explicit user override.

## `web3 security trace`

Returns only the `simulation.trace` subtree of the security check response. Same `--transaction-id` parameter as `web3 security check`. The default `check` does not include this subtree; use `trace` when the user explicitly asks for the full simulation trace or you need EVM-level call detail beyond what the summarized `checks[]` findings expose.

| Param | Required | Description |
|-------|----------|-------------|
| `--transaction-id` | Yes | Transaction intent ID |

## `web3 security override`

Explicitly records user acceptance for a transaction intent that still requires security review.

| Param | Required | Description |
|-------|----------|-------------|
| `--transaction-id` | Yes | Transaction intent ID |
| `--reason` | Yes | User's explicit reason for accepting the risk |
| `--challenge-id` | After Passkey approval | Approved Passkey challenge ID from the first override response |

Example:

```bash
web3 security override --transaction-id txi_123 --reason "User explicitly accepts the risk after reviewing the warning"
```

If the response returns `status=pending_user_auth`, stop and wait for the user to approve the Passkey challenge. Then retry with the **byte-identical** reason and returned challenge id:

```bash
web3 security override --transaction-id txi_123 --reason "User explicitly accepts the risk after reviewing the warning" --challenge-id ch_123
```

The `--reason` on the resume call must be byte-identical to the first call. Any difference (whitespace, punctuation, paraphrase) causes the resume to be rejected. See `~/.claude/skills/_shared/passkey.md` for the full resume flow.

Only continue to signing if the response returns `status=security_overridden`. User override does not bypass policy checks or later signing Passkey approval when policy requires it.
