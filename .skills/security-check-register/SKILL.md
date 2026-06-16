---
name: security-check-register
description: "Use when: register security check, add security rule, 注册security check, 新增安全检测规则, wire up new static check, install security rule. NOT for changing existing check logic — only for adding a new rule struct."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Security Check Register — Wire a new static check into the registry

This skill registers a user-authored static security check `.go` file into the project, then verifies it builds and existing tests still pass.

It does NOT design the check logic. The user must already have a `.go` file that implements one of the five `security.*Check` interfaces. If they don't, this skill tells them where to put it and exits.

## Step 0 — Locate the new check file

All built-in security check structs live in `internal/security/checks/`. To find the user's new file, do NOT rely on a hard-coded list (it goes stale). Use one of these in order:

1. **Preferred — git status:**
   ```bash
   git status --porcelain internal/security/checks/ | grep -E '^\?\?|^ M|^A ' | awk '{print $2}' | grep '\.go$'
   ```
   Any untracked or newly-added `.go` file under `internal/security/checks/` is a candidate.

2. **Fallback — scan for unregistered structs:**
   ```bash
   grep -hE '^func \([A-Za-z]+\) Rule\(\) string' internal/security/checks/*.go | grep -v _test.go
   ```
   Cross-reference with `RegisterDefaultChecks` in `common.go`. Any rule whose struct does not appear there is the candidate.

### Handling cases

- **No candidate file** → STOP. Tell the user:

  > Please place your check at `internal/security/checks/<rule>.go` (one file per rule). Pick the interface that matches the data your rule needs — `ShapeCheck`, `CalldataCheck`, `LabelEmittingCheck`, `SimulationCheck`, or `EffectCheck` — and implement its four methods (`Rule`, `AppliesTo`, `Description`, `Run`). See interface-contract.md for the contract. Let me know the filename when you're ready, and I'll continue with registration.

- **Multiple candidate files** → ask the user which file(s) to register. Offer to register them all in one shot if they confirm.

- **Single candidate** → continue to Step 1.

## Step 1 — Validate the interface contract

Read the user's file and verify the struct implements one of the six typed Check interfaces. The full contract is in [interface-contract.md](interface-contract.md). At minimum:

| Check | Why |
|-------|-----|
| Struct type defined (e.g. `MyRule`) | needed to register |
| 4 interface methods: `Rule`, `AppliesTo`, `Description`, `Run` | required by every Check interface |
| The `Run` signature matches exactly one of the six (Context, return-value) shapes documented in interface-contract.md — pick the one whose Context names the data the rule actually reads and whose return values fit what the rule emits | determines the registration slot |
| `Rule()` returns a unique snake_case string not used by any existing check | dynamic check, see below |
| `AppliesTo()` declared explicitly (`nil` for all tx_types, or a slice) | load-bearing, see below |
| `Description()` returns a populated `RuleDescription` (non-empty Name + Description, Statuses map covers every status `Run` can emit) | feeds the response surface — see below |

### Rule name collision — dynamic check

Do NOT hard-code the list of taken names. Run:

```bash
grep -hE 'return "[a-z0-9_]+"' internal/security/checks/*.go | grep -v _test.go | sort -u
```

If the new check's `Rule()` collides with any existing return value, STOP and ask the user to rename. Names appear in JSON responses, in metrics, and in `companion/security.md` cross-validation rules — collisions silently shadow established behaviour.

### AppliesTo — be explicit, not generous

`AppliesTo() []string` filters the check at the registry level before `Run` is called. Semantics:

| Return value | Meaning |
|--------------|---------|
| `nil` | applies to every tx_type — use for foundational rules that should never be filtered |
| `[]string{"swap", "bridge"}` | whitelist; check runs only for those tx_types |
| `[]string{}` | explicitly disabled |

`nil` is tempting but often wrong. If the rule fundamentally only makes sense for some tx_types (e.g. `usd_value_conservation` only fires for swaps because bridges are cross-chain by construction), encode that in `AppliesTo` — keeping the filter at the metadata level instead of an `if req.TxType == ...` prologue inside `Run`. Confirm with the user before accepting either pattern.

