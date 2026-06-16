# bridge.Adapter — Interface Contract

Source: `internal/bridge/adapter.go`. Any new adapter must implement all five methods.

## Required methods

```go
type Adapter interface {
    Name() string
    SupportedPairs() []ChainPair
    Capabilities() BridgeCapabilities
    Quote(ctx context.Context, req QuoteRequest) (*QuoteResult, error)
    Build(ctx context.Context, req BuildRequest) (*BuildResult, error)
}
```

| Method | What it returns | Notes |
|--------|----------------|-------|
| `Name()` | unique bridge id, lowercase | e.g. `"bungee"`, must not collide with existing adapters |
| `SupportedPairs()` | cross-chain pairs | use `bridgePairs(fromChains, toChains)` helper to expand a full matrix |
| `Capabilities()` | unified capability struct | drives approve/deposit dispatch + wrap/unwrap insertion + slippage advertisement — see below |
| `Quote(ctx, req)` | `*QuoteResult` or error | fetches a quote, populates the per-mode quote-time field |
| `Build(ctx, req)` | `*BuildResult` or error | turns the RawQuote map back into a single executable call |

## BridgeCapabilities — single unified capability surface

```go
type BridgeCapabilities struct {
    Native           NativeCapability  // wrap/unwrap dispatch + destination-side rejection
    Approve          ApproveCapability // how the bridge collects funds (Mode + Style)
    SupportsSlippage bool              // does the user's --slippage actually do anything?
}

type ApproveCapability struct {
    Mode  string // "approve" | "deposit"
    Style string // "erc20" | "permit2" (only when Mode="approve")
}
```

Valid `Approve.Mode` + `Approve.Style` combinations (declared as constants in `internal/bridge/adapter.go`):

| `Mode`                    | `Style`                | Use case |
|---------------------------|------------------------|----------|
| `ApproveModeApprove`      | `ApproveStyleERC20`    | bridge pulls tokens via `token.approve(router, amount)` then router call |
| `ApproveModeApprove`      | `ApproveStylePermit2`  | bridge pulls tokens via Permit2 |
| `ApproveModeDeposit`      | (empty)                | user sends tokens directly to a per-quote deposit address; no approve |

Anything else is invalid.

**Handler behavior:**

- `Approve.Mode="approve"` → handler calls `bridge.ComputeApproveSequence` using `QuoteResult.ApproveTarget` as spender and prepends approve calls to the ERC-7821 batch. Adapter must never touch RPC for allowance.
- `Approve.Mode="deposit"` → handler skips the approve sequence entirely and surfaces `QuoteResult.DepositAddress` to the security agent so it can verify the simulated transfer target.

### SupportsSlippage — declare explicitly, do NOT derive from Approve.Mode

`SupportsSlippage` tells `GET /bridge/protocols` (and downstream skills) whether the user-passed slippage parameter actually does anything for this bridge. **Do not derive it from `Approve.Mode`** — while it's correlated with mode in current adapters, it is NOT logically equivalent. Counter-examples:

| Bridge type                                  | `Mode`     | `SupportsSlippage` |
|----------------------------------------------|------------|--------------------|
| Bungee / Relay / Squid (deposit-address)     | `deposit`  | `false`            |
| LiFi / 1inch Fusion Cross-chain (aggregator) | `approve`  | `true`             |
| **Canonical L2 bridges** (Optimism, Arbitrum, Polygon PoS) | `approve`  | **`false`** — 1:1 lock-and-mint |
| **Across, UniswapX cross-chain** (intent)    | `approve`  | **`false`** — off-chain spread |

Set `SupportsSlippage: true` only when the user's slippage percent actually constrains what they receive — either via an on-chain `amountOutMin` enforced by the router, or a parameter the upstream API honors server-side.

## NativeCapability — drives wrap/unwrap AND quote rejection

```go
type NativeCapability struct {
    AcceptsNativeIn bool // source-chain: bridge can pull native input directly?
    AcceptsWETHIn   bool // source-chain: bridge can pull WETH input directly?
    OutputsNative   bool // destination-chain: bridge can deliver native directly?
    OutputsWETH     bool // destination-chain: bridge can deliver WETH directly?
}
```

**Input side (`Accepts*In`)** — when the user's form doesn't match, the handler prepends a WETH `deposit()` or `withdraw()` call before the bridge call and adjusts approve/value accordingly.

