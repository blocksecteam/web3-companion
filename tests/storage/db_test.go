// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package storage_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/money"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNewDBSQLiteAppliesSafetyPragmasAndPool(t *testing.T) {
	db, err := storage.NewDB(config.DBConfig{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "web3.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("DB handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("journal_mode pragma: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("busy_timeout pragma: %v", err)
	}
	if busyTimeout < 5000 {
		t.Fatalf("busy_timeout = %d, want >= 5000", busyTimeout)
	}

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	if sqlDB.Stats().MaxOpenConnections != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1 for SQLite", sqlDB.Stats().MaxOpenConnections)
	}
}

func TestNewDBRejectsPostgres(t *testing.T) {
	_, err := storage.NewDB(config.DBConfig{Driver: "postgres", DSN: "postgres://localhost/web3"})
	if err == nil || !strings.Contains(err.Error(), "sqlite") {
		t.Fatalf("NewDB postgres error = %v, want sqlite-only rejection", err)
	}
}

func TestResolveSQLiteDSNPlacesRelativePathUnderWorkdir(t *testing.T) {
	workdir := t.TempDir()
	got := storage.ResolveSQLiteDSN(config.DBConfig{Driver: "sqlite", DSN: "web3.db"}, workdir)
	want := filepath.Join(workdir, "web3.db")
	if got != want {
		t.Fatalf("ResolveSQLiteDSN = %q, want %q", got, want)
	}
}

func TestResolveSQLiteDSNKeepsAbsolutePath(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "web3.db")
	got := storage.ResolveSQLiteDSN(config.DBConfig{Driver: "sqlite", DSN: absolute}, t.TempDir())
	if got != absolute {
		t.Fatalf("ResolveSQLiteDSN = %q, want %q", got, absolute)
	}
}

