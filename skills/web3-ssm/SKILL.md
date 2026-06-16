---
name: web3-ssm
description: "Use when: signing transactions, sign status, EIP-7702 delegation, Passkey confirmation, account status, onboarding status, setup progress, feature list, what can I do. 中文: 签名, 签名状态, 委托激活, Passkey 确认, 账户状态, 注册状态, 首次使用, 功能列表, 能做什么."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 SSM — Secure Signing & Delegation

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Use this skill for transaction signing, EIP-7702 delegation, Passkey approval, and account status checks. Wallet lifecycle, signing policy management, and audit review are not available via the CLI — direct the user to the corresponding UI page.

## Step 0 — Re-route check

- **Same-chain token swap** — HARD BLOCK: route to `web3-swap`.
- **Cross-chain bridge transfer** — HARD BLOCK: route to `web3-bridge`.
- **Token transfer (same-chain native or ERC-20)** — HARD BLOCK: route to `web3-chain`.
- **Portfolio / balance / USD value** — HARD BLOCK: route to `web3-portfolio`.
- **Token search / metadata / price** — HARD BLOCK: route to `web3-token`.
- **Pre-execution security check or override** — HARD BLOCK: route to `web3-security`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Sign transaction / EIP-7702 delegation / account status** → stay in this skill.

## Account Status

When the user asks about account status, onboarding progress, or what they can do:

```
web3 context status
```

Match the response's `readiness` field against the table below and respond in the user's language:

| `readiness` | Response |
|-------------|----------|
| `status_unavailable` | Tell the user the CLI cannot read server status and surface the `blocking_reason` field from the response. |
| `needs_wallet` | Tell the user no wallet exists yet. Direct them to the Wallets page to create or recover a wallet. |
| `pending_delegation` | Tell the user delegation is still confirming on-chain and to retry in 15–60 seconds. |
| `ready` | Show the feature list below. |

### Ready — Feature List

Check each wallet's `delegation_status` from the status output:

- If **all wallets delegated**: list all features without hints.
- If **any wallet `not_delegated`**: append a delegation hint after each write-operation feature.

