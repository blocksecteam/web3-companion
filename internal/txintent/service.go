// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package txintent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

const (
	StatusBuilt                  = "built"
	StatusSecurityPassed         = "security_passed"
	StatusSecurityOverridden     = "security_overridden"
	StatusSecurityReviewRequired = "security_review_required"
	StatusPendingDelegation      = "pending_delegation"
	StatusPendingUserAuth        = "pending_user_auth"
	StatusSigned                 = "signed"
	StatusBroadcasting           = "broadcasting"
	StatusSubmitted              = "submitted"
	StatusExpired                = "expired"
	// StatusFailed is reserved — the current broadcast failure path rolls
	// the intent back to StatusSigned via ReleaseBroadcastClaim
	// (chain/broadcast.go) so the user can retry the same intent. The
	// constant is intentionally retained as a sentinel for a future hard
	// terminal failure state (e.g. "RPC permanently unable to broadcast
	// this nonce"); if one is added, wire its producers to this constant
	// instead of re-defining the string literal.
	StatusFailed = "failed"

	// DefaultTTL bounds how long an intent stays signable after build. Five
	// minutes is chosen so that every stale-dependency window the build
	// captures — quote output amounts, bridge deposit addresses, slippage
	// applicability, gas price, on-chain state used by simulation — is
	// refreshed before sign rather than carried into broadcast. Past this
	// point the user is expected to re-quote (or re-issue the transfer
	// request for non-quote tx types) so they see the current price and
	// decide whether to proceed.
	DefaultTTL = 5 * time.Minute
)

var (
	ErrIntentNotFound       = errors.New("transaction intent not found")
	ErrIntentExpired        = errors.New("transaction intent expired")
	ErrSecurityNotSatisfied = errors.New("transaction security check has not passed")
	ErrSignedTxUnavailable  = errors.New("signed transaction unavailable")
	ErrInvalidTransition    = errors.New("invalid transaction intent state transition")
)

type Service struct {
	db  *gorm.DB
	kms *vault.KMSClient
	Now func() time.Time
}

func NewService(db *gorm.DB, kms *vault.KMSClient) *Service {
	return &Service{
		db:  db,
		kms: kms,
		Now: func() time.Time { return time.Now().UTC() },
	}
}

type CreateRequest struct {
	UserID         string
	WalletID       string
	WalletUID      string
	Chain          string
	ChainID        int64
	TxType         string
	Owner          string
	To             string
	Value          string
	Data           string
	GasLimit       uint64
	GasPrice       string // wei, decimal string; effective price observed at build time for security simulation
	GasPriceSource string // "rpc", "fallback", or "" — how GasPrice was obtained
	Recipient      string
	SummaryJSON    string
}

type SecurityResult struct {
	Verdict        string
	JSON           string
	RequiresReview bool
}

type SecurityClearance struct {
	Reviewer   string
	Decision   string
	Confidence string
	Reason     string
}

