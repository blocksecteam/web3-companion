// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package passkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/internal/webauthnutil"
	"gorm.io/gorm"
)

var (
	ErrCredentialNotFound       = errors.New("credential not found")
	ErrChallengeExpired         = errors.New("challenge expired")
	ErrChallengeCancelled       = errors.New("challenge cancelled")
	ErrChallengeAlreadyUsed     = errors.New("challenge already used")
	ErrSignCountInvalid         = errors.New("sign count not increased")
	ErrChallengePayloadMismatch = errors.New("challenge payload mismatch")
	ErrChallengeTypeMismatch    = errors.New("challenge type mismatch")
)

type Server struct {
	db  *gorm.DB
	cfg *config.WebAuthnConfig
}

func NewServer(db *gorm.DB, cfg *config.WebAuthnConfig) *Server {
	return &Server{db: db, cfg: cfg}
}

type RegistrationOptions struct {
	Challenge              string                 `json:"challenge"`
	RP                     RelyingParty           `json:"rp"`
	User                   UserEntity             `json:"user"`
	PubKeyCredParams       []PubKeyCredParam      `json:"pubKeyCredParams"`
	Timeout                int                    `json:"timeout"`
	Attestation            string                 `json:"attestation"`
	AuthenticatorSelection AuthenticatorSelection `json:"authenticatorSelection"`
}

type RelyingParty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserEntity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type PubKeyCredParam struct {
	Type string `json:"type"`
	Alg  int    `json:"alg"`
}

type AuthenticatorSelection struct {
	UserVerification string `json:"userVerification"`
	ResidentKey      string `json:"residentKey"`
}

func (s *Server) BeginRegistration(userID, userName string) (*storage.Challenge, *RegistrationOptions, error) {
	challenge := randomBytes(32)
	challengeID := "ch_" + base64.RawURLEncoding.EncodeToString(randomBytes(16))
	challengeB64 := base64.RawURLEncoding.EncodeToString(challenge)

	ch := &storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        "webauthn_register",
		Status:      "pending",
		PayloadJSON: mustJSON(map[string]string{
			"challenge": challengeB64,
		}),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.registrationTimeout()),
	}
	if err := s.db.Create(ch).Error; err != nil {
		return nil, nil, fmt.Errorf("create challenge: %w", err)
	}

	rpID := s.cfg.RPID
	if rpID == "" {
		rpID = "localhost"
	}
	opts := &RegistrationOptions{
		Challenge: challengeB64,
		RP: RelyingParty{
			ID:   rpID,
			Name: s.cfg.RPName,
		},
		User: UserEntity{
			ID:          base64.RawURLEncoding.EncodeToString([]byte(userID)),
			Name:        userName,
			DisplayName: userName,
		},
		PubKeyCredParams: []PubKeyCredParam{
			{Type: "public-key", Alg: -7},
			{Type: "public-key", Alg: -257},
		},
		Timeout:     int(s.registrationTimeout().Milliseconds()),
		Attestation: "none",
		AuthenticatorSelection: AuthenticatorSelection{
			UserVerification: "required",
			ResidentKey:      "required",
		},
	}

	return ch, opts, nil
}

