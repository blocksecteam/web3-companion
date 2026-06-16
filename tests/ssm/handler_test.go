// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	ssmhandler "github.com/blocksecteam/web3-companion/internal/ssm/handler"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func approvedChallengeForPayload(t *testing.T, db *gorm.DB, userID, typ string, payload interface{}) string {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal challenge payload: %v", err)
	}
	sum := sha256.Sum256(payloadBytes)
	envelope, err := json.Marshal(map[string]interface{}{
		"payload":      payload,
		"payload_hash": hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatalf("marshal challenge envelope: %v", err)
	}
	ch := storage.Challenge{
		ChallengeID: "ch_" + typ + "_handler_" + time.Now().Format("150405.000000000"),
		UserID:      userID,
		Type:        typ,
		Status:      "approved",
		PayloadJSON: string(envelope),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	return ch.ChallengeID
}

func createSignIntent(t *testing.T, db *gorm.DB, userID string, wallet *storage.Wallet, opts ...func(*txintent.CreateRequest)) storage.TransactionIntent {
	t.Helper()
	req := txintent.CreateRequest{
		UserID:    userID,
		WalletID:  wallet.WalletID,
		WalletUID: wallet.WalletUID,
		Chain:     "ethereum",
		ChainID:   1,
		TxType:    "transfer",
		Owner:     wallet.Address,
		To:        "0x1234567890123456789012345678901234567890",
		Value:     "0",
		Data:      "0x",
		GasLimit:  21000,
		Recipient: "0x1234567890123456789012345678901234567890",
	}
	for _, opt := range opts {
		opt(&req)
	}
	intent, err := txintent.NewService(db, vault.NewTestKMSClient()).Create(t.Context(), req)
	if err != nil {
		t.Fatalf("create sign intent: %v", err)
	}
	intent.Status = txintent.StatusSecurityPassed
	intent.SecurityVerdict = "pass"
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save sign intent security status: %v", err)
	}
	return intent
}

func setupSSMHandler(t *testing.T) (*gin.Engine, string, *gorm.DB, *vault.Vault) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)

	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{{Name: "ethereum", ChainID: 1}}
	auth.SetExternalURL(cfg.Server.ExternalURL)
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, cfg.SSMKeysDir(), nil, nil)
	pk := passkey.NewServer(db, &cfg.WebAuthn)

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	ssmhandler.RegisterReadRoutesWithConfig(api, db)
	ssmhandler.RegisterWebAuthnBrowserRoutes(api, pk)
	ssmhandler.RegisterCompanionRoutesWithConfig(api, r, v, db, pk, cfg)

	_ = userID
	return r, apiKey, db, v
}

func setupSSMHandlerWithRPC(t *testing.T, rpcURL string) (*gin.Engine, string, *gorm.DB, *vault.Vault) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userID, apiKey := testutil.CreateTestUser(t, db)

	cfg := testutil.NewTestConfig()
	cfg.Chains = []config.ChainConfig{{Name: "ethereum", ChainID: 1, RPCURL: rpcURL}}
	auth.SetExternalURL(cfg.Server.ExternalURL)
	kms := vault.NewTestKMSClient()
	v := vault.New(kms, db, cfg.SSMKeysDir(), map[string]string{"ethereum": rpcURL}, map[string]int{"ethereum": 1})
	pk := passkey.NewServer(db, &cfg.WebAuthn)

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	ssmhandler.RegisterReadRoutesWithConfig(api, db)
	ssmhandler.RegisterWebAuthnBrowserRoutes(api, pk)
	ssmhandler.RegisterCompanionRoutesWithConfig(api, r, v, db, pk, cfg)

	_ = userID
	return r, apiKey, db, v
}

func delegatedRPCServer(t *testing.T) *httptest.Server {
	t.Helper()
	delegationCode := "0xef0100" + strings.TrimPrefix(strings.ToLower(vault.MMDelegatorAddress), "0x")
	return delegatedRPCServerWithListener(t, delegationCode)
}