**Output side (`Outputs*`)** — consulted at quote time. If the user's requested output form is not supported, the quote is REJECTED. There is no post-processing on the destination chain.

Most modern bridges support both forms on both sides → return all `true`. Only set a field to `false` if the protocol genuinely refuses that form.

## QuoteRequest

```go
type QuoteRequest struct {
    Bridge         string  // adapter name (filtering hint, usually unused by adapter)
    FromChain      string  // e.g. "ethereum"
    ToChain        string  // e.g. "base"
    FromToken      string  // input token address; "native" or 0xEeee... for native
    ToToken        string  // output token address (may be empty; resolveBridgeToToken fills it)
    Amount         string  // raw integer string (USDC 1.0 → "1000000")
    FromAddress    string  // user wallet address (bridges always send back to this address on the destination chain — no third-party recipient)
    Slippage       float64 // e.g. 0.5 means 0.5%
}
```

## QuoteResult

```go
type QuoteResult struct {
    Bridge           string      // adapter name (must match Name())
    InputAmount      string      // raw integer string, pass-through from request
    EstimatedReceive string      // raw integer string, expected output before slippage
    ApproveTarget    string      // REQUIRED when ApproveCapability.Mode == "approve"
    DepositAddress   string      // REQUIRED when ApproveCapability.Mode == "deposit"
    RawQuote         interface{} // flat map[string]interface{}; handler passes to Build unchanged
}
```

Per-mode field requirements:

| Mode      | `ApproveTarget`       | `DepositAddress`     |
|-----------|-----------------------|----------------------|
| `approve` | spender / router      | leave empty          |
| `deposit` | leave empty           | per-quote deposit addr |

Both are quote-time, per-quote metadata that the security agent reads. Only one is non-empty per mode.

## RawQuote — flat map design

`RawQuote` MUST be `map[string]interface{}` with only the fields `Build()` actually needs. No nested structures, no redundant fields.

Why flat: the value is JSON-round-trippable, so the quote can be cached, logged, or shipped to another process and still rebuilt later. A typed private struct forces a type assertion that breaks the moment Quote crosses a serialization boundary.

`Build()` reads top-level keys via `stringFromMap(raw, "to")`, etc. Use `rawQuoteMap()` to coerce `req.Quote` back to a map at the top of `Build()`.

## BuildRequest

```go
type BuildRequest struct {
    Quote interface{} // equals QuoteResult.RawQuote
}
```

The request does not carry the bridge name. If the adapter needs it inside `Build()`, call `a.Name()`.

## BuildResult

```go
type BuildResult struct {
    To       string // router/spender — single call target
    Value    string // decimal-string ETH value to attach (see below)
    Data     string // hex calldata, "0x..."-prefixed
    GasLimit uint64 // gas limit; use defaultBridgeGas (300000) if unknown
}
```

The handler prepends wrap + approve calls before wrapping into the ERC-7821 batch. The adapter always returns exactly one call.

`Value` semantics depend on the bridge API shape:

- **Bridge API returns a ready-to-send tx** (Bungee, Relay style) — pass through the API's `value` field via `decimalFromHexOrString`. The handler may override the value when it inserts a WETH wrap/unwrap step.
- **Adapter constructs calldata itself against a router contract** — set `Value = Amount` when `FromToken` is native (plus any native protocol fee the bridge charges), else `"0"`.

## Constructor patterns

The skill registers via `reg.Register(NewXxxBridge(...))` in `common.go`'s `RegisterAll`. Pick the pattern that matches your constructor:

| Constructor takes | Signature | Registration |
|-------------------|-----------|--------------|
| nothing | `NewMyBridge() bridge.Adapter` | `reg.Register(NewMyBridge())` |
| decimals resolver | `NewMyBridge(resolver bridge.Resolver) bridge.Adapter` | `reg.Register(NewMyBridge(resolver))` |

`resolver` is `bridge.Resolver` (`Resolve(ctx, chain, asset) (uint8, error)`). It is the same value already passed into `RegisterAll(reg, resolver)` — no new plumbing needed.

Constructor needs anything else (API key, custom client, RPC provider) → STOP and ask the user how to source it. Do not silently invent config plumbing.

## Reference implementations

- Deposit-mode (no approve, no resolver): `bungee.go`
- Deposit-mode + decimals resolver: `relay.go`
- Approve-mode (ERC-20 style): `lifi.go` (Mode=`approve`, Style=`erc20`). The `example_bridge.go.template` is also kept as a copy-and-rename starting point.