### SimulationCheck / LabelEmittingCheck — failed-simulation guard

If the new check implements `SimulationCheck` or `LabelEmittingCheck` AND its `Run()` reads `c.Sim.Data` (trace, balance changes, logs), inspect the body for an explicit `Sim.Code != 0` short-circuit. Grep the file:

```bash
grep -nE 'Sim\.Code|StatusSkipped' internal/security/checks/<rule>.go
```

The orchestrator does NOT gate the Simulation phase on `Sim.Code == 0` — only the Effect phase has that gate. `SimulationContext` is delivered to the rule even when the simulator reverted; `Sim.Data` may carry a half-recorded pre-revert trace whose contents look wrong relative to a successful run. A SimulationCheck that walks the trace looking for completion evidence (deposit reached an address, recipient received an amount, etc.) will mis-report a legitimate transaction as `fail` when the simulator failed for unrelated reasons (out of gas, user balance, etc.). `SimulationSuccess` already surfaces the failure on its own row; double-counting across multiple rules pollutes the verdict.

Expected pattern (after the standard `Sim == nil || Sim.Data == nil` warn guard):

```go
if c.Sim.Code != 0 {
    return security.CheckResult{
        Rule:          "<rule_name>",
        Status:        security.StatusSkipped,
        StatusMeaning: "Rule was not consulted for this transaction.",
        Detail:        "simulation did not succeed; cannot evaluate <what this rule checks>.",
    }
}
```

If the rule does NOT read `Sim.Data` contents — only checks shape-level properties like `len(c.FundFlow)` or a label set independent of trace correctness — the guard is unnecessary; note this to the user and continue. EffectChecks never need this guard (the orchestrator gates the entire Effect phase on `Sim.Code == 0`).

If the rule clearly needs the guard but it is missing, STOP and ask the user to add it — do not auto-edit their check file. Reference `bridge_deposit_match.go` and `transfer_intent_match.go` as in-tree examples of the pattern.

### Description — must be populated

`Description()` is what the LLM agent and end user see in the rendered "All Checks" table. An empty Name or Description means the rule shows up as raw snake_case, which is hostile. The skill enforces:

- `Name` non-empty (human-readable English; the LLM may translate per locale)
- `Description` non-empty (one sentence on what the rule checks and why)
- `Statuses` map covers every status string this rule's `Run()` can emit (at minimum `"pass"`, plus whichever of `"warn"` / `"fail"` the implementation uses)

If any of these are empty or missing, STOP and ask the user to fill them. Do not auto-fill — the rule author understands the rule's intent.

## Step 2 — Plan the registration (dry-run)

Look at the rule's interface (the `Run` signature) and decide which `Register*` slot it lands in:

| `Run` takes | Interface | Register call |
|-------------|-----------|---------------|
| `ShapeContext` (returns `CheckResult`) | `ShapeCheck` | `reg.RegisterShape(...)` |
| `ShapeContext` (returns `(CheckResult, []Operation)`) | `DataProducingCheck` | `reg.RegisterDataProducing(...)` — rare; almost no user check needs this |
| `CalldataContext` | `CalldataCheck` | `reg.RegisterCalldata(...)` |
| `SimulationContext` (returns `(CheckResult, []AddressLabel)`) | `LabelEmittingCheck` | `reg.RegisterLabelEmitting(...)` |
| `SimulationContext` (returns `CheckResult`) | `SimulationCheck` | `reg.RegisterSimulation(...)` |
| `EffectContext` | `EffectCheck` | `reg.RegisterEffect(...)` |

**Show the diff before writing.** Print to the user:

> I'm going to append this line at the end of `RegisterDefaultChecks` in `internal/security/checks/common.go`:
>
> ```go
> reg.RegisterXxx(MyRule{})   // ← new line
> ```
>
> Confirm to proceed?

Wait for confirmation.

## Step 3 — Write the registration line

