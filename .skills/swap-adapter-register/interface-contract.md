# swap.ProtocolAdapter — Interface Contract

Source: `internal/swap/adapter.go`. Any new adapter must implement all five methods.

## Required methods

```go
type ProtocolAdapter interface {
    Name() string
    SupportedChains() []string
    Capabilities() SwapCapabilities
    Quote(ctx context.Context, req QuoteRequest) (*QuoteResult, error)
    Build(ctx context.Context, req BuildRequest) (*BuildResult, error)
}
```

| Method | What it returns | Notes |
|--------|----------------|-------|
| `Name()` | unique protocol id, lowercase | e.g. `"kyber"`, must not collide with existing adapters |
| `SupportedChains()` | chain names | e.g. `["ethereum", "base"]` |
| `Capabilities()` | unified capability struct | drives wrap/unwrap insertion + slippage advertisement — see below |
| `Quote(ctx, req)` | `*QuoteResult` or error | fetches price + ApproveTarget + RawQuote |
| `Build(ctx, req)` | `*BuildResult` or error | turns RawQuote into a single executable call |

## SwapCapabilities — single unified capability surface

```go
type SwapCapabilities struct {
    Native           NativeCapability // wrap/unwrap dispatch
    SupportsSlippage bool             // does the user's --slippage actually do anything?
}

type NativeCapability struct {
    AcceptsNativeIn bool // can protocol accept native input directly?
    AcceptsWETHIn   bool // can protocol accept WETH input directly?
    OutputsNative   bool // can protocol output native directly?
    OutputsWETH     bool // can protocol output WETH directly?
}
```

### Native — wrap/unwrap dispatch

Most modern aggregators support both forms on both sides → return all `true`. Only set a field to `false` if the protocol genuinely refuses that form.

The handler uses this to decide whether to insert WETH `deposit()`/`withdraw()` calls around the swap.

### SupportsSlippage — exposed through `/swap/protocols`

Set `SupportsSlippage`:

- `true` for AMM-style aggregators where the user's slippage percent maps to an on-chain `amountOutMin` (Uniswap-style routers, PancakeSwap) or to a server-side parameter the upstream API honors (Odos, OpenOcean, Kyber, ParaSwap, LiFi as a swap aggregator). The 7 current adapters all return `true`.
- `false` for **RFQ-style** (1inch Fusion, CowSwap), **limit-order** (1inch limit order, UniswapX same-chain), or **Dutch-auction** style protocols where the user signs a fixed-rate intent and a percent slippage doesn't meaningfully constrain what they receive.

`Capabilities()` is the long-term home for any future user-facing protocol metadata; new fields land on `SwapCapabilities` rather than as new interface methods.

## QuoteResult

```go
type QuoteResult struct {
    Protocol      string      // adapter name
    SourceAsset   string      // passthrough from request
    TargetAsset   string      // passthrough from request
    InputAmount   string      // raw integer string, passthrough
    OutputAmount  string      // raw integer string, expected output before slippage
    RawQuote      interface{} // adapter-defined; handler passes back to Build unchanged
    ApproveTarget string      // see ApproveStyle decision matrix
    ApproveStyle  string      // REQUIRED — must be "erc20" or "permit2"
}
```

## ApproveStyle — required field

| Value | Meaning | ApproveTarget |
|-------|---------|---------------|
| `"erc20"` | Standard ERC-20 approve | see decision matrix below |
| `"permit2"` | Standard Uniswap Permit2 (approve Permit2 + Permit2.approve) | Permit2 contract address |
| `""` (empty) | **Forbidden.** The register skill refuses to register an adapter without an explicit ApproveStyle. |

Non-standard Permit2 ABIs are not supported. If your protocol uses a custom Permit2 contract, wrap it as a regular ERC-20 approve from the adapter's perspective.

### ApproveTarget — choosing for `"erc20"`

Two valid patterns, depending on whether your protocol's quote response tells you the spender:

| Your quote API tells you the spender? | What to do | Examples in this repo |
|---------------------------------------|------------|----------------------|
| **No** — quote response only gives the router. Router IS the spender. | Leave `ApproveTarget: ""`. Handler falls back to `BuildResult.To`. | Odos, OpenOcean |
| **Yes** — quote response includes an explicit `tokenTransferProxy` / `routerAddress` field that may differ from `BuildResult.To`. | Set `ApproveTarget` to that explicit address. | ParaSwap (`TokenTransferProxy`), Kyber (`RouterAddress`) |

Rule: **if `ApproveTarget` is empty, the handler approves to `BuildResult.To`.** Pick whichever pattern matches your protocol's API surface. When in doubt, leave it empty — fewer moving parts.

## BuildResult

```go
type BuildResult struct {
    Protocol string // adapter name
    To       string // router/spender — single call target
    Value    string // hex ETH value to attach
    Data     string // hex calldata, "0x..."-prefixed
    GasLimit uint64 // 300_000 if unknown
}
```

ERC-7821 batching, approve calls, and WETH wrap/unwrap calls are added by the handler. The adapter always returns exactly one call.

`Value` semantics:

- `"0x" + hex(amountIn)` when the protocol receives native ETH (either `AcceptsNativeIn=true` and user provided native, OR `AcceptsWETHIn=false` and handler unwrapped WETH → native).
- `"0x0"` for plain ERC-20 input or when handler wrapped native → WETH.

## Constructor patterns

The skill registers via `reg.Register(NewXxxAdapter(...))` in `common.go`. Pick the pattern that matches your constructor:

| Constructor takes | Signature | Registration |
|-------------------|-----------|--------------|
| nothing | `NewMyAdapter() *MyAdapter` | `reg.Register(NewMyAdapter())` |
| decimals resolver | `NewMyAdapter(resolver *swap.DecimalsResolver) *MyAdapter` | `reg.Register(NewMyAdapter(resolver))` |
| RPC provider | `NewMyAdapterWithRPCProvider(provider func() map[string]string) *MyAdapter` | `reg.Register(NewMyAdapterWithRPCProvider(provider))` |

Constructor needs anything else (API key, custom client) → STOP and ask the user how to source it. Do not silently invent config plumbing.

## Reference implementations

- API-aggregator pattern: `odos.go`, `kyber.go`, `openocean.go`, `paraswap.go`
- On-chain routing pattern: `uniswap.go`, `pancake.go`
- Template: `example_adapter.go.template`
