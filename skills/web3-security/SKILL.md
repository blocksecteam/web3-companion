---
name: web3-security
description: "Use when: security check, transaction simulation, is this safe, dry-run, pre-execution check, approve safety, token risk, scan transaction. 中文: 安全检查, 交易模拟, 安全扫描, 是否安全, 蜜罐检测, 授权检查."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Web3 Security — Simulation & Checks

Tag semantics (`<NEVER>` / `<MUST>` / `<SHOULD>`): see `~/.claude/skills/_shared/tags.md`.

Supported chains are determined by the deployment configuration — run `web3 chain list` for the current set. Do not hardcode chain names in this skill.

Simulate a transaction and run risk checks before signing or broadcasting.

## Step 0 — Re-route check

- **Same-chain token swap (initiation)** — HARD BLOCK: route to `web3-swap`. The swap flow builds the intent before returning here for security check.
- **Cross-chain bridge transfer (initiation)** — HARD BLOCK: route to `web3-bridge`. The bridge flow builds the intent before returning here for security check.
- **Token transfer (initiation)** — HARD BLOCK: route to `web3-chain`. The transfer flow builds the intent before returning here for security check.
- **Portfolio / balance / USD value** — HARD BLOCK: route to `web3-portfolio`.
- **Token search / metadata / price** — HARD BLOCK: route to `web3-token`.
- **Sign transaction / EIP-7702 delegation / account status** — HARD BLOCK: route to `web3-ssm`.
- **Wallet create / delete / export** → guide the user to the Wallets page.
- **Signing policies management** → guide the user to the Signing Policies page.
- **SSM audit log review** → guide the user to the SSM Audit page.
- **Pre-execution security check or user override** → stay in this skill.

## Fail-safe Principle

**Scan completed + risk detected** → follow the verdict. Never override `fail` yourself.

**Scan failed (network error, timeout, server error)** → is NOT a pass. Stop and inform the user.

## Running a Check

The CLI accepts only the intent id.

```bash
web3 security check --transaction-id txi_123
```

The response includes:

- `verdict` — top-level aggregated outcome: `pass` / `warn` / `fail` (priority `fail` > `warn` > `pass`).
- `checks[]` — per-rule findings. Each entry has `rule` (rule name), `status` (`pass` / `warn` / `fail`), `detail` (human-readable explanation), and optional `observed` evidence. **The runtime list of rules and their semantics live in this array — do not maintain a separate rule enumeration in this doc.** When you need to explain a specific rule's finding to the user, read its `detail` directly.
- `effect` — per-owner spending / receiving / gas / net USD summary derived from the simulation.

The `simulation.trace` subtree is not in the default response. Run `web3 security trace --transaction-id <transaction_id>` when the user explicitly asks for the full trace or you need low-level call detail beyond `checks[]`/`effect`.

## Verdict & Decision Fields

This skill documents what the agent sees. The end-to-end flow (check → decision → sign → broadcast, including override) lives in `~/.claude/skills/_shared/security-handling.md`.

After `web3 security check` runs, you may receive a system message containing `decision=<label>`. The labels and required actions are:

| `decision=` label    | Required action                                                                                       |
|----------------------|-------------------------------------------------------------------------------------------------------|
| `passkey_required`   | The intent is cleared. Proceed to `web3 ssm sign` (Passkey still gates the actual signing).            |
| `stop`               | Do not sign. Explain the failing reason to the user.                                                   |
| `wait_user_confirm`  | Do not sign. Ask the user to explicitly confirm; if they do, run `web3 security override`.             |

The system message also includes:

- `static_verdict=<verdict>` — same `verdict` field from the check output (`pass` / `warn` / `fail`)
- `verdict=<safe|unsafe>` — the Security Agent's own review verdict
- `confidence=<high|medium|low>` — the Security Agent's confidence
- `reason=<text>` — free-form review reason (optional; present when the Security Agent provided one)

When explaining `stop` or `wait_user_confirm` to the user, prefer the `reason=` field if present; otherwise cite `static_verdict` and pull specifics from `checks[]` in the check output.

If a `decision=` label arrives, follow the table above. If a `pass` arrives with no `decision=` label, proceed to sign.

## ERC-7821 Handling

ERC-7821 and normal transactions are semantically checked the same way. The only difference is calldata decoding:

- normal transaction → one operation
- ERC-7821 `execute(bytes32,bytes)` → decoded inner operations

Approvals and value movement are checked over the normalized operations.

## Self-Transfer Recognition

`tx_summary.wallet_address` is the originating wallet. When it equals
`tx_summary.to` (transfer) or appears as an inner op `to` (ERC-7821), the
wallet is sending value to itself. Decoded ops show gross value;
`effect.spending`/`receiving` show net (zero by definition). Do not flag
this gross-vs-net divergence as a data contradiction.

<MUST>
- Always run security before signing or broadcasting transaction intents.
- A failed scan is NOT safe.
</MUST>

## Error Codes

For any `error.code` returned by `web3 security check` / `web3 security trace` / `web3 security override` (`simulation_failed`, `security_override_rejected`, `security_override_failed`, the `challenge_*` family on override resume, `invalid_state_transition`, etc.) consult `~/.claude/skills/_shared/error-codes.md`. `transaction_intent_expired` has its own file (`~/.claude/skills/_shared/intent-expiry.md`).