type SignedTx struct {
	Hex            string
	Hash           string
	PolicyID       string
	PolicyDecision string
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (storage.TransactionIntent, error) {
	now := s.now()
	intent := storage.TransactionIntent{
		TransactionID:  "txi_" + randomHex(14),
		UserID:         strings.TrimSpace(req.UserID),
		WalletID:       strings.TrimSpace(req.WalletID),
		WalletUID:      strings.TrimSpace(req.WalletUID),
		Chain:          strings.ToLower(strings.TrimSpace(req.Chain)),
		ChainID:        req.ChainID,
		TxType:         strings.TrimSpace(req.TxType),
		Owner:          strings.TrimSpace(req.Owner),
		To:             strings.TrimSpace(req.To),
		Value:          strings.TrimSpace(req.Value),
		Data:           strings.TrimSpace(req.Data),
		GasLimit:       req.GasLimit,
		GasPrice:       strings.TrimSpace(req.GasPrice),
		GasPriceSource: strings.TrimSpace(req.GasPriceSource),
		Recipient:      strings.TrimSpace(req.Recipient),
		SummaryJSON:    req.SummaryJSON,
		Status:         StatusBuilt,
		ExpiresAt:      now.Add(DefaultTTL),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if intent.Value == "" {
		intent.Value = "0"
	}
	if intent.Data == "" {
		intent.Data = "0x"
	}
	if intent.GasLimit == 0 {
		intent.GasLimit = 21000
	}
	if intent.Recipient == "" {
		intent.Recipient = intent.To
	}
	if err := validateCreate(intent); err != nil {
		return storage.TransactionIntent{}, err
	}
	intent.PayloadHash = IntentPayloadHash(intent)
	if err := s.db.WithContext(ctx).Create(&intent).Error; err != nil {
		return storage.TransactionIntent{}, err
	}
	return intent, nil
}

func (s *Service) Get(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	var intent storage.TransactionIntent
	err := s.db.WithContext(ctx).
		Where("transaction_id = ? AND user_id = ?", strings.TrimSpace(transactionID), strings.TrimSpace(userID)).
		First(&intent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.TransactionIntent{}, ErrIntentNotFound
	}
	return intent, err
}

func (s *Service) LoadForSigning(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	intent, err := s.Get(ctx, transactionID, userID)
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.ensureNotExpired(ctx, &intent); err != nil {
		return storage.TransactionIntent{}, ErrIntentExpired
	}
	if IntentPayloadHash(intent) != strings.TrimSpace(intent.PayloadHash) {
		return storage.TransactionIntent{}, fmt.Errorf("transaction payload hash mismatch")
	}
	if !SecurityAllowsSigning(intent) {
		return storage.TransactionIntent{}, ErrSecurityNotSatisfied
	}
	return intent, nil
}

func (s *Service) LoadForBroadcast(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	intent, err := s.Get(ctx, transactionID, userID)
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.ensureNotExpired(ctx, &intent); err != nil {
		return storage.TransactionIntent{}, ErrIntentExpired
	}
	if IntentPayloadHash(intent) != strings.TrimSpace(intent.PayloadHash) {
		return storage.TransactionIntent{}, fmt.Errorf("transaction payload hash mismatch")
	}
	return intent, nil
}

func (s *Service) RecordSecurityResult(ctx context.Context, transactionID, userID string, result SecurityResult) (storage.TransactionIntent, error) {
	intent, err := s.Get(ctx, transactionID, userID)
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.ensureNotExpired(ctx, &intent); err != nil {
		return storage.TransactionIntent{}, ErrIntentExpired
	}
	allowed := []string{StatusBuilt, StatusSecurityPassed, StatusSecurityReviewRequired}
	if !canRecordSecurity(intent.Status) {
		return storage.TransactionIntent{}, ErrInvalidTransition
	}
	status := StatusSecurityReviewRequired
	switch strings.ToLower(strings.TrimSpace(result.Verdict)) {
	case "pass":
		if result.RequiresReview {
			status = StatusSecurityReviewRequired
		} else {
			status = StatusSecurityPassed
		}
	case "fail":
		status = StatusSecurityReviewRequired
	case "warn":
		status = StatusSecurityReviewRequired
	default:
		status = StatusSecurityReviewRequired
	}
	updates := map[string]interface{}{
		"security_verdict": strings.ToLower(strings.TrimSpace(result.Verdict)),
		"security_result":  result.JSON,
		"status":           status,
		"updated_at":       s.now(),
	}
	if err := s.updateIntentCAS(ctx, transactionID, userID, allowed, updates); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func (s *Service) ClearSecurityWarning(ctx context.Context, transactionID, userID string, clearance SecurityClearance) (storage.TransactionIntent, error) {
	return s.ClearSecurityReview(ctx, transactionID, userID, clearance)
}

func (s *Service) ClearSecurityReview(ctx context.Context, transactionID, userID string, clearance SecurityClearance) (storage.TransactionIntent, error) {
	intent, err := s.Get(ctx, transactionID, userID)
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.ensureNotExpired(ctx, &intent); err != nil {
		return storage.TransactionIntent{}, ErrIntentExpired
	}
	if intent.Status != StatusSecurityReviewRequired {
		return storage.TransactionIntent{}, ErrSecurityNotSatisfied
	}
	if !securityClearanceAllowsSigning(clearance) {
		return storage.TransactionIntent{}, ErrSecurityNotSatisfied
	}
	status := StatusSecurityPassed
	if isUserSecurityOverride(clearance) {
		status = StatusSecurityOverridden
	}
	now := s.now()
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{StatusSecurityReviewRequired}, map[string]interface{}{
		"status":                status,
		"security_confirmed_at": &now,
		"updated_at":            now,
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func securityClearanceAllowsSigning(clearance SecurityClearance) bool {
	decision := strings.ToLower(strings.TrimSpace(clearance.Decision))
	if isUserSecurityOverride(clearance) {
		return true
	}
	if decision != "safe" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(clearance.Confidence)) {
	case "medium", "high":
		return true
	default:
		return false
	}
}

func isUserSecurityOverride(clearance SecurityClearance) bool {
	return strings.ToLower(strings.TrimSpace(clearance.Reviewer)) == "user_override" &&
		strings.ToLower(strings.TrimSpace(clearance.Decision)) == "override" &&
		strings.TrimSpace(clearance.Reason) != ""
}

func (s *Service) MarkPendingUserAuth(ctx context.Context, transactionID, userID, challengeID, policyID, decision string) (storage.TransactionIntent, error) {
	if _, err := s.Get(ctx, transactionID, userID); err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{
		StatusSecurityPassed,
		StatusSecurityOverridden,
		StatusPendingUserAuth,
	}, map[string]interface{}{
		"status":          StatusPendingUserAuth,
		"challenge_id":    strings.TrimSpace(challengeID),
		"policy_id":       strings.TrimSpace(policyID),
		"policy_decision": strings.TrimSpace(decision),
		"updated_at":      s.now(),
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func (s *Service) MarkPendingDelegation(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	if _, err := s.Get(ctx, transactionID, userID); err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{
		StatusSecurityPassed,
		StatusSecurityOverridden,
		StatusPendingDelegation,
	}, map[string]interface{}{
		"status":     StatusPendingDelegation,
		"updated_at": s.now(),
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func (s *Service) StoreSignedTx(ctx context.Context, transactionID, userID string, signed SignedTx) (storage.TransactionIntent, error) {
	if _, err := s.Get(ctx, transactionID, userID); err != nil {
		return storage.TransactionIntent{}, err
	}
	if s.kms == nil {
		return storage.TransactionIntent{}, fmt.Errorf("kms unavailable")
	}
	dataKey, encryptedDataKey, err := s.kms.GenerateDataKey(ctx)
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	defer vault.ZeroBytes(dataKey)
	ciphertext, err := vault.Encrypt(dataKey, []byte(strings.TrimSpace(signed.Hex)))
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	now := s.now()
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{
		StatusSecurityPassed,
		StatusSecurityOverridden,
		StatusPendingUserAuth,
		StatusPendingDelegation,
	}, map[string]interface{}{
		"status":              StatusSigned,
		"tx_hash":             strings.ToLower(strings.TrimSpace(signed.Hash)),
		"signed_tx_encrypted": ciphertext,
		"signed_tx_data_key":  encryptedDataKey,
		"policy_id":           strings.TrimSpace(signed.PolicyID),
		"policy_decision":     strings.TrimSpace(signed.PolicyDecision),
		"updated_at":          now,
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func (s *Service) DecryptSignedTx(ctx context.Context, intent storage.TransactionIntent) (string, error) {
	if len(intent.SignedTxEncrypted) == 0 || len(intent.SignedTxDataKey) == 0 {
		return "", ErrSignedTxUnavailable
	}
	if s.kms == nil {
		return "", fmt.Errorf("kms unavailable")
	}
	dataKey, err := s.kms.Decrypt(ctx, intent.SignedTxDataKey)
	if err != nil {
		return "", err
	}
	defer vault.ZeroBytes(dataKey)
	plain, err := vault.Decrypt(dataKey, intent.SignedTxEncrypted)
	if err != nil {
		return "", err
	}
	defer vault.ZeroBytes(plain)
	return string(plain), nil
}

func (s *Service) ClaimBroadcast(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	intent, err := s.Get(ctx, transactionID, userID)
	if err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.ensureNotExpired(ctx, &intent); err != nil {
		return storage.TransactionIntent{}, ErrIntentExpired
	}
	if IntentPayloadHash(intent) != strings.TrimSpace(intent.PayloadHash) {
		return storage.TransactionIntent{}, fmt.Errorf("transaction payload hash mismatch")
	}
	now := s.now()
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{StatusSigned}, map[string]interface{}{
		"status":     StatusBroadcasting,
		"updated_at": now,
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func (s *Service) ReleaseBroadcastClaim(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	if _, err := s.Get(ctx, transactionID, userID); err != nil {
		return storage.TransactionIntent{}, err
	}
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{StatusBroadcasting}, map[string]interface{}{
		"status":     StatusSigned,
		"updated_at": s.now(),
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

// MarkSubmittedByHash lazily reconciles drift left by a broadcast whose RPC
// succeeded but whose follow-up MarkSubmitted DB write failed. Any intent of
// `userID` with this tx hash on this chain still stuck in `broadcasting` is
// advanced to `submitted`. Best-effort: silently returns nil if no matching
// intent exists.
func (s *Service) MarkSubmittedByHash(ctx context.Context, userID, txHash, chain string) error {
	if s == nil || s.db == nil || userID == "" || txHash == "" || chain == "" {
		return nil
	}
	now := s.now()
	return s.db.WithContext(ctx).Model(&storage.TransactionIntent{}).
		Where("user_id = ? AND LOWER(tx_hash) = ? AND LOWER(chain) = ? AND status = ?",
			userID, strings.ToLower(strings.TrimSpace(txHash)), strings.ToLower(strings.TrimSpace(chain)), StatusBroadcasting).
		Updates(map[string]interface{}{
			"status":       StatusSubmitted,
			"submitted_at": &now,
			"updated_at":   now,
		}).Error
}

func (s *Service) MarkSubmitted(ctx context.Context, transactionID, userID string) (storage.TransactionIntent, error) {
	if _, err := s.Get(ctx, transactionID, userID); err != nil {
		return storage.TransactionIntent{}, err
	}
	now := s.now()
	if err := s.updateIntentCAS(ctx, transactionID, userID, []string{StatusBroadcasting}, map[string]interface{}{
		"status":       StatusSubmitted,
		"submitted_at": &now,
		"updated_at":   now,
	}); err != nil {
		return storage.TransactionIntent{}, err
	}
	return s.Get(ctx, transactionID, userID)
}

func SecurityAllowsSigning(intent storage.TransactionIntent) bool {
	return intent.Status == StatusSecurityPassed ||
		intent.Status == StatusSecurityOverridden ||
		intent.Status == StatusPendingDelegation ||
		intent.Status == StatusPendingUserAuth
}

func canRecordSecurity(status string) bool {
	switch status {
	case StatusBuilt, StatusSecurityPassed, StatusSecurityReviewRequired:
		return true
	default:
		return false
	}
}

func (s *Service) updateIntentCAS(ctx context.Context, transactionID, userID string, from []string, updates map[string]interface{}) error {
	if len(from) == 0 {
		return ErrInvalidTransition
	}
	result := s.db.WithContext(ctx).Model(&storage.TransactionIntent{}).
		Where("transaction_id = ? AND user_id = ? AND status IN ?", strings.TrimSpace(transactionID), strings.TrimSpace(userID), from).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrInvalidTransition
	}
	return nil
}

// IntentPayloadHash computes the anti-tamper hash of a TransactionIntent's
// load-bearing fields (wallet identity, chain, to/value/data, gas, recipient).
// Stored on the row at creation and re-checked before every state transition.
//
// Named `IntentPayloadHash` rather than `PayloadHash` to avoid collision with
// `passkey.PayloadHash`, which serves a different purpose (passkey challenge
// payload integrity) and uses canonical-JSON key ordering rather than the
// struct-declaration ordering this function relies on. Mixing the two would
// produce silent hash mismatches.
func IntentPayloadHash(intent storage.TransactionIntent) string {
	payload := struct {
		WalletID  string `json:"wallet_id"`
		WalletUID string `json:"wallet_uid"`
		Chain     string `json:"chain"`
		ChainID   int64  `json:"chain_id"`
		TxType    string `json:"tx_type"`
		Owner     string `json:"owner"`
		To        string `json:"to"`
		Value     string `json:"value"`
		Data      string `json:"data"`
		GasLimit  uint64 `json:"gas_limit"`
		Recipient string `json:"recipient"`
	}{
		WalletID:  intent.WalletID,
		WalletUID: strings.TrimSpace(intent.WalletUID),
		Chain:     strings.ToLower(strings.TrimSpace(intent.Chain)),
		ChainID:   intent.ChainID,
		TxType:    strings.TrimSpace(intent.TxType),
		Owner:     strings.TrimSpace(intent.Owner),
		To:        strings.TrimSpace(intent.To),
		Value:     strings.TrimSpace(intent.Value),
		Data:      strings.TrimSpace(intent.Data),
		GasLimit:  intent.GasLimit,
		Recipient: strings.TrimSpace(intent.Recipient),
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateCreate(intent storage.TransactionIntent) error {
	if intent.UserID == "" || intent.WalletID == "" || intent.WalletUID == "" || intent.Chain == "" || intent.ChainID <= 0 || intent.TxType == "" || intent.To == "" {
		return fmt.Errorf("incomplete transaction intent")
	}
	return nil
}

func (s *Service) isExpired(intent storage.TransactionIntent) bool {
	return !intent.ExpiresAt.After(s.now())
}

func (s *Service) ensureNotExpired(ctx context.Context, intent *storage.TransactionIntent) error {
	if intent == nil || !s.isExpired(*intent) {
		return nil
	}
	// Don't clobber statuses that represent on-chain progress or terminal
	// state. Flipping `broadcasting`/`submitted` to `expired` detaches the
	// intent from the on-chain tx and bypasses MarkSubmittedByHash drift
	// recovery (which filters on status="broadcasting"). `failed`/`expired`
	// are already terminal — no need to rewrite. Pre-broadcast states
	// (built, security_*, pending_*, signed) still clobber to expired
	// because no on-chain effect exists yet.
	switch intent.Status {
	case StatusBroadcasting, StatusSubmitted, StatusFailed, StatusExpired:
		return ErrIntentExpired
	}
	now := s.now()
	result := s.db.WithContext(ctx).Model(intent).
		Where("status NOT IN ?", []string{
			StatusBroadcasting, StatusSubmitted, StatusFailed, StatusExpired,
		}).
		Updates(map[string]interface{}{
			"status":     StatusExpired,
			"updated_at": now,
		})
	if result.Error == nil && result.RowsAffected > 0 {
		intent.Status = StatusExpired
		intent.UpdatedAt = now
	}
	return ErrIntentExpired
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
