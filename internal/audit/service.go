// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/storage"
)

const (
	SeverityInfo     = "INFO"
	SeverityHigh     = "HIGH"
	SeverityCritical = "CRITICAL"

	EventWalletCreated           = "WALLET_CREATED"
	EventWalletDeleted           = "WALLET_DELETED"
	EventWalletExported          = "WALLET_EXPORTED"
	EventSignRejected            = "SIGN_REJECTED"
	EventSignFailed              = "SIGN_FAILED"
	EventSign                    = "SIGN"
	EventSignAuto                = "SIGN_AUTO"
	EventDelegationAuto          = "DELEGATION_AUTO"
	EventPolicyDefinitionCreated = "POLICY_DEFINITION_CREATED"
	EventPolicyDefinitionUpdated = "POLICY_DEFINITION_UPDATED"
	EventPolicyDefinitionDeleted = "POLICY_DEFINITION_DELETED"
	EventPolicyInstanceBound     = "POLICY_INSTANCE_BOUND"
)

type Service struct {
	db  *gorm.DB
	Now func() time.Time
}

type Event struct {
	EventType   string
	Severity    string
	UserID      string
	WalletID    string
	WalletUID   string
	ChallengeID string
	RequestID   string
	Data        interface{}
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Record(ctx context.Context, event Event) (storage.AuditLog, error) {
	if s == nil || s.db == nil {
		return storage.AuditLog{}, fmt.Errorf("audit service unavailable")
	}
	now := s.now()
	dataJSON, err := encodeData(event.Data)
	if err != nil {
		return storage.AuditLog{}, err
	}
	entry := storage.AuditLog{
		EventID:     "evt_" + randomHex(16),
		EventType:   strings.TrimSpace(event.EventType),
		Severity:    strings.TrimSpace(event.Severity),
		UserID:      strings.TrimSpace(event.UserID),
		WalletID:    strings.TrimSpace(event.WalletID),
		WalletUID:   strings.TrimSpace(event.WalletUID),
		ChallengeID: strings.TrimSpace(event.ChallengeID),
		RequestID:   strings.TrimSpace(event.RequestID),
		DataJSON:    dataJSON,
		CreatedAt:   now,
	}
	if entry.EventType == "" || entry.Severity == "" {
		return storage.AuditLog{}, fmt.Errorf("audit event type and severity are required")
	}
	if err := s.db.WithContext(ctx).Create(&entry).Error; err != nil {
		return storage.AuditLog{}, err
	}
	return entry, nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func encodeData(data interface{}) (string, error) {
	if data == nil {
		return "{}", nil
	}
	if raw, ok := data.(string); ok {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "{}", nil
		}
		if json.Valid([]byte(raw)) {
			return raw, nil
		}
		data = map[string]interface{}{"detail": raw}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("encode audit data: %w", err)
	}
	return string(encoded), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