func (s *Server) CompleteRegistrationWithAttestation(userID, challengeID, credentialID, clientDataJSON, attestationObject string, deviceName string) error {
	var ch storage.Challenge
	if err := s.db.Where("challenge_id = ? AND user_id = ?", challengeID, userID).First(&ch).Error; err != nil {
		return fmt.Errorf("challenge not found: %w", err)
	}
	if !isRegistrationChallengeType(ch.Type) {
		return ErrChallengeTypeMismatch
	}
	if time.Now().After(ch.ExpiresAt) {
		return ErrChallengeExpired
	}
	if ch.Status != "pending" {
		return errors.New("challenge already completed")
	}

	payload := map[string]interface{}{}
	_ = json.Unmarshal([]byte(ch.PayloadJSON), &payload)
	expectedChallenge := stringField(payload, "challenge")
	if _, _, err := webauthnutil.ValidateClientData(clientDataJSON, "webauthn.create", expectedChallenge, s.cfg.RPOrigin); err != nil {
		return err
	}
	att, err := webauthnutil.ParseAttestationObject(attestationObject, s.cfg.RPID)
	if err != nil {
		return err
	}
	if att.CredentialID != credentialID {
		return fmt.Errorf("credential ID mismatch")
	}

	cred := &storage.WebAuthnCredential{
		CredentialID: credentialID,
		UserID:       userID,
		PublicKey:    att.PublicKeyDER,
		Name:         deviceName,
		DeviceName:   deviceName,
		AAGUID:       att.AAGUID,
		SignCount:    att.SignCount,
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
	}
	if err := s.db.Create(cred).Error; err != nil {
		return fmt.Errorf("store credential: %w", err)
	}

	if err := s.db.Model(&ch).Update("status", "approved").Error; err != nil {
		return fmt.Errorf("approve challenge: %w", err)
	}

	return nil
}

type AuthenticationOptions struct {
	Challenge        string   `json:"challenge"`
	AllowCredentials []string `json:"allowCredentials"`
	Timeout          int      `json:"timeout"`
	UserVerification string   `json:"userVerification"`
	RPID             string   `json:"rpId"`
}

type ChallengeSummary struct {
	Operation string                  `json:"operation"`
	Title     string                  `json:"title"`
	WalletID  string                  `json:"wallet_id,omitempty"`
	PolicyID  string                  `json:"policy_id,omitempty"`
	Details   []ChallengeSummaryField `json:"details,omitempty"`
}

type ChallengeSummaryField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (s *Server) BeginAuthentication(userID, challengeID string) (*AuthenticationOptions, error) {
	ch, err := s.GetChallengeForUser(userID, challengeID)
	if err != nil {
		return nil, fmt.Errorf("challenge not found: %w", err)
	}
	if ch.Status == "cancelled" {
		return nil, ErrChallengeCancelled
	}
	if ch.Status != "pending" {
		return nil, ErrChallengeAlreadyUsed
	}
	if isRegistrationChallengeType(ch.Type) {
		return nil, ErrChallengeTypeMismatch
	}
	if s.expireChallengeIfNeeded(ch) {
		return nil, ErrChallengeExpired
	}
	payload := map[string]interface{}{}
	_ = json.Unmarshal([]byte(ch.PayloadJSON), &payload)
	challengeB64 := stringField(payload, "auth_challenge")
	if challengeB64 == "" {
		challenge := randomBytes(32)
		challengeB64 = base64.RawURLEncoding.EncodeToString(challenge)
		payload["auth_challenge"] = challengeB64

		if err := s.db.Model(&storage.Challenge{}).
			Where("challenge_id = ? AND user_id = ? AND status = ?", challengeID, userID, "pending").
			Update("payload", mustJSON(payload)).Error; err != nil {
			return nil, err
		}
	}
	creds, err := s.GetCredentials(userID)
	if err != nil {
		return nil, err
	}

	opts := &AuthenticationOptions{
		Challenge:        challengeB64,
		AllowCredentials: credentialIDs(creds),
		Timeout:          int(s.cfg.ChallengeTimeoutSeconds * 1000),
		UserVerification: "required",
		RPID:             s.cfg.RPID,
	}

	return opts, nil
}