Output as a markdown bullet list. Example structure (translate to the user's language):

- Portfolio — view balances and holdings
- Token search — find tokens and prices
- Swap — exchange tokens [+ "(requires EIP-7702 delegation to execute)" if needed]
- Bridge — cross-chain transfer [+ delegation hint if needed]
- Transfer — send tokens [+ delegation hint if needed]
- Signing policies — Signing Policies page
- SSM audit log — SSM Audit page

When mentioning swap or bridge protocol counts, run `web3 swap protocols` or `web3 bridge protocols` to get the actual numbers — never invent a count.

If all wallets are delegated, omit the delegation footnote and hints.

## Actions Not Available via CLI

<NEVER>
Never create, delete, or export wallets from chat or CLI. Never manage policies or inspect audit logs from chat or CLI.
</NEVER>

Use these responses instead:

- Wallet creation: "Wallet creation lives in the Wallets page — open it to create a wallet there."
- Wallet deletion/export: "Wallet deletion/export are custody-sensitive actions. Open the Wallets page and use the wallet controls with Passkey confirmation."
- Policy changes: "Signing policies live in the Signing Policies page."
- Audit review: "SSM audit lives in the SSM Audit page."

## EIP-7702 Delegation

All managed wallets must be EIP-7702 delegated before write operations (swap, bridge, transfer). Delegation is an explicit user-confirmed flow; write flows return `not_delegated` instead of silently starting delegation. User approval is always required.

See `~/.claude/skills/_shared/preflight.md` § Required Parameters. The table below lists this skill's two delegate parameters.

| Parameter | Rule |
|-----------|------|
| `<wallet_id>` | Must be confirmed. If the user has multiple wallets and hasn't specified which, ask. |
| `--chain` | Must be confirmed by the user; the CLI has no default. Delegation is per-chain and irreversible without another on-chain transaction. |

Run `web3 context status` and match the wallet's `chains[]` entry for the target chain by its `delegation_status` field:

| `delegation_status` in `chains[]` | Behavior |
|-----------------------------------|----------|
| `delegated` | Proceed normally |
| `not_delegated` / missing chain | Follow the delegation sub-flow below |
| `pending_delegation` | Wait for on-chain confirmation (~15–60 seconds), then retry |

If any command in this skill returns an `error.code`, look it up in `~/.claude/skills/_shared/error-codes.md` for the required recovery (`insufficient_gas`, `not_delegated`, `pending_delegation`, `delegation_failed`, `policy_rejected`, `policy_not_found`, the `challenge_*` family, etc. are all listed there). `transaction_intent_expired` has its own file (`~/.claude/skills/_shared/intent-expiry.md`).

### Delegation Sub-flow

When a swap/bridge/transfer fails with `not_delegated`, or the user asks to activate/delegate a wallet, or the user asks how much delegation would cost:

<NEVER>
Never state a concrete delegation gas figure, post-delegation residual balance, or recommended top-up amount unless it comes verbatim from a `web3 ssm delegate-estimate` response or an `insufficient_gas.gas_estimate` field. Gas varies across chains by orders of magnitude; any number reasoned from memory will be wrong.

After `web3 ssm delegate` returns a Passkey challenge (`pending_user_auth`), end your turn. Do **not** append funding suggestions, gas figures, or follow-up commentary in the same message — the user has not clicked Passkey yet and any speculation about cost is unsupported.
</NEVER>

**Step 1 — Preview the cost (always run first, read-only)**

```
web3 ssm delegate-estimate --wallet <wallet_id> --chain <chain>
```

This is a read-only precheck: no Passkey, no on-chain action. Returns:

- `delegated` — if `true`, the wallet is already delegated on that chain; tell the user that and stop. Do not initiate delegate.
- `status` — `not_delegated`, `pending_delegation`, or `delegated`. For `pending_delegation`, surface the state and tell the user to wait for on-chain confirmation, then retry.
- `gas_estimate` (wei) — live cost estimate. Convert to native units (divide by 10^18 — all EIP-7702 chains use 18-decimal natives).
- `current_balance` (wei) — wallet's current native-token balance.
- `needs_gas` (bool) — `true` if `current_balance < gas_estimate`.
- `gas_estimate_source` — `"rpc"` (live) or `"fallback"` (RPC unavailable, offline heuristic used; estimate may be inaccurate).

**Step 2 — Show the user the real numbers and ask**

Construct a confirmation that **always** cites the estimate from Step 1:

If `needs_gas: false` (sufficient balance):
> "Activating `<wallet_id>` on `<chain>` will cost about `<gas_estimate in native units>` `<native_symbol>`. Your current balance is `<current_balance in native units>` `<native_symbol>` — enough to proceed. Activate now?"

If `needs_gas: true` (insufficient balance):
> "Activating `<wallet_id>` on `<chain>` needs about `<gas_estimate in native units>` `<native_symbol>` for gas. Your current balance is `<current_balance in native units>` `<native_symbol>`. Please send at least `<deficit in native units>` `<native_symbol>` to `<address>` first, then ask me to retry."

If `gas_estimate_source` is `"fallback"`, append: "(This is an offline estimate; the actual amount may differ.)"

Respond in the user's language. Wait for explicit confirmation before proceeding.

**Step 3 — Initiate delegate**

```
web3 ssm delegate --wallet <wallet_id> --chain <chain>
```

Backend reruns the same precheck. Likely responses:

- `insufficient_gas` (402) — only fires when the wallet's balance changed between Step 1 and Step 3. Surface the new `gas_estimate` / `current_balance` numbers (same format as Step 2) and stop. See `_shared/error-codes.md`.
- `delegated: true` (200) — already delegated, race between Step 1 and Step 3. Tell the user, stop.
- `status: "pending_delegation"` (202) — an earlier delegation tx is still confirming on-chain. Tell the user to wait a few seconds and retry; do not initiate another.
- `wrong_delegator` (409) — wallet's delegation points at an unrecognised delegator. See `_shared/error-codes.md` — write flows through this companion are blocked until resolved; do not start a new delegation.
- Passkey challenge (`pending_user_auth`) — the normal path. Follow `~/.claude/skills/_shared/passkey.md` — the delegate resume row in step 4 is the relevant one (re-run `web3 ssm delegate --wallet <wallet_id> --chain <chain> --challenge-id <id>`, **not** `sign-status`). **End your turn after telling the user to click Passkey; do not add anything else.**

**Step 4 — Confirm result**

If `delegated: true`:
> "Delegation activated. You can now continue with the previous operation."

If `pending_delegation`:
> "Delegation transaction submitted, confirming on-chain (~15–60 seconds). Retry shortly to continue."

(Respond in the user's language.)

For `delegation_failed` and other error codes during delegation, see `~/.claude/skills/_shared/error-codes.md`.

### Asking "how much does delegation cost?" without executing

When the user asks the cost without intent to execute (e.g. "在 Polygon 上激活要花多少 gas?"), run `web3 ssm delegate-estimate` from Step 1 and surface the converted number plus the `gas_estimate_source` qualifier. Do **not** call `web3 ssm delegate`.

## Wallet State

There is no `web3 ssm wallets` namespace. For wallet state needed in chat, use `web3 context status`. For wallet creation, inspection, deletion, and export, guide the user to the Wallets page.

## Signing

```
web3 ssm sign --transaction-id txi_123
```

Signing only accepts a transaction intent id. Build and security check must have already produced and cleared the intent.

<MUST>
Before calling `web3 ssm sign`, the `transaction_id` must have passed `web3 security check` with a result that permits signing — a `decision=passkey_required` system message, an explicit user override via `web3 security override`, or a static `pass` verdict that arrived with no `decision=` label. See `~/.claude/skills/_shared/security-handling.md`.
</MUST>

### Auto-sign (within policy limits)

```json
{
  "status": "signed",
  "transaction_id": "txi_123",
  "tx_hash": "0xabcd..."
}
```

<MUST>
Display what was signed, then explicitly run `web3 chain broadcast <chain> --transaction-id <transaction_id>` to submit the signed transaction when the calling workflow expects execution. The CLI does not auto-broadcast — the agent runs broadcast as the next step.
</MUST>

### Passkey Required

```json
{
  "status": "pending_user_auth",
  "transaction_id": "<transaction_id>",
  "challenge_id": "<challenge_id>",
  "sign_url": "<sign_url>",
  "tx_summary": {
    "type": "<tx_type>",
    "amount_usd": <amount_usd>,
    "to": "<to>",
    "chain": "<chain>",
    "wallet": "<wallet_address>"
  }
}
```

Follow `~/.claude/skills/_shared/passkey.md` — the sign resume row in step 4 is the relevant one (resume with `web3 ssm sign-status <challenge_id>` only after the user sends a chat message confirming they clicked Passkey). On `status=signed`, broadcast the returned `transaction_id`:

```
web3 chain broadcast <chain> --transaction-id <transaction_id>
```

**After user confirms** (`status=signed`):
> "Confirmed. I will retry the protected operation now."

**After timeout or user cancellation** (`status=timeout`): both a user clicking cancel in the browser and inactivity surface as `timeout`. Use one phrasing that covers both:
> "Confirmation didn't complete. Would you like to try again?"

If the user agrees, re-run `web3 ssm sign --transaction-id <txid>` to issue a fresh challenge against the same intent.

## Security Rules

- Never sign without explicit user confirmation.
- Never show raw private keys.
- Never tell the user to manage wallet lifecycle, policies, or audit through CLI.
- Always surface why Passkey is required: amount, recipient, chain, or policy boundary.
- Never initiate delegation without explicit user confirmation.