Edit `internal/security/checks/common.go`. Find `RegisterDefaultChecks` and append the line in the appropriate phase group (Shape, Calldata, LabelEmitting, Simulation, Effect — registration order inside each phase is the response-array order, append at the end of the relevant block). **Remember the exact line text** — needed for rollback in Step 4.

## Step 4 — Verify, with rollback on failure

Run the [checklist](checklist.md):

```bash
go build ./...
go test -count=1 -short ./tests/security/...
grep -E "reg\.Register[A-Za-z]*\(<YourRule>" internal/security/checks/common.go
```

The `grep` is a literal self-check. It catches typos in the constructor name or registrations that landed in the wrong file. Note that `TestSecurity_Registry_AllChecksRegistered` does ALSO assert the registry's rule set (both directions: missing rule fails; unexpected rule fails), so registering a new rule without updating the test's `want` list is a real test failure — see below.

**If build fails:** revert the line you added in Step 3 (use Edit to remove that exact line from `common.go`), then show the error to the user. Do NOT edit the check struct file — surface the error and let the user fix their code, then re-run the skill.

**If tests fail, diagnose before reverting.** Two failure modes are common and have a known fix that does NOT require rolling back the registration:

1. **`TestSecurity_Registry_AllChecksRegistered` — "unexpected rule X in registry"**.
   Add the new `Rule()` string to the `want` slice in `tests/security/registry_test.go` (preserving the registration-order grouping by phase). Re-run tests.

2. **An existing handler-level test (e.g. `TestSecurity_Check_Pass`, `TestSecurity_Check_SkippedRuleAppearsInResponse`) flips from `pass` to `warn` / `fail`**.
   Almost always means the new rule's `AppliesTo` matches a `tx_type` used in the test's fixture, AND the new rule reads input the fixture does not supply (typically `tx_summary.*` fields, or `SimulationContext.FundFlow`). Fix the fixture, not the rule: supply a `tx_summary` matching the simulated effect via `createSecurityIntentWithTxTypeAndSummary`, and add the expected recipient/router to the stub simulator's `fundFlow` if the rule walks it. Do NOT relax the rule to make the test pass.

If a test failure is neither of those, then it points at a real problem with the new check (`Run()` mutating shared state, `AppliesTo` semantically wrong for the rule, etc.). Show the failure to the user, leave the register line in place, let them decide.

**If grep finds nothing:** the registration line did not land where intended (typo, wrong constructor, edited wrong file). Re-open `common.go`, fix the line, and re-run the three checks.

**If all three pass:** continue to Step 5.

## Step 5 — Report

Tell the user:

> ✅ Check `<Rule()>` registered.
>
> - `go build ./...` passed
> - `go test ./tests/security/...` passed
> - grep self-check matched
>
> To make the running server use the new check, rebuild the server binary:
> ```bash
> CGO_ENABLED=1 go build -o ./dist/web3-server ./cmd/server/
> ```
>
> Recommended (strongly): add a regression test in `tests/security/handler_test.go` that constructs a request scenario where your `Run()` should return non-pass, builds a registry with only your check, and asserts the expected status. Security checks are higher-stakes than swap adapters — bad behaviour silently lets transactions through. Tests prevent future refactors from breaking your judgement.

<MUST>
- Never auto-generate check logic from a vague description. If the user has no file, stop and ask for one.
- Never edit the user's check file to make it compile. Surface the error and stop.
- Never invent rule names. `Rule()` must come from the user's file; collisions are a hard stop.
- Always show the diff before writing to `common.go`.
- Always run `go build ./...` AND `go test ./tests/security/...` AND the grep self-check before reporting success.
- On build failure, revert the line you added. Never leave broken code in `common.go`.
- `Description()` Name + Description + Statuses must all be populated by the user. Never fill them yourself.
- `AppliesTo()` must be declared explicitly. If the user's intent on tx_type filtering is unclear, ask before accepting `nil`.
- A regression test is strongly recommended; mention it in Step 5 even when not required for registration.
</MUST>
