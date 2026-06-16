// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package delegation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/delegation"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func TestCheckBeforeBuildRequiresDelegationForRequestedChain(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&storage.Wallet{}, &storage.WalletDelegation{}, &storage.Transaction{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	wallet := storage.Wallet{
		UserID:          "user_1",
		WalletID:        "main",
		DisplayWalletID: "main",
		WalletUID:       "wuid_main",
		Generation:      1,
		Address:         "0x1111111111111111111111111111111111111111",
		KeyFile:         "/tmp/key.enc",
		KeyStatus:       "active",
	}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if err := db.Create(&storage.WalletDelegation{
		WalletUID: "wuid_main",
		UserID:    "user_1",
		WalletID:  "main",
		Chain:     "ethereum",
		Delegated: true,
		Delegator: "0x63c0c19a282a1b52b07dd5a65b58948a07dae32b",
		Status:    "delegated",
	}).Error; err != nil {
		t.Fatalf("create delegation: %v", err)
	}

	ethereum := delegation.CheckBeforeBuild(context.Background(), db, "", wallet, "ethereum")
	if ethereum.Status != delegation.OK {
		t.Fatalf("ethereum status = %v, want OK", ethereum.Status)
	}

	base := delegation.CheckBeforeBuild(context.Background(), db, "", wallet, "base")
	if base.Status != delegation.NotDelegated {
		t.Fatalf("base status = %v, want NotDelegated", base.Status)
	}
}

func TestCheckBeforeBuildRefreshesMissingDelegationStateFromChain(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&storage.Wallet{}, &storage.WalletDelegation{}, &storage.Transaction{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	wallet := storage.Wallet{
		UserID:          "user_1",
		WalletID:        "main",
		DisplayWalletID: "main",
		WalletUID:       "wuid_main",
		Generation:      1,
		Address:         "0x1111111111111111111111111111111111111111",
		KeyFile:         "/tmp/key.enc",
		KeyStatus:       "active",
	}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	rpc := delegatedCodeRPC(t)
	defer rpc.Close()

	result := delegation.CheckBeforeBuild(context.Background(), db, rpc.URL, wallet, "base")
	if result.Status != delegation.OK {
		t.Fatalf("status = %v, want OK: %s", result.Status, result.Message)
	}

	var state storage.WalletDelegation
	if err := db.First(&state, "wallet_uid = ? AND chain = ?", "wuid_main", "base").Error; err != nil {
		t.Fatalf("delegation state was not stored: %v", err)
	}
	if !state.Delegated || state.Status != "delegated" {
		t.Fatalf("unexpected delegation state: %#v", state)
	}
}

func TestErrorCodeUsesDelegationConfirmedName(t *testing.T) {
	if got := delegation.ErrorCode(delegation.DelegationConfirmed); got != "delegation_confirmed" {
		t.Fatalf("ErrorCode(DelegationConfirmed) = %q, want delegation_confirmed", got)
	}
}

func delegatedCodeRPC(t *testing.T) *httptest.Server {
	t.Helper()
	code := "0xef0100" + "63c0c19a282a1b52b07dd5a65b58948a07dae32b"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{}   `json:"id"`
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc: %v", err)
		}
		result := interface{}("0x")
		if req.Method == "eth_getCode" {
			result = code
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
}
