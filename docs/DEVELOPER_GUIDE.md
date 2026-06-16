# Web3 Companion Developer Guide

[简体中文](DEVELOPER_GUIDE.zh.md)

This guide is for contributors who want to build, test, extend, and release Web3 Companion.

## Project Shape

Web3 Companion is a Docker-first self-hosted distribution. The repository contains:

| Path | Purpose |
| --- | --- |
| `cmd/` | Go entrypoints for server and CLI |
| `internal/` | Go server packages |
| `frontend/` | React browser app |
| `companion/` | TypeScript companion runtime and prompts |
| `skills/` | Runtime skills loaded by the companion agent |
| `.skills/` | Developer extension skills |
| `tests/` | Go unit and integration tests |
| `docker/` | Docker runtime files |
| `scripts/` | local test, release, and build helpers |

SQLite is the only supported database engine.

## Local Development Setup

Install:

- Go 1.26+
- Bun
- Docker

Start the service:

```bash
cd docker
./start.sh
```

`start.sh` builds the image, prepares the data directory, and starts Docker Compose. It does not require `docker/.env` or `docker/config.toml`.

Do not document or require manual `.env` or `config.toml` setup for the normal source workflow. `./start.sh` is the supported local entrypoint.

Deployment overrides can be passed as shell environment variables before `./start.sh` or `docker compose`.

Open:

```text
http://localhost:8081
```

## Configuration Boundaries

Runtime configuration starts from `docker/`.

- Root-level `.env` and root-level `config.toml` are not runtime entrypoints.
- Normal startup does not require `docker/.env` or `docker/config.toml`.
- Both published and source-built images bake `docker/config.docker.toml` into `/opt/web3/config.toml`.
- Deployment overrides can be passed through environment variables.
- The authenticated Settings page can update runtime LLM and RPC settings.

Never commit:

- `.env` files with real values
- private keys
- mnemonic or seed phrases
- wallet exports
- `WEB3_SSM_KEY`
- `WEB3_JWT_SECRET`
- AWS credentials
- LLM provider keys
- RPC provider keys
- Portals or MetaSleuth keys
- Docker runtime data

## Runtime Security Model

The Go server is the authority for secrets, wallet state, policy, security checks, signing, and broadcast.

The companion runtime and Claude process must not receive raw deployment secrets. Environment passing is allowlist-based.

Do not pass these values to companion or Claude:

- `WEB3_SSM_KEY`
- AWS credentials
- JWT secret
- raw LLM env key
- RPC provider keys
- Portals key
- MetaSleuth key
- private keys

## Wallet and Transaction Assumptions

All wallets are EIP-7702 managed.

- Delegator: `0x63c0c19a282a1b52b07dd5a65b58948a07dae32b`
- Non-EIP-7702 wallets are rejected.
- Delegation is a separate explicit flow.
- Write builds reject `not_delegated`.
- `insufficient_gas` means the user must add native token.
- Protocol writes, ERC-20 writes, and native transfers use ERC-7821 `execute(bytes32,bytes)` calldata.
- Security checks run after build and before SSM signing/broadcast.
- Scan failure is not pass.

## Test-Driven Development

Use TDD for code changes:

1. Write or update the failing test first.
2. Implement the smallest change that makes the test pass.
3. Refactor while keeping tests green.

New behavior should have tests at the correct level:

- Go package/service behavior: Go tests under `internal/` or `tests/`.
- Frontend logic and copy: Bun tests under `frontend/src`.
- Companion runtime behavior: Bun tests under `companion/src`.
- Live provider or chain behavior: Go integration tests under `tests/integration`.

The project target is **85%+ meaningful line coverage** for changed and security-critical areas. Current scripts do not yet enforce a repository-wide coverage threshold automatically, so release candidates should include coverage evidence in addition to passing tests. See `TESTING.md` for the full testing rules.

## Running Tests

Run the local CI-equivalent checks:

```bash
./scripts/test.sh
```

This runs:

- Go build
- Go vet
- Go short tests
- frontend type check and build
- frontend tests
- companion type check
- companion tests with isolated `HOME`
- Gitleaks current-tree scan
- project-specific sensitive data checks

Run Go tests directly:

```bash
go test -short ./internal/... ./tests/... -count=1 -timeout=120s
```

Run frontend checks:

```bash
cd frontend
bun install --frozen-lockfile
bun run check
bun test
```

Run companion checks:

```bash
cd companion
bun install --frozen-lockfile
bun run check
HOME="$(mktemp -d)" bun test
```

Use an isolated `HOME` for companion tests so local Claude authentication does not affect results.

## Integration Tests

Integration tests live under:

```text
tests/integration/
```

They require:

```bash
WEB3_INTEGRATION=1
```

They also need live provider configuration, usually through shell environment or `docker/.env.integration`:

```text
WEB3_RPC_ETHEREUM_URL=...
WEB3_RPC_BASE_URL=...
WEB3_RPC_POLYGON_URL=...
WEB3_RPC_BSC_URL=...
PORTALS_API_KEY=...
METASLEUTH_API_KEY=...
```