func (s *Server) VerifyAssertion(userID, challengeID, credentialID, clientDataJSON, authenticatorData, signature string) error {
	var ch storage.Challenge
	if err := s.db.Where("challenge_id = ? AND user_id = ?", challengeID, userID).First(&ch).Error; err != nil {
		return fmt.Errorf("challenge not found: %w", err)
	}
	if isRegistrationChallengeType(ch.Type) {
		return ErrChallengeTypeMismatch
	}
	if ch.Status == "cancelled" {
		return ErrChallengeCancelled
	}
	if time.Now().After(ch.ExpiresAt) {
		_ = s.ExpireChallenge(challengeID)
		return ErrChallengeExpired
	}
	if ch.Status != "pending" {
		return ErrChallengeAlreadyUsed
	}
	payload := map[string]interface{}{}
	_ = json.Unmarshal([]byte(ch.PayloadJSON), &payload)
	expectedChallenge := stringField(payload, "auth_challenge")
	if expectedChallenge == "" {
		expectedChallenge = stringField(payload, "challenge")
	}

	var cred storage.WebAuthnCredential
	if err := s.db.Where("credential_id = ? AND user_id = ?", credentialID, userID).First(&cred).Error; err != nil {
		return ErrCredentialNotFound
	}
	signCount, err := webauthnutil.VerifyAssertion(cred.PublicKey, clientDataJSON, authenticatorData, signature, expectedChallenge, s.cfg.RPOrigin, s.cfg.RPID)
	if err != nil {
		return err
	}
	if err := validateSignCount(cred.SignCount, signCount); err != nil {
		return err
	}
	return s.db.Model(&cred).Updates(map[string]interface{}{
		"sign_count":   signCount,
		"last_used_at": time.Now(),
	}).Error
}

func validateSignCount(stored, reported uint32) error {
	if stored == 0 && reported == 0 {
		return nil
	}
	if reported <= stored {
		return ErrSignCountInvalid
	}
	return nil
}

func (s *Server) ApproveChallenge(challengeID string) error {
	result := s.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND status = ?", challengeID, "pending").
		Update("status", "approved")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("challenge not found")
	}
	return nil
}

func (s *Server) CancelChallenge(userID, challengeID string) error {
	ch, err := s.GetChallengeForUser(userID, challengeID)
	if err != nil {
		return fmt.Errorf("challenge not found: %w", err)
	}
	if ch.Status == "cancelled" {
		return nil
	}
	if ch.Status != "pending" {
		return ErrChallengeAlreadyUsed
	}
	if s.expireChallengeIfNeeded(ch) {
		return ErrChallengeExpired
	}
	result := s.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND user_id = ? AND status = ?", challengeID, userID, "pending").
		Update("status", "cancelled")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrChallengeAlreadyUsed
	}
	return nil
}

func isRegistrationChallengeType(challengeType string) bool {
	return challengeType == "webauthn_register" || challengeType == "passkey_registration"
}

func (s *Server) ConsumeApprovedChallenge(userID, challengeID, challengeType string, expectedPayload interface{}) error {
	ch, err := s.GetChallengeForUser(userID, challengeID)
	if err != nil {
		return fmt.Errorf("approved challenge required: %w", err)
	}
	if ch.Type != challengeType {
		return ErrChallengeTypeMismatch
	}
	if ch.Status == "cancelled" {
		return ErrChallengeCancelled
	}
	if ch.Status != "approved" {
		return ErrChallengeAlreadyUsed
	}
	if time.Now().After(ch.ExpiresAt) {
		return ErrChallengeExpired
	}
	expectedHash, err := PayloadHash(expectedPayload)
	if err != nil {
		return err
	}
	actualHash, err := challengePayloadHash(ch.PayloadJSON)
	if err != nil {
		return err
	}
	if actualHash == "" || actualHash != expectedHash {
		return ErrChallengePayloadMismatch
	}
	result := s.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND status = ?", challengeID, "approved").
		Update("status", "used")
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("challenge already used")
	}
	return nil
}

func (s *Server) GetCredentials(userID string) ([]storage.WebAuthnCredential, error) {
	var creds []storage.WebAuthnCredential
	if err := s.db.Where("user_id = ?", userID).Find(&creds).Error; err != nil {
		return nil, err
	}
	return creds, nil
}

func credentialIDs(creds []storage.WebAuthnCredential) []string {
	ids := make([]string, 0, len(creds))
	for _, cred := range creds {
		if cred.CredentialID != "" {
			ids = append(ids, cred.CredentialID)
		}
	}
	return ids
}

