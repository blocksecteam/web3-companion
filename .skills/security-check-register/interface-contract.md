# security.*Check — Interface Contract

Source: `internal/security/check.go`. A new check must implement exactly one of the six interfaces below; the interface chosen also declares the phase the check runs in.

## The six interfaces

```go
type ShapeCheck interface {
    Rule() string
    AppliesTo() []string
    Description() RuleDescription
    Run(ShapeContext) CheckResult
}

type DataProducingCheck interface {
    Rule() string
    AppliesTo() []string
    Description() RuleDescription
    Run(ShapeContext) (CheckResult, []Operation)
}

type CalldataCheck interface {
    Rule() string
    AppliesTo() []string
    Description() RuleDescription
    Run(CalldataContext) CheckResult
}

type LabelEmittingCheck interface {
    Rule() string
    AppliesTo() []string
    Description() RuleDescription
    Run(SimulationContext) (CheckResult, []AddressLabel)
}

type SimulationCheck interface {
    Rule() string
    AppliesTo() []string
    Description() RuleDescription
    Run(SimulationContext) CheckResult
}

type EffectCheck interface {
    Rule() string
    AppliesTo() []string
    Description() RuleDescription
    Run(EffectContext) CheckResult
}
```

Pick whichever Context type names the inputs your judgement needs; you cannot read fields outside that Context's fields.

## Phase order

The orchestrator runs phases in fixed order: Shape → DataProducing → Calldata → LabelEmitting → Simulation → Effect. The Effect phase is gated — the orchestrator skips it entirely when `Sim.Code != 0` or simulation data is missing.

DataProducing is a tiny sub-phase between Shape and Calldata: it runs on ShapeContext and emits the `[]Operation` slice that gets threaded into `CalldataContext.Ops`. The canonical implementation is `CalldataDecode` (built-in), which parses ERC-7821 batches and direct calls. Authors writing new rules will almost never need to add another DataProducingCheck — the interface is documented for completeness, not as the suggested extension point. If you do add one, only the first non-nil `[]Operation` from the registered set survives into CalldataContext.

LabelEmitting is a sub-phase between simulate and SimulationChecks: its emitted labels are attached to `Sim.Data.Trace` before SimulationChecks read it.

## Context upper-bound chain (compile-time safety)

Each phase's Context embeds the previous phase's, so a Run() method gets exactly the fields that its phase guarantees and nothing extra. Reading a field that is not in your Context is a compile error.

```
ShapeContext      { Ctx, Req, Cfg }
CalldataContext   ⊃ ShapeContext + { Ops }
SimulationContext ⊃ CalldataContext + { Sim, FundFlow, Labels }   // Sim non-nil; Sim.Code may be non-zero
EffectContext     ⊃ SimulationContext + { Effect }                // Effect non-nil
```

Notes:
- `Sim` is non-nil inside SimulationContext, but `Sim.Code` may be non-zero. SimulationCheck implementations must inspect `Sim.Code` before reading `Sim.Data`. `simulation_success` is the gate-keeper rule — anything more nuanced should depend on it implicitly via the verdict aggregation, not by repeating its job.
- `Labels` is populated by LabelEmittingChecks before regular SimulationChecks run. The orchestrator attaches them to `Sim.Data.Trace` between the two sub-phases, so SimulationChecks that read the trace see annotated calls.
- `Effect` in EffectContext is dereferenceable; the orchestrator only enters this phase when `ExtractTxEffect` succeeded.

## RuleDescription

```go
type RuleDescription struct {
    Name        string            // Human-readable label (English; LLM may translate per locale)
    Description string            // One sentence: what this rule checks and why
    Statuses    map[string]string // status → human explanation of what that status means for this rule
}
```

| Field | Rule |
|-------|------|
| `Name` | Non-empty. Shown in the agent's "All Checks" table. Plain English; avoid the snake_case rule id. |
| `Description` | Non-empty. One sentence on what the rule looks at and what makes the judgement meaningful. |
| `Statuses` | Must cover every status value `Run()` can emit. At minimum `"pass"` plus whichever of `"warn"` / `"fail"` the implementation uses. Each value is a short user-facing sentence — not "WARN: see detail", but a real explanation of what that status means for this rule. |

The orchestrator copies Name + Description verbatim onto each CheckResult, and resolves `Statuses[result.Status]` into `result.StatusMeaning` so the agent does not need to look up a separate table.

## CheckResult

```go
type CheckResult struct {
    Rule          string      // snake_case rule id, comes from Rule()
    Name          string      // populated by orchestrator from Description().Name
    Description   string      // populated by orchestrator from Description().Description
    Status        string      // "pass" | "warn" | "fail" — what your Run() decided
    StatusMeaning string      // populated by orchestrator from Description().Statuses[Status]
    Detail        string      // dynamic explanation specific to this invocation
    Observed      interface{} // optional structured payload for debugging
}
```

Your `Run()` populates `Rule`, `Status`, `Detail`, and optionally `Observed`. The orchestrator fills `Name`, `Description`, and `StatusMeaning` from your `Description()`. Setting them yourself is harmless but redundant — they would be overwritten.

Detail is dynamic: "Spender 0x… is not in the known-router whitelist for chain 1". StatusMeaning is static: "warn means an approve looks suspicious; sign only if you trust the spender." Both flow to the user.

## AppliesTo semantics

```go
func (MyRule) AppliesTo() []string {
    return nil               // applies to every tx_type
    // return []string{"swap"}   // whitelist
    // return []string{}         // explicitly disabled
}
```

The orchestrator filters by tx_type before calling `Run`. Encode tx_type-specific behaviour here, not inside `Run()`. Runtime conditions finer than tx_type (cross_chain flag, specific chain, amount threshold) still live in `Run()`.

## Examples

- Single-line shape check: `internal/security/checks/transaction_shape.go`
- Data-producing check (calldata decoder): `internal/security/checks/calldata_decode.go`
- Calldata check with bundled sub-rules: `internal/security/checks/erc20_approve_sanity.go`
- Label-emitting check (returns extra labels): `internal/security/checks/address_label_risk.go`
- Effect check with tx_type whitelist: `internal/security/checks/usd_value_conservation.go`

## Helpers available

Implementations are encouraged to reuse these existing helpers rather than re-implementing parsing or formatting:

- `internal/security/primitives/` — `ParseAddress`, `ParseInteger`, `ParseHexBytes`, `DecodeApprove`, `DecodeSetApprovalForAll`, `MethodID`, `MaxUint256`, `SelectorApprove`, `FirstString`, `AddrEq`, `DecimalRat`, `FormatRat`, `CommonAddress`
- `internal/security/` (exported) — `NativeUSDPerWei`, `CollectLabelAddresses`, `HighRiskLabelReasons`, `CompactLabels`
- `internal/security/checks/common.go` (check-private) — `knownRouter`, `hasActionCall`, `copyStringMap`

The split is principled: anything in `primitives` has no dependency on security package types. Anything exported from the security package itself depends on `*SimulationData` / `AddressLabel`. Anything in `checks/common.go` is consumed only by check structs in the same package.
