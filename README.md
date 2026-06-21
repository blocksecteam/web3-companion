<p align="center">
  <img src="docs/assets/logo.png" alt="Web3 Companion logo" width="280">
</p>

<h1 align="center">Web3 Companion</h1>

<p align="center"><strong>The Secure Agentic Wallet</strong></p>

<p align="center">
  <em>An AI-native wallet that operates on-chain autonomously — with private keys the agent can never reach and policies it can never change.</em>
</p>

<p align="center">
  <a href="https://hub.docker.com/r/blocksecteam/web3-companion"><img src="https://img.shields.io/docker/pulls/blocksecteam/web3-companion" alt="Docker"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT"></a>
</p>

<p align="center"><a href="README.zh.md">简体中文</a></p>

## Why Web3 Companion

AI agents can automate on-chain work — but most agentic wallets hold your private keys in the same process that reads untrusted content. One prompt injection, one malicious skill, one compromised dependency, and your funds are gone. Permanently.

Web3 Companion is a secure agentic wallet that takes a different approach. The AI agent can do everything a Web3 power user needs — inspect assets, prepare transfers, quote swaps and bridges, run security analysis, and sign transactions — but it operates within three independent security boundaries:

- **The agent never touches private keys.** Keys live in an isolated Secure Signature Module. The agent works with intent IDs, not key material.
- **Signing follows code-enforced policies.** Per-transaction caps, daily budgets, recipient allowlists. The agent cannot modify these — changes require your Passkey.
- **Passkey guards every sensitive action.** Policy changes, wallet export, manual signing approvals — all require hardware-bound biometric proof that you are physically present.

Even if the agent is fully compromised, it cannot drain the wallet. That's the design.

Read the full [Security Design](docs/SECURITY_DESIGN.md) for the threat model, architecture, and how each boundary works.

## Quick Start

```bash
docker pull blocksecteam/web3-companion
docker run -d --name web3-companion -p 8081:8081 -v web3-data:/home/web3 blocksecteam/web3-companion
```

Open **http://localhost:8081** — the Setup Wizard will guide you through LLM and RPC configuration. No `.env` file or pre-configuration needed.

<details>
<summary>Custom domain (HTTPS + Passkey binding)</summary>

```bash
docker run -d --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  -e WEB3_EXTERNAL_URL=https://companion.example.com \
  blocksecteam/web3-companion
```

Put a reverse proxy (Nginx/Caddy) in front with HTTPS. Passkeys bind to the domain in `WEB3_EXTERNAL_URL` — changing it later invalidates existing Passkeys.
</details>

<details>
<summary>AWS KMS wallet encryption</summary>

```bash
docker run -d --name web3-companion \
  -p 8081:8081 \
  -v web3-data:/home/web3 \
  -e AWS_REGION=us-east-1 \
  -e AWS_KMS_KEY_ID=arn:aws:kms:us-east-1:123456789:key/your-key-id \
  -e AWS_ACCESS_KEY_ID=AKIA... \
  -e AWS_SECRET_ACCESS_KEY=... \
  blocksecteam/web3-companion
```

All four AWS variables must be set together. Without them, wallet keys are encrypted locally with an auto-generated AES-256 key stored in SQLite.
</details>

<details>
<summary>Custom port</summary>

```bash
docker run -d --name web3-companion \
  -p 9090:8081 \
  -v web3-data:/home/web3 \
  -e WEB3_EXTERNAL_URL=http://localhost:9090 \
  blocksecteam/web3-companion
```

The container always listens on port 8081 internally. Map it to any host port with `-p <host>:8081`.

