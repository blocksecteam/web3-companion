// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package chain

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/chain"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/money"
	policyservice "github.com/blocksecteam/web3-companion/internal/policy"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/txintent"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupChain(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	db := testutil.NewTestDB(t)
	_, apiKey := testutil.CreateTestUser(t, db)

	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, RPCURL: "https://eth.llamarpc.com"},
			{Name: "base", ChainID: 8453, RPCURL: "https://mainnet.base.org"},
		},
	}

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.MiddlewareForTest(db))
	chain.RegisterRoutes(api, cfg)
	return r, apiKey
}

type fakeChainRPC struct {
	sendHash  string
	receipt   *types.Receipt
	blockTime time.Time
}

func (f fakeChainRPC) SendRawTransaction(_ context.Context, _ string) (string, error) {
	return f.sendHash, nil
}

func (f fakeChainRPC) TransactionReceipt(_ context.Context, _ string) (*types.Receipt, error) {
	if f.receipt == nil {
		return nil, errors.New("not found")
	}
	return f.receipt, nil
}

func (f fakeChainRPC) BlockTimestamp(_ context.Context, _ *big.Int) (*time.Time, error) {
	return &f.blockTime, nil
}

func setupChainTransactions(t *testing.T, db *gorm.DB, rpc fakeChainRPC, jwtSecret []byte) (*gin.Engine, string, string) {
	t.Helper()
	userID, _ := testutil.CreateTestUser(t, db)
	tokenPair, err := auth.NewTokenService(db, jwtSecret).Issue(t.Context(), auth.SessionRequest{
		UserID: userID,
	})
	if err != nil {
		t.Fatalf("issue chain test jwt: %v", err)
	}
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, RPCURL: "test://ethereum"},
		},
	}

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(auth.Middleware(db, auth.WithJWTSecret(jwtSecret)))
	chain.RegisterAuthenticatedRoutesWithOptions(
		api,
		cfg,
		chain.WithTransactionStore(db),
		chain.WithKMSClient(vault.NewTestKMSClient()),
		chain.WithRPCClientFactory(func(*config.ChainConfig) chain.TransactionRPC { return rpc }),
	)
	return r, userID, tokenPair.AccessToken
}

func TestChain_GasPrice_Ethereum(t *testing.T) {
	r, apiKey := setupChain(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/chain/gas/ethereum", apiKey, nil)
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 200 or 503: %s", w.Code, w.Body.String())
	}
}

func TestChain_GasPrice_ByID(t *testing.T) {
	r, apiKey := setupChain(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/chain/gas/1", apiKey, nil)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 200 or 503: %s", w.Code, w.Body.String())
	}
}

func TestChain_GasPrice_BaseChain(t *testing.T) {
	r, apiKey := setupChain(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/chain/gas/base", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Logf("base gas: %s", w.Body.String())
	}
}

