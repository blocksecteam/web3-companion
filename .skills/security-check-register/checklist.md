# Post-Registration Checklist

Run these in order from the repo root after editing `common.go`. SKILL.md Step 4 invokes this — the rollback policy is defined there. This file just lists the commands and what failure means.

## 1. Compile

```bash
go build ./...
```

Must exit 0. If it fails:

- Common causes: the struct does not implement every method of the chosen interface (most often a missing or mistyped `Description()`); the `Run` signature uses the wrong Context type; the struct lives in `package security` instead of `package checks`; a missing `primitives` / `security` import.
- **Auto-revert the register line in `common.go`** (per SKILL.md Step 4) and show the error to the user.
- Do NOT edit the check file — that's the user's code.

## 2. Security package tests

```bash
go test -count=1 -short ./tests/security/...
```

Must end with `ok` for every package. If it fails:

Two failure modes are routine and have a known fix that does NOT require rolling back the registration:

1. **`TestSecurity_Registry_AllChecksRegistered` failing with "unexpected rule X in registry"**: add the new `Rule()` string to the `want` slice in `tests/security/registry_test.go`, preserving the registration-order grouping by phase.
2. **An existing handler-level test (`TestSecurity_Check_Pass`, `TestSecurity_Check_SkippedRuleAppearsInResponse`, `TestSecurity_Check_PassAllowsSigningWhenReviewModeWarn`, …) starts producing `warn` or `fail` where it used to produce `pass`**: usually means the new rule's `AppliesTo` matches the test's `tx_type` and the rule reads input the fixture does not supply (typically `tx_summary.*` fields or `SimulationContext.FundFlow`). Fix the fixture — use `createSecurityIntentWithTxTypeAndSummary` to supply a `tx_summary` that matches the simulated effect, and extend the stub simulator's `fundFlow` if the rule walks it. Do NOT relax the rule to make the test pass.

Anything else (e.g. `Run` mutates shared state and breaks a downstream check) likely points at a real bug in the new check. Do NOT auto-revert. Show the failing test name and output. Let the user decide whether to fix the check, update the test, or remove the registration.

> **Note:** `TestSecurity_Registry_AllChecksRegistered` asserts the registry's rule set bidirectionally (both missing rules and unexpected rules fail), but only via the `want` slice the test maintainer keeps in sync. The literal grep in step 3 is independent — it catches typos in the constructor name or registrations that landed in the wrong file.

## 3. Registration self-check (literal)

```bash
grep -E "reg\.Register[A-Za-z]*\(<YourRule>" internal/security/checks/common.go
```

Must print one matching line. If it prints nothing:

- The registration line did not land where intended (typo, wrong constructor name, wrong `Register*` method for the phase, edited wrong file).
- Re-open `common.go`, fix the line, and re-run steps 1–3.

## What to skip

- `go vet` / linters — only run if the user asks.
- `internal/security/...` package-internal tests — covered by `tests/security/...` indirectly.

## Rebuilding the server binary (for end-to-end verification)

After all checks pass, the user can rebuild the server binary so the running deployment picks up the new check:

```bash
CGO_ENABLED=1 go build -o ./dist/web3-server ./cmd/server/
```

The CLI binary does not need rebuilding — security checks run server-side and the CLI just renders whatever the server returns.

## Strongly recommended — add a regression test

Existing tests in `tests/security/handler_test.go` follow a fixture pattern: build a registry that contains ONLY the check under test, drive a request through `/api/v1/security/check`, and assert via `assertRuleStatus` that the rule's status matches expectations.

Suggest to the user (use stronger language than for swap/bridge — security checks let real money through):

> **Strongly recommended:** add a `TestSecurity_Check_<YourRule>_*` test that constructs a scenario where your `Run()` should emit non-pass (and a separate scenario for pass), builds a registry containing only your check via `security.WithCheckRegistry`, and asserts the resulting status via `assertRuleStatus`. Security checks silently letting transactions through is the failure mode you most want a regression test to catch.

Do not generate these tests yourself — they require understanding the precise calldata or simulation conditions that should trigger your rule, which the user knows better.
