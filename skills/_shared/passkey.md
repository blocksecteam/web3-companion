# Shared Passkey Confirmation Flow

Used whenever a CLI command returns `status: "pending_user_auth"` with a `challenge_id`. Today that's `web3 ssm sign` (sign flow), `web3 ssm delegate` (delegation flow), and `web3 security override` (override flow).

## Steps

1. Read and store the `challenge_id` field from the JSON response **before** generating any output.
2. Treat the JSON `challenge_id` as the source of truth. The Passkey confirmation button is rendered automatically from the JSON output.
3. Tell the user that Passkey confirmation is required and summarize the operation. Do not ask the user to open a challenge link; the on-screen Passkey button opens the confirmation flow.
4. After step 3, **end your turn and wait for the user's next message**. Do not poll. Do not call the resume command speculatively to "check if approved". Do not rerun the originating command. The user clicks Passkey in the browser (a separate UI from this chat); they will send you a chat message once they are done. Only then run the resume command from the table below — each originating command has its own resume path:

   | Originating command | Resume command |
   |---|---|
   | `web3 ssm sign --transaction-id <txid>` | `web3 ssm sign-status <challenge_id>` |
   | `web3 ssm delegate --wallet <wallet_id> --chain <chain>` | `web3 ssm delegate --wallet <wallet_id> --chain <chain> --challenge-id <challenge_id>` |
   | `web3 security override --transaction-id <txid> --reason "<reason>"` | `web3 security override --transaction-id <txid> --reason "<same reason>" --challenge-id <challenge_id>` |

   **The resume paths are not interchangeable.** Using the wrong resume command (e.g. `sign-status` for a delegate challenge, or invoking `delegate` to resume a sign challenge) will fail.

   **For override**, the `--reason` on the resume call must be byte-identical to the first call. Any change to the reason (whitespace, punctuation, paraphrase) causes the resume to be rejected.

   The override resume is the **same command** with `--challenge-id` appended, not a separate "status" / "poll" call. There is no `security override-status` command — the resume IS how you read the result. Calling `web3 security override` without `--challenge-id` after the user approved Passkey creates a brand-new challenge and re-prompts the user, producing a Passkey-confirm loop the user perceives as "I already confirmed but it keeps asking".

5. Interpret the resume response per the calling flow:
   - Sign flow → on `status=signed`, run the broadcast command specified by the calling skill.
   - Delegate flow → on `delegated: true`, the wallet is delegated; do not broadcast. On `pending_delegation`, wait for on-chain confirmation (~15–60 s) and retry from the originating action.
   - Override flow → on `status=security_overridden`, proceed to `web3 ssm sign --transaction-id <txid>`. Any other status means the override did not take effect — do not sign.

## NEVER

<NEVER>
Never ask the user to open a challenge link.
Never display, wrap, reconstruct, shorten, or retype `sign_url`.
Never parse challenge_id from a displayed URL — always read it from the JSON `challenge_id` field before generating any output, and use that stored value in all subsequent steps.
Never alter, shorten, retype, or repair a `challenge_id`; one missing character makes it unmatchable.
Never rerun the originating command (re-build, re-sign, re-delegate from scratch) while a Passkey challenge is pending — resume with `--challenge-id` instead.
Never "check status" by re-invoking `web3 security override` without `--challenge-id` after the user approved Passkey. Override has no separate status endpoint — the resume command (same command + `--challenge-id`) IS the status check. Re-running override without `--challenge-id` creates a fresh challenge and traps the user in a confirm loop.
Never check transaction status (`web3 chain tx`) before broadcasting — sign-flow broadcasts have not been sent yet at that point.
Never use the wrong resume path (e.g. `sign-status` for a delegate challenge).
</NEVER>
