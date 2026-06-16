// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package passkey

import (
	"errors"
	"testing"
)

func TestValidateSignCount(t *testing.T) {
	tests := []struct {
		name     string
		stored   uint32
		reported uint32
		wantErr  error
	}{
		{
			name:     "both zero passes (Apple Touch ID, no counter)",
			stored:   0,
			reported: 0,
			wantErr:  nil,
		},
		{
			name:     "first authentication with non-zero counter passes",
			stored:   0,
			reported: 5,
			wantErr:  nil,
		},
		{
			name:     "monotonic increment passes",
			stored:   5,
			reported: 10,
			wantErr:  nil,
		},
		{
			name:     "replay (reported == stored, both non-zero) is rejected",
			stored:   5,
			reported: 5,
			wantErr:  ErrSignCountInvalid,
		},
		{
			name:     "rollback (reported < stored) is rejected",
			stored:   10,
			reported: 5,
			wantErr:  ErrSignCountInvalid,
		},
		{
			name:     "rollback to zero from non-zero is rejected (clone signal)",
			stored:   5,
			reported: 0,
			wantErr:  ErrSignCountInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSignCount(tt.stored, tt.reported)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateSignCount(stored=%d, reported=%d) = %v, want %v",
					tt.stored, tt.reported, err, tt.wantErr)
			}
		})
	}
}
