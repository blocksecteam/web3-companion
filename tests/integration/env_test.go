// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package integration

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/blocksecteam/web3-companion/internal/config"
)

const (
	integrationTimeout = 30 * time.Second

	// Well-known addresses used across integration tests.
	addrVitalik    = "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045"
	addrDead       = "0x000000000000000000000000000000000000dEaD"
	addrUSDCEth    = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	addrUSDCBase   = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	addrUSDCPoly   = "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359"
	addrWETHEth    = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	addrWETHBase   = "0x4200000000000000000000000000000000000006"
	addrKyberRoute = "0x6131B5fae19EA4f9D964eAc0408E4408b66337b5"
)

func integrationCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	t.Cleanup(cancel)
	return ctx
}

func checkIntegrationEnabled(t *testing.T) bool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
		return false
	}
	if os.Getenv("WEB3_INTEGRATION") != "1" {
		t.Skip("skipping integration test; set WEB3_INTEGRATION=1 to run")
		return false
	}
	return true
}

func checkIntegrationEnabledFromDotenv(t *testing.T) bool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
		return false
	}
	if os.Getenv("WEB3_INTEGRATION") == "1" {
		return true
	}
	values := readIntegrationDotenv(t)
	if strings.TrimSpace(values["WEB3_INTEGRATION"]) != "1" {
		t.Skip("skipping integration test; set WEB3_INTEGRATION=1 to run")
		return false
	}
	return true
}

func envOr(t *testing.T, key, defaultVal string) string {
	t.Helper()
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func rpcURLForChain(t *testing.T, chainName string) string {
	t.Helper()
	cfg := loadIntegrationConfig(t)
	ch := cfg.ChainByName(chainName)
	if ch == nil || ch.RPCURL == "" {
		t.Fatalf("RPC URL for chain %q must be configured in docker/.env or docker/config.toml", chainName)
	}
	if err := (&config.Config{
		AWS:      config.AWSConfig{Region: "test", KMSKeyID: "test", AccessKeyID: "test", SecretAccessKey: "test"},
		WebAuthn: config.WebAuthnConfig{RPID: "localhost", RPOrigin: "http://localhost:8080"},
		Auth:     config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir:  config.WorkdirConfig{Path: "./dist"},
		Chains:   []config.ChainConfig{*ch},
	}).Validate(); err != nil {
		t.Fatalf("invalid chain RPC config for %q: %v", chainName, err)
	}
	return ch.RPCURL
}

func configuredRPCURLs() map[string]string {
	cfg := loadIntegrationConfig(nil)
	if cfg == nil {
		return nil
	}
	out := make(map[string]string, len(cfg.Chains))
	for _, ch := range cfg.Chains {
		if ch.Name != "" && isUsableRPCURL(ch.RPCURL) {
			out[ch.Name] = ch.RPCURL
		}
	}
	return out
}

func isUsableRPCURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "${") {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func loadIntegrationConfig(t *testing.T) *config.Config {
	loadIntegrationDotenv(t)
	path := os.Getenv("WEB3_CONFIG_PATH")
	if path == "" {
		path = findFileUp(filepath.Join("docker", "config.toml"))
	}
	cfg, err := config.Load(path)
	if err != nil {
		if t != nil {
			t.Fatalf("load integration config: %v", err)
		}
		return nil
	}
	return cfg
}

func loadIntegrationDotenv(t *testing.T) {
	values := readIntegrationDotenv(t)
	for _, key := range []string{
		"WEB3_INTEGRATION",
		"WEB3_RPC_ETHEREUM_URL",
		"WEB3_RPC_BSC_URL",
		"WEB3_RPC_POLYGON_URL",
		"WEB3_RPC_BASE_URL",
		"PORTALS_API_KEY",
		"METASLEUTH_API_KEY",
		"AWS_REGION",
		"AWS_KMS_KEY_ID",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
	} {
		if os.Getenv(key) == "" && values[key] != "" {
			_ = os.Setenv(key, values[key])
		}
	}
}

func readIntegrationDotenv(t *testing.T) map[string]string {
	for _, name := range []string{
		filepath.Join("docker", ".env.integration"),
		filepath.Join("docker", ".env"),
	} {
		path := findFileUp(name)
		if path == "" {
			continue
		}
		values, err := godotenv.Read(path)
		if err != nil {
			continue
		}
		return values
	}
	return nil
}

func findFileUp(name string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		next := filepath.Dir(dir)
		if next == dir {
			return ""
		}
		dir = next
	}
}
