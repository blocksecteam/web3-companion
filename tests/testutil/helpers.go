// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/settings"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

// testWorkdir keeps test artifacts (encrypted key files, etc.) under a
// per-UID directory so concurrent users on a shared machine don't collide on
// a single /tmp/web3-test owned by whoever ran the suite first.
func testWorkdir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("web3-test-%d", os.Getuid()))
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}

func NewTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.AutoMigrate(
		&storage.User{},
		&storage.Wallet{},
		&storage.WalletDelegation{},
		&storage.PolicyDefinition{},
		&storage.PolicyDefinitionVersion{},
		&storage.PolicyInstance{},
		&storage.PolicyUsageEvent{},
		&storage.WebAuthnCredential{},
		&storage.Challenge{},
		&storage.AuditLog{},
		&storage.Transaction{},
		&storage.TransactionIntent{},
		&storage.AuthSession{},
		&storage.ContainerCredential{},
		&storage.WebSocketTicket{},
		&storage.WalletAssetSnapshot{},
		&storage.AppSetting{},
	); err != nil {
		t.Fatalf("migrate test DB: %v", err)
	}
	return db
}

func CreateTestUser(t *testing.T, db *gorm.DB) (string, string) {
	t.Helper()
	userID := "test_user_" + strings.ReplaceAll(t.Name(), "/", "_")
	user := &storage.User{
		ID: userID, Email: userID + "@test.local",
		PasskeySetup: true, Name: "test",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create test user: %v", err)
	}
	tokenPair, err := auth.NewTokenService(db, []byte(auth.TestJWTSecret)).Issue(t.Context(), auth.SessionRequest{
		UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("issue test jwt: %v", err)
	}
	return user.ID, tokenPair.AccessToken
}

func CreateTestWallet(t *testing.T, db *gorm.DB, userID, walletID, address string) {
	t.Helper()
	walletUID := testWalletUID(userID, walletID)
	w := &storage.Wallet{
		WalletID:        walletID,
		UserID:          userID,
		DisplayWalletID: walletID,
		WalletUID:       walletUID,
		Generation:      1,
		Address:         address,
		KeyFile:         "/tmp/test-wallet.enc",
		KeyStatus:       "active",
	}
	if err := db.Create(w).Error; err != nil {
		t.Fatalf("create test wallet: %v", err)
	}
	for _, chain := range []string{"ethereum", "base", "bsc", "polygon", "arbitrum", "optimism", "avalanche"} {
		if err := db.Create(&storage.WalletDelegation{
			WalletUID: walletUID,
			UserID:    userID,
			WalletID:  walletID,
			Chain:     chain,
			Delegated: true,
			Delegator: "0x63c0c19a282a1b52b07dd5a65b58948a07dae32b",
			Status:    "delegated",
		}).Error; err != nil {
			t.Fatalf("create test wallet delegation: %v", err)
		}
	}
}

func CreateUndelegatedTestWallet(t *testing.T, db *gorm.DB, userID, walletID, address string) {
	t.Helper()
	w := &storage.Wallet{
		WalletID:        walletID,
		UserID:          userID,
		DisplayWalletID: walletID,
		WalletUID:       testWalletUID(userID, walletID),
		Generation:      1,
		Address:         address,
		KeyFile:         "/tmp/test-wallet.enc",
		KeyStatus:       "active",
	}
	if err := db.Create(w).Error; err != nil {
		t.Fatalf("create undelegated test wallet: %v", err)
	}
}

func testWalletUID(userID, walletID string) string {
	sum := sha256.Sum256([]byte(userID + "\x00" + walletID))
	return "wuid_" + hex.EncodeToString(sum[:])[:24]
}

func NewTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{ExternalURL: "http://localhost:8080", ReadTimeout: 30, WriteTimeout: 30},
		DB:     config.DBConfig{Driver: "sqlite", DSN: ":memory:"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPName: "test", RPOrigin: "http://localhost:8080",
			ChallengeTimeoutSeconds: 120, RegistrationTimeoutSeconds: 300,
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: testWorkdir()},
		Log:     config.LogConfig{Level: "error"},
	}
}

func DoRequest(r http.Handler, method, path string, body interface{}, headers map[string]string) *httptest.ResponseRecorder {
	var bodyReader *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = strings.NewReader(string(b))
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func DoAuthRequest(r http.Handler, method, path, apiKey string, body interface{}) *httptest.ResponseRecorder {
	return DoRequest(r, method, path, body, map[string]string{"Authorization": "Bearer " + apiKey})
}

func ParseJSON(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("parse response: %v\nbody: %s", err, w.Body.String())
	}
}

func BackgroundCtx() context.Context { return context.Background() }

func NewSettingsService(db *gorm.DB) *settings.Service {
	return settings.NewService(db)
}

func DefaultLLM() settings.LLMSettings {
	return settings.DefaultLLMSettings()
}
