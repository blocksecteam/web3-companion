// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server_test

import (
	"testing"

	"github.com/blocksecteam/web3-companion/internal/server"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.1.0", "v0.1.1", true},
		{"v0.1.0", "v1.0.0", true},
		{"v0.2.0", "v0.1.0", false},
		{"v0.1.0", "v0.1.0", false},
		{"v1.0.0", "v0.99.99", false},
		{"dev", "v0.1.0", false},
		{"v0.1.0", "", false},
		{"", "v0.1.0", false},
		{"v0.1.0", "invalid", false},
		{"invalid", "v0.1.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.current+"_vs_"+tt.latest, func(t *testing.T) {
			got := server.IsNewerVersion(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}
