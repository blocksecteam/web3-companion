// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/token"
)

func TestReadableToRaw(t *testing.T) {
	tests := []struct {
		name     string
		readable string
		decimals uint8
		want     string
		wantErr  bool
	}{
		{
			name:     "ETH 0.001 with 18 decimals",
			readable: "0.001",
			decimals: 18,
			want:     "1000000000000000",
		},
		{
			name:     "ETH 1 with 18 decimals",
			readable: "1",
			decimals: 18,
			want:     "1000000000000000000",
		},
		{
			name:     "USDC 100 with 6 decimals (integer)",
			readable: "100",
			decimals: 6,
			want:     "100000000",
		},
		{
			name:     "USDC 1.5 with 6 decimals",
			readable: "1.5",
			decimals: 6,
			want:     "1500000",
		},
		{
			name:     "WBTC 0.5 with 8 decimals",
			readable: "0.5",
			decimals: 8,
			want:     "50000000",
		},
		{
			name:     "exact precision boundary: 6 fractional digits, 6 decimals",
			readable: "1.234567",
			decimals: 6,
			want:     "1234567",
		},
		{
			name:     "zero is zero",
			readable: "0",
			decimals: 18,
			want:     "0",
		},
		{
			name:     "zero with fraction is zero",
			readable: "0.0",
			decimals: 18,
			want:     "0",
		},
		{
			name:     "leading + trailing whitespace is trimmed",
			readable: "  0.001  ",
			decimals: 18,
			want:     "1000000000000000",
		},
		{
			name:     "over-precision rejected: 7 digits vs 6 decimals",
			readable: "1.2345678",
			decimals: 6,
			wantErr:  true,
		},
		{
			name:     "empty string rejected",
			readable: "",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "non-numeric rejected",
			readable: "abc",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "negative rejected",
			readable: "-1",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "multiple dots rejected",
			readable: "1.2.3",
			decimals: 18,
			wantErr:  true,
		},
		{
			name:     "very large value preserves precision (no float math)",
			readable: "123456789012345.123456789012345678",
			decimals: 18,
			want:     "123456789012345123456789012345678",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := token.ReadableToRaw(tt.readable, tt.decimals)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ReadableToRaw(%q, %d) err = %v, wantErr = %v", tt.readable, tt.decimals, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("ReadableToRaw(%q, %d)\n  got:  %s\n  want: %s", tt.readable, tt.decimals, got, tt.want)
			}
		})
	}
}
