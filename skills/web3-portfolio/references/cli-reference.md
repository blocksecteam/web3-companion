# Web3 Portfolio — CLI Reference

## `web3 portfolio`

Get account balance snapshot with USD valuation. Two invocation forms:

1. **By wallet ID (preferred when the user has wallets configured):**
   ```bash
   web3 portfolio --wallet <wallet_id> --chain <chain>
   ```
   The CLI resolves the wallet's address from the configured wallets for you.

2. **By explicit address:**
   ```bash
   web3 portfolio <chain> <address>
   ```

| Param | Required | Default | Description |
|-------|----------|---------|-------------|
| `--wallet` | Conditional | — | Wallet ID; required for form 1 |
| `--chain` | Conditional | — | Chain name or comma-separated list (e.g. `ethereum` or `ethereum,base,bsc,polygon`); required for form 1. Per SKILL.md, confirm the chain with the user (single-chain) or follow the Multi-chain queries section. |
| `<chain>` (arg) | Conditional | — | Chain name or comma-separated list (same semantics as `--chain`); required for form 2 |
| `<address>` (arg) | Conditional | — | Wallet address; required for form 2 |

**Return fields:**

| Field | Type | Description |
|-------|------|-------------|
| `chain` | String | Echoes the requested chain string (single chain or CSV) |
| `address` | String | Queried address |
| `total_usd` | Number | Total portfolio value in USD across all queried chains |
| `balances` | Array | Token holdings; multi-chain queries interleave entries from every chain queried |

Each balance entry:

| Field | Type | Description |
|-------|------|-------------|
| `address` | String | Contract address |
| `symbol` | String | Token symbol |
| `name` | String | Token name |
| `balance` | Number | Balance in UI units |
| `balanceUSD` | Number | USD value |
| `price` | Number | Current price |
| `decimals` | Number | Token decimals |
| `network` | String | Chain this balance is from (essential for multi-chain queries; single-chain queries echo the requested chain) |
