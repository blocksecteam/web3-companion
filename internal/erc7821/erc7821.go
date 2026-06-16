// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package erc7821

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

var executeSelector = []byte{0xe9, 0xae, 0x5c, 0x53}
var modeSingleBatch = [32]byte{0x01}

type Call struct {
	To    common.Address
	Value *big.Int
	Data  []byte
}

type HexCall struct {
	To    string
	Value *big.Int
	Data  string
}

func EncodeExecuteHex(calls []HexCall) (string, error) {
	typed := make([]Call, 0, len(calls))
	for _, call := range calls {
		if !common.IsHexAddress(call.To) {
			return "", fmt.Errorf("invalid call address %q", call.To)
		}
		data, err := decodeHexBytes(call.Data)
		if err != nil {
			return "", fmt.Errorf("invalid calldata for %s: %w", call.To, err)
		}
		typed = append(typed, Call{
			To:    common.HexToAddress(call.To),
			Value: call.Value,
			Data:  data,
		})
	}
	return EncodeExecute(typed)
}

func EncodeExecute(calls []Call) (string, error) {
	if len(calls) == 0 {
		return "", fmt.Errorf("empty calls list")
	}

	callsArray, err := abi.NewType("tuple[]", "", []abi.ArgumentMarshaling{
		{Name: "to", Type: "address"},
		{Name: "value", Type: "uint256"},
		{Name: "data", Type: "bytes"},
	})
	if err != nil {
		return "", fmt.Errorf("build calls abi type: %w", err)
	}

	tuples := make([]struct {
		To    common.Address
		Value *big.Int
		Data  []byte
	}, len(calls))
	for i, call := range calls {
		value := call.Value
		if value == nil {
			value = big.NewInt(0)
		}
		tuples[i].To = call.To
		tuples[i].Value = value
		tuples[i].Data = call.Data
	}

	executionData, err := (abi.Arguments{{Type: callsArray}}).Pack(tuples)
	if err != nil {
		return "", fmt.Errorf("encode calls: %w", err)
	}

	bytes32Type, err := abi.NewType("bytes32", "", nil)
	if err != nil {
		return "", fmt.Errorf("build bytes32 abi type: %w", err)
	}
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return "", fmt.Errorf("build bytes abi type: %w", err)
	}
	payload, err := (abi.Arguments{{Type: bytes32Type}, {Type: bytesType}}).Pack(modeSingleBatch, executionData)
	if err != nil {
		return "", fmt.Errorf("encode execute: %w", err)
	}

	result := make([]byte, len(executeSelector)+len(payload))
	copy(result, executeSelector)
	copy(result[len(executeSelector):], payload)
	return "0x" + hex.EncodeToString(result), nil
}

func MustHexDecode(s string) []byte {
	b, err := decodeHexBytes(s)
	if err != nil {
		panic(fmt.Sprintf("decode hex: %v", err))
	}
	return b
}

func decodeHexBytes(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "0x")
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex string")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return b, nil
}
