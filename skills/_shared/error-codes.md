# Backend Error Codes — Recovery Reference

You reached this file because a `web3` CLI command returned a JSON error body of the form:

```json
{ "error": { "code": "<error_code>", "message": "..." } }
```

Match the value of `error.code` against the tables below to pick the required next step.

`transaction_intent_expired` is **not** in this file — its recovery is per-tx-type (swap vs bridge vs transfer) and lives in `~/.claude/skills/_shared/intent-expiry.md`. Go there instead.

If you receive a code that is not listed below, default to category **B2** (surface the `error.message` to the user, do not retry, do not silently work around it).

## Categories

- **A — Recovery-actionable**: a specific next step (retry, restart, re-route, adjust input) clears the error.
- **B1 — Retryable**: external dependency transient failure. Surface the message and ask the user whether to retry.
- **B2 — Terminal**: nothing to retry. Surface the message and stop.
- **B3 — User input fix**: agent passed an invalid value; ask the user to correct their input.

## A — Recovery-actionable

| code | HTTP | Triggered when | Recovery |
|------|------|----------------|----------|
| `insufficient_gas` | 402 | Wallet lacks native token to pay EIP-7702 delegation gas (`web3 ssm delegate` initial call, or `web3 ssm sign` when the wallet is not yet delegated). Response carries `gas_estimate` (wei), and on the delegate path also `current_balance` (wei) and `gas_estimate_source` (`"rpc"` = live, `"fallback"` = offline heuristic) | Convert `gas_estimate` / `current_balance` to native units (divide by 10^18). Tell the user to send at least the difference to `address`, then retry. If `gas_estimate_source = "fallback"`, append "(offline estimate; actual amount may differ)" so they know the figure is not authoritative. The retry will re-run the precheck. |
| `not_delegated` | 409 | Build (`web3 chain transfer` / `web3 swap build` / `web3 bridge build`) or sign refused because the wallet has no on-chain delegation on this chain | Walk the user through the delegation sub-flow in `web3-ssm/SKILL.md` (`web3 ssm delegate --wallet <wallet_id> --chain <chain>`). After it succeeds, retry the original write command. |
| `pending_delegation` | 409 | Build refused because a delegation transaction is still confirming on-chain (only emitted by build paths; `web3 ssm delegate` and `web3 ssm sign` instead return `status: "pending_delegation"` on a 202 response, not an `error.code` — same meaning, different shape). | Wait a few seconds for on-chain confirmation, then retry the same command. Do not start a new delegation. |
| `delegation_confirmed` | 409 | The wallet's delegation just confirmed on-chain during the build's preflight check | Retry the same command immediately. No user prompt needed. |
| `unsupported_chain` | 400 | The chain in `--chain` / `--from-chain` / `--to-chain` is not in the deployment's white-list | Run `web3 chain list` and ask the user to choose from the actual supported set. Do not guess the chain name. |
| `policy_rejected` | 403 | Sign refused by a policy limit. `error.message` is one of: `"per-transaction limit exceeded"` / `"daily transfer limit exceeded"` / `"daily trade limit exceeded"` | Read the message. For per-transaction → suggest splitting the amount. For daily limits → tell the user the limit is reached for today, or offer to raise it via the Signing Policies page. Do not auto-retry. |
| `policy_not_found` | 403 | The wallet has no active signing policy at sign time | Tell the user this wallet has no signing policy bound; ask them to configure one via the Signing Policies page, then retry. |
| `delegation_failed` | 500/502 | Delegation failed (network or signer error). `error.message` carries the underlying detail | If `message` contains `"invalid chain ID"` → surface the error and tell the user the RPC for the target chain looks misconfigured (the deployment admin needs to verify it). Otherwise surface the message and stop. |
| `delegation_check_failed` | 500 | `web3 ssm delegate-estimate` could not run the read-only precheck (RPC dial failed, balance query timed out, etc.) | Surface `error.message`. Tell the user the deployment's RPC for that chain is currently unreachable so the cost can't be previewed; suggest retrying shortly. Do not invent a number — falling back to memory defeats the purpose of the estimate endpoint. |
| `wrong_delegator` | 409 | The wallet already has on-chain delegation code, but it points at a non-MetaMask delegator the runtime doesn't recognise. Emitted by `web3 ssm delegate`, `web3 ssm delegate-estimate`, and the just-in-time precheck inside `web3 ssm sign`. | Tell the user the wallet is delegated elsewhere and write operations through this companion are blocked until they undelegate or recreate. Do not start a new delegation — it would conflict with the existing one. |
| `passkey_override_required` | 403 | `web3 security clear-review` was rejected because the address label risk check returned `warn` or `fail`. Security Agent clearance alone is not sufficient — the user must approve via Passkey. | Do NOT retry `clear-review`. Instead, run `web3 security override --transaction-id <id> --reason "<reason>"` which will create a Passkey challenge. Wait for the user to approve the Passkey, then resume. This is a code-enforced gate — the Security Agent cannot bypass it regardless of its verdict. |
| `security_check_required` | 403 | `web3 ssm sign` was called for an intent that has not passed `web3 security check` yet | Run `web3 security check --transaction-id <id>` first, follow the result per `_shared/security-handling.md`, then retry `web3 ssm sign`. |
| `transaction_not_signed` | 403 | `web3 chain broadcast` was called for an intent whose `status` is not `signed` | Run `web3 ssm sign --transaction-id <id>` first, then retry broadcast. |
| `signed_operation_not_found` | 403 | Broadcast can't locate the signed payload (TTL expired, or signing was bypassed) | Re-sign the intent. If the intent has also expired you will receive `transaction_intent_expired` on the re-sign — follow `_shared/intent-expiry.md`. |
| `signed_tx_unavailable` | 403 | Broadcast located the intent but could not decrypt its signed payload | Re-sign the intent (`web3 ssm sign --transaction-id <id>`), then retry broadcast. If the intent has expired you will receive `transaction_intent_expired` on the re-sign — follow `_shared/intent-expiry.md`. |
| `transaction_broadcast_in_progress` | 409 | Another broadcast call for the same `transaction_id` is already in flight | Do not retry immediately. Wait for the in-flight broadcast to finish, then check `web3 chain tx <chain> <hash>` against the hash returned by the earlier call. If the user lost the earlier hash, query the intent status to recover it before reattempting. |
| `transaction_intent_not_found` | 404 | Intent id does not exist (typo, or already cleaned up) | Rebuild from scratch: re-run the originating `quote` + `build` / `chain transfer` step to obtain a fresh `transaction_id`. |
| `wallet_not_found` | 404 | The supplied wallet id does not match any wallet for this user | Run `web3 context status` and ask the user to pick the right wallet id; do not guess. |
| `unknown_protocol` | 400 | `--protocol` for swap is not a recognized protocol name | Run `web3 swap protocols` and tell the user the actual list. Do not hardcode protocol names from memory. |
| `unknown_bridge` | 400 | `--bridge` is not recognized, or the pair is unsupported on that bridge | Run `web3 bridge protocols` and tell the user the actual list, plus the from→to chain pair. |
| `unsupported_output` | 400 | `web3 bridge build` — the requested output token isn't supported on the destination chain by this bridge | Ask the user to pick a different `--to` token. Smart-quote (`web3 bridge quote` without `--bridge`) may surface a compatible bridge for the originally requested token. |
| `wrong_challenge_type` | 400 | Agent used the wrong Passkey resume command (e.g. `web3 ssm sign-status` against a `delegate` challenge) | Pick the correct resume command per `_shared/passkey.md` step 4 resume table. The `type` field in the response identifies the originating operation. |
| `challenge_expired` | 410 | The Passkey challenge timed out before the user approved | Restart from the originating command (`web3 ssm sign` / `web3 ssm delegate` / `web3 security override`) to issue a fresh challenge. |
| `challenge_cancelled` | 410 | The user explicitly cancelled the Passkey prompt in the browser | Ask the user whether to retry. Do not auto-retry — they intentionally cancelled. |
| `challenge_payload_mismatch` | 403 | The resume call's payload doesn't match the original challenge (different `transaction_id`, or — for `security override` — a different `reason`) | Re-issue the resume call with the exact original `transaction_id` and (for override) a byte-identical `--reason` string. Any whitespace or punctuation change in the reason is rejected. See `_shared/passkey.md`. |
| `challenge_required` | 403 | Agent attempted to consume an "approved" challenge that does not exist (or is no longer "approved") | The user has not approved the Passkey yet. End your turn and wait for the user's next message; only then resume with the correct sign-status / delegate-resume / override-resume command per `_shared/passkey.md`. Do not retry without a fresh user message. Do not invent a `challenge_id`. |
| `webauthn_required` | 403 | Resume command was invoked but the Passkey challenge has not been approved yet (user hasn't clicked) | Same as `challenge_required`: end your turn and wait for the user's next message; only then resume. Do not retry without a fresh user message. |
| `challenge_not_found` | 404 | The supplied `challenge_id` does not exist for this user | Read the `challenge_id` straight from the JSON of the originating command's response. If the original challenge expired or was cleaned up, restart from the originating command. |

## B1 — Retryable

| code | HTTP | Triggered when | Recovery |
|------|------|----------------|----------|
| `broadcast_failed` | 503 | RPC broadcast failed. The intent stays in `signed` status and can be retried. | Show the `message` to the user. Common messages and actions: **`"insufficient funds for gas * price + value"`** → the wallet balance cannot cover gas fee + transfer amount at current gas prices. Tell the user the balance is too low for this transfer at current gas prices, show the shortfall, and suggest waiting for lower gas or reducing the amount. **`"nonce too low"`** → another transaction already confirmed; rebuild from scratch. **`"already known"`** → tx is already in the mempool; check status with `web3 chain tx`. **`"gas tip cap 0, minimum needed 1"`** → gas price was stale; rebuild to get fresh gas price. For other messages: surface and offer retry. Do not re-sign; the signed bytes are still valid. |
| `transaction_broadcast_claim_failed` | 500 | Broadcast pre-step (acquiring the broadcast lock for the intent) failed internally | Surface `error.message` to the user and offer to retry `web3 chain broadcast` with the same `transaction_id`. Do not re-sign. |
| `quote_failed` | 503 | A specific bridge/swap adapter (third-party RPC/HTTP) returned an error | Offer the user a retry. For better odds run smart-quote (`web3 swap quote` / `web3 bridge quote` without `--protocol` / `--bridge`) — smart-quote tolerates per-adapter failures, so one bad adapter does not break the whole call. |
| `simulation_failed` | 503 | The security simulation failed | Offer to retry `web3 security check`. If it persists, ask the operator. |
| `decimals_error` | 503 | `web3 chain transfer` could not resolve the ERC-20 token's `decimals()` (resolver transient failure) | Offer to retry. If it persists, ask the user to double-check the token contract address. |
| `portals_error` | 503 | Portals API call (used by `web3 portfolio`, `web3 token search`, `web3 token info`) failed | Surface the message and offer to retry. Portals is a paid third-party service; sustained failures indicate provider downtime. |
| `rpc_error` | 503 | `web3 chain gas` failed because the chain's RPC could not be reached | Offer to retry. Sustained failures indicate the RPC for that chain is down. |
| `nonce_fetch_failed` | 502 | Sign path failed to read the wallet's nonce from chain | Offer to retry. Sustained failures indicate the deployment's RPC for that chain is down. |

## B2 — Terminal (surface and stop)

| code | HTTP | Triggered when | What to tell the user |
|------|------|----------------|----------------------|
| `no_quote` | 503 | Every adapter in a smart-quote fan-out returned no quote — already handled inline in `web3-swap/SKILL.md` and `web3-bridge/SKILL.md` Quote sections | "No route available for this pair right now. Want to try a different pair, smaller amount, or come back later?" Do not auto-retry. |
| `build_failed` | 503 | Adapter built an unusable transaction | Surface the `message`, do not retry — the same call will fail the same way. Suggest a different protocol/bridge. |
| `sign_failed` | 500 | Signer error | The user cannot self-recover. Surface the `message` and suggest contacting the operator. |
| `security_override_rejected` | 403 | Override rejected because the intent is no longer in a state that accepts it (already passed security, or already overridden) | Surface the `message`. If the intent has already passed security, proceed directly to sign; otherwise restart from `web3 security check`. |
| `security_override_failed` | 500 | Server-side error during override clearance | Surface the `message` and stop. |
| `invalid_state_transition` | 409 | A state-machine call (e.g. `security check` on an already-signed intent) was made out of order | Surface the `message`. Recheck the intent's `status` via the originating flow and continue from the correct step. |
| `wrap_failed` | 400/503 | The bridge cannot wrap native token for this chain | Surface the message and stop. |
| `challenge_completed` | 409 | Polled `sign-status` for a challenge that already finished but whose payload could not be unwrapped (edge case) | Surface and stop. If the user expected a result, restart the sign flow. |
| `invalid_challenge_payload` | 400 | Challenge payload could not be read (very rare) | Surface and stop. Restart from the originating command. |
| `challenge_claim_failed` | 500 | Server error while consuming the approved challenge | Surface and stop. |

## B3 — User input fix

| code | HTTP | Triggered when | What to tell the user |
|------|------|----------------|----------------------|
| `invalid_amount` | 400 | Amount string couldn't be parsed (`web3 chain transfer`, `web3 swap quote/build`, `web3 bridge quote/build`) | Ask the user to give a positive human-readable number (e.g. `1`, `0.001`), not a string with units or commas. |
| `invalid_token` | 400 | `--token` (chain transfer) or a bridge/swap token argument is neither `"native"` nor a valid contract address | Ask the user for `"native"` or a 0x-prefixed ERC-20 contract address. Bare symbols like `"usdc"` are not accepted. |
| `invalid_recipient` | 400 | `--to` for `web3 chain transfer` is not a valid 0x-prefixed 40-character hex address (too short, too long, contains non-hex characters, empty) | Ask the user for an explicit 0x-prefixed 40-character hex address. Do not attempt to resolve ENS / username / nickname — the CLI does not accept those. Do not guess by truncating or padding the user's input. |
| `decimals_unresolvable` | 400 | A given token address does not expose a `decimals()` method | Ask the user to verify the contract address; this is almost always a wrong-network address (e.g. an ERC-20 address from the wrong chain). |