func delegatedRPCServerWithListener(t *testing.T, delegationCode string) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test rpc: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		result := "0x"
		if req.Method == "eth_getCode" {
			result = delegationCode
		}
		if req.Method == "eth_getTransactionCount" {
			result = "0x7"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func delayedDelegationRPCServer(t *testing.T) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test rpc: %v", err)
	}
	delegationCode := "0xef0100" + strings.TrimPrefix(strings.ToLower(vault.MMDelegatorAddress), "0x")
	var receiptPolls atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{}   `json:"id"`
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result interface{} = "0x"
		switch req.Method {
		case "eth_getCode":
			if receiptPolls.Load() > 0 {
				result = delegationCode
			}
		case "eth_getBalance":
			result = "0xde0b6b3a7640000"
		case "eth_gasPrice":
			result = "0x3b9aca00"
		case "eth_getTransactionCount":
			result = "0x7"
		case "eth_chainId":
			result = "0x1"
		case "eth_sendRawTransaction":
			result = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		case "eth_getTransactionReceipt":
			if receiptPolls.Add(1) < 2 {
				result = nil
			} else {
				result = map[string]interface{}{
					"transactionHash":   "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					"transactionIndex":  "0x0",
					"blockHash":         "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"blockNumber":       "0x1",
					"from":              "0x0000000000000000000000000000000000000001",
					"to":                "0x0000000000000000000000000000000000000002",
					"cumulativeGasUsed": "0x1",
					"gasUsed":           "0x1",
					"contractAddress":   nil,
					"logs":              []interface{}{},
					"logsBloom":         "0x" + strings.Repeat("00", 256),
					"status":            "0x1",
					"type":              "0x4",
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func neverMinedDelegationRPCServer(t *testing.T) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test rpc: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result interface{} = "0x"
		switch req.Method {
		case "eth_getBalance":
			result = "0xde0b6b3a7640000"
		case "eth_gasPrice":
			result = "0x3b9aca00"
		case "eth_getTransactionCount":
			result = "0x7"
		case "eth_chainId":
			result = "0x1"
		case "eth_sendRawTransaction":
			result = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		case "eth_getTransactionReceipt":
			result = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func pendingDelegationReceiptRPCServer(t *testing.T, sendRawCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test rpc: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result interface{} = "0x"
		switch req.Method {
		case "eth_getBalance":
			result = "0xde0b6b3a7640000"
		case "eth_gasPrice":
			result = "0x3b9aca00"
		case "eth_sendRawTransaction":
			sendRawCalls.Add(1)
			result = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		case "eth_getTransactionReceipt":
			result = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		})
	}))
	srv.Listener = listener
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestSSM_DelegateCreatesPasskeyChallenge(t *testing.T) {
	// Delegation reads the wallet's on-chain code and funding (eth_getCode,
	// eth_getBalance, eth_gasPrice) before issuing the passkey challenge. This
	// stub reports an undelegated, funded wallet, so the flow proceeds to
	// pending_user_auth without broadcasting.
	rpc := delayedDelegationRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_DelegateCreatesPasskeyChallenge"
	if _, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "delegate_wallet"); err != nil {
		t.Fatalf("create wallet: %v", err)
	}

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/delegate", apiKey, map[string]interface{}{
		"wallet_id": "delegate_wallet",
		"chain":     "ethereum",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("delegate = %d, want 202 challenge: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status      string `json:"status"`
		ChallengeID string `json:"challenge_id"`
		SignURL     string `json:"sign_url"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Status != "pending_user_auth" || resp.ChallengeID == "" || resp.SignURL == "" {
		t.Fatalf("unexpected delegate challenge response: %+v", resp)
	}
	var ch storage.Challenge
	if err := db.First(&ch, "challenge_id = ?", resp.ChallengeID).Error; err != nil {
		t.Fatalf("load delegate challenge: %v", err)
	}
	if ch.Type != "delegate_wallet" || !strings.Contains(ch.PayloadJSON, "delegate_wallet") || !strings.Contains(ch.PayloadJSON, "ethereum") {
		t.Fatalf("unexpected delegate challenge: %+v", ch)
	}
}

func TestSSM_DelegateConsumesApprovedChallenge(t *testing.T) {
	rpc := delayedDelegationRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_DelegateConsumesApprovedChallenge"
	if _, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "delegate_resume_wallet"); err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	payload := map[string]interface{}{"operation": "delegate_wallet", "wallet_id": "delegate_resume_wallet", "chain": "ethereum"}
	challengeID := approvedChallengeForPayload(t, db, userID, "delegate_wallet", payload)

	w := testutil.DoAuthRequest(r, http.MethodPost, "/api/v1/ssm/delegate", apiKey, map[string]interface{}{
		"wallet_id":    "delegate_resume_wallet",
		"chain":        "ethereum",
		"challenge_id": challengeID,
	})
	if w.Code != http.StatusOK && w.Code != http.StatusAccepted {
		t.Fatalf("delegate resume = %d, want 200 or 202: %s", w.Code, w.Body.String())
	}
}

func TestSSM_PollSignRejectsDelegateChallengeWithTypeHint(t *testing.T) {
	r, apiKey, db, _ := setupSSMHandler(t)
	userID := "test_user_TestSSM_PollSignRejectsDelegateChallengeWithTypeHint"
	payload := map[string]interface{}{"operation": "delegate_wallet", "wallet_id": "main", "chain": "bsc"}
	challengeID := approvedChallengeForPayload(t, db, userID, "delegate_wallet", payload)

	w := testutil.DoAuthRequest(r, http.MethodGet, "/api/v1/ssm/sign/"+challengeID, apiKey, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("poll sign delegate challenge = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "delegate") {
		t.Fatalf("response should tell caller to resume delegation: %s", w.Body.String())
	}
}

func TestSSM_Sign_MissingFields(t *testing.T) {
	r, apiKey, _, _ := setupSSMHandler(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]string{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestSSM_Sign_RejectsRawTransactionFields(t *testing.T) {
	r, apiKey, _, _ := setupSSMHandler(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": "txi_123",
		"to":             "0x1234567890123456789012345678901234567890",
		"value":          "0",
		"data":           "0x",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("sign with raw tx fields = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestSSM_Audit(t *testing.T) {
	r, apiKey, _, _ := setupSSMHandler(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/audit", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200: %s", w.Code, w.Body.String())
	}
}

func TestSSM_Sign_RequiresWalletOwnership(t *testing.T) {
	r, apiKey, db, _ := setupSSMHandler(t)
	testutil.CreateTestWallet(t, db, "other_user", "foreign_wallet", "0x1234567890123456789012345678901234567890")
	foreign := storage.Wallet{WalletID: "foreign_wallet", WalletUID: "wuid_foreign_wallet", Address: "0x1234567890123456789012345678901234567890"}
	intent := createSignIntent(t, db, "other_user", &foreign)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestSSM_Sign_RequiresSecurityCheck(t *testing.T) {
	r, apiKey, db, _ := setupSSMHandler(t)
	userID := "test_user_TestSSM_Sign_RequiresSecurityCheck"
	testutil.CreateTestWallet(t, db, userID, "owned_wallet", "0x1234567890123456789012345678901234567890")
	wallet := &storage.Wallet{WalletID: "owned_wallet", Address: "0x1234567890123456789012345678901234567890"}
	intent, err := txintent.NewService(db, vault.NewTestKMSClient()).Create(t.Context(), txintent.CreateRequest{
		UserID:    userID,
		WalletID:  wallet.WalletID,
		WalletUID: "wuid_owned_wallet",
		Chain:     "ethereum",
		ChainID:   1,
		TxType:    "transfer",
		Owner:     wallet.Address,
		To:        "0x1234567890123456789012345678901234567890",
		Value:     "0",
		Data:      "0x",
		GasLimit:  21000,
		Recipient: "0x1234567890123456789012345678901234567890",
	})
	if err != nil {
		t.Fatalf("create intent: %v", err)
	}

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestSSM_SignRejectsNotDelegatedWalletWithoutAutoDelegating(t *testing.T) {
	var sendRawCalls atomic.Int32
	rpc := pendingDelegationReceiptRPCServer(t, &sendRawCalls)
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_SignRejectsNotDelegatedWalletWithoutAutoDelegating"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "not_delegated_sign_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	bindHandlerAutoPolicy(t, db, userID, wallet.WalletID)
	intent := createSignIntent(t, db, userID, wallet)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("sign = %d, want 409 not delegated: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	errBody, _ := resp["error"].(map[string]interface{})
	if errBody["code"] != "not_delegated" {
		t.Fatalf("unexpected sign response: %#v", resp)
	}
	if got := sendRawCalls.Load(); got != 0 {
		t.Fatalf("sign should not broadcast delegation tx, got %d eth_sendRawTransaction calls", got)
	}
	var state storage.WalletDelegation
	if err := db.First(&state, "wallet_uid = ? AND chain = ?", wallet.WalletUID, "ethereum").Error; err != nil {
		t.Fatalf("load delegation state: %v", err)
	}
	if state.Delegated || state.Status != "not_delegated" {
		t.Fatalf("delegation state must remain not_delegated before explicit delegation: %+v", state)
	}
	var updatedIntent storage.TransactionIntent
	if err := db.First(&updatedIntent, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if updatedIntent.Status != txintent.StatusSecurityPassed {
		t.Fatalf("intent status = %q, want security_passed", updatedIntent.Status)
	}
}

func TestSSM_SignPendingDelegationRetryDoesNotRebroadcast(t *testing.T) {
	var sendRawCalls atomic.Int32
	rpc := pendingDelegationReceiptRPCServer(t, &sendRawCalls)
	oldTimeout := vault.DelegationReceiptWaitTimeout
	vault.DelegationReceiptWaitTimeout = 20 * time.Millisecond
	t.Cleanup(func() { vault.DelegationReceiptWaitTimeout = oldTimeout })
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_SignPendingDelegationRetryDoesNotRebroadcast"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "pending_delegation_retry_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	if err := db.Create(&storage.WalletDelegation{
		WalletUID:      wallet.WalletUID,
		UserID:         userID,
		WalletID:       wallet.WalletID,
		Chain:          "ethereum",
		Status:         "pending_delegation",
		DelegationHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}).Error; err != nil {
		t.Fatalf("mark delegation pending: %v", err)
	}
	bindHandlerAutoPolicy(t, db, userID, wallet.WalletID)
	intent := createSignIntent(t, db, userID, wallet)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("sign = %d, want 202 pending delegation: %s", w.Code, w.Body.String())
	}
	if got := sendRawCalls.Load(); got != 0 {
		t.Fatalf("pending delegation retry rebroadcasted %d delegation txs", got)
	}
}

func TestSSM_Sign_NoPolicyRejected(t *testing.T) {
	rpc := delegatedRPCServer(t)
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_Sign_NoPolicyRejected"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "nopolicy_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	intent := createSignIntent(t, db, userID, wallet)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("got %d, want policy error: %s", w.Code, w.Body.String())
	}
}

func TestSSM_Sign_ManualPolicyReturnsChallenge(t *testing.T) {
	rpc := delegatedRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	auth.SetExternalURL("https://api.web3.example.com")
	t.Cleanup(func() { auth.SetExternalURL("http://localhost:8080") })
	userID := "test_user_TestSSM_Sign_ManualPolicyReturnsChallenge"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "manual_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	bindHandlerManualPolicy(t, db, userID, wallet.WalletID)
	intent := createSignIntent(t, db, userID, wallet)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusAccepted {
		t.Errorf("got %d, want delegation error or 202: %s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusAccepted {
		t.Fatalf("manual policy sign = %d, want 202: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	challengeID := resp["challenge_id"].(string)
	if resp["transaction_id"] != intent.TransactionID {
		t.Fatalf("transaction_id = %v, want %s", resp["transaction_id"], intent.TransactionID)
	}
	wantSignURL := "https://api.web3.example.com/confirm/" + challengeID
	if resp["sign_url"] != wantSignURL {
		t.Fatalf("sign_url = %v, want %s", resp["sign_url"], wantSignURL)
	}
	var challenge storage.Challenge
	if err := db.First(&challenge, "challenge_id = ?", challengeID).Error; err != nil {
		t.Fatalf("load sign challenge: %v", err)
	}
	var envelope struct {
		Payload map[string]interface{} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(challenge.PayloadJSON), &envelope); err != nil {
		t.Fatalf("decode sign challenge payload: %v", err)
	}
	for key, want := range map[string]interface{}{
		"transaction_id": intent.TransactionID,
		"wallet_id":      wallet.WalletID,
		"chain":          intent.Chain,
		"to":             intent.To,
		"tx_type":        intent.TxType,
		"payload_hash":   intent.PayloadHash,
	} {
		if envelope.Payload[key] != want {
			t.Fatalf("challenge payload[%s] = %#v, want %#v in %s", key, envelope.Payload[key], want, challenge.PayloadJSON)
		}
	}
	security, ok := envelope.Payload["security"].(map[string]interface{})
	if !ok || security["verdict"] != "pass" {
		t.Fatalf("challenge security summary = %#v in %s", envelope.Payload["security"], challenge.PayloadJSON)
	}
}

func TestSSM_Sign_PendingIntentRequiresChallengeResume(t *testing.T) {
	rpc := delegatedRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_Sign_PendingIntentRequiresChallengeResume"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "manual_pending_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	bindHandlerManualPolicy(t, db, userID, wallet.WalletID)
	intent := createSignIntent(t, db, userID, wallet)

	first := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if first.Code != http.StatusAccepted {
		t.Fatalf("first sign = %d, want 202: %s", first.Code, first.Body.String())
	}

	second := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if second.Code != http.StatusConflict {
		t.Fatalf("second sign = %d, want 409: %s", second.Code, second.Body.String())
	}

	var challenges int64
	if err := db.Model(&storage.Challenge{}).Where("type = ? AND user_id = ?", "sign", userID).Count(&challenges).Error; err != nil {
		t.Fatalf("count challenges: %v", err)
	}
	if challenges != 1 {
		t.Fatalf("sign challenges = %d, want exactly 1", challenges)
	}
}

func TestSSM_PollSign_SignsApprovedChallenge(t *testing.T) {
	rpc := delegatedRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_PollSign_SignsApprovedChallenge"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "poll_sign_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	policyID := bindHandlerManualPolicy(t, db, userID, wallet.WalletID)

	intent := createSignIntent(t, db, userID, wallet)
	signPayload := map[string]interface{}{"transaction_id": intent.TransactionID}
	challengeID := approvedChallengeForPayload(t, db, userID, "sign", signPayload)

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/sign/"+challengeID, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("poll sign = %d, want 200: %s", w.Code, w.Body.String())
	}
	var signed struct {
		Status        string `json:"status"`
		TransactionID string `json:"transaction_id"`
		TxHash        string `json:"tx_hash"`
	}
	testutil.ParseJSON(t, w, &signed)
	if signed.Status != "signed" || signed.TransactionID != intent.TransactionID || signed.TxHash == "" {
		t.Fatalf("unexpected signed response: %+v body=%s", signed, w.Body.String())
	}
	var signedIntent storage.TransactionIntent
	if err := db.First(&signedIntent, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("load signed intent: %v", err)
	}
	signedHex, err := txintent.NewService(db, vault.NewTestKMSClient()).DecryptSignedTx(t.Context(), signedIntent)
	if err != nil {
		t.Fatalf("decrypt signed tx: %v", err)
	}
	tx := decodeSignedTx(t, signedHex)
	if tx.Nonce() != 7 {
		t.Fatalf("signed tx nonce = %d, want pending nonce 7", tx.Nonce())
	}
	var event storage.PolicyUsageEvent
	if err := db.First(&event, "tx_hash = ?", signed.TxHash).Error; err != nil {
		t.Fatalf("signed operation event missing: %v", err)
	}
	if event.Status != "signed" || event.PolicyID != policyID || event.AmountUSD.String() != "0" {
		t.Fatalf("unexpected signed operation event: %+v", event)
	}
	var instance storage.PolicyInstance
	if err := db.First(&instance, "policy_instance_id = ?", policyID).Error; err != nil {
		t.Fatalf("load policy instance: %v", err)
	}
	if instance.DailyTransferUsedUSD.String() != "0" || instance.AutoSignDailyUsedUSD.String() != "0" {
		t.Fatalf("signing should not mark submitted usage: %+v", instance)
	}

	w = testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/sign/"+challengeID, apiKey, nil)
	var repeated struct {
		Status        string `json:"status"`
		TransactionID string `json:"transaction_id"`
		TxHash        string `json:"tx_hash"`
	}
	testutil.ParseJSON(t, w, &repeated)
	if repeated != signed {
		t.Fatalf("completed challenge should return stored result, got %+v want %+v", repeated, signed)
	}
}

func TestSSM_RequestSign_ConsumesApprovedChallengeID(t *testing.T) {
	rpc := delegatedRPCServer(t)
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_RequestSign_ConsumesApprovedChallengeID"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "retry_sign_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	bindHandlerManualPolicy(t, db, userID, wallet.WalletID)
	intent := createSignIntent(t, db, userID, wallet)
	body := map[string]interface{}{"transaction_id": intent.TransactionID}
	challengeID := approvedChallengeForPayload(t, db, userID, "sign", body)
	retryBody := map[string]interface{}{"challenge_id": challengeID}

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, retryBody)
	if w.Code != http.StatusOK {
		t.Fatalf("retry sign = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	if resp["transaction_id"] != intent.TransactionID || resp["tx_hash"] == "" {
		t.Fatalf("retry sign missing transaction id or tx hash: %#v", resp)
	}
	var ch storage.Challenge
	if err := db.Where("challenge_id = ?", challengeID).First(&ch).Error; err != nil {
		t.Fatalf("load challenge: %v", err)
	}
	if ch.Status != "used" {
		t.Fatalf("challenge status = %s, want used", ch.Status)
	}
}

func TestSSM_RequestSign_RejectsChallengeForDifferentWallet(t *testing.T) {
	rpc := delegatedRPCServer(t)
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_RequestSign_RejectsChallengeForDifferentWallet"
	for _, walletID := range []string{"retry_wallet_a", "retry_wallet_b"} {
		wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, walletID)
		if err != nil {
			t.Fatalf("create wallet %s: %v", walletID, err)
		}
		bindHandlerManualPolicy(t, db, userID, wallet.WalletID)
	}
	var walletA storage.Wallet
	if err := db.First(&walletA, "wallet_id = ?", "retry_wallet_a").Error; err != nil {
		t.Fatalf("load wallet a: %v", err)
	}
	var walletB storage.Wallet
	if err := db.First(&walletB, "wallet_id = ?", "retry_wallet_b").Error; err != nil {
		t.Fatalf("load wallet b: %v", err)
	}
	intentA := createSignIntent(t, db, userID, &walletA)
	intentB := createSignIntent(t, db, userID, &walletB)
	body := map[string]interface{}{"transaction_id": intentA.TransactionID}
	challengeID := approvedChallengeForPayload(t, db, userID, "sign", body)
	body["transaction_id"] = intentB.TransactionID
	body["challenge_id"] = challengeID

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/ssm/sign", apiKey, body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("mismatched retry sign = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestSSM_PollSign_RecordsPriceUnknownSignedOperation(t *testing.T) {
	rpc := delegatedRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	userID := "test_user_TestSSM_PollSign_RecordsPriceUnknownSignedOperation"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "price_unknown_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	policyID := bindHandlerManualPolicy(t, db, userID, wallet.WalletID)

	intent := createSignIntent(t, db, userID, wallet)
	signPayload := map[string]interface{}{"transaction_id": intent.TransactionID}
	challengeID := approvedChallengeForPayload(t, db, userID, "sign", signPayload)

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/sign/"+challengeID, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("poll sign = %d, want 200: %s", w.Code, w.Body.String())
	}
	var signed struct {
		Status string `json:"status"`
		TxHash string `json:"tx_hash"`
	}
	testutil.ParseJSON(t, w, &signed)

	var event storage.PolicyUsageEvent
	if err := db.First(&event, "tx_hash = ?", signed.TxHash).Error; err != nil {
		t.Fatalf("signed operation event missing: %v", err)
	}
	if event.Status != "signed" || !event.PriceUnknown || event.PolicyID != policyID {
		t.Fatalf("unexpected price-unknown signed operation event: %+v", event)
	}
	var instance storage.PolicyInstance
	if err := db.First(&instance, "policy_instance_id = ?", policyID).Error; err != nil {
		t.Fatalf("load policy instance: %v", err)
	}
	if instance.DailyTransferUsedUSD.String() != "0" || instance.AutoSignDailyUsedUSD.String() != "0" {
		t.Fatalf("price-unknown signing should not mark submitted usage: %+v", instance)
	}
}

func TestSSM_PollSign_ConcurrentApprovedChallengeSignsOnce(t *testing.T) {
	rpc := delegatedRPCServer(t)
	defer rpc.Close()
	r, apiKey, db, v := setupSSMHandlerWithRPC(t, rpc.URL)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	userID := "test_user_TestSSM_PollSign_ConcurrentApprovedChallengeSignsOnce"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "poll_concurrent_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	policyID := bindHandlerManualPolicy(t, db, userID, wallet.WalletID)

	intent := createSignIntent(t, db, userID, wallet)
	signPayload := map[string]interface{}{"transaction_id": intent.TransactionID}
	challengeID := approvedChallengeForPayload(t, db, userID, "sign", signPayload)

	var ready sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	var signedResponses int32
	var completedResponses int32
	txHashes := make(map[string]struct{})
	var txHashesMu sync.Mutex
	for i := 0; i < 12; i++ {
		ready.Add(1)
		go func() {
			ready.Done()
			start.Wait()
			w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/sign/"+challengeID, apiKey, nil)
			if w.Code == http.StatusOK && strings.Contains(w.Body.String(), `"tx_hash"`) {
				atomic.AddInt32(&signedResponses, 1)
				var signed struct {
					TxHash string `json:"tx_hash"`
				}
				testutil.ParseJSON(t, w, &signed)
				txHashesMu.Lock()
				txHashes[signed.TxHash] = struct{}{}
				txHashesMu.Unlock()
			}
			if w.Code == http.StatusOK || w.Code == http.StatusConflict {
				atomic.AddInt32(&completedResponses, 1)
			}
		}()
	}
	ready.Wait()
	start.Done()
	time.Sleep(300 * time.Millisecond)

	txHashesMu.Lock()
	uniqueTxHashes := len(txHashes)
	txHashesMu.Unlock()
	if signedResponses == 0 || uniqueTxHashes != 1 {
		t.Fatalf("signed responses = %d unique tx hashes = %d, want pollers to share exactly one signed result", signedResponses, uniqueTxHashes)
	}
	var events int64
	if err := db.Model(&storage.PolicyUsageEvent{}).Where("policy_id = ?", policyID).Count(&events).Error; err != nil {
		t.Fatalf("count policy events: %v", err)
	}
	if events != 1 {
		t.Fatalf("signed operation events = %d, want exactly 1; completedResponses=%d", events, completedResponses)
	}
}

func TestSSM_PollSign_PendingChallengeDoesNotSign(t *testing.T) {
	r, apiKey, db, v := setupSSMHandler(t)
	userID := "test_user_TestSSM_PollSign_PendingChallengeDoesNotSign"
	wallet, err := v.CreateWallet(testutil.BackgroundCtx(), userID, "poll_pending_wallet")
	if err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	bindHandlerManualPolicy(t, db, userID, wallet.WalletID)
	payload, err := json.Marshal(map[string]interface{}{
		"challenge": "webauthn_challenge",
		"payload": map[string]interface{}{
			"transaction_id": createSignIntent(t, db, userID, wallet).TransactionID,
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	challengeID := "ch_sign_pending"
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID, UserID: userID, Type: "sign", Status: "pending",
		PayloadJSON: string(payload), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/ssm/sign/"+challengeID, apiKey, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("poll sign = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Status != "pending" {
		t.Fatalf("pending challenge should not sign, got %+v", resp)
	}
}

func bindHandlerManualPolicy(t *testing.T, db *gorm.DB, userID, walletID string) string {
	t.Helper()
	current := bindHandlerPolicy(t, db, userID, walletID, policyservice.DefinitionInput{
		Name: "manual",
		Mode: "manual",
	})
	return current.PolicyID
}

func bindHandlerAutoPolicy(t *testing.T, db *gorm.DB, userID, walletID string) string {
	t.Helper()
	current := bindHandlerPolicy(t, db, userID, walletID, policyservice.DefinitionInput{
		Name:                  "auto",
		Mode:                  "auto",
		MaxPerTransactionUSD:  usd("100"),
		DailyTransferLimitUSD: usd("1000"),
		DailyTradeLimitUSD:    usd("1000"),
		AutoSignEnabled:       true,
		AutoSignMaxPerTxUSD:   usd("100"),
		AutoSignDailyLimitUSD: usd("1000"),
		AutoSignMaxCountDay:   10,
	})
	return current.PolicyID
}

func usd(raw string) money.Amount {
	return money.MustParse(raw)
}

func bindHandlerPolicy(t *testing.T, db *gorm.DB, userID, walletID string, input policyservice.DefinitionInput) *policyservice.Policy {
	t.Helper()
	svc := policyservice.NewService(db)
	def, err := svc.CreatePolicyDefinition(userID, input)
	if err != nil {
		t.Fatalf("CreatePolicyDefinition: %v", err)
	}
	if _, err := svc.BindPolicyDefinition(userID, walletID, def.PolicyDefinitionID); err != nil {
		t.Fatalf("BindPolicyDefinition: %v", err)
	}
	current, err := svc.CurrentPolicy(userID, walletID, time.Now())
	if err != nil {
		t.Fatalf("CurrentPolicy: %v", err)
	}
	return current
}

func decodeSignedTx(t *testing.T, signedHex string) *types.Transaction {
	t.Helper()
	raw := common.FromHex(signedHex)
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		t.Fatalf("decode signed tx: %v", err)
	}
	return tx
}
