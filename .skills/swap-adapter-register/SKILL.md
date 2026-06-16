---
name: swap-adapter-register
description: "Use when: register swap adapter, add swap protocol, 注册swap, 新增swap协议, wire up new DEX, plug in new aggregator, install swap adapter. NOT for swap execution flow (use web3-swap). NOT for bridge adapters."
license: MIT
metadata:
  author: blocksecteam
  version: "1.0.0"
---

# Swap Adapter Register — Wire a new protocol into the swap registry

This skill registers a user-authored swap adapter `.go` file into the project, then verifies it builds and existing tests still pass.

It does NOT design the adapter logic. The user must already have a `.go` file that implements `swap.ProtocolAdapter`. If they don't, this skill tells them where to put it and exits.

## Step 0 — Locate the new adapter file

All swap adapters live in `internal/swap/adapters/`. To find the user's new file, do NOT rely on a hard-coded list (it goes stale). Use one of these in order:

1. **Preferred — git status:**
   ```bash
   git status --porcelain internal/swap/adapters/ | grep -E '^\?\?|^ M|^A ' | awk '{print $2}' | grep '\.go$'
   ```
   Any untracked or newly-added `.go` file under `internal/swap/adapters/` is a candidate.

2. **Fallback — scan for unregistered constructors:**
   ```bash
   grep -E '^func New[A-Z][a-zA-Z]+Adapter' internal/swap/adapters/*.go | grep -v _test.go
   ```
   Cross-reference with `RegisterAllWithRPCProvider` in `common.go`. Any `NewXxxAdapter` not appearing there is the candidate.

### Handling cases

- **No candidate file** → STOP. Tell the user:

  > Please place your adapter file at `internal/swap/adapters/<protocol>.go`. You can copy `example_adapter.go.template` as a starting point. Let me know the filename when you're ready, and I'll continue with registration.

- **Multiple candidate files** → ask the user which file(s) to register. Offer to register them all in one shot if they confirm.

- **Single candidate** → continue to Step 1.

## Step 1 — Validate the interface contract

Read the user's file and verify it implements `swap.ProtocolAdapter`. The full contract is in [interface-contract.md](interface-contract.md). At minimum:

| Check | Why |
|-------|-----|
| Struct type defined (e.g. `MyProtocolAdapter`) | needed to register |
| Constructor `NewMyProtocolAdapter(...)` | needed to register |
| 5 interface methods: `Name`, `SupportedChains`, `Capabilities`, `Quote`, `Build` | required by `swap.ProtocolAdapter` |
| `Name()` does not collide with existing adapters | dynamic check, see below |
| `Quote()` body explicitly sets `ApproveStyle` | load-bearing field, see below |
| `Capabilities().Native` bools are sensible | confirm with user, see below |
| `Capabilities().SupportsSlippage` set explicitly | true for AMM-style, false for RFQ/limit-order/Dutch-auction — see interface-contract.md |

### Name collision — dynamic check

Do NOT hard-code the list of taken names. Run:

```bash
grep -hE 'return "[a-z_]+"' internal/swap/adapters/*.go | grep -v _test.go
```

Or more precisely, parse each file's `Name()` method. If the new adapter's `Name()` collides with any existing return value, STOP and ask the user to rename.

### ApproveStyle — must be explicit

`ApproveStyle` is load-bearing in this project — the handler routes approve-call construction based on it. An empty/unknown value silently defaults to `"erc20"` (see `GetApproveCalls` in `internal/swap/handler.go`), which can mask a permit2-only protocol being misconfigured as a plain ERC-20 approve. Enforce explicit declaration:

```bash
grep -n 'ApproveStyle' <adapter_file>
```

If grep finds no `ApproveStyle:` assignment inside the `Quote()` return value, STOP and tell the user:

> Your adapter does not explicitly set `ApproveStyle`. It must be `"erc20"` or `"permit2"`. Please add it to the QuoteResult returned from `Quote()`, then let me continue with registration. See interface-contract.md to decide which value applies.

Do not auto-fix. The choice has security implications and the user must decide.

### Native capability — confirm with user

Read the `Capabilities().Native` literal and extract the 4 bools. Print them back to the user before continuing:

