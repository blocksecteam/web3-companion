// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package setup

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	keyJWTSecret = "jwt_secret"
	keySSMKey    = "ssm_key"
)

type GeneratedSecrets struct {
	JWTSecret string
	SSMKey    string
}

func IsSetupComplete(db *gorm.DB) bool {
	var count int64
	db.Model(&storage.User{}).Count(&count)
	return count > 0
}

func EnsureSecrets(db *gorm.DB) (GeneratedSecrets, error) {
	jwtSecret := strings.TrimSpace(os.Getenv("WEB3_JWT_SECRET"))
	ssmKey := strings.TrimSpace(os.Getenv("WEB3_SSM_KEY"))

	envJWT := !isPlaceholderOrEmpty(jwtSecret)
	envSSM := !isPlaceholderOrEmpty(ssmKey)

	if envJWT && envSSM {
		saveSetting(db, keyJWTSecret, jwtSecret)
		saveSetting(db, keySSMKey, ssmKey)
		return GeneratedSecrets{JWTSecret: jwtSecret, SSMKey: ssmKey}, nil
	}

	if !envJWT {
		dbVal, err := readSetting(db, keyJWTSecret)
		if err == nil && dbVal != "" {
			jwtSecret = dbVal
		} else {
			b := make([]byte, 64)
			if _, err := rand.Read(b); err != nil {
				return GeneratedSecrets{}, fmt.Errorf("generate JWT secret: %w", err)
			}
			jwtSecret = base64.RawURLEncoding.EncodeToString(b)
			saveSetting(db, keyJWTSecret, jwtSecret)
		}
	}

	if !envSSM {
		dbVal, err := readSetting(db, keySSMKey)
		if err == nil && dbVal != "" {
			ssmKey = dbVal
		} else {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return GeneratedSecrets{}, fmt.Errorf("generate SSM key: %w", err)
			}
			ssmKey = base64.RawURLEncoding.EncodeToString(b)
			saveSetting(db, keySSMKey, ssmKey)
		}
	}

	os.Setenv("WEB3_JWT_SECRET", jwtSecret)
	os.Setenv("WEB3_SSM_KEY", ssmKey)

	return GeneratedSecrets{JWTSecret: jwtSecret, SSMKey: ssmKey}, nil
}

func ReadSecrets(db *gorm.DB) (GeneratedSecrets, error) {
	jwt, err := readSetting(db, keyJWTSecret)
	if err != nil {
		return GeneratedSecrets{}, fmt.Errorf("read jwt_secret: %w", err)
	}
	ssm, errS := readSetting(db, keySSMKey)
	if errS != nil {
		return GeneratedSecrets{}, fmt.Errorf("read ssm_key: %w", errS)
	}
	return GeneratedSecrets{JWTSecret: jwt, SSMKey: ssm}, nil
}

func MigrateFromFiles(db *gorm.DB, settingsDir string) {
	migrateSecretsFile(db, settingsDir)
	migrateJSONFile(db, filepath.Join(settingsDir, "settings", "llm.json"), "llm")
	migrateJSONFile(db, filepath.Join(settingsDir, "settings", "rpc.json"), "rpc")

	marker := filepath.Join(settingsDir, ".setup_complete")
	if _, err := os.Stat(marker); err == nil {
		os.Rename(marker, marker+".migrated")
	}
}

func migrateSecretsFile(db *gorm.DB, workdir string) {
	path := filepath.Join(workdir, "generated_secrets.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "WEB3_JWT_SECRET":
			if existing, _ := readSetting(db, keyJWTSecret); existing == "" {
				saveSetting(db, keyJWTSecret, v)
			}
		case "WEB3_SSM_KEY":
			if existing, _ := readSetting(db, keySSMKey); existing == "" {
				saveSetting(db, keySSMKey, v)
			}
		}
	}
	os.Rename(path, path+".migrated")
}

func migrateJSONFile(db *gorm.DB, path, key string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if existing, _ := readSetting(db, key); existing != "" {
		return
	}
	var parsed interface{}
	if json.Unmarshal(data, &parsed) != nil {
		return
	}
	saveSetting(db, key, string(data))
	os.Rename(path, path+".migrated")
}

func readSetting(db *gorm.DB, key string) (string, error) {
	var row storage.AppSetting
	if err := db.Where("key = ?", key).First(&row).Error; err != nil {
		return "", err
	}
	return row.Value, nil
}

func saveSetting(db *gorm.DB, key, value string) {
	db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&storage.AppSetting{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	})
}

func isPlaceholderOrEmpty(v string) bool {
	return v == "" || strings.HasPrefix(v, "change-me")
}
