// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package primitives

import (
	"math/big"
	"strings"
)

// FirstString returns the first non-empty string in values, or "" when all
// are empty. Trims whitespace before comparing.
func FirstString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// AddrEq is case-insensitive equality on EVM addresses presented as hex
// strings. Both inputs are normalized via strings.EqualFold; no further
// validation is performed.
func AddrEq(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// DecimalRat parses a fixed-point decimal string (e.g. "12.34") into a
// *big.Rat. Empty input is treated as invalid (returns false). Whitespace is
// trimmed before parsing.
func DecimalRat(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, false
	}
	return r, true
}

// FormatRat renders a *big.Rat as a fixed-point decimal string with up to 18
// fractional digits, trimming trailing zeros and a trailing decimal point.
// Used for human-readable USD amounts inside CheckResult details.
func FormatRat(v *big.Rat) string {
	if v == nil {
		return ""
	}
	s := v.FloatString(18)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}