func TestChain_GasPrice_UnknownChain(t *testing.T) {
	r, apiKey := setupChain(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/chain/gas/solana", apiKey, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestChain_GasPrice_Unauthorized(t *testing.T) {
	r, _ := setupChain(t)
	w := testutil.DoRequest(r, "GET", "/api/v1/chain/gas/ethereum", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestChain_Estimate_RequiredFields(t *testing.T) {
	r, apiKey := setupChain(t)
	tests := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"missing chain", map[string]interface{}{"from": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"}, 400},
		{"missing from", map[string]interface{}{"chain": "ethereum"}, 400},
		{"valid simple transfer", map[string]interface{}{
			"chain": "ethereum",
			"from":  "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
			"to":    "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
		}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/estimate", apiKey, tt.body)
			if tt.want == 0 {

				if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
					t.Errorf("got %d, want 200 or 503: %s", w.Code, w.Body.String())
				}
			} else if w.Code != tt.want {
				t.Errorf("got %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestChain_Estimate_UnknownChain(t *testing.T) {
	r, apiKey := setupChain(t)
	w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/estimate", apiKey, map[string]interface{}{
		"chain": "solana",
		"from":  "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestChain_Broadcast_RequiredFields(t *testing.T) {
	r, apiKey := setupChain(t)
	tests := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{"missing transaction_id", map[string]interface{}{}, 400},
		{"raw signed tx rejected", map[string]interface{}{"chain": "ethereum", "signed_tx": "0xdeadbeef"}, 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/broadcast", apiKey, tt.body)
			if w.Code != tt.want {
				t.Errorf("got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestChain_BroadcastRecordsTransaction(t *testing.T) {
	db := testutil.NewTestDB(t)
	signedHex, txHash := signedTestTx(t)
	rpc := fakeChainRPC{sendHash: txHash}
	r, userID, apiKey := setupChainTransactions(t, db, rpc, []byte(auth.TestJWTSecret))
	testutil.CreateTestWallet(t, db, userID, "broadcast_wallet", "0x1234567890123456789012345678901234567890")
	policyID := bindChainPolicy(t, db, userID, "broadcast_wallet")
	if err := db.Create(&storage.PolicyUsageEvent{
		EventID: "pue_broadcast", UserID: userID, WalletID: "broadcast_wallet", PolicyID: policyID,
		TxHash: txHash, Chain: "ethereum", TxType: "transfer", AmountUSD: money.MustParse("22"),
		Decision: "webauthn", Status: "signed", UsageDate: time.Now().UTC().Format("2006-01-02"),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create signed operation: %v", err)
	}
	intent := createSignedBroadcastIntent(t, db, userID, "broadcast_wallet", signedHex, txHash)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/broadcast", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("broadcast = %d: %s", w.Code, w.Body.String())
	}

	var tx storage.Transaction
	if err := db.Where("chain = ? AND hash = ?", "ethereum", txHash).First(&tx).Error; err != nil {
		t.Fatalf("transaction was not recorded: %v", err)
	}
	if tx.UserID != userID || tx.Status != "pending" || tx.SubmittedAt == nil {
		t.Fatalf("unexpected stored transaction: %#v", tx)
	}
	if tx.WalletID != "broadcast_wallet" || tx.TxType != "transfer" {
		t.Fatalf("transaction missing policy metadata: %#v", tx)
	}
	var event storage.PolicyUsageEvent
	if err := db.First(&event, "event_id = ?", "pue_broadcast").Error; err != nil {
		t.Fatalf("load policy event: %v", err)
	}
	if event.Status != "submitted" || event.SubmittedAt == nil {
		t.Fatalf("usage event was not submitted: %#v", event)
	}
}

func bindChainPolicy(t *testing.T, db *gorm.DB, userID, walletID string) string {
	t.Helper()
	svc := policyservice.NewService(db)
	def, err := svc.CreatePolicyDefinition(userID, policyservice.DefinitionInput{Name: "default", Mode: "manual"})
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
	return current.PolicyID
}

func TestChain_BroadcastRejectsUntrackedSignedTransaction(t *testing.T) {
	db := testutil.NewTestDB(t)
	signedHex, txHash := signedTestTx(t)
	r, userID, apiKey := setupChainTransactions(t, db, fakeChainRPC{sendHash: txHash}, []byte(auth.TestJWTSecret))
	testutil.CreateTestWallet(t, db, userID, "untracked_wallet", "0x1234567890123456789012345678901234567890")
	intent := createSignedBroadcastIntent(t, db, userID, "untracked_wallet", signedHex, txHash)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/broadcast", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("broadcast = %d, want 403: %s", w.Code, w.Body.String())
	}
	var txCount int64
	db.Model(&storage.Transaction{}).Where("chain = ? AND hash = ?", "ethereum", txHash).Count(&txCount)
	if txCount != 0 {
		t.Fatalf("untracked broadcast should not record transaction, count=%d", txCount)
	}
}

func TestChain_BroadcastRejectsExpiredIntent(t *testing.T) {
	db := testutil.NewTestDB(t)
	signedHex, txHash := signedTestTx(t)
	r, userID, apiKey := setupChainTransactions(t, db, fakeChainRPC{sendHash: txHash}, []byte(auth.TestJWTSecret))
	testutil.CreateTestWallet(t, db, userID, "expired_wallet", "0x1234567890123456789012345678901234567890")
	if err := db.Create(&storage.PolicyUsageEvent{
		EventID: "pue_expired", UserID: userID, WalletID: "expired_wallet", PolicyID: "pol_expired",
		TxHash: txHash, Chain: "ethereum", TxType: "transfer", AmountUSD: money.MustParse("22"),
		Decision: "webauthn", Status: "signed", UsageDate: time.Now().UTC().Format("2006-01-02"),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create signed operation: %v", err)
	}
	intent := createSignedBroadcastIntent(t, db, userID, "expired_wallet", signedHex, txHash)
	intent.ExpiresAt = time.Now().UTC().Add(-time.Second)
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("expire intent: %v", err)
	}

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/broadcast", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusGone {
		t.Fatalf("broadcast expired intent = %d, want 410: %s", w.Code, w.Body.String())
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if stored.Status != txintent.StatusExpired {
		t.Fatalf("expired intent status = %q, want %q", stored.Status, txintent.StatusExpired)
	}
}

func TestChain_BroadcastReleasesClaimOnUnsupportedIntentChain(t *testing.T) {
	db := testutil.NewTestDB(t)
	signedHex, txHash := signedTestTx(t)
	r, userID, apiKey := setupChainTransactions(t, db, fakeChainRPC{sendHash: txHash}, []byte(auth.TestJWTSecret))
	testutil.CreateTestWallet(t, db, userID, "unsupported_chain_wallet", "0x1234567890123456789012345678901234567890")
	intent := createSignedBroadcastIntentOnChain(t, db, userID, "unsupported_chain_wallet", "solana", 999, signedHex, txHash)

	w := testutil.DoAuthRequest(r, "POST", "/api/v1/chain/broadcast", apiKey, map[string]interface{}{
		"transaction_id": intent.TransactionID,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("broadcast unsupported chain = %d, want 400: %s", w.Code, w.Body.String())
	}

	var stored storage.TransactionIntent
	if err := db.First(&stored, "transaction_id = ?", intent.TransactionID).Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	if stored.Status != txintent.StatusSigned {
		t.Fatalf("intent status after unsupported chain = %q, want %q", stored.Status, txintent.StatusSigned)
	}
}

func signedTestTx(t *testing.T) (string, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return signTxHex(t, key, types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     1,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(2),
		Gas:       21000,
		To:        ptrAddress(common.HexToAddress("0x1234567890123456789012345678901234567890")),
		Value:     big.NewInt(0),
	}))
}

func signTxHex(t *testing.T, key *ecdsa.PrivateKey, tx *types.Transaction) (string, string) {
	t.Helper()
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(1)), key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal signed tx: %v", err)
	}
	return "0x" + common.Bytes2Hex(raw), signed.Hash().Hex()
}

func createSignedBroadcastIntent(t *testing.T, db *gorm.DB, userID, walletID, signedHex, txHash string) storage.TransactionIntent {
	t.Helper()
	return createSignedBroadcastIntentOnChain(t, db, userID, walletID, "ethereum", 1, signedHex, txHash)
}

func createSignedBroadcastIntentOnChain(t *testing.T, db *gorm.DB, userID, walletID, chainName string, chainID int64, signedHex, txHash string) storage.TransactionIntent {
	t.Helper()
	intent, err := txintent.NewService(db, vault.NewTestKMSClient()).Create(t.Context(), txintent.CreateRequest{
		UserID:    userID,
		WalletID:  walletID,
		WalletUID: "wuid_" + walletID,
		Chain:     chainName,
		ChainID:   chainID,
		TxType:    "transfer",
		Owner:     "0x1234567890123456789012345678901234567890",
		To:        "0x1234567890123456789012345678901234567890",
		Value:     "0",
		Data:      "0x",
		GasLimit:  21000,
		Recipient: "0x1234567890123456789012345678901234567890",
	})
	if err != nil {
		t.Fatalf("create broadcast intent: %v", err)
	}
	intent.Status = txintent.StatusSecurityPassed
	intent.SecurityVerdict = "pass"
	if err := db.Save(&intent).Error; err != nil {
		t.Fatalf("save intent security: %v", err)
	}
	intent, err = txintent.NewService(db, vault.NewTestKMSClient()).StoreSignedTx(t.Context(), intent.TransactionID, userID, txintent.SignedTx{
		Hex: signedHex, Hash: txHash, PolicyID: "pol_broadcast", PolicyDecision: "webauthn",
	})
	if err != nil {
		t.Fatalf("store signed tx on intent: %v", err)
	}
	return intent
}

func ptrAddress(addr common.Address) *common.Address {
	return &addr
}

func TestChain_Transaction_NotFound(t *testing.T) {
	r, apiKey := setupChain(t)

	w := testutil.DoAuthRequest(r, "GET", "/api/v1/chain/transaction/ethereum/0x00000000000000000000000000000000000000000000000000000000000000ab", apiKey, nil)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200 (should return not_found status)", w.Code)
	}
}

func TestChain_Transaction_UnknownChain(t *testing.T) {
	r, apiKey := setupChain(t)
	w := testutil.DoAuthRequest(r, "GET", "/api/v1/chain/transaction/solana/0xabc", apiKey, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestChain_TransactionsListsCurrentJWTUserAndRefreshesPending(t *testing.T) {
	db := testutil.NewTestDB(t)
	rpc := fakeChainRPC{
		receipt: &types.Receipt{
			Status:      1,
			BlockNumber: big.NewInt(42),
		},
		blockTime: time.Date(2026, 5, 15, 12, 1, 30, 0, time.UTC),
	}
	r, userID, _ := setupChainTransactions(t, db, rpc, []byte(auth.TestJWTSecret))
	otherID := "other_user_" + t.Name()
	if err := db.Create(&storage.User{
		ID:           otherID,
		Email:        otherID + "@test.local",
		PasskeySetup: true,
		Name:         "other",
	}).Error; err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	submittedAt := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	if err := db.Create(&storage.Transaction{
		Chain:       "ethereum",
		Hash:        "0x2222222222222222222222222222222222222222222222222222222222222222",
		UserID:      userID,
		Status:      "pending",
		SubmittedAt: &submittedAt,
	}).Error; err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	if err := db.Create(&storage.Transaction{
		Chain:       "ethereum",
		Hash:        "0x3333333333333333333333333333333333333333333333333333333333333333",
		UserID:      otherID,
		Status:      "pending",
		SubmittedAt: &submittedAt,
	}).Error; err != nil {
		t.Fatalf("seed other transaction: %v", err)
	}
	tokenPair, err := auth.NewTokenService(db, []byte(auth.TestJWTSecret)).Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientBrowser,
		Scope:      auth.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue jwt: %v", err)
	}

	w := testutil.DoRequest(r, "GET", "/api/v1/chain/transactions", nil, map[string]string{
		"Authorization": "Bearer " + tokenPair.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("transactions = %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Transactions []struct {
			Chain       string  `json:"chain"`
			Hash        string  `json:"hash"`
			Status      string  `json:"status"`
			SubmittedAt string  `json:"submitted_at"`
			ConfirmedAt *string `json:"confirmed_at"`
		} `json:"transactions"`
	}
	testutil.ParseJSON(t, w, &response)
	if len(response.Transactions) != 1 {
		t.Fatalf("transactions length = %d, want 1: %#v", len(response.Transactions), response.Transactions)
	}
	got := response.Transactions[0]
	if got.Hash != "0x2222222222222222222222222222222222222222222222222222222222222222" || got.Status != "confirmed" {
		t.Fatalf("unexpected transaction row: %#v", got)
	}
	if got.ConfirmedAt == nil || *got.ConfirmedAt != "2026-05-15T12:01:30Z" {
		t.Fatalf("confirmed_at = %#v, want confirmation timestamp", got.ConfirmedAt)
	}
}

func TestChain_ResolveByName(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, RPCURL: "https://eth.llamarpc.com"},
			{Name: "base", ChainID: 8453, RPCURL: "https://mainnet.base.org"},
		},
	}
	ch := cfg.ChainByName("Ethereum")
	if ch == nil || ch.ChainID != 1 {
		t.Error("chain resolution by name failed")
	}
}

func TestChain_ResolveByID(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, RPCURL: "https://eth.llamarpc.com"},
		},
	}
	ch := cfg.ChainByID(1)
	if ch == nil || ch.Name != "ethereum" {
		t.Error("chain resolution by ID failed")
	}
}

func TestChain_ResolveUnknown(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, RPCURL: "https://eth.llamarpc.com"},
		},
	}
	if ch := cfg.ChainByName("solana"); ch != nil {
		t.Error("expected nil for unknown chain")
	}
	if ch := cfg.ChainByID(999); ch != nil {
		t.Error("expected nil for unknown chain ID")
	}
}
