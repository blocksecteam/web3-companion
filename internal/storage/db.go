// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package storage

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewDB(cfg config.DBConfig) (*gorm.DB, error) {
	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: open: %w", err)
	}

	if cfg.Driver == "sqlite" {
		if err := configureSQLite(db); err != nil {
			return nil, fmt.Errorf("storage: sqlite configure: %w", err)
		}
	}

	if err := db.AutoMigrate(
		&User{},
		&Wallet{},
		&WalletDelegation{},
		&PolicyDefinition{},
		&PolicyDefinitionVersion{},
		&PolicyInstance{},
		&PolicyUsageEvent{},
		&WebAuthnCredential{},
		&Challenge{},
		&AuditLog{},
		&Transaction{},
		&TransactionIntent{},
		&AuthSession{},
		&ContainerCredential{},
		&WebSocketTicket{},
		&WalletAssetSnapshot{},
		&AppSetting{},
	); err != nil {
		return nil, fmt.Errorf("storage: migrate: %w", err)
	}

	if err := runManualMigrations(db); err != nil {
		return nil, fmt.Errorf("storage: manual migrations: %w", err)
	}

	slog.Info("database initialized", "driver", cfg.Driver)
	return db, nil
}

func dialectorFor(cfg config.DBConfig) (gorm.Dialector, error) {
	driver := strings.TrimSpace(cfg.Driver)
	if driver == "" {
		driver = "sqlite"
	}
	if !strings.EqualFold(driver, "sqlite") {
		return nil, fmt.Errorf("unsupported DB driver %q (supported: sqlite)", cfg.Driver)
	}
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		dsn = "web3.db"
	}
	return sqlite.Open(dsn), nil
}

func ResolveSQLiteDSN(cfg config.DBConfig, workdir string) string {
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		dsn = "web3.db"
	}
	if dsn == ":memory:" || strings.HasPrefix(dsn, "file:") || filepath.IsAbs(dsn) {
		return dsn
	}
	if strings.TrimSpace(workdir) == "" {
		return dsn
	}
	return filepath.Join(workdir, dsn)
}

func configureSQLite(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
		return err
	}
	if err := db.Exec("PRAGMA busy_timeout=5000").Error; err != nil {
		return err
	}
	if err := db.Exec("PRAGMA foreign_keys=ON").Error; err != nil {
		return err
	}
	return nil
}

func runManualMigrations(db *gorm.DB) error {
	var piDelCount int
	if err := db.Raw(
		"SELECT COUNT(*) FROM pragma_table_info('policy_instances') WHERE name = 'deleted_at'",
	).Scan(&piDelCount).Error; err != nil {
		return fmt.Errorf("check policy_instances.deleted_at: %w", err)
	}
	if piDelCount > 0 {
		if err := db.Exec("DELETE FROM policy_instances WHERE deleted_at IS NOT NULL").Error; err != nil {
			return fmt.Errorf("cleanup soft-deleted policy_instances: %w", err)
		}
		if err := db.Exec("DROP INDEX IF EXISTS idx_policy_instances_deleted_at").Error; err != nil {
			return fmt.Errorf("drop index idx_policy_instances_deleted_at: %w", err)
		}
	}

	drops := []struct{ table, col string }{
		{"transaction_intents", "amount_usd"},
		{"transaction_intents", "price_unknown"},
		{"policy_instances", "deleted_at"},
		{"transactions", "is_batch"},
		{"transactions", "tx_data"},
		{"policy_definition_versions", "expires_at"},
		{"policy_definition_versions", "max_requests_per_minute"},
		{"policy_definition_versions", "cooldown_seconds"},
	}
	for _, d := range drops {
		var count int
		if err := db.Raw(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", d.table, d.col,
		).Scan(&count).Error; err != nil {
			return fmt.Errorf("check column %s.%s: %w", d.table, d.col, err)
		}
		if count > 0 {
			// d.table and d.col are hardcoded constants, not user input
			sql := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", d.table, d.col)
			if err := db.Exec(sql).Error; err != nil {
				return fmt.Errorf("drop column %s.%s: %w", d.table, d.col, err)
			}
		}
	}
	return nil
}