**Important:** When using a non-default port, you **must** set `WEB3_EXTERNAL_URL` to match. Passkeys are bound to the exact origin (scheme + host + port) — if `WEB3_EXTERNAL_URL` doesn't match the URL in your browser, Passkey registration and authentication will fail with "origin mismatch."
</details>

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/blocksecteam/web3-companion.git
cd web3-companion/docker
./start.sh
```

`start.sh` builds the image and starts Docker Compose. It does not require `docker/.env` or `docker/config.toml`; the first-run Setup Wizard handles normal configuration.
</details>

## What the Agent Can Do

- **Portfolio & assets** — inspect holdings across Ethereum, Base, Polygon, and BSC
- **Transfers** — send native tokens and ERC-20 tokens to any address
- **Swaps** — quote and execute same-chain swaps through 7 DEX aggregators (Odos, Kyber, ParaSwap, OpenOcean, Uniswap, PancakeSwap, LiFi)
- **Bridges** — quote and execute cross-chain bridges through Bungee, Relay, and LiFi
- **Security review** — simulate transactions, decode calldata, check for unlimited approvals, burn addresses, value conservation, and address risk labels before signing
- **Auto-sign** — sign within policy limits without manual approval; stop and ask for Passkey when limits are exceeded

## How Security Works

Every transaction goes through three independent layers. Each assumes the previous layer has been bypassed:

```
Layer 1: Security Review     → "Is this transaction safe?"
   Simulation, 14 static checks, address labels, AI review
   Can be bypassed by: missing data, prompt injection, novel attacks

Layer 2: Signing Policy      → "Is this transaction allowed?"
   Per-tx caps, daily budgets, recipient allowlists
   Code-enforced. Agent CANNOT modify. Changes need Passkey.

Layer 3: Passkey Approval    → "Does the human confirm?"
   Hardware-bound biometric challenge
   Cannot be bypassed without physical user presence
```

| If this is compromised... | These still protect you |
|--------------------------|----------------------|
| Agent (prompt injection) | No keys → can't sign. Policy → can't exceed limits. Passkey → can't approve. |
| Security review (manipulated) | Policy enforces hard limits regardless. Passkey still required. |
| Everything except Passkey | Hardware-bound credential → attacker needs your physical body. |

## Supported Chains

| Chain | Chain ID | Native token |
| --- | ---: | --- |
| Ethereum | 1 | ETH |
| Base | 8453 | ETH |
| Polygon | 137 | POL |
| BSC | 56 | BNB |

Adding a new EVM chain is configuration-only — append a `[[chains]]` block to `config.toml` and rebuild.

## Documentation

| Document | What it covers |
|----------|---------------|
| [User Guide](docs/USER_GUIDE.md) | Setup Wizard, account registration, wallet management, workflows, troubleshooting |
| [Developer Guide](docs/DEVELOPER_GUIDE.md) | Local development, testing, release process, extending skills and adapters |
| [Security Design](docs/SECURITY_DESIGN.md) | Threat model, private key isolation, transaction security layers, policy design |
| [中文用户手册](docs/USER_GUIDE.zh.md) / [中文开发指南](docs/DEVELOPER_GUIDE.zh.md) / [中文安全设计](docs/SECURITY_DESIGN.zh.md) | Chinese translations |

## Open-Source Research Preview

This repository is an open-source research version. It is provided for research, review, and experimentation. It has not completed a comprehensive production security audit or full safety validation.

Use it at your own risk. BlockSec does not guarantee the security, correctness, availability, or fitness of this software, and is not responsible for security incidents, functional failures, transaction errors, key loss, asset loss, or any direct or indirect losses arising from use of this repository or derived deployments.

## Repository Map

```text
cmd/                  # entrypoints: server, cli, dbdump
companion/            # AI agent runtime (TypeScript + Claude Agent SDK)
frontend/             # embedded React browser app
internal/             # Go API server
  ├── auth/           # JWT + email + Passkey login
  ├── ssm/            # Secure Signature Module (vault, sign, delegate)
  ├── security/       # transaction simulation + 14 static checks + AI review
  ├── policy/         # signing policies + auto-sign budget tracking
  ├── swap/           # 7 DEX aggregator adapters
  ├── bridge/         # 3 bridge protocol adapters
  ├── chain/          # native + ERC-20 transfer, broadcast
  └── ...
skills/               # agent skills baked into the Docker image
.skills/              # developer extension skills for contributors
tests/                # Go unit + integration + Playwright E2E tests
docker/               # Dockerfile, compose, config, entrypoint
```

## License

Licensed under the [MIT License](LICENSE).
