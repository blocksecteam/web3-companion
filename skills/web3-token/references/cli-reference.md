# Web3 Token — CLI Reference

## `web3 token search`

Search tokens by name, symbol, or address.

| Param | Required | Default | Description |
|-------|----------|---------|-------------|
| `--query` | Yes | — | Search query (name, symbol, or address) |
| `--chain` | Yes | — | Chain key (one of the chains returned by `web3 chain list`). Per SKILL.md, confirm the chain with the user — token addresses and decimals differ across chains. |

**Return:** Array of token objects with `symbol`, `name`, `address`, `chain` (or `network`), `decimals`, `price`, `liquidity`.

## `web3 token info --chain <chain> <address>`

Get detailed info for a specific token.

| Param | Required | Default | Description |
|-------|----------|---------|-------------|
| `--chain` | Yes | — | Chain name. Confirm with the user. |
| `address` | Yes | — | Token contract address |

**Return fields:** `symbol`, `name`, `decimals`, `address`, `chain`, `price`, plus optional `marketCap`, `totalSupply`, `liquidity`.