func TestMoneyColumnsUseSingleCanonicalDecimalField(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&storage.PolicyDefinitionVersion{},
		&storage.PolicyInstance{},
		&storage.PolicyUsageEvent{},
		&storage.Transaction{},
		&storage.TransactionIntent{},
		&storage.WalletAssetSnapshot{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Create(&storage.PolicyDefinitionVersion{
		PolicyDefinitionVersionID: "pver_1",
		PolicyDefinitionID:        "pdef_1",
		UserID:                    "user_1",
		Version:                   1,
		MaxPerTransactionUSD:      money.MustParse("0.3000"),
		DailyTransferLimitUSD:     money.MustParse("1.25"),
		DailyTradeLimitUSD:        money.MustParse("2.5"),
		AutoSignMaxPerTxUSD:       money.MustParse("0.2"),
		AutoSignDailyLimitUSD:     money.MustParse("0.3"),
	}).Error; err != nil {
		t.Fatalf("create policy version: %v", err)
	}
	if err := db.Create(&storage.PolicyInstance{
		PolicyInstanceID:          "pinst_1",
		UserID:                    "user_1",
		WalletID:                  "wallet_1",
		PolicyDefinitionID:        "pdef_1",
		PolicyDefinitionVersionID: "pver_1",
		DailyTransferUsedUSD:      money.MustParse("0.1"),
		DailyTradeUsedUSD:         money.MustParse("0.2"),
		AutoSignDailyUsedUSD:      money.MustParse("0.3"),
	}).Error; err != nil {
		t.Fatalf("create policy instance: %v", err)
	}
	if err := db.Create(&storage.PolicyUsageEvent{
		EventID:   "pue_1",
		UserID:    "user_1",
		WalletID:  "wallet_1",
		PolicyID:  "pinst_1",
		Chain:     "ethereum",
		TxHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		TxType:    "transfer",
		AmountUSD: money.MustParse("0.3000"),
		Decision:  "auto",
		Status:    "signed",
		UsageDate: now.Format("2006-01-02"),
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create usage event: %v", err)
	}
	if err := db.Create(&storage.Transaction{
		Hash:      "0x2222222222222222222222222222222222222222222222222222222222222222",
		Chain:     "ethereum",
		UserID:    "user_1",
		Status:    "pending",
		AmountUSD: money.MustParse("0.3000"),
	}).Error; err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	if err := db.Create(&storage.TransactionIntent{
		TransactionID: "txi_1",
		UserID:        "user_1",
		WalletID:      "wallet_1",
		Chain:         "ethereum",
		ChainID:       1,
		TxType:        "transfer",
		To:            "0x2222222222222222222222222222222222222222",
		Value:         "0",
		GasLimit:      21000,
		PayloadHash:   "hash",
		Status:        "built",
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
		UpdatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create intent: %v", err)
	}
	if err := db.Create(&storage.WalletAssetSnapshot{
		UserID:     "user_1",
		WalletID:   "wallet_1",
		TotalUSD:   money.MustParse("0.3000"),
		ObservedAt: now,
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create asset snapshot: %v", err)
	}

	var version storage.PolicyDefinitionVersion
	if err := db.First(&version, "policy_definition_version_id = ?", "pver_1").Error; err != nil {
		t.Fatalf("load version: %v", err)
	}
	if version.MaxPerTransactionUSD.String() != "0.3" || version.AutoSignDailyLimitUSD.String() != "0.3" {
		t.Fatalf("version money = %+v", version)
	}
	var instance storage.PolicyInstance
	if err := db.First(&instance, "policy_instance_id = ?", "pinst_1").Error; err != nil {
		t.Fatalf("load instance: %v", err)
	}
	if instance.DailyTransferUsedUSD.String() != "0.1" || instance.AutoSignDailyUsedUSD.String() != "0.3" {
		t.Fatalf("instance money = %+v", instance)
	}
	var event storage.PolicyUsageEvent
	if err := db.First(&event, "event_id = ?", "pue_1").Error; err != nil {
		t.Fatalf("load event: %v", err)
	}
	if event.AmountUSD.String() != "0.3" {
		t.Fatalf("event amount = %q, want 0.3", event.AmountUSD.String())
	}
	var transaction storage.Transaction
	if err := db.First(&transaction, "hash = ? AND chain = ?", "0x2222222222222222222222222222222222222222222222222222222222222222", "ethereum").Error; err != nil {
		t.Fatalf("load transaction: %v", err)
	}
	if transaction.AmountUSD.String() != "0.3" {
		t.Fatalf("transaction amount = %q, want 0.3", transaction.AmountUSD.String())
	}
	var intent storage.TransactionIntent
	if err := db.First(&intent, "transaction_id = ?", "txi_1").Error; err != nil {
		t.Fatalf("load intent: %v", err)
	}
	var snapshot storage.WalletAssetSnapshot
	if err := db.First(&snapshot, "user_id = ? AND wallet_id = ?", "user_1", "wallet_1").Error; err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.TotalUSD.String() != "0.3" {
		t.Fatalf("snapshot total = %q, want 0.3", snapshot.TotalUSD.String())
	}

	for _, tc := range []struct {
		table  string
		column string
	}{
		{"policy_usage_events", oldUSDColumn("amount")},
		{"transactions", oldUSDColumn("amount")},
		{"transactions", "block_time"},
		{"transaction_intents", oldUSDColumn("amount")},
		{"transaction_intents", "amount_usd"},
		{"transaction_intents", "price_unknown"},
		{"wallet_asset_snapshots", oldUSDColumn("total")},
	} {
		if columnExists(t, db, tc.table, tc.column) {
			t.Fatalf("%s.%s should not exist", tc.table, tc.column)
		}
	}
	if !columnExists(t, db, "transactions", "confirmed_at") {
		t.Fatalf("transactions.confirmed_at should exist")
	}

	var storedAmount string
	if err := db.Raw("SELECT amount_usd FROM policy_usage_events WHERE event_id = ?", "pue_1").Scan(&storedAmount).Error; err != nil {
		t.Fatalf("stored amount query: %v", err)
	}
	if storedAmount != "0.3" {
		t.Fatalf("stored amount = %q, want 0.3", storedAmount)
	}
}

func TestWalletScopedHistoryTablesPersistWalletUID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&storage.AuditLog{},
		&storage.Transaction{},
		&storage.TransactionIntent{},
		&storage.PolicyUsageEvent{},
		&storage.WalletAssetSnapshot{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC()
	uid := "wuid_history_1"
	if err := db.Create(&storage.AuditLog{
		EventID:   "evt_history_1",
		EventType: "WALLET_CREATED",
		Severity:  "INFO",
		UserID:    "user_1",
		WalletID:  "wallet_1",
		WalletUID: uid,
		DataJSON:  "{}",
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create audit: %v", err)
	}
	if err := db.Create(&storage.Transaction{
		Hash:      "0x3333333333333333333333333333333333333333333333333333333333333333",
		Chain:     "ethereum",
		UserID:    "user_1",
		WalletID:  "wallet_1",
		WalletUID: uid,
		Status:    "pending",
		AmountUSD: money.Amount{},
	}).Error; err != nil {
		t.Fatalf("create transaction: %v", err)
	}
	if err := db.Create(&storage.TransactionIntent{
		TransactionID: "txi_history_1",
		UserID:        "user_1",
		WalletID:      "wallet_1",
		WalletUID:     uid,
		Chain:         "ethereum",
		ChainID:       1,
		TxType:        "transfer",
		To:            "0x2222222222222222222222222222222222222222",
		Value:         "0",
		GasLimit:      21000,
		PayloadHash:   "hash",
		Status:        "built",
		ExpiresAt:     now.Add(time.Hour),
		CreatedAt:     now,
		UpdatedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create intent: %v", err)
	}
	if err := db.Create(&storage.PolicyUsageEvent{
		EventID:   "pue_history_1",
		UserID:    "user_1",
		WalletID:  "wallet_1",
		WalletUID: uid,
		PolicyID:  "pinst_1",
		Chain:     "ethereum",
		TxHash:    "0x4444444444444444444444444444444444444444444444444444444444444444",
		TxType:    "transfer",
		AmountUSD: money.Amount{},
		Decision:  "auto",
		Status:    "signed",
		UsageDate: now.Format("2006-01-02"),
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create policy usage: %v", err)
	}
	if err := db.Create(&storage.WalletAssetSnapshot{
		UserID:     "user_1",
		WalletID:   "wallet_1",
		WalletUID:  uid,
		TotalUSD:   money.Amount{},
		ObservedAt: now,
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create wallet asset snapshot: %v", err)
	}

	for _, tc := range []struct {
		table string
	}{
		{"audit_log"},
		{"transactions"},
		{"transaction_intents"},
		{"policy_usage_events"},
		{"wallet_asset_snapshots"},
	} {
		if !columnExists(t, db, tc.table, "wallet_uid") {
			t.Fatalf("%s.wallet_uid should exist", tc.table)
		}
		var got string
		if err := db.Raw("SELECT wallet_uid FROM " + tc.table + " LIMIT 1").Scan(&got).Error; err != nil {
			t.Fatalf("select wallet_uid from %s: %v", tc.table, err)
		}
		if got != uid {
			t.Fatalf("%s.wallet_uid = %q, want %q", tc.table, got, uid)
		}
	}
}

func TestWalletSchemaKeepsDisplayIDAndGenerationIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&storage.Wallet{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if !columnExists(t, db, "wallets", "display_wallet_id") {
		t.Fatalf("wallets.display_wallet_id should exist")
	}
	if !columnExists(t, db, "wallets", "generation") {
		t.Fatalf("wallets.generation should exist")
	}

	type columnInfo struct {
		Name string
		PK   int
	}
	var cols []columnInfo
	if err := db.Raw("PRAGMA table_info(wallets)").Scan(&cols).Error; err != nil {
		t.Fatalf("table info for wallets: %v", err)
	}
	primaryColumns := make([]string, 0, 2)
	for _, col := range cols {
		if col.PK > 0 {
			primaryColumns = append(primaryColumns, col.Name)
		}
	}
	if len(primaryColumns) != 2 || primaryColumns[0] != "user_id" || primaryColumns[1] != "wallet_id" {
		t.Fatalf("wallet primary columns = %v, want [user_id wallet_id]", primaryColumns)
	}
	if !hasUniqueIndex(t, db, "wallets", "wallet_uid") {
		t.Fatalf("wallets.wallet_uid should have a unique index")
	}
}

func hasUniqueIndex(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	type indexList struct {
		Name   string
		Unique int
	}
	var indexes []indexList
	if err := db.Raw("PRAGMA index_list(" + table + ")").Scan(&indexes).Error; err != nil {
		t.Fatalf("index list for %s: %v", table, err)
	}
	for _, idx := range indexes {
		if idx.Unique == 0 {
			continue
		}
		type indexInfo struct {
			Name string
		}
		var cols []indexInfo
		if err := db.Raw("PRAGMA index_info(" + idx.Name + ")").Scan(&cols).Error; err != nil {
			t.Fatalf("index info for %s: %v", idx.Name, err)
		}
		for _, col := range cols {
			if col.Name == column {
				return true
			}
		}
	}
	return false
}

func oldUSDColumn(prefix string) string {
	return prefix + "_usd" + "_str"
}

func columnExists(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	type columnInfo struct {
		Name string
	}
	var cols []columnInfo
	if err := db.Raw("PRAGMA table_info(" + table + ")").Scan(&cols).Error; err != nil {
		t.Fatalf("table info for %s: %v", table, err)
	}
	for _, col := range cols {
		if col.Name == column {
			return true
		}
	}
	return false
}
