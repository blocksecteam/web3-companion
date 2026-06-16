# Web3 SSM — CLI Reference

## Not Available via CLI

The CLI does not expose wallet creation, wallet inspection, wallet deletion, wallet export, policy management, audit review, or Passkey credential management. It does expose the EIP-7702 delegation sub-flow.

Guide users to the frontend:

- Wallet creation, inspection, deletion, export: Wallets page.
- Signing policies: Signing Policies page.
- SSM audit log: SSM Audit page.

## Delegation Commands

### `web3 ssm delegate-estimate --wallet <wallet_id> --chain <chain>`

Read-only preview of the delegation precheck. Returns the wallet's
current delegation state, gas estimate, and balance without burning a
Passkey approval or touching chain state. Use this before
`web3 ssm delegate` so the user can see the real cost before confirming.

| Param | Required | Description |
|-------|----------|-------------|
| `--wallet` | Yes | Display wallet id from `web3 context status` |
| `--chain` | Yes | Chain to estimate against |

Returns:

- `wallet_id`, `address`, `delegated` (bool), `status`
  (`delegated` / `not_delegated` / `pending_delegation`)
- `needs_gas` (bool)
- `gas_estimate` (wei) — live cost estimate
- `current_balance` (wei)
- `gas_estimate_source` — `"rpc"` (live RPC) or `"fallback"` (offline
  heuristic used because the RPC was unavailable; the figure may be
  inaccurate, surface this qualifier when relaying to the user)

### `web3 ssm delegate --wallet <wallet_id> --chain <chain>`

Initiate EIP-7702 delegation for a wallet on one chain. Backend runs
the same precheck as `delegate-estimate` on the initial call, so:

- Already-delegated wallets short-circuit to a 200 with the current
  delegation state — no Passkey burned.
- Insufficient balance returns 402 `insufficient_gas` immediately with
  `gas_estimate`, `current_balance`, `gas_estimate_source` — no Passkey
  burned.
- Sufficient balance creates the Passkey gate as before.

| Param | Required | Description |
|-------|----------|-------------|
| `--wallet` | Yes | Display wallet id from `web3 context status` |
| `--chain` | Yes | Chain to delegate on. No default; user must explicitly confirm the target chain. |
| `--challenge-id` | No | Approved Passkey challenge ID |

First call without `--challenge-id` to request Passkey confirmation. After approval, retry the same command with `--challenge-id`.

## Signing Command

### `web3 ssm sign`

Request a transaction signature.

| Param | Required | Description |
|-------|----------|-------------|
| `--transaction-id` | Yes, unless `--challenge-id` is used | Transaction intent ID |
| `--wait` | No | Wait for approval and return signed transaction |
| `--poll-interval` | No | Poll interval for `--wait` |
| `--timeout` | No | Max wait time for `--wait` |
| `--challenge-id` | No | Approved Passkey challenge ID to consume |

The command signs transaction intents.

**Auto-sign response:**

```json
{
  "status": "signed",
  "transaction_id": "txi_123",
  "tx_hash": "0xabcd..."
}
```

**Passkey challenge response:**

```json
{
  "status": "pending_user_auth",
  "transaction_id": "txi_123",
  "challenge_id": "ch_xyz789",
  "sign_url": "https://web3.example.com/confirm/ch_xyz789",
  "timeout_at": "2026-05-10T12:02:00Z",
  "tx_summary": {
    "type": "transfer",
    "amount_usd": 2500.0,
    "to": "0x6131...37b5",
    "chain": "ethereum",
    "wallet": "0xd8dA...6045"
  }
}
```

### `web3 ssm sign-status <challenge_id>`

Resume the sign flow after the user has approved the Passkey in the browser. Call this **once**, after the user sends a chat message confirming they clicked — never call it speculatively.

Returns:

- `signed` — challenge completed. Includes `transaction_id`, `tx_hash`, and `tx_summary` (same shape as the `pending_user_auth` response — see SKILL.md Passkey Required section). Broadcast with `web3 chain broadcast <chain> --transaction-id <transaction_id>`.
- `pending` — the user hasn't actually clicked yet. Stop and wait for the next user message before retrying.
- `timeout` — the challenge expired before approval (the WebAuthn protocol has no explicit user-rejection signal; if the user cancels in the browser, the challenge stays `pending` until it times out).

Error responses (`wrong_challenge_type`, `challenge_not_found`, `challenge_completed`) — consult `~/.claude/skills/_shared/error-codes.md`.
