# Changelog

## v0.1.0 — Initial Release

First open-source release of Web3 Companion — the secure agentic wallet.

### Features

- **Secure agentic wallet** — AI-native wallet with private key isolation, code-enforced signing policies, and Passkey approval
- **Self-hosted wallet management** — create, export, and delete EIP-7702 managed wallets with ERC-7821 batch execution
- **AI agent** — chat interface powered by Claude Agent SDK with Web3-specific skills (transfer, swap, bridge, portfolio)
- **Transaction security review** — static checks + AI agent review with simulation trace analysis before signing
- **Passkey authentication** — WebAuthn-based login and transaction approval
- **Signing policies** — per-wallet policies with daily limits, per-transaction caps, and optional auto-sign mode
- **Multi-chain support** — Ethereum, Base, Polygon, BSC out of the box
- **Setup wizard** — guided first-run configuration for RPC, LLM, and account setup
- **SSM encryption** — wallet private keys encrypted with local AES-256 or AWS KMS envelope encryption
- **Docker deployment** — single-container deployment with SQLite, persistent volume, and health checks
