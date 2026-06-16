// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package setup

import (
	"os"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/setup"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestIsSetupComplete_NoUsers(t *testing.T) {
	db := testutil.NewTestDB(t)
	if setup.IsSetupComplete(db) {
		t.Error("expected false when no users exist")
	}
}

func TestIsSetupComplete_WithUser(t *testing.T) {
	db := testutil.NewTestDB(t)
	db.Create(&storage.User{ID: "u1", Email: "a@b.com", Name: "test"})
	if !setup.IsSetupComplete(db) {
		t.Error("expected true when user exists")
	}
}

func TestEnsureSecrets_GeneratesWhenEmpty(t *testing.T) {
	db := testutil.NewTestDB(t)
	t.Setenv("WEB3_JWT_SECRET", "")
	t.Setenv("WEB3_SSM_KEY", "")

	secrets, err := setup.EnsureSecrets(db)
	if err != nil {
		t.Fatalf("EnsureSecrets: %v", err)
	}
	if secrets.JWTSecret == "" {
		t.Error("expected non-empty JWTSecret")
	}
	if secrets.SSMKey == "" {
		t.Error("expected non-empty SSMKey")
	}

	read, err := setup.ReadSecrets(db)
	if err != nil {
		t.Fatalf("ReadSecrets: %v", err)
	}
	if read.JWTSecret != secrets.JWTSecret {
		t.Error("DB JWTSecret doesn't match generated")
	}
	if read.SSMKey != secrets.SSMKey {
		t.Error("DB SSMKey doesn't match generated")
	}
}

func TestEnsureSecrets_ReplacesPlaceholder(t *testing.T) {
	db := testutil.NewTestDB(t)
	t.Setenv("WEB3_JWT_SECRET", "change-me-generate-a-long-random-secret-for-local-compose")
	t.Setenv("WEB3_SSM_KEY", "change-me-generate-at-least-32-random-bytes-for-local-compose")

	secrets, err := setup.EnsureSecrets(db)
	if err != nil {
		t.Fatalf("EnsureSecrets: %v", err)
	}
	if secrets.JWTSecret == "change-me-generate-a-long-random-secret-for-local-compose" {
		t.Error("expected placeholder to be replaced")
	}
}

func TestEnsureSecrets_KeepsExistingEnvValues(t *testing.T) {
	db := testutil.NewTestDB(t)
	t.Setenv("WEB3_JWT_SECRET", "my-existing-jwt-secret")
	t.Setenv("WEB3_SSM_KEY", "my-existing-ssm-key-not-placeholder")

	secrets, err := setup.EnsureSecrets(db)
	if err != nil {
		t.Fatalf("EnsureSecrets: %v", err)
	}
	if secrets.JWTSecret != "my-existing-jwt-secret" {
		t.Errorf("expected env JWT secret preserved, got %q", secrets.JWTSecret)
	}
	if secrets.SSMKey != "my-existing-ssm-key-not-placeholder" {
		t.Errorf("expected env SSM key preserved, got %q", secrets.SSMKey)
	}
}

func TestEnsureSecrets_ReadsFromDB(t *testing.T) {
	db := testutil.NewTestDB(t)
	t.Setenv("WEB3_JWT_SECRET", "")
	t.Setenv("WEB3_SSM_KEY", "")

	secrets1, _ := setup.EnsureSecrets(db)

	t.Setenv("WEB3_JWT_SECRET", "")
	t.Setenv("WEB3_SSM_KEY", "")
	secrets2, _ := setup.EnsureSecrets(db)

	if secrets2.JWTSecret != secrets1.JWTSecret {
		t.Error("second call should read same JWT from DB")
	}
	if secrets2.SSMKey != secrets1.SSMKey {
		t.Error("second call should read same SSM from DB")
	}
}

func TestMigrateFromFiles_SecretsFile(t *testing.T) {
	db := testutil.NewTestDB(t)
	dir := t.TempDir()

	os.WriteFile(dir+"/generated_secrets.env", []byte("WEB3_JWT_SECRET=file-jwt\nWEB3_SSM_KEY=file-ssm\n"), 0o600)

	setup.MigrateFromFiles(db, dir)

	secrets, err := setup.ReadSecrets(db)
	if err != nil {
		t.Fatalf("ReadSecrets: %v", err)
	}
	if secrets.JWTSecret != "file-jwt" {
		t.Errorf("expected file-jwt, got %q", secrets.JWTSecret)
	}
	if secrets.SSMKey != "file-ssm" {
		t.Errorf("expected file-ssm, got %q", secrets.SSMKey)
	}

	if _, err := os.Stat(dir + "/generated_secrets.env"); !os.IsNotExist(err) {
		t.Error("expected original file to be renamed")
	}
	if _, err := os.Stat(dir + "/generated_secrets.env.migrated"); os.IsNotExist(err) {
		t.Error("expected .migrated file to exist")
	}
}

func TestMigrateFromFiles_JSONFiles(t *testing.T) {
	db := testutil.NewTestDB(t)
	dir := t.TempDir()

	os.MkdirAll(dir+"/settings", 0o700)
	os.WriteFile(dir+"/settings/llm.json", []byte(`{"provider":"deepseek"}`), 0o600)
	os.WriteFile(dir+"/settings/rpc.json", []byte(`{"chains":{"ethereum":{"url":"https://eth.test"}}}`), 0o600)

	setup.MigrateFromFiles(db, dir)

	svc := testutil.NewSettingsService(db)
	llm, _ := svc.ReadLLM(testutil.DefaultLLM())
	if llm.Provider != "deepseek" {
		t.Errorf("expected deepseek provider from migrated file, got %q", llm.Provider)
	}

	rpc, _ := svc.ReadRPC()
	if _, ok := rpc.Chains["ethereum"]; !ok {
		t.Error("expected ethereum chain from migrated rpc.json")
	}
}