func (s *Server) DeleteCredential(userID, credentialID string) error {
	result := s.db.Where("credential_id = ? AND user_id = ?", credentialID, userID).Delete(&storage.WebAuthnCredential{})
	if result.RowsAffected == 0 {
		return ErrCredentialNotFound
	}
	return result.Error
}

func (s *Server) CreateChallenge(userID, challengeType, payloadJSON string) (*storage.Challenge, error) {
	challengeID := "ch_" + base64.RawURLEncoding.EncodeToString(randomBytes(16))
	challengeB64 := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	timeout := s.cfg.ChallengeTimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	payload := map[string]interface{}{
		"challenge": challengeB64,
	}
	if payloadJSON != "" {
		var nested interface{}
		if err := json.Unmarshal([]byte(payloadJSON), &nested); err == nil {
			payload["payload"] = nested
			if hash, err := PayloadHash(nested); err == nil {
				payload["payload_hash"] = hash
			}
		} else {
			payload["payload"] = payloadJSON
			if hash, err := PayloadHash(payloadJSON); err == nil {
				payload["payload_hash"] = hash
			}
		}
	}
	ch := &storage.Challenge{
		ChallengeID: challengeID,
		UserID:      userID,
		Type:        challengeType,
		Status:      "pending",
		PayloadJSON: mustJSON(payload),
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Duration(timeout) * time.Second),
	}
	if err := s.db.Create(ch).Error; err != nil {
		return nil, fmt.Errorf("create challenge: %w", err)
	}
	return ch, nil
}

func (s *Server) GetChallenge(challengeID string) (*storage.Challenge, error) {
	var ch storage.Challenge
	if err := s.db.Where("challenge_id = ?", challengeID).First(&ch).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *Server) GetChallengeForUser(userID, challengeID string) (*storage.Challenge, error) {
	var ch storage.Challenge
	if err := s.db.Where("challenge_id = ? AND user_id = ?", challengeID, userID).First(&ch).Error; err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *Server) RefreshChallengeStatus(ch *storage.Challenge) error {
	if ch == nil {
		return nil
	}
	if ch.Status == "pending" && time.Now().After(ch.ExpiresAt) {
		if err := s.ExpireChallenge(ch.ChallengeID); err != nil {
			return err
		}
		ch.Status = "timeout"
	}
	return nil
}

func (s *Server) GetPendingChallenges(userID string) ([]storage.Challenge, error) {
	var chs []storage.Challenge
	if err := s.db.Where("user_id = ? AND status = ?", userID, "pending").
		Where("expires_at > ?", time.Now()).
		Find(&chs).Error; err != nil {
		return nil, err
	}
	return chs, nil
}

func (s *Server) ExpireChallenge(challengeID string) error {
	return s.db.Model(&storage.Challenge{}).
		Where("challenge_id = ? AND status = ?", challengeID, "pending").
		Update("status", "timeout").Error
}

func (s *Server) expireChallengeIfNeeded(ch *storage.Challenge) bool {
	if ch == nil || ch.Status != "pending" || !time.Now().After(ch.ExpiresAt) {
		return false
	}
	_ = s.ExpireChallenge(ch.ChallengeID)
	ch.Status = "timeout"
	return true
}

