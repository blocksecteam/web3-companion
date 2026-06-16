# Post-Registration Checklist

Run these in order from the repo root after editing `common.go`. SKILL.md Step 4 invokes this — the rollback policy is defined there. This file just lists the commands and what failure means.

## 1. Compile

```bash
go build ./...
```

Must exit 0. If it fails:

- Common causes: constructor signature mismatch (e.g. you wrote `NewXxx(resolver)` but the adapter constructor takes no args, or vice versa); adapter struct missing a method from `bridge.Adapter`; import cycle.
- **Auto-revert the register line in `common.go`** (per SKILL.md Step 4) and show the error to the user.
- Do NOT edit the adapter file — that's the user's code.

## 2. Bridge package tests

```bash
go test -count=1 -short ./tests/bridge/...
```

Must end with `ok` for every package. If it fails:

- Likely causes: the new adapter's `Name()` collides with assumptions in an existing test; its `SupportedPairs()` overlap breaks `best_quote_test.go` route selection; its `ApproveCapability`/`NativeCapability` shape breaks `wrap_approve_test.go` or `approve_test.go`.
- Do NOT auto-revert. The register line is logically correct; the failure indicates a real issue in the adapter or in the test's assumptions.
- Show the failing test name and output. Let the user decide whether to fix the adapter, the test, or remove the registration.

> **Note:** The bridge test suite calls `RegisterAll(reg, nil)` but does not assert which adapter names are present — so a missing or typo'd registration line will not fail the suite. The literal grep in step 3 below is what catches that class of error.

## 3. Registration self-check (literal)

```bash
grep -E "reg\.Register\(New<YourBridge>" internal/bridge/adapters/common.go
```

Must print one matching line. If it prints nothing:

- The registration line did not land where intended (typo, wrong constructor, edited wrong file).
- Re-open `common.go`, fix the line, and re-run steps 1–3.

## 4. Chain tests (smoke)

```bash
go test -count=1 -short ./tests/chain/... 2>&1 | tail -10
```

These should pass without the new adapter affecting them. If they fail it's almost certainly unrelated — note it but don't block.

## What to skip

- `ssm`, `user`, `integration` tests — these need a writable `/tmp/web3-test` and external RPC. Their failure is a known environment issue, not a regression from registration.
- `go vet` / linters — only run if the user asks.

## Rebuilding the CLI binary (for end-to-end verification)

After all tests pass, the user can rebuild the CLI to see the new bridge via `web3 bridge protocols`:

```bash
go build -o ./bin/web3 ./cmd/cli/
./bin/web3 bridge protocols
```

If they need the server binary too (only needed if testing through the API, not pure CLI):

```bash
CGO_ENABLED=1 go build -o ./bin/web3-server ./cmd/server/
```

## Optional but recommended — add a test

Existing bridge adapters have test coverage in `tests/bridge/` (e.g. `bungee_test.go`). Suggest to the user:

> Recommended: add a `<bridge>_test.go` under `tests/bridge/` (model after the existing `bungee_test.go`). Not required for registration, but it protects your adapter from future refactors.

Do not generate these tests yourself — they require understanding the bridge API's quote/build response shape, which the user knows better.
