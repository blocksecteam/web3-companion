# Testing Rules

## Core Principles

1. **Every test case must have a clear goal.** If you cannot state in one sentence what behavior the test verifies, delete it.
2. **Never write tests just to increase coverage.** Coverage is a metric, not a goal. A test that exercises code without asserting meaningful behavior is worse than no test — it creates a false sense of safety.
3. **Test behavior, not implementation.** Assert what the user/caller observes (HTTP status, response body, UI state), not internal variable values or call counts.
4. **One behavior per test.** If a test name contains "and", split it. Exception: setup/teardown that validates a complete lifecycle.

## Coverage Target

- Target **85%+ meaningful line coverage** for changed and security-critical areas.
- Current scripts do not yet enforce a repository-wide coverage threshold automatically.
- Release candidates should include coverage evidence alongside passing build/test/security checks.
- Prefer per-domain/package coverage over a single global percentage.

### Critical security domains:

| Package | Why |
|---------|-----|
| `internal/auth` | Authentication bypass = total compromise |
| `internal/passkey` | WebAuthn verification errors = auth bypass |
| `internal/ssm` | Key management bugs = fund loss |
| `internal/policy` | Policy bypass = unauthorized signing |
| `internal/txintent` | State machine bugs = double-sign or stuck intents |
| `internal/security` | Check bugs = false pass on dangerous tx |
| `internal/delegation` | Delegation errors = wallet unusable or insecure |

### What NOT to cover-test:

- Trivial getters/setters, `String()` methods, `Error()` implementations
- Third-party library wrappers that just delegate (trust the library)
- Generated code, embedded assets, build tags
- One-off migration scripts

## Test Layers

### Layer 1: Unit Tests (Go + TypeScript)

**Location:** `tests/<package>/` for Go, `src/**/*.test.ts` for frontend/companion

**Run:** `go test ./internal/... ./tests/...` and `bun test`

**Rules:**
- Mock external services (HTTP, RPC, DB for isolation)
- Each test < 100ms. Slow tests use `t.Skip("slow")` and run separately.
- Use `t.Helper()` in all test helpers
- Use table-driven tests (`t.Run` subtests) for parameterized cases
- Never test the mock — if your test passes with any return value from the mock, the test is useless

### Layer 2: Integration Tests (Go, live APIs)

**Location:** `tests/integration/`

**Run:** `WEB3_INTEGRATION=1 go test ./tests/integration/... -timeout=300s`

**Config:** `docker/.env.integration` (gitignored, real API keys)

**Rules:**
- Always use `integrationCtx(t)` (30s timeout) — never `context.Background()`
- Use `checkIntegrationEnabled(t)` gate — tests skip gracefully without config
- External API errors (429, timeout) → `t.Skipf`, not `t.Fatalf` — flaky external APIs must not block CI
- Use centralized address constants from `env_test.go` — no scattered hardcoded addresses
- Validate response structure, not just non-empty — use `validateOutputAmount()` pattern

### Layer 3: E2E Browser Tests (Playwright)

**Location:** `tests/e2e/`

**Run:** `bunx playwright test`

**Rules:**
- Test from the user's perspective: "I click X, I see Y"
- Use accessible selectors (`getByRole`, `getByLabel`, `getByText`) — not CSS classes or test-ids unless necessary
- Each test is self-contained: creates its own user, cleans up after
- WebAuthn uses Playwright's virtual authenticator — no real hardware
- Test the three states of every async flow: loading, success, error
- No `page.waitForTimeout()` — use `page.waitForSelector()` or `expect().toBeVisible()`
- E2E tests run against a local Docker instance started by the test harness

### Required E2E Scenarios

| Scenario | What it verifies |
|----------|-----------------|
| Setup Wizard complete flow | Steps 1-6 end-to-end, redirect to chat |
| Register + Login | Email → Passkey create → verify → session |
| Settings save + reload | LLM/RPC/Providers persist across page reload |
| Wallet list + default policy | Create wallet, see it in list, policy bound |
| Chat send + receive | Send message, see assistant response stream |
| Session switch | Switch sessions, messages don't bleed |
| Passkey pending state | Trigger sensitive action, see pending UI |
| Version check banner | Mock Docker Hub → banner appears in Settings |

## Writing Test Cases

### Good test:
```go
func TestTransferIntentMatch_DelegateCallToTrustedDelegate_NotFlagged(t *testing.T) {
    // GOAL: EIP-7702 designator DELEGATECALL to MM Delegator must not
    // trigger trace_delegatecall finding — it's expected wallet behavior.
    ...
}
```

### Bad test (do NOT write):
```go
func TestTransferIntentMatch_Coverage(t *testing.T) {
    // No clear goal — just calls the function to bump coverage
    TransferIntentMatch{}.Run(someContext)
}
```

### Bad test (do NOT write):
```go
func TestHandler_ReturnsJSON(t *testing.T) {
    // Tests the framework, not our code
    w := DoRequest(...)
    if w.Header().Get("Content-Type") != "application/json" {
        t.Error("not json")
    }
}
```

## Coverage Evidence

```bash
go test -coverprofile=coverage.out -covermode=atomic ./internal/... ./tests/...
go tool cover -func=coverage.out | tail -n 20
```

Frontend/companion coverage via `bun test --coverage`.

## Pre-Push Hook

`scripts/pre-push` runs before every `git push`:

1. Sensitive data scan (blocks if secrets found)
2. Integration tests (if `docker/.env.integration` exists with `WEB3_INTEGRATION=1`)

E2E tests are NOT in pre-push because they are too slow. They run in the local release pipeline (`scripts/release-local.sh`).