func SummaryForChallenge(ch storage.Challenge) ChallengeSummary {
	payload := challengePayloadValue(ch.PayloadJSON)
	payloadMap, _ := payload.(map[string]interface{})
	operation := stringField(payloadMap, "operation")
	if operation == "" {
		operation = ch.Type
	}
	summary := ChallengeSummary{
		Operation: operation,
		Title:     titleForOperation(operation),
		WalletID:  stringField(payloadMap, "wallet_id"),
		PolicyID:  stringField(payloadMap, "policy_id"),
	}
	if summary.WalletID != "" {
		summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Wallet", Value: summary.WalletID})
	}
	if summary.PolicyID != "" {
		summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Policy", Value: summary.PolicyID})
	}
	appendStringDetail := func(label, key string) {
		if value := stringField(payloadMap, key); value != "" {
			summary.Details = append(summary.Details, ChallengeSummaryField{Label: label, Value: value})
		}
	}
	appendStringDetail("Type", "tx_type")
	appendStringDetail("Chain", "chain")
	appendStringDetail("To", "to")
	if amount := numberField(payloadMap, "amount_usd"); amount != "" {
		summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Amount USD", Value: amount})
	}
	appendStringDetail("Reason", "reason")
	appendSecurityDetails(&summary, payloadMap)
	if request, ok := payloadMap["request"].(map[string]interface{}); ok {
		if name := stringField(request, "name"); name != "" {
			summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Policy name", Value: name})
		}
		if mode := stringField(request, "mode"); mode != "" {
			summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Mode", Value: mode})
		}
		if walletID := stringField(request, "wallet_id"); walletID != "" && summary.WalletID == "" {
			summary.WalletID = walletID
			summary.Details = append([]ChallengeSummaryField{{Label: "Wallet", Value: walletID}}, summary.Details...)
		}
	}
	if len(summary.Details) == 0 {
		summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Challenge", Value: ch.ChallengeID})
	}
	return summary
}

func appendSecurityDetails(summary *ChallengeSummary, payloadMap map[string]interface{}) {
	security, ok := payloadMap["security"].(map[string]interface{})
	if !ok {
		return
	}
	if verdict := stringField(security, "verdict"); verdict != "" {
		summary.Details = append(summary.Details, ChallengeSummaryField{Label: "Security verdict", Value: verdict})
	}
	checks, _ := security["checks"].([]interface{})
	for _, raw := range checks {
		check, _ := raw.(map[string]interface{})
		status := stringField(check, "status")
		if status != "warn" && status != "fail" {
			continue
		}
		rule := stringField(check, "rule")
		detail := stringField(check, "detail")
		if rule == "" && detail == "" {
			continue
		}
		value := rule
		if detail != "" {
			if value != "" {
				value += ": "
			}
			value += detail
		}
		label := "Security warning"
		if status == "fail" {
			label = "Security block"
		}
		summary.Details = append(summary.Details, ChallengeSummaryField{Label: label, Value: value})
	}
}

func (s *Server) registrationTimeout() time.Duration {
	if s.cfg.RegistrationTimeoutSeconds <= 0 {
		return 300 * time.Second
	}
	return time.Duration(s.cfg.RegistrationTimeoutSeconds) * time.Second
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func PayloadHash(v interface{}) (string, error) {
	canonical, err := canonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(v interface{}) ([]byte, error) {
	if raw, ok := v.(json.RawMessage); ok {
		var normalized interface{}
		if err := json.Unmarshal(raw, &normalized); err != nil {
			return nil, err
		}
		return json.Marshal(normalized)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var normalized interface{}
	if err := json.Unmarshal(b, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func challengePayloadHash(payloadJSON string) (string, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", err
	}
	hash, _ := payload["payload_hash"].(string)
	return hash, nil
}

func challengePayloadValue(payloadJSON string) interface{} {
	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &envelope); err != nil {
		return map[string]interface{}{}
	}
	if payload, ok := envelope["payload"]; ok {
		return payload
	}
	return envelope
}

func titleForOperation(operation string) string {
	switch operation {
	case "delete_wallet":
		return "Delete wallet"
	case "export_wallet":
		return "Export wallet"
	case "update_current_policy", "policy_change":
		return "Update signing policy"
	case "sign":
		return "Approve signature"
	case "security_override":
		return "Approve security override"
	default:
		if operation == "" {
			return "Confirm operation"
		}
		return operation
	}
}

func stringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func numberField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', 2, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', 2, 64)
	case int:
		return strconv.FormatFloat(float64(v), 'f', 2, 64)
	case int64:
		return strconv.FormatFloat(float64(v), 'f', 2, 64)
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return ""
		}
		return strconv.FormatFloat(n, 'f', 2, 64)
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return ""
		}
		return strconv.FormatFloat(n, 'f', 2, 64)
	default:
		return ""
	}
}
