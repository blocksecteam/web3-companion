// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package audit_test

import (
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/audit"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestServiceRecordPersistsStructuredWalletScopedAudit(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := audit.NewService(db)

	entry, err := svc.Record(t.Context(), audit.Event{
		EventType:   audit.EventWalletCreated,
		Severity:    audit.SeverityCritical,
		UserID:      "user_1",
		WalletID:    "wallet_1",
		WalletUID:   "wuid_wallet_1",
		ChallengeID: "ch_1",
		RequestID:   "req_1",
		Data: map[string]interface{}{
			"detail": "wallet created",
		},
	})
	if err != nil {
		t.Fatalf("record audit: %v", err)
	}
	if !strings.HasPrefix(entry.EventID, "evt_") {
		t.Fatalf("event id = %q, want evt_ prefix", entry.EventID)
	}

	var stored storage.AuditLog
	if err := db.First(&stored, "event_id = ?", entry.EventID).Error; err != nil {
		t.Fatalf("load audit: %v", err)
	}
	if stored.EventType != audit.EventWalletCreated || stored.Severity != audit.SeverityCritical {
		t.Fatalf("stored audit metadata = %+v", stored)
	}
	if stored.WalletUID != "wuid_wallet_1" || stored.WalletID != "wallet_1" {
		t.Fatalf("stored wallet scope = wallet_id:%q wallet_uid:%q", stored.WalletID, stored.WalletUID)
	}
	if !strings.Contains(stored.DataJSON, "wallet created") {
		t.Fatalf("data json = %s, want detail", stored.DataJSON)
	}
}
