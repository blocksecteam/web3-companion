# Shared Security Check and Sign Decision Flow

This file describes the shared flow swap, bridge, and chain transfer all
go through after `web3 ... build` (or `web3 chain transfer`) returns a
`transaction_id`. The flow ends either at `web3 chain broadcast` or at
a hard stop.

## 1. Run the security check

After build returns a `transaction_id`, immediately run:

```
web3 security check --transaction-id <transaction_id>
```

`build` (or `transfer`) does not sign, broadcast, or evaluate risk; the
security check is the gate that decides whether signing is allowed.

## 2. Interpret the decision

After the check, you may receive a system message containing
`decision=<label>`. Follow it:

| `decision=` label | Action |
|-------------------|--------|
| `passkey_required` | Run `web3 ssm sign --transaction-id <transaction_id>` |
| `stop` | **Stop.** Explain the failing reason to the user. Do not sign. |
| `wait_user_confirm` | **Stop.** Ask the user to confirm in a new message before signing. |

If no `decision=` label arrives and the static `verdict` is `pass`,
proceed to sign. If the static verdict is `warn` or `fail` and no
`decision=` label arrives, do not sign without explicit user override
(see step 3).

### Surface informational rule details

Even when a check's `status` is `pass`, surface its `detail` to the user if the message conveys a user-visible monetary value (gas cost, fee estimate, etc.). Relay the `detail` text as-is — it already carries any "estimated / fallback" qualifiers needed.

## 3. User override (only when the user explicitly accepts the risk)

If the user reads the findings from the security check and explicitly
accepts the risk, run:

```
web3 security override --transaction-id <transaction_id> --reason "<user's explicit reason>"
```

The first call returns `status=pending_user_auth` + `challenge_id`.
Follow `~/.claude/skills/_shared/passkey.md` — the override resume row
in step 4 is the relevant one. Only continue to sign after the resume
response returns `status=security_overridden`.

## 4. Sign

```
web3 ssm sign --transaction-id <transaction_id>
```

If sign returns `pending_user_auth`, read
`~/.claude/skills/_shared/passkey.md` and follow it.

## 5. Broadcast

Only after signing is complete:

```
web3 chain broadcast <chain> --transaction-id <transaction_id>
```

(For bridge, use the source chain: `web3 chain broadcast <from-chain> --transaction-id <transaction_id>`.)

Then render the broadcast output following
`~/.claude/skills/_shared/broadcast-output.md` plus your skill's
field-table extension.

## Hard rules

<NEVER>
Never call `web3 ssm sign` before `web3 security check` has produced a
decision that permits signing (`passkey_required`, or
`security_overridden` after explicit user acceptance). The check is the
gate; bypassing it defeats every static and runtime defence.
</NEVER>

<NEVER>
Never run `web3 security override` proactively. The override is only
issued after the user has seen the findings in a previous turn AND
explicitly stated they accept the risk in the current turn. "User
clicked confirm" or "user said yes" without seeing the findings is not
sufficient.
</NEVER>

<NEVER>
Never re-run `web3 ... build` (or `web3 chain transfer`) for the same
transaction after a Passkey challenge is pending. Resume the existing
challenge per `~/.claude/skills/_shared/passkey.md` step 4 — the resume
command differs by originating command (sign / delegate / override).
</NEVER>
