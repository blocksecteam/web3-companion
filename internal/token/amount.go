// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ReadableToRaw converts a human-readable amount (e.g. "1.5") into a raw
// integer string scaled by the token's decimals (e.g. "1500000" for 6
// decimals). Rejects negatives, multiple decimal points, non-digit chars,
// and fractional parts longer than decimals.
func ReadableToRaw(readable string, decimals uint8) (string, error) {
	s := strings.TrimSpace(readable)
	if s == "" {
		return "", errors.New("readable amount is empty")
	}
	if strings.HasPrefix(s, "-") {
		return "", errors.New("readable amount must be non-negative")
	}
	if strings.HasPrefix(s, "+") {
		s = s[1:]
	}

	parts := strings.Split(s, ".")
	switch len(parts) {
	case 1:
		parts = append(parts, "")
	case 2:
	default:
		return "", fmt.Errorf("readable amount %q has multiple decimal points", readable)
	}
	intPart, fracPart := parts[0], parts[1]

	if intPart == "" {
		intPart = "0"
	}
	if !isAllDigits(intPart) {
		return "", fmt.Errorf("readable amount %q has non-digit in integer part", readable)
	}
	if fracPart != "" && !isAllDigits(fracPart) {
		return "", fmt.Errorf("readable amount %q has non-digit in fractional part", readable)
	}
	if uint8(len(fracPart)) > decimals {
		return "", fmt.Errorf("readable amount %q has %d fractional digits but token only supports %d", readable, len(fracPart), decimals)
	}

	padded := fracPart + strings.Repeat("0", int(decimals)-len(fracPart))
	combined := intPart + padded
	combined = strings.TrimLeft(combined, "0")
	if combined == "" {
		return "0", nil
	}

	if _, ok := new(big.Int).SetString(combined, 10); !ok {
		return "", fmt.Errorf("readable amount %q does not parse as integer", readable)
	}
	return combined, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
