// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package primitives

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// ParseAddress validates and parses a hex EVM address. Strips surrounding
// whitespace; rejects anything that does not pass common.IsHexAddress.
func ParseAddress(input string) (common.Address, bool) {
	input = strings.TrimSpace(input)
	if !common.IsHexAddress(input) {
		return common.Address{}, false
	}
	return common.HexToAddress(input), true
}

// CommonAddress reports whether input parses as a valid EVM address.
// Convenience wrapper around ParseAddress for callers that just want a
// boolean validity check.
func CommonAddress(v string) bool {
	_, ok := ParseAddress(v)
	return ok
}

// ParseInteger parses a decimal or 0x-prefixed hex integer string into a
// non-negative *big.Int. An empty string is treated as zero. Returns false
// when the string is malformed or represents a negative number.
func ParseInteger(input string) (*big.Int, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return big.NewInt(0), true
	}
	n := new(big.Int)
	base := 10
	if strings.HasPrefix(input, "0x") || strings.HasPrefix(input, "0X") {
		input = input[2:]
		base = 16
	}
	if input == "" {
		return big.NewInt(0), true
	}
	if _, ok := n.SetString(input, base); !ok || n.Sign() < 0 {
		return nil, false
	}
	return n, true
}

// ParseHexBytes decodes a hex string (with or without 0x prefix) into bytes.
// An empty or "0x" input returns nil bytes with no error. Odd-length input
// is an error.
func ParseHexBytes(input string) ([]byte, error) {
	input = normalizeHex(input)
	if input == "0x" {
		return nil, nil
	}
	body := strings.TrimPrefix(input, "0x")
	if len(body)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex string")
	}
	out, err := hex.DecodeString(body)
	if err != nil {
		return nil, err
	}
	return out, nil
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
