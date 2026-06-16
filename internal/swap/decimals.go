// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/blocksecteam/web3-companion/internal/token"
)

type DecimalsSource interface {
	Decimals(ctx context.Context, chain, asset string) (uint8, error)
}

type DecimalsResolver struct {
	rpc     DecimalsSource
	portals DecimalsSource
	cache   map[string]uint8
	mu      sync.RWMutex
}

func NewDecimalsResolver(rpc, portals DecimalsSource) *DecimalsResolver {
	return &DecimalsResolver{
		rpc:     rpc,
		portals: portals,
		cache:   make(map[string]uint8),
	}
}

func (r *DecimalsResolver) Resolve(ctx context.Context, chain, asset string) (uint8, error) {
	a := strings.TrimSpace(asset)
	if a == "" {
		return 0, fmt.Errorf("asset is empty")
	}

	if token.IsNativeToken(a) {
		return 18, nil
	}

	if !isHexAddress(a) {
		return 0, fmt.Errorf("asset %q is not a valid address; resolve token symbol to a chain-specific address first (via token info)", asset)
	}

	key := cacheKey(chain, a)
	if d, ok := r.cacheGet(key); ok {
		return d, nil
	}

	if r.rpc != nil {
		if d, err := r.rpc.Decimals(ctx, chain, a); err == nil {
			r.cacheSet(key, d)
			return d, nil
		}
	}

	if r.portals != nil {
		if d, err := r.portals.Decimals(ctx, chain, a); err == nil {
			r.cacheSet(key, d)
			return d, nil
		}
	}

	return 0, fmt.Errorf("could not resolve decimals for asset %q on chain %q", asset, chain)
}

func (r *DecimalsResolver) cacheGet(key string) (uint8, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.cache[key]
	return d, ok
}

func (r *DecimalsResolver) cacheSet(key string, d uint8) {
	r.mu.Lock()
	r.cache[key] = d
	r.mu.Unlock()
}

func cacheKey(chain, asset string) string {
	return strings.ToLower(chain) + "|" + strings.ToLower(asset)
}

func isHexAddress(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 42 {
		return false
	}
	if s[0] != '0' || (s[1] != 'x' && s[1] != 'X') {
		return false
	}
	for _, c := range s[2:] {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
