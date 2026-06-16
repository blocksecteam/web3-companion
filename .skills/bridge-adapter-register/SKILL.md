---
name: bridge-adapter-register
description: "Use when: register bridge adapter, add bridge protocol, 注册bridge, 新增bridge协议, wire up new bridge, plug in new cross-chain adapter, install bridge adapter. NOT for bridge execution flow (use web3-bridge). NOT for swap adapters."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Bridge Adapter Register — Wire a new bridge into the bridge registry

This skill registers a user-authored bridge adapter `.go` file into the project, then verifies it builds and existing tests still pass.

It does NOT design the adapter logic. The user must already have a `.go` file that implements `bridge.BridgeAdapter`. If they don't, this skill tells them where to put it and exits.

## Step 0 — Locate the new adapter file

All bridge adapters live in `internal/bridge/adapters/`. To find the user's new file, do NOT rely on a hard-coded list (it goes stale). Use one of these in order:

1. **Preferred — git status:**
   ```bash
   git status --porcelain internal/bridge/adapters/ | grep -E '^\?\?|^ M|^A ' | awk '{print $2}' | grep '\.go$'
   ```
   Any untracked or newly-added `.go` file under `internal/bridge/adapters/` is a candidate.

2. **Fallback — scan for unregistered constructors:**
   ```bash
   grep -E '^func New[A-Z][a-zA-Z]+Bridge' internal/bridge/adapters/*.go | grep -v _test.go
   ```
   Cross-reference with `RegisterAll` in `common.go`. Any `NewXxxBridge` not appearing there is the candidate.

### Handling cases

- **No candidate file** → STOP. Tell the user:

  > Please place your adapter file at `internal/bridge/adapters/<bridge>.go`. You can copy `example_bridge.go.template` as a starting point. Let me know the filename when you're ready, and I'll continue with registration.

- **Multiple candidate files** → ask the user which file(s) to register. Offer to register them all in one shot if they confirm.

- **Single candidate** → continue to Step 1.

## Step 1 — Validate the interface contract

Read the user's file and verify it implements `bridge.BridgeAdapter`. The full contract is in [interface-contract.md](interface-contract.md). At minimum:

| Check | Why |
|-------|-----|
| Struct type defined (e.g. `MyBridge`) | needed to register |
| Constructor `NewMyBridge(...)` | needed to register |
| 5 interface methods: `Name`, `SupportedPairs`, `Capabilities`, `Quote`, `Build` | required by `bridge.BridgeAdapter` |
| `Name()` does not collide with existing adapters | dynamic check, see below |
| `Capabilities().Approve` returns a valid `(Mode, Style)` combination | load-bearing, see below |
| `Capabilities().SupportsSlippage` set explicitly | declare based on actual behaviour, **do NOT derive from Approve.Mode** — see interface-contract.md |
| `Quote()` populates the per-mode quote-time field (`ApproveTarget` or `DepositAddress`) | needed by handler — approve/deposit dispatch |
| `Capabilities().Native` bools are sensible | confirm with user, see below |

### Name collision — dynamic check

Do NOT hard-code the list of taken names. Run:

```bash
grep -hE 'return "[a-z_]+"' internal/bridge/adapters/*.go | grep -v _test.go
```

Or more precisely, parse each file's `Name()` method. If the new adapter's `Name()` collides with any existing return value, STOP and ask the user to rename.

### Approve capability — must be valid and explicit

`Capabilities().Approve` is load-bearing: it drives whether the handler computes an approve sequence at all (`approve` mode) or surfaces a deposit address to the security agent (`deposit` mode). Read the `Capabilities()` body and extract `Approve.Mode` and `Approve.Style`. Valid combinations:

| `Mode`     | `Style`   | Quote-time field required on `QuoteResult` |
|------------|-----------|--------------------------------------------|
| `"approve"`| `"erc20"` | `ApproveTarget` (spender / router)         |
| `"approve"`| `"permit2"` | `ApproveTarget` (the Permit2 address)    |
| `"deposit"`| `""`      | `DepositAddress` (per-quote deposit addr)  |

Anything else (empty `Mode`, `Mode="approve"` with empty/unknown `Style`, `Mode="deposit"` with a non-empty `Style`) is invalid — STOP and tell the user to fix the declaration.

Then grep the `Quote()` body for the matching quote-time field:

```bash
grep -n 'ApproveTarget\|DepositAddress' <adapter_file>
```

- If `Mode="approve"` and no `ApproveTarget:` assignment is found in the `Quote()` return value, STOP and ask the user to set it.
- If `Mode="deposit"` and no `DepositAddress:` assignment is found in the `Quote()` return value, STOP and ask the user to set it.

