# Web3 Companion Frontend — Development Guide

## Project Overview

This frontend is the first step toward a personal Web3 Companion: a warm, security-conscious chatbot that uses natural language to drive wallet, portfolio, swap, bridge, chain, and security workflows.

The current implementation is split between the frontend client and the backend-managed companion runtime:

- Frontend: Bun + React + Vite + Tailwind CSS + shadcn-style UI primitives.
- Companion server: lives in `../companion` and is launched by the Go backend per user.
- Backend integration: the React app talks to the Go backend for auth, companion gatewaying, passkey confirmations, and JWT-protected calls.

## Design Context

### Users
Web3 Companion serves regular web3 users first, including people who actively use DeFi but do not want to think like protocol engineers. They arrive with practical jobs: inspect what they own, understand risk, move assets, swap tokens, bridge chains, and approve sensitive actions with confidence. The interface should make natural-language chain operations feel calm, legible, and recoverable.

### Brand Personality
Warm, capable, careful. The product should feel like a personal web3 assistant with Claude/Anthropic-like clarity: conversational, precise, humane, and security-aware. It should avoid casino energy, trading-terminal intimidation, and generic dark neon AI aesthetics.

### Aesthetic Direction
A refined light interface with ink, parchment, clay, graphite, and muted copper/green accents. The mood is personal command center rather than exchange dashboard. Use restrained motion, strong typography, generous whitespace, and high-trust interaction details. The memorable signal is a chat surface that feels like a secure operating desk for on-chain actions, with tool calls rendered as understandable, inspectable steps.

### Design Principles

- Make irreversible web3 actions feel understandable before they feel fast.
- Lead with conversation, but expose state, sessions, and tool activity clearly.
- Keep the experience friendly for regular users and sufficiently dense for DeFi-heavy users.
- Favor warm, precise, low-noise visual language over dark sci-fi decoration.
- Treat Passkey, policies, and confirmations as first-class trust moments, not technical interruptions.

## Implementation Insights

### Local Passkey Registration

- For localhost development, do not explicitly set WebAuthn `rp.id` or auth `rpId`; let the browser derive the relying party from the current origin. Hardcoding `window.location.hostname` can fail across `localhost`, `127.0.0.1`, or in-app browser contexts before the native passkey prompt appears.
- Passkey flows must run in a secure context. `localhost` is acceptable; arbitrary LAN IPs usually are not. Prefer `http://localhost:5175/` for manual testing.
- Before opening a passkey ceremony, check `window.isSecureContext`, `browserSupportsWebAuthn()`, and `platformAuthenticatorIsAvailable()`. Clear early failures are easier to debug than a silent browser ceremony that eventually reports timeout.
- Keep the app-level passkey watchdog longer than the WebAuthn ceremony timeout. Browser-owned WebAuthn UI may take time to surface, and the frontend should not cancel it immediately after the browser begins its own timeout behavior.

## Core User Flow

1. Landing page opens first. It should be simple, technology-rich, and personal, not a marketing splash screen.
2. Login/Register uses one email entry point:
   - Registered email: show passkey login challenge.
   - New email: guide passkey binding, then require a second passkey login confirmation.
3. Chatbot is the primary product surface:
   - Server uses Claude Agent SDK.
   - Hono + WebSocket streams SDK messages, including partial messages.
   - Client creates a query `session` id before first message so every conversation has a stable id from the start.
   - Multiple sessions are supported, but only one session may be running at a time.
   - Workdir is fixed server-side and must not be user-controlled.
   - Tool calls should render with varied, understandable components rather than raw JSON dumps.
4. User management sits in the sessions sidebar:
   - Generated avatar and email are part of the sidebar account area instead of floating over the chat.
   - Menu order: portfolio, transactions, settings, logout.
   - Portfolio, transactions, and settings are placeholder pages for now.

## Companion Server Requirements

- Use `@anthropic-ai/claude-agent-sdk` with Hono and WebSocket.
- In self-hosted container mode, the companion runtime uses the service user's mounted home. Keep `HOME`, `CLAUDE_CONFIG_DIR`, and `WEB3_AGENT_WORKDIR` server-controlled.
- Use DeepSeek through the Anthropic-compatible endpoint:
  - Model/provider defaults and auth are written by the backend into the companion user's `~/.claude/settings.json`.
  - Do not read model auth from frontend, companion, or Claude process env.
  - Do not set `ANTHROPIC_SMALL_FAST_MODEL`.
- Use `settingSources: ["user", "project"]` so the backend-managed `~/.claude/settings.json` and project memory are both loaded.
- Use `appendSystemPrompt` and load the prompt from `../companion/web3_companion.md`.
- Synchronize backend skills into the Claude config dir for SDK use; the repo `skills/` directory is the source of truth.
- Fixed `cwd`: the backend-provided per-user `workdir`.
- Session store persists user sessions to SQLite.
- Support listing sessions, reading session history, and deleting sessions. Forking is intentionally unsupported.

## Commands

```bash
bun install
bun run dev
bun run check
```