> From your `Capabilities().Native` I read:
> - AcceptsNativeIn: `true/false`
> - AcceptsWETHIn:   `true/false`
> - OutputsNative:   `true/false`
> - OutputsWETH:     `true/false`
>
> Every adapter currently in the tree returns all-true (the protocol can handle both native and WETH on either side). If your protocol has restrictions, the handler will automatically insert WETH `deposit()` / `withdraw()` calls around the swap. Is this correct?

Wait for confirmation before continuing. If they correct it, ask them to update the source and re-run.

## Step 2 — Plan the registration (dry-run)

Decide which constructor pattern applies (see [interface-contract.md](interface-contract.md) for the 3 patterns):

- No deps → `reg.Register(NewMyProtocolAdapter())`
- Decimals resolver → `reg.Register(NewMyProtocolAdapter(resolver))`
- RPC provider → `reg.Register(NewMyProtocolAdapterWithRPCProvider(provider))`
- Anything else (API key, custom client) → STOP and ask the user how to source it. Do not invent config plumbing.

**Show the diff before writing.** Print to the user:

> I'm going to append this line at the end of `RegisterAllWithRPCProvider` in `internal/swap/adapters/common.go`:
>
> ```go
> reg.Register(NewMyProtocolAdapter())   // ← new line
> ```
>
> Confirm to proceed?

Wait for confirmation.

## Step 3 — Write the registration line

Edit `internal/swap/adapters/common.go`. Find `RegisterAllWithRPCProvider` and append the line. **Remember the exact line text** — needed for rollback in Step 4.

## Step 4 — Verify, with rollback on failure

Run the [checklist](checklist.md):

```bash
go build ./...
go test -count=1 -short ./tests/swap/...
grep -E "reg\.Register\(New<YourProtocol>" internal/swap/adapters/common.go
```

The `grep` is a literal self-check. `TestSwap_Registry_AllAdaptersRegistered` asserts a fixed list of expected adapter names — adding a new adapter without updating that list is a real test failure (which the register skill expects you to fix by extending `want` in the test). But the test does not catch a typo in the registration line itself (e.g. `reg.Regsiter(...)` or registering a different constructor): those would compile and the new adapter simply would not appear in the registry. The grep self-check covers that gap.

**If build fails:** revert the line you added in Step 3 (use Edit to remove that exact line from `common.go`), then show the error to the user. Do NOT edit the adapter file — surface the error and let the user fix their code, then re-run the skill.

**If tests fail:** do NOT auto-revert (the register line is logically correct; the failure means the new adapter has unintended side effects). Show failing tests, leave the register line in place, let the user decide.

**If grep finds nothing:** the registration line did not land as intended (typo, wrong location, edited the wrong file). Re-open `common.go`, fix the line, and re-run the three checks.

**If all three pass:** continue to Step 5.

## Step 5 — Report

Tell the user:

> ✅ Adapter `<Name()>` registered.
>
> - `go build ./...` passed
> - `go test ./tests/swap/...` passed
>
> To make the CLI see the new protocol, rebuild the binary:
> ```bash
> go build -o ./bin/web3 ./cmd/cli/
> ```
>
> Verify: `./bin/web3 swap protocols` should list `<Name()>`.
>
> Test a quote (force the protocol so it doesn't pollute best-route selection):
> ```bash
> ./bin/web3 swap quote --chain <chain> --protocol <Name()> \
>   --from-address <wallet> --from <src> --to <tgt> --amount <n>
> ```
>
> Recommended: add `Test<Protocol>Quote_*` / `Test<Protocol>Build_*` functions at the end of `tests/swap/adapters/protocols_test.go`, modeled after the existing `TestOdosQuote_*` / `TestKyberBuild_*`. Not required for registration, but protects your adapter from future refactors.

<MUST>
- Never auto-generate adapter logic from a vague description. If the user has no file, stop and ask for one.
- Never edit the user's adapter file to make it compile. Surface the error and stop.
- Never invent config plumbing (API keys, env vars). Ask the user.
- Always show the diff before writing to `common.go`.
- Always run `go build ./...` AND `go test ./tests/swap/...` before reporting success.
- On build failure, revert the line you added. Never leave broken code in `common.go`.
- ApproveStyle must be explicit. Never auto-fill it.
- Print `Capabilities().Native` values back to the user before continuing — silent acceptance can lead to broken wrap/unwrap behavior.
</MUST>
