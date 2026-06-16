# Shared Broadcast Output Rendering

After `web3 chain broadcast` returns `submitted`, render the result as a
markdown table the user can read at a glance. The broadcast response by
itself only carries `hash`, `status`, `chain`, `transaction_id` — the
human-facing fields (amounts, tokens, addresses, route info) live on the
earlier build/transfer response's `summary`. Combine the two.

## Rendering rules

**Render as real markdown — NOT inside a fenced code block**, otherwise
the table will not render and the explorer link will not be clickable.

### Hash display

- The visible `Tx hash` cell shows a shortened form: **first 10 chars +
  `…` + last 8 chars**, wrapped in backticks so it renders as inline
  code. Example: `0x14079574…d19cad16`.
- The full untruncated hash MUST appear inside the `[…](url)` link
  target. Never truncate the URL — that is what users click to open the
  block explorer.

### Explorer name + base URL by chain

Use the **full** hash in the link target.

The block-explorer name and base URL for a chain are not returned by the CLI — the table below is the source of truth. If `web3 chain list` reports a chain not in this table, **stop and ask the user for the explorer URL** rather than guessing.

| Chain | Explorer name | Base URL |
|---|---|---|
| ethereum | Etherscan | `https://etherscan.io/tx/` |
| base | Basescan | `https://basescan.org/tx/` |
| bsc | BscScan | `https://bscscan.com/tx/` |
| polygon | Polygonscan | `https://polygonscan.com/tx/` |

### Status wording

Always say "broadcast, pending on-chain confirmation". Never say
"complete", "successful", or "confirmed" at this point — the tx has
only been submitted, not yet confirmed on-chain. Confirmation comes
later via `web3 chain tx`.

## Per-skill field table

Your calling skill (swap / bridge / chain transfer) defines its own
field table based on the `summary` fields it actually has. The shared
shape is:

```
**<TxType> broadcast — <chain header>**

| Field    | Value                                              |
|----------|----------------------------------------------------|
| ...      | per-skill fields                                   |
| Tx hash  | `0xabc1234…<last8>`                                |
| Explorer | [<chain explorer name>](https://<base-url>/<full-hash>) |
```

Show only the fields you actually have. **Never invent** amounts, token
symbols, fees, ETAs, or recipient addresses you do not see in the
`summary`.

## Hard rules

<NEVER>
Never dump the raw broadcast JSON response to the user. It contains
only `hash / status / chain / transaction_id` — the user needs the
amounts and tokens, which are on the `summary` of the earlier build /
transfer response.
</NEVER>

<NEVER>
Never put the full hash in the visible cell or the truncated hash in
the URL. The visible cell is for skimming; the URL is for clicking.
Swapping them breaks the explorer link.
</NEVER>

<NEVER>
Never say "Swap complete" / "Transfer complete" / "Bridge complete"
right after broadcast. Use "broadcast, pending on-chain confirmation".
</NEVER>
