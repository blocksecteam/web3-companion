// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

// Package primitives holds the small, self-contained building blocks the
// security checks compose into rules: calldata decoding, integer/address
// parsing, USD amount formatting, etc.
//
// Functions here have no dependency on the parent security package's
// orchestration types (Handler, Registry, CheckResult, Simulator, …). They
// take and return primitive Go types or types from third-party packages
// (math/big, go-ethereum's common). This keeps the dependency direction
// strictly upward: security imports primitives, never the other way around.
//
// Anything that intrinsically depends on a security-package type
// (NativeUSDPerWei wants *SimulationData, label query helpers want the
// AddressLabel struct) stays in the security package and is exported there.
//
// Files are organised by domain rather than by visibility:
//
//	parse.go   - hex / integer / address parsing
//	decode.go  - ABI-level calldata decoding (selectors, approve, setApprovalForAll)
//	format.go  - string/numeric formatting helpers used in CheckResult details
package primitives
