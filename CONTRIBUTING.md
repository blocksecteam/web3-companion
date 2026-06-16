# Contributing to Web3 Companion

Thank you for your interest in contributing! Here is how to get started.

## Development Setup

```bash
# Clone the repo
git clone https://github.com/blocksecteam/web3-companion.git
cd web3-companion

# Start with Docker (recommended)
cd docker
./start.sh
```

`start.sh` builds the image, prepares the data directory, and starts Docker Compose. It does not require `docker/.env` or `docker/config.toml`; use the Setup Wizard or Settings for normal LLM/RPC configuration.

Do not require contributors to copy `.env.example` or `config.example.toml` manually for the normal local workflow. Deployment overrides can be passed as shell environment variables.

### Git Hooks

Install the pre-push hook to block accidental secret leaks:

```bash
cp scripts/pre-push .git/hooks/pre-push
```

### Running Tests

```bash
# Go tests
go test ./internal/... ./tests/... -count=1

# Frontend tests
cd frontend && bun install && bun test

# Companion tests
cd companion && bun install && bun test

# Type checks
cd frontend && bunx tsc --noEmit
cd companion && bunx tsc --noEmit
```

## Pull Request Process

1. Fork the repository and create a branch from `main`.
2. Make your changes. Write tests for new functionality.
3. Ensure all tests pass and type checks are clean.
4. Open a pull request with a clear description of the change.

### Commit Messages

Use short, descriptive commit messages with a type prefix:

- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation only
- `ui:` frontend/UI change
- `refactor:` code restructuring without behavior change
- `test:` adding or updating tests

### Code Style

- **Go**: follow standard `gofmt` formatting. Run `go vet ./...` before committing.
- **TypeScript**: follow the existing project conventions. Run `tsc --noEmit` before committing.
- **Tests**: write tests before implementation (TDD). New features require tests.

## Reporting Issues

Open a GitHub issue with:
- Steps to reproduce
- Expected vs actual behavior
- Docker version, OS, and browser (if UI-related)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