Do not auto-fix either. The choice has security implications (the security agent uses these fields to verify the simulated transfer target).

### Native capability — confirm with user, flag output mismatches

Read the `Capabilities().Native` literal and extract the 4 bools. Print them back to the user before continuing:

> From your `Capabilities().Native` I read:
> - AcceptsNativeIn: `true/false`
> - AcceptsWETHIn:   `true/false`
> - OutputsNative:   `true/false`
> - OutputsWETH:     `true/false`
>
> When the input side (`Accepts*In`) doesn't match the user's token form, the handler inserts a WETH `deposit()` / `withdraw()` call before the bridge call to adjust the input form. **When the output side (`Outputs*`) doesn't match, the handler does NOT post-process — the quote is rejected at request time** so the user can pick a different bridge. Is this correct?

Wait for confirmation. If they correct it, ask them to update the source and re-run.

## Step 2 — Plan the registration (dry-run)

Decide which constructor pattern applies (see [interface-contract.md](interface-contract.md) for the patterns):

- No deps → `reg.Register(NewMyBridge())`
- Decimals resolver → `reg.Register(NewMyBridge(resolver))` (the `resolver` parameter is the one passed into `RegisterAll`)
- Anything else (API key, custom HTTP client, RPC provider) → STOP and ask the user how to source it. Do not invent config plumbing.

**Show the diff before writing.** Print to the user:

> I'm going to append this line at the end of `RegisterAll` in `internal/bridge/adapters/common.go`:
>
> ```go
> reg.Register(NewMyBridge())   // ← new line
> ```
>
> Confirm to proceed?

Wait for confirmation.

## Step 3 — Write the registration line

Edit `internal/bridge/adapters/common.go`. Find `RegisterAll` and append the line. **Remember the exact line text** — needed for rollback in Step 4.

## Step 4 — Verify, with rollback on failure

Run the [checklist](checklist.md):

```bash
go build ./...
go test -count=1 -short ./tests/bridge/...
grep -E "reg\.Register\(New<YourBridge>" internal/bridge/adapters/common.go
```

The `grep` is a literal self-check: the bridge tests use `NewHandlerWithOptions(opts, adapters...)` and inject adapters explicitly, so they will pass even if `RegisterAll` never picks up the new line. The grep catches typos like `reg.Regsiter(...)` or registering a different constructor.

**If build fails:** revert the line you added in Step 3 (use Edit to remove that exact line from `common.go`), then show the error to the user. Do NOT edit the adapter file — surface the error and let the user fix their code, then re-run the skill.

**If tests fail:** do NOT auto-revert (the register line is logically correct; the failure means the new adapter has unintended side effects). Show failing tests, leave the register line in place, let the user decide.

**If grep finds nothing:** the registration line did not land as intended (typo, wrong location, edited the wrong file). Re-open `common.go`, fix the line, and re-run the three checks.

**If all three pass:** continue to Step 5.

## Step 5 — Report

Tell the user:

> ✅ Bridge `<Name()>` registered.
>
> - `go build ./...` passed
> - `go test ./tests/bridge/...` passed
>
> To make the CLI see the new bridge, rebuild the binary:
> ```bash
> go build -o ./bin/web3 ./cmd/cli/
> ```
>
> Verify: `./bin/web3 bridge protocols` should list `<Name()>`.
>
> Test a quote (force the bridge so it doesn't pollute best-route selection):
> ```bash
> ./bin/web3 bridge quote --bridge <Name()> \
>   --from-chain <chain> --to-chain <chain> \
>   --from-address <wallet> --from <src> --to <tgt> --amount <n>
> ```
>
> Recommended: add `<bridge>_test.go` under `tests/bridge/`, modeled after the existing `bungee_test.go`. Not required for registration, but protects your adapter from future refactors.

<MUST>
- Never auto-generate adapter logic from a vague description. If the user has no file, stop and ask for one.
- Never edit the user's adapter file to make it compile. Surface the error and stop.
- Never invent config plumbing (API keys, env vars, RPC URLs). Ask the user.
- Always show the diff before writing to `common.go`.
- Always run `go build ./...` AND `go test ./tests/bridge/...` before reporting success.
- On build failure, revert the line you added. Never leave broken code in `common.go`.
- `Capabilities().Approve` (Mode + Style) must be a valid combination. Never auto-fill it.
- The per-mode quote-time field (ApproveTarget for approve, DepositAddress for deposit) must be populated by Quote(). Never auto-fix.
- Print `Capabilities().Native` values back to the user, and explicitly flag that output-side mismatches reject the quote at request time (no handler post-processing).
</MUST>
