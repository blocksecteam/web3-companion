# Intent Expiry — recovery from `transaction_intent_expired`

You reached this file because a CLI command returned **HTTP 410** with
`{ "error": { "code": "transaction_intent_expired", ... } }`. Follow the
recovery path below for whichever flow you were in.

## Why the window exists

A built intent stays signable for **5 minutes**. After that, the
captured pricing and simulation context are stale — the user must see
refreshed information (output amount, deposit address, gas estimate)
before re-confirming.

## Recovery — restart from the right command

Choose the recovery path based on which flow you were running.

### Swap

Restart from `web3 swap quote`, **not** `web3 swap build`. The quote
shows the user a refreshed price so they can see what changed before
re-confirming.

> The previous quote expired (more than 5 minutes elapsed). Re-fetching
> the current quote so you can see the latest price and decide whether
> to proceed.

Then re-display the new quote, get explicit user confirmation, and only
then run `web3 swap build` again with a new `transaction_id`.

### Bridge

Restart from `web3 bridge quote`, **not** `web3 bridge build`. For
deposit-mode bridges (those with `mode: deposit` in `web3 bridge protocols`) the deposit address itself may have
changed — sending funds to a stale deposit address risks refund or stuck
funds.

> The previous quote expired (more than 5 minutes elapsed). Re-fetching
> the current quote so you can see the latest output amount and deposit
> address before proceeding.

Then re-display the new quote, get explicit user confirmation, and only
then run `web3 bridge build` again with a new `transaction_id`.

### Chain transfer

Re-run `web3 chain transfer` with the same parameters. Transfers do not
have a quote, so the displayed amount and recipient do not change. Remind
the user of the parameters being rebuilt before re-issuing, so they
understand a fresh `transaction_id` is being issued for the same transfer.

> The previous transfer intent expired (more than 5 minutes elapsed).
> Rebuilding so the security check evaluates current state.

## Hard rules

<NEVER>
Never silently re-run `build` (or `transfer` for chain) after
`transaction_intent_expired`. The new intent may carry a different
output amount, deposit address, or security check outcome. The user
must see the refreshed information before re-confirming.
</NEVER>

<NEVER>
Never tell the user the expiry happened "because the server timed out"
or "because the connection was slow". The expiry is intentional; tell
them plainly that prices and state drift past five minutes and you
are fetching fresh values.
</NEVER>
