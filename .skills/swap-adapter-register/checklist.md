# Post-Registration Checklist

Run these in order from the repo root after editing `common.go`. SKILL.md Step 4 invokes this — the rollback policy is defined there. This file just lists the commands and what failure means.

## 1. Compile

```bash
go build ./...
```

Must exit 0. If it fails:

- Common causes: constructor signature mismatch (e.g. you wrote `NewXxx(resolver)` but the adapter constructor takes no args); adapter struct missing a method from `swap.ProtocolAdapter`; import cycle.
- **Auto-revert the register line in `common.go`** (per SKILL.md Step 4) and show the error to the user.
- Do NOT edit the adapter file — that's the user's code.

## 2. Swap package tests

```bash
go test -count=1 -short ./tests/swap/...
```

Must end with `ok` for every package. If it fails:

- Likely cause: the new adapter altered shared state — e.g. its `Name()` collides with assumptions in an existing test, or its `SupportedChains()` overlaps in a way that breaks `bestAdapter` route selection.
- Do NOT auto-revert. The register line is logically correct; the failure indicates a real issue in the adapter.
- Show the failing test name and output. Let the user decide whether to fix the adapter, the test, or remove the registration.

> **Note:** `TestSwap_Registry_AllAdaptersRegistered` asserts a fixed `want` list of adapter names — adding an adapter without updating that list is a real test failure, which the register skill expects you to fix by extending `want`. The test does not, however, catch typos in the registration line itself (the registration would compile but silently leave the adapter out of the registry). The literal grep in step 3 is what catches that class of error.

## 3. Registration self-check (literal)

```bash
grep -E "reg\.Register\(New<YourProtocol>" internal/swap/adapters/common.go
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

After all tests pass, the user can rebuild the CLI to see the new protocol via `web3 swap protocols`:

```bash
go build -o ./bin/web3 ./cmd/cli/
./bin/web3 swap protocols
```

If they need the server binary too (only needed if testing through the API, not pure CLI):

```bash
CGO_ENABLED=1 go build -o ./bin/web3-server ./cmd/server/
```

## Optional but recommended — add a test

Existing adapters all have test coverage in `tests/swap/adapters/protocols_test.go`. Look at `TestOdosQuote_*`, `TestKyberBuild_*`, `TestOpenOceanQuote_*`, etc. for patterns.

Suggest to the user:

> Recommended: add a few `Test<Protocol>Quote_*` / `Test<Protocol>Build_*` functions at the end of `tests/swap/adapters/protocols_test.go` (model after existing tests like `TestOdosQuote_NormalizesNativeToZeroAddressAndRequiresPathID`). Not required for registration, but it protects your adapter from future refactors.

Do not generate these tests yourself — they require understanding the protocol's quote/build API behavior, which the user knows better.
