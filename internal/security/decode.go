// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

const selectorERC7821Execute = "0xe9ae5c53"

// DecodeOperations parses the request's calldata into a list of Operation
// values for downstream calldata checks to inspect, plus a CheckResult under
// the `calldata_decode` rule describing whether decoding succeeded.
//
// It deliberately does NOT validate request shape fields beyond what is
// needed for decoding: req.To and req.Value are parsed best-effort
// (defaulting to zero when invalid) and passed straight into the resulting
// Operation. Shape validation is TransactionShape's job — running both
// here would duplicate-emit the same rule. If TransactionShape flagged
// owner/to/value as malformed, the aggregate verdict is already fail; the
// Operation we hand downstream might carry zero addresses, but downstream
// checks short-circuit cleanly on that.
func DecodeOperations(req CheckRequest) ([]Operation, CheckResult) {
	to, _ := primitives.ParseAddress(req.To)
	value, _ := primitives.ParseInteger(req.Value)
	if value == nil {
		value = big.NewInt(0)
	}
	data, err := primitives.ParseHexBytes(req.Data)
	if err != nil {
		return nil, CheckResult{
			Rule:   "calldata_decode",
			Status: "fail",
			Detail: fmt.Sprintf("transaction calldata is invalid: %v", err),
		}
	}
	if !strings.HasPrefix(strings.ToLower(normalizeHex(req.Data)), selectorERC7821Execute) {
		return []Operation{{To: to, Value: value, Data: data}}, CheckResult{
			Rule:   "calldata_decode",
			Status: "pass",
			Detail: fmt.Sprintf("direct transaction calldata method=%s.", primitives.MethodID(data)),
			Observed: []OperationView{{
				To:     to.Hex(),
				Value:  value.String(),
				Method: primitives.MethodID(data),
			}},
		}
	}

	ops, err := decodeERC7821Execute(data)
	if err != nil {
		return nil, CheckResult{
			Rule:   "calldata_decode",
			Status: "fail",
			Detail: fmt.Sprintf("ERC-7821 execute calldata could not be decoded: %v", err),
		}
	}
	views := make([]OperationView, 0, len(ops))
	for _, op := range ops {
		views = append(views, OperationView{
			To:     op.To.Hex(),
			Value:  op.Value.String(),
			Method: primitives.MethodID(op.Data),
		})
	}
	return ops, CheckResult{
		Rule:     "calldata_decode",
		Status:   "pass",
		Detail:   fmt.Sprintf("decoded %d ERC-7821 operation(s).", len(ops)),
		Observed: views,
	}
}

func decodeERC7821Execute(data []byte) ([]Operation, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("calldata shorter than selector")
	}
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	bytesType, _ := abi.NewType("bytes", "", nil)
	executeArgs := abi.Arguments{
		{Type: bytes32Type},
		{Type: bytesType},
	}
	unpacked, err := executeArgs.Unpack(data[4:])
	if err != nil {
		return nil, fmt.Errorf("decode execute args: %w", err)
	}
	if len(unpacked) != 2 {
		return nil, fmt.Errorf("expected 2 execute args, got %d", len(unpacked))
	}
	executionData, ok := unpacked[1].([]byte)
	if !ok {
		return nil, fmt.Errorf("executionData has unexpected type %T", unpacked[1])
	}

	callArray, _ := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
	})
	args := abi.Arguments{{Type: callArray}}
	values, err := args.Unpack(executionData)
	if err != nil {
		return nil, fmt.Errorf("decode executionData: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("expected one calls array, got %d values", len(values))
	}
	calls, ok := values[0].([]struct {
		To    common.Address `json:"to"`
		Value *big.Int       `json:"value"`
		Data  []byte         `json:"data"`
	})
	if !ok {
		return nil, fmt.Errorf("calls array has unexpected type %T", values[0])
	}
	if len(calls) == 0 {
		return nil, fmt.Errorf("empty calls array")
	}

	ops := make([]Operation, 0, len(calls))
	for _, call := range calls {
		value := call.Value
		if value == nil {
			value = big.NewInt(0)
		}
		ops = append(ops, Operation{
			To:    call.To,
			Value: new(big.Int).Set(value),
			Data:  append([]byte(nil), call.Data...),
		})
	}
	return ops, nil
}

func normalizeHex(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return "0x"
	}
	if strings.HasPrefix(input, "0x") || strings.HasPrefix(input, "0X") {
		return "0x" + input[2:]
	}
	return "0x" + input
}