Run:

```bash
WEB3_INTEGRATION=1 go test -count=1 -v ./tests/integration/ -timeout=600s
```

Some tests may skip when optional provider keys, chain balances, or RPC URLs are absent. Release testing should use real provider keys and trace-capable RPC endpoints.

## E2E Browser Tests

E2E tests use Playwright with a virtual WebAuthn authenticator. They run against a live Docker instance.

Location: `tests/e2e/`

Specs:

| File | What it tests |
| --- | --- |
| `setup-wizard.spec.ts` | 6-step wizard flow, default RPCs, trace warning |
| `auth.spec.ts` | Register + login with virtual Passkey |
| `settings.spec.ts` | LLM/RPC display, save + reload persistence |
| `wallets.spec.ts` | Wallet list, encryption mode, policy binding |
| `chat.spec.ts` | Send message, session switch (no bleeding), scroll behavior |
| `passkey-pending.spec.ts` | Export/delete wallet shows Passkey pending state |
| `version-check.spec.ts` | Version display, /health endpoint, update banner |

Run locally:

```bash
cd tests/e2e
bun install
E2E_BASE_URL=http://localhost:8081 bunx playwright test
```

E2E tests are included in the local release pipeline (`scripts/release-local.sh`). They are NOT in the pre-push hook (too slow).

## Secret Scanning

Run:

```bash
docker run --rm -v "$PWD:/repo" -w /repo ghcr.io/gitleaks/gitleaks:v8.28.0 dir --redact --verbose .
./scripts/check-sensitive-data.sh
```

Gitleaks catches common secrets. The project-specific script checks tracked sensitive filenames, private-key-like assignments, mnemonic assignments, and Docker ignore coverage.

## Git Hooks

Install the pre-push hook:

```bash
cp scripts/pre-push .git/hooks/pre-push
chmod +x .git/hooks/pre-push
```

The hook runs secret scans. If `docker/.env.integration` exists and sets `WEB3_INTEGRATION=1`, it also runs integration tests.

## Local Release

Builds and releases are run locally.

Dry-run a release build without pushing:

```bash
scripts/release-local.sh --skip-integration v1.2.3
```

Formal publish:

```bash
git status --short
git tag v1.2.3
docker login
WEB3_INTEGRATION=1 scripts/release-local.sh --push v1.2.3
```

`--push` requires:

- clean worktree
- tag exists and points to `HEAD`
- secret scans pass
- Go/frontend/companion checks pass
- integration tests pass
- Docker buildx multi-arch build succeeds

Default published tags:

```text
blocksecteam/web3-companion:v1.2.3
blocksecteam/web3-companion:latest
```

## Extending Runtime Skills

User-facing runtime skills live in:

```text
skills/
```

Shared contracts live in:

```text
skills/_shared/
```

Before editing a flow-specific skill, check whether the behavior is governed by a shared contract:

- `_shared/preflight.md`
- `_shared/error-codes.md`
- `_shared/passkey.md`
- `_shared/security-handling.md`
- `_shared/intent-expiry.md`
- `_shared/amount-resolution.md`
- `_shared/token-resolution.md`
- `_shared/slippage.md`
- `_shared/broadcast-output.md`
- `_shared/tags.md`

Skill files are baked into the Docker image. Rebuild and restart the image after changing them.

## Adding Integrations

Developer extension flows live under `.skills/`.

Use the relevant skill rather than inventing a new registration pattern:

| Task | Skill |
| --- | --- |
| Register a swap adapter | `.skills/swap-adapter-register/` |
| Register a bridge adapter | `.skills/bridge-adapter-register/` |
| Register a static security check | `.skills/security-check-register/` |

Adding a new EVM chain is configuration-only for self-built deployments: update the baked Docker config and rebuild. The chain must support EIP-7702.

## Frontend Development Notes

The frontend uses React, Vite, Bun, and i18n.

User-visible strings should go through `frontend/src/i18n.ts`. Tests enforce that translatable attributes such as placeholders and aria labels are not hardcoded in many locations.

Run:

```bash
cd frontend
bun run check
bun test
```

## Companion Development Notes

The companion server uses the Claude Agent SDK and reads the main prompt from:

```text
companion/web3_companion.md
```

The Security Agent prompt is:

```text
companion/security.md
```

Both prompt files are baked into the Docker image. Rebuild and restart after changing them.

## Go Development Notes

Follow standard Go formatting:

```bash
gofmt -w <files>
go vet ./...
go test -short ./internal/... ./tests/... -count=1
```

Keep package boundaries aligned with existing responsibilities. If a package becomes very small, especially under 200 lines, consider merging it into an adjacent responsibility unless doing so would create dependency cycles or blur a stable boundary.

## Pull Request Checklist

- Tests were written or updated first for behavior changes.
- `./scripts/test.sh` passes.
- Integration tests were run when behavior touches live chain/provider flows.
- No secrets or local runtime files are tracked.
- User-facing text is localized.
- Documentation is updated when behavior, setup, release, or extension contracts change.
- Coverage evidence is provided for significant changes.
