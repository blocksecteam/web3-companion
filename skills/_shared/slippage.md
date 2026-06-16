# Shared Slippage UX

Used by web3-swap and web3-bridge. Both `web3 swap protocols` and `web3 bridge protocols` return a `supports_slippage` boolean per adapter — the rules below key off that field, so they apply uniformly.

## Announce the default before showing the quote

Default slippage is **0.5%**. Tell the user explicitly, in their language, before (or alongside) presenting the quote:

> Default slippage is **0.5%**. Let me know if you'd like a different value.

Do not let the user discover the default silently. The default is a real choice — if they want 5% (volatile / meme) or 0.1% (stables), they must say so.

**Never expose the CLI flag name to the user.** Users speak in percentages, not `--slippage`. Pass `--slippage <value>` to the CLI but do not say "I'll set --slippage to X%" — say "I'll use X% slippage".

## Per-adapter behaviour

After the user has picked a protocol / bridge, branch on its `supports_slippage` field:

- **`supports_slippage: true`** — pass `--slippage <value>` to the CLI and confirm the chosen percent in the summary (without naming the flag).
- **`supports_slippage: false`** — slippage has no effect for this protocol/bridge. **Omit `--slippage` entirely** and tell the user plainly:

  > This protocol sets the exchange rate at execution time, so a slippage tolerance does not apply here.

`supports_slippage` is the authoritative signal. Do not infer from protocol name or bridge `mode` — follow the field.

## Suggested ranges (when the adapter supports slippage)

| Scenario               | Slippage  |
|------------------------|-----------|
| Stablecoin pairs       | 0.1–0.3%  |
| Mainstream (ETH/BTC/majors) | 0.5–1%    |
| Volatile / low-cap     | 5–20%     |

Large-trade size warnings (e.g. >$1K or >10% price impact) belong in the calling skill's Risk Controls table, not here.
