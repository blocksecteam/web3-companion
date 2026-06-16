// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package swap_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/swap"
)

type fakeDecimalsSource struct {
	responses map[string]uint8
	errors    map[string]bool
	callCount int
}

func (f *fakeDecimalsSource) Decimals(ctx context.Context, chain, asset string) (uint8, error) {
	f.callCount++
	key := strings.ToLower(chain + "|" + asset)
	if f.errors[key] {
		return 0, errors.New("fake source error")
	}
	if d, ok := f.responses[key]; ok {
		return d, nil
	}
	return 0, errors.New("not found in fake source")
}

func newResolver(rpc, portals *fakeDecimalsSource) *swap.DecimalsResolver {
	return swap.NewDecimalsResolver(rpc, portals)
}

func TestResolveDecimals_Native(t *testing.T) {
	rpc := &fakeDecimalsSource{}
	portals := &fakeDecimalsSource{}
	r := newResolver(rpc, portals)

	tests := []string{
		"native",
		"0x0",
		"0x0000000000000000000000000000000000000000",
		"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",
		"0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	for _, asset := range tests {
		t.Run("native_"+asset, func(t *testing.T) {
			d, err := r.Resolve(context.Background(), "ethereum", asset)
			if err != nil {
				t.Fatalf("Resolve(%q) err = %v", asset, err)
			}
			if d != 18 {
				t.Errorf("Resolve(%q) = %d, want 18", asset, d)
			}
		})
	}
	if rpc.callCount != 0 || portals.callCount != 0 {
		t.Errorf("native assets must not hit RPC or Portals; rpc=%d portals=%d", rpc.callCount, portals.callCount)
	}
}

func TestResolveDecimals_BareSymbolRejected(t *testing.T) {
	rpc := &fakeDecimalsSource{responses: map[string]uint8{"ethereum|usdc": 6}}
	portals := &fakeDecimalsSource{responses: map[string]uint8{"ethereum|usdc": 6}}
	r := newResolver(rpc, portals)

	for _, sym := range []string{"USDC", "usdc", "DAI", "WBTC", "TKN"} {
		t.Run(sym, func(t *testing.T) {
			_, err := r.Resolve(context.Background(), "ethereum", sym)
			if err == nil {
				t.Errorf("Resolve(%q) expected error, got nil", sym)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "symbol") &&
				!strings.Contains(strings.ToLower(err.Error()), "address") {
				t.Errorf("error should mention symbol/address resolution required; got: %v", err)
			}
		})
	}
	if rpc.callCount != 0 || portals.callCount != 0 {
		t.Errorf("symbol input must not hit RPC or Portals; rpc=%d portals=%d", rpc.callCount, portals.callCount)
	}
}

func TestResolveDecimals_AddressViaRPC(t *testing.T) {
	addr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	rpc := &fakeDecimalsSource{
		responses: map[string]uint8{strings.ToLower("ethereum|" + addr): 6},
	}
	portals := &fakeDecimalsSource{}
	r := newResolver(rpc, portals)

	d, err := r.Resolve(context.Background(), "ethereum", addr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d != 6 {
		t.Errorf("got %d, want 6", d)
	}
	if rpc.callCount != 1 {
		t.Errorf("expected 1 RPC call, got %d", rpc.callCount)
	}
	if portals.callCount != 0 {
		t.Errorf("Portals should not be called when RPC succeeds; got %d", portals.callCount)
	}
}

func TestResolveDecimals_AddressRPCFailPortalsSuccess(t *testing.T) {
	addr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	rpc := &fakeDecimalsSource{
		errors: map[string]bool{strings.ToLower("ethereum|" + addr): true},
	}
	portals := &fakeDecimalsSource{
		responses: map[string]uint8{strings.ToLower("ethereum|" + addr): 6},
	}
	r := newResolver(rpc, portals)

	d, err := r.Resolve(context.Background(), "ethereum", addr)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if d != 6 {
		t.Errorf("got %d, want 6 (from Portals fallback)", d)
	}
	if rpc.callCount != 1 || portals.callCount != 1 {
		t.Errorf("rpc=%d portals=%d, want both 1", rpc.callCount, portals.callCount)
	}
}

func TestResolveDecimals_AllFail(t *testing.T) {
	addr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	rpc := &fakeDecimalsSource{errors: map[string]bool{strings.ToLower("ethereum|" + addr): true}}
	portals := &fakeDecimalsSource{errors: map[string]bool{strings.ToLower("ethereum|" + addr): true}}
	r := newResolver(rpc, portals)

	_, err := r.Resolve(context.Background(), "ethereum", addr)
	if err == nil {
		t.Fatal("expected error when both sources fail")
	}
}

func TestResolveDecimals_CacheHit(t *testing.T) {
	addr := "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	rpc := &fakeDecimalsSource{
		responses: map[string]uint8{strings.ToLower("ethereum|" + addr): 6},
	}
	portals := &fakeDecimalsSource{}
	r := newResolver(rpc, portals)

	for i := 0; i < 3; i++ {
		d, err := r.Resolve(context.Background(), "ethereum", addr)
		if err != nil {
			t.Fatalf("call %d err = %v", i, err)
		}
		if d != 6 {
			t.Errorf("call %d got %d, want 6", i, d)
		}
	}
	if rpc.callCount != 1 {
		t.Errorf("RPC called %d times; expected exactly 1 (subsequent calls hit cache)", rpc.callCount)
	}
}

func TestResolveDecimals_AddressCaseInsensitive(t *testing.T) {
	upper := "0xA0B86991C6218B36C1D19D4A2E9EB0CE3606EB48"
	lower := strings.ToLower(upper)
	rpc := &fakeDecimalsSource{
		responses: map[string]uint8{strings.ToLower("ethereum|" + upper): 6},
	}
	r := newResolver(rpc, &fakeDecimalsSource{})

	_, err := r.Resolve(context.Background(), "ethereum", upper)
	if err != nil {
		t.Fatalf("upper err = %v", err)
	}

	_, err = r.Resolve(context.Background(), "ethereum", lower)
	if err != nil {
		t.Fatalf("lower err = %v", err)
	}
	if rpc.callCount != 1 {
		t.Errorf("case-insensitive cache miss: RPC called %d times, want 1", rpc.callCount)
	}
}

func TestResolveDecimals_MalformedRejected(t *testing.T) {
	r := newResolver(&fakeDecimalsSource{}, &fakeDecimalsSource{})

	bad := []string{
		"",
		"   ",
		"0x",
		"0xABC",
		"0xZZZZeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"not-an-address",
	}
	for _, asset := range bad {
		t.Run("bad_"+asset, func(t *testing.T) {
			_, err := r.Resolve(context.Background(), "ethereum", asset)
			if err == nil {
				t.Errorf("Resolve(%q) expected error, got nil", asset)
			}
		})
	}
}

func TestResolveDecimals_NilSources(t *testing.T) {
	r := swap.NewDecimalsResolver(nil, nil)

	d, err := r.Resolve(context.Background(), "ethereum", "native")
	if err != nil || d != 18 {
		t.Errorf("native token with nil sources: got d=%d err=%v, want d=18 err=nil", d, err)
	}

	_, err = r.Resolve(context.Background(), "ethereum", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	if err == nil {
		t.Error("unknown address with nil sources should error")
	}

	_, err = r.Resolve(context.Background(), "ethereum", "USDC")
	if err == nil {
		t.Error("symbol with nil sources should error")
	}
}
