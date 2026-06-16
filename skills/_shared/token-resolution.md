# Shared Token Resolution

Used by web3-swap, web3-bridge, web3-chain.

## Symbol → address

`--from` / `--to` / `--token` accept a contract address or the literal `native` (chain's native gas token). **Never pass a bare symbol** — the CLI rejects it.

Resolve symbol → address in this order, **per chain** (USDC on Ethereum ≠ USDC on Base — same symbol, different contract):

1. Look up in `web3 chain list` output for the relevant chain. Canonical entries (the chain's native token, common stables, etc.) map each symbol to exactly one address. Use it directly.
2. If the symbol is NOT in `web3 chain list`: run `web3 token search --query <symbol> --chain <chain>`, show results to the user, and wait for confirmation before proceeding.

## Decimals

Token decimals can differ per chain (e.g. BSC USDC/USDT use 18 decimals rather than the 6 used on most other chains). Do not assume a default.

- **For agent display** (formatting balances, summary fields): read decimals from whichever lookup returned the token — `web3 chain list` for canonical entries, `web3 token search` / `web3 token info` for the rest.
- **For build / quote / transfer calls**: pass `--amount` as a human-readable quantity. Never pass raw wei — the CLI handles decimals for you.

## Native token

Pass `native` for the chain's native gas token. The native symbol per chain is in `web3 chain list` as `native_symbol` (ETH on Ethereum, BNB on BSC, POL on Polygon, etc.) — do not maintain a hardcoded chain → symbol table.

## NEVER

<NEVER>
Never pass a bare symbol (e.g. `--from USDC`) — always resolve to an address first.
Never assume cross-chain symbol equivalence (USDC on chain A ≠ USDC on chain B).
Never hardcode token addresses or decimals in this skill.
</NEVER>
