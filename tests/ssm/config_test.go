// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package ssm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/config"
)

func TestConfig_Load_DefaultsToHomeWorkdirAndSQLite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.DB.Driver != "sqlite" {
		t.Errorf("default DB driver = %s, want sqlite", cfg.DB.Driver)
	}
	if cfg.Workdir.Path != home {
		t.Errorf("default workdir = %s, want HOME %s", cfg.Workdir.Path, home)
	}
	if cfg.SSMKeysDir() != filepath.Join(home, "keys") {
		t.Errorf("default SSM keys dir = %s, want HOME/keys", cfg.SSMKeysDir())
	}
	if cfg.SettingsDir() != filepath.Join(home, "settings") {
		t.Errorf("default settings dir = %s, want HOME/settings", cfg.SettingsDir())
	}
}

func TestConfig_Load_FromFile(t *testing.T) {
	content := `
[server]
port = 9090
host = "127.0.0.1"

[db]
driver = "sqlite"
dsn = "web3.db"

[log]
level = "debug"
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("host = %s, want 127.0.0.1", cfg.Server.Host)
	}
	if cfg.DB.Driver != "sqlite" {
		t.Errorf("driver = %s, want sqlite", cfg.DB.Driver)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("log level = %s, want debug", cfg.Log.Level)
	}
}

func TestConfig_DoesNotOverrideServerDBFromEnv(t *testing.T) {
	t.Setenv("WEB3_SERVER_PORT", "7777")
	t.Setenv("WEB3_DB_DSN", "ignored.db")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("env must not override config server port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.DB.DSN != "web3.db" {
		t.Errorf("env must not override config DB DSN = %s, want web3.db", cfg.DB.DSN)
	}
}

func TestConfig_RuntimeSecretsAndProviderKeysComeFromEnv(t *testing.T) {
	content := `
[server]
external_url = "https://from-config.example"
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_KMS_KEY_ID", "arn:aws:kms:us-east-1:123456789012:key/test")
	t.Setenv("AWS_ACCESS_KEY_ID", "access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("WEB3_SSM_KEY", "env-ssm-key-is-ignored-when-aws-is-complete")
	t.Setenv("WEB3_RP_ID", "localhost")
	t.Setenv("WEB3_RP_NAME", "env rp")
	t.Setenv("WEB3_RP_ORIGIN", "http://localhost:8080")
	t.Setenv("WEB3_CHALLENGE_TIMEOUT_SECONDS", "121")
	t.Setenv("WEB3_REGISTRATION_TIMEOUT_SECONDS", "301")
	t.Setenv("WEB3_JWT_SECRET", "env-jwt")
	t.Setenv("WEB3_API_URL", "http://localhost:19090")
	t.Setenv("WEB3_WORKDIR", "/tmp/web3-work")
	t.Setenv("WEB3_LLM_PROVIDER", "deepseek")
	t.Setenv("WEB3_LLM_BASE_URL", "https://api.deepseek.com/anthropic")
	t.Setenv("WEB3_LLM_MODEL", "deepseek-v4-pro")
	t.Setenv("WEB3_LLM_API_KEY", "env-llm-key")
	t.Setenv("PORTALS_API_KEY", "env-portals")
	t.Setenv("METASLEUTH_API_KEY", "env-metasleuth")
	t.Setenv("WEB3_WALLET_ADDRESS", "0xowner")
	t.Setenv("WEB3_ALLOWED_ORIGINS", "http://localhost:5173, https://app.example")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AWS.Region != "us-east-1" || cfg.AWS.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/test" {
		t.Fatalf("AWS config must come from env, got %#v", cfg.AWS)
	}
	if cfg.SSM.RootKey != "env-ssm-key-is-ignored-when-aws-is-complete" {
		t.Fatalf("SSM root key must come from env, got %#v", cfg.SSM)
	}
	if cfg.WebAuthn.RPID != "localhost" || cfg.WebAuthn.RPName != "env rp" || cfg.WebAuthn.RPOrigin != "http://localhost:8080" {
		t.Fatalf("WebAuthn config must come from env, got %#v", cfg.WebAuthn)
	}
	if cfg.WebAuthn.ChallengeTimeoutSeconds != 121 || cfg.WebAuthn.RegistrationTimeoutSeconds != 301 {
		t.Fatalf("WebAuthn timeouts must come from env, got %#v", cfg.WebAuthn)
	}
	if cfg.Auth.JWTSecret != "env-jwt" {
		t.Fatalf("JWT secret must come from env, got %#v", cfg.Auth)
	}
	if cfg.CLI.APIURL != "http://localhost:19090" || cfg.CLI.WalletAddress != "0xowner" {
		t.Fatalf("CLI config must come from env, got %#v", cfg.CLI)
	}
	if cfg.Workdir.Path != "/tmp/web3-work" {
		t.Fatalf("workdir must come from env, got %#v", cfg.Workdir)
	}
	if cfg.LLM.Provider != "deepseek" || cfg.LLM.BaseURL != "https://api.deepseek.com/anthropic" || cfg.LLM.ModelName != "deepseek-v4-pro" || cfg.LLM.APIKey != "env-llm-key" {
		t.Fatalf("LLM initial settings must come from env, got %#v", cfg.LLM)
	}
	if cfg.Providers.PortalsAPIKey != "env-portals" || cfg.Providers.MetaSleuthAPIKey != "env-metasleuth" {
		t.Fatalf("provider secrets must come from env, got %#v", cfg.Providers)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 || cfg.CORS.AllowedOrigins[0] != "http://localhost:5173" || cfg.CORS.AllowedOrigins[1] != "https://app.example" {
		t.Fatalf("CORS allowed origins must come from env, got %#v", cfg.CORS)
	}
}

func TestConfig_WorkdirEnvDerivesRuntimeDirs(t *testing.T) {
	t.Setenv("WEB3_WORKDIR", "/tmp/web3-work")
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Workdir.Path != "/tmp/web3-work" {
		t.Fatalf("workdir = %s, want /tmp/web3-work", cfg.Workdir.Path)
	}
	if cfg.SSMKeysDir() != "/tmp/web3-work/keys" {
		t.Fatalf("SSMKeysDir = %s, want /tmp/web3-work/keys", cfg.SSMKeysDir())
	}
	if cfg.SettingsDir() != "/tmp/web3-work/settings" {
		t.Fatalf("SettingsDir = %s, want /tmp/web3-work/settings", cfg.SettingsDir())
	}
}

func TestConfig_ChainRPCEnvOverridesConfigURL(t *testing.T) {
	content := `
[[chains]]
name = "ethereum"
chain_id = 1
rpc_url = "https://config-eth.example"

[[chains]]
name = "base"
chain_id = 8453
rpc_url = "https://config-base.example"

[[chains]]
name = "bsc"
chain_id = 56
rpc_url = "https://config-bsc.example"
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB3_RPC_ETHEREUM_URL", "https://eth.example")
	t.Setenv("WEB3_RPC_BASE_URL", "https://base.example")
	t.Setenv("WEB3_RPC_BSC_URL", "https://bsc.example")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ChainByName("ethereum").RPCURL; got != "https://eth.example" {
		t.Fatalf("ethereum RPCURL = %q", got)
	}
	if got := cfg.ChainByName("base").RPCURL; got != "https://base.example" {
		t.Fatalf("base RPCURL = %q", got)
	}
	if got := cfg.ChainByName("bsc").RPCURL; got != "https://bsc.example" {
		t.Fatalf("bsc RPCURL = %q", got)
	}
}

func TestConfig_ValidateRejectsUnresolvedRPCPlaceholder(t *testing.T) {
	cfg := &config.Config{
		SSM: config.SSMConfig{RootKey: "test-ssm-root-key-that-is-long-enough"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: "./dist"},
		Chains:  []config.ChainConfig{{Name: "ethereum", ChainID: 1, RPCURL: "https://example.com/${MISSING_RPC_KEY}"}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "MISSING_RPC_KEY") {
		t.Fatalf("Validate error = %v, want missing env placeholder", err)
	}
}

func TestConfig_ValidateRejectsPostgres(t *testing.T) {
	cfg := &config.Config{
		DB:  config.DBConfig{Driver: "postgres", DSN: "postgres://localhost/web3"},
		SSM: config.SSMConfig{RootKey: "test-ssm-root-key-that-is-long-enough"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: "./dist"},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "sqlite") {
		t.Fatalf("Validate error = %v, want sqlite-only rejection", err)
	}
}

func TestConfig_ChainRegistry(t *testing.T) {
	cfg := &config.Config{
		Chains: []config.ChainConfig{
			{Name: "ethereum", ChainID: 1, RPCURL: "https://eth.example.com"},
			{Name: "base", ChainID: 8453, RPCURL: "https://base.example.com"},
			{Name: "polygon", ChainID: 137, RPCURL: "https://polygon.example.com"},
		},
	}

	if eth := cfg.ChainByName("ethereum"); eth == nil || eth.ChainID != 1 {
		t.Error("ChainByName ethereum failed")
	}
	if cfg.ChainByName("Ethereum") == nil {
		t.Error("ChainByName case insensitive failed")
	}
	if base := cfg.ChainByID(8453); base == nil || base.Name != "base" {
		t.Error("ChainByID 8453 failed")
	}
	if ch := cfg.ChainByName("solana"); ch != nil {
		t.Error("unknown chain should return nil")
	}
	if ch := cfg.ChainByID(999); ch != nil {
		t.Error("unknown chain ID should return nil")
	}
}

func TestConfig_WebAuthnDefaults(t *testing.T) {
	cfg, _ := config.Load("")
	if timeout := cfg.GetSignChallengeTimeout(); timeout.Seconds() != 120 {
		t.Errorf("sign challenge timeout = %v, want 120s", timeout)
	}
	if regTimeout := cfg.GetRegistrationTimeout(); regTimeout.Seconds() != 300 {
		t.Errorf("registration timeout = %v, want 300s", regTimeout)
	}
}

func TestConfig_ExternalURL(t *testing.T) {
	content := `
[server]
external_url = "https://api.web3.example.com"
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(path)
	if cfg.Server.ExternalURL != "https://api.web3.example.com" {
		t.Errorf("external_url = %s", cfg.Server.ExternalURL)
	}
}

func TestConfig_WebAuthnDefaultsDeriveFromConfigExternalURL(t *testing.T) {
	content := `
[server]
external_url = "https://wallet.example.com/self-hosted"
`
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebAuthn.RPOrigin != "https://wallet.example.com" {
		t.Fatalf("RPOrigin = %q, want origin derived from server.external_url", cfg.WebAuthn.RPOrigin)
	}
	if cfg.WebAuthn.RPID != "wallet.example.com" {
		t.Fatalf("RPID = %q, want hostname derived from server.external_url", cfg.WebAuthn.RPID)
	}
}
