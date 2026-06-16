// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package erc7821

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type GasPolicy func(providerGas uint64, callCount int) uint64

type Bundle struct {
	To       string
	Data     string
	Value    *big.Int
	GasLimit uint64
}

// ValueHex formats the bundle's total native value for transaction intent
// callers that store EVM JSON-RPC-style hex quantities.
func (b Bundle) ValueHex() string {
	if b.Value == nil || b.Value.Sign() == 0 {
		return "0x0"
	}
	return fmt.Sprintf("0x%x", b.Value)
}

// ValueDecimal formats the bundle's total native value for existing HTTP
// surfaces that expose decimal wei strings.
func (b Bundle) ValueDecimal() string {
	if b.Value == nil {
		return "0"
	}
	return b.Value.String()
}

// BuildExecuteBundle wraps ordered calls into a single ERC-7821 execute
// transaction targeting walletAddress. It sums call values, encodes the
// execute calldata, and delegates gas-limit selection to gasPolicy.
func BuildExecuteBundle(walletAddress string, calls []HexCall, providerGas uint64, gasPolicy GasPolicy) (Bundle, error) {
	if !common.IsHexAddress(walletAddress) {
		return Bundle{}, fmt.Errorf("invalid managed wallet address: %s", walletAddress)
	}
	if len(calls) == 0 {
		return Bundle{}, fmt.Errorf("no calls to execute")
	}
	total := new(big.Int)
	normalized := make([]HexCall, len(calls))
	for i, call := range calls {
		value := call.Value
		if value == nil {
			value = big.NewInt(0)
		}
		total.Add(total, value)
		normalized[i] = HexCall{
			To:    call.To,
			Value: new(big.Int).Set(value),
			Data:  call.Data,
		}
	}

	data, err := EncodeExecuteHex(normalized)
	if err != nil {
		return Bundle{}, err
	}

	gasLimit := providerGas
	if gasPolicy != nil {
		gasLimit = gasPolicy(providerGas, len(normalized))
	}
	return Bundle{
		To:       walletAddress,
		Data:     data,
		Value:    total,
		GasLimit: gasLimit,
	}, nil
}

// MultiplyGasWithFloor returns a gas policy that applies a ratio to the
// provider estimate and enforces a minimum gas limit.
//
// NOTE: callers choose their own floor values. Transfer uses eth_estimateGas
// with a 200K floor; swap uses adapter estimates with a 1M floor. The skill
// doc (skills/_shared/amount-resolution.md) instructs the companion to use
// dynamic gas estimation rather than hardcoded limits.
func MultiplyGasWithFloor(numerator, denominator, floor uint64) GasPolicy {
	return func(providerGas uint64, _ int) uint64 {
		gasLimit := uint64(0)
		if denominator != 0 {
			gasLimit = providerGas * numerator / denominator
		}
		if gasLimit < floor {
			return floor
		}
		return gasLimit
	}
}

// HeuristicGas returns a gas policy that chooses the larger of providerGas and
// minBase plus perCall for every encoded child call.
func HeuristicGas(minBase, perCall uint64) GasPolicy {
	return func(providerGas uint64, callCount int) uint64 {
		heuristic := minBase + perCall*uint64(callCount)
		if providerGas > heuristic {
			return providerGas
		}
		return heuristic
	}
}
