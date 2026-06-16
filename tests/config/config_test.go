// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/blocksecteam/web3-companion/internal/chain"
	"github.com/blocksecteam/web3-companion/internal/config"
)

func testConfig(chains ...string) *config.Config {
	cfg := &config.Config{}
	for _, name := range chains {
		cfg.Chains = append(cfg.Chains, config.ChainConfig{Name: name})
	}
	return cfg
}

func TestValidateChain_SupportedChain_ReturnsNil(t *testing.T) {
	cfg := testConfig("ethereum", "bsc", "polygon", "base")
	if err := cfg.ValidateChain("ethereum"); err != nil {
		t.Errorf("expected nil for supported chain, got %v", err)
	}
}

func TestValidateChain_UnsupportedChain_ReturnsError(t *testing.T) {
	cfg := testConfig("ethereum", "bsc", "polygon", "base")
	err := cfg.ValidateChain("arbitrum")
	if err == nil {
		t.Error("expected error for unsupported chain arbitrum")
	}
}

func TestValidateChain_CaseInsensitive(t *testing.T) {
	cfg := testConfig("ethereum", "bsc", "polygon", "base")
	if err := cfg.ValidateChain("Ethereum"); err != nil {
		t.Errorf("expected nil for Ethereum (case-insensitive), got %v", err)
	}
	if err := cfg.ValidateChain("BSC"); err != nil {
		t.Errorf("expected nil for BSC (case-insensitive), got %v", err)
	}
}

func TestValidateChain_ErrorMessageListsSupportedChains(t *testing.T) {
	cfg := testConfig("ethereum", "bsc")
	err := cfg.ValidateChain("solana")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("error message should not be empty")
	}
}

func TestValidateChain_EmptyChainName_ReturnsError(t *testing.T) {
	cfg := testConfig("ethereum", "bsc", "polygon", "base")
	if err := cfg.ValidateChain(""); err == nil {
		t.Error("expected error for empty chain name")
	}
}

func TestConfig_JWTSecretFromEnv(t *testing.T) {
	t.Setenv("WEB3_JWT_SECRET", "test-secret-from-env-with-enough-length")

	cfg := config.LoadFromEnv()
	if cfg.Auth.JWTSecret != "test-secret-from-env-with-enough-length" {
		t.Fatalf("JWTSecret = %q", cfg.Auth.JWTSecret)
	}
}

func TestConfig_ExternalURLFromEnvFeedsBrowserURLAndCLIURLFallback(t *testing.T) {
	t.Setenv("WEB3_EXTERNAL_URL", "http://localhost:18080")

	cfg := config.LoadFromEnv()
	if cfg.Server.ExternalURL != "http://localhost:18080" {
		t.Fatalf("Server.ExternalURL = %q", cfg.Server.ExternalURL)
	}
	if cfg.CLI.APIURL != "http://localhost:18080" {
		t.Fatalf("CLI.APIURL = %q, want external URL fallback", cfg.CLI.APIURL)
	}
}

func TestConfig_WebAuthnDefaultsDeriveFromEnvExternalURL(t *testing.T) {
	t.Setenv("WEB3_EXTERNAL_URL", "https://wallet.example.com:9443/app")

	cfg := config.LoadFromEnv()
	if cfg.WebAuthn.RPOrigin != "https://wallet.example.com:9443" {
		t.Fatalf("RPOrigin = %q, want origin derived from WEB3_EXTERNAL_URL", cfg.WebAuthn.RPOrigin)
	}
	if cfg.WebAuthn.RPID != "wallet.example.com" {
		t.Fatalf("RPID = %q, want hostname derived from WEB3_EXTERNAL_URL", cfg.WebAuthn.RPID)
	}
}

func TestConfig_WebAuthnEnvOverridesExternalURLDerivation(t *testing.T) {
	t.Setenv("WEB3_EXTERNAL_URL", "https://wallet.example.com")
	t.Setenv("WEB3_RP_ORIGIN", "https://passkeys.example.net")
	t.Setenv("WEB3_RP_ID", "passkeys.example.net")

	cfg := config.LoadFromEnv()
	if cfg.WebAuthn.RPOrigin != "https://passkeys.example.net" {
		t.Fatalf("RPOrigin = %q, want explicit WEB3_RP_ORIGIN", cfg.WebAuthn.RPOrigin)
	}
	if cfg.WebAuthn.RPID != "passkeys.example.net" {
		t.Fatalf("RPID = %q, want explicit WEB3_RP_ID", cfg.WebAuthn.RPID)
	}
}

func TestConfig_Web3APIURLOverridesExternalURLForCLIOnly(t *testing.T) {
	t.Setenv("WEB3_EXTERNAL_URL", "http://localhost:18080")
	t.Setenv("WEB3_API_URL", "http://127.0.0.1:8080")

	cfg := config.LoadFromEnv()
	if cfg.Server.ExternalURL != "http://localhost:18080" {
		t.Fatalf("Server.ExternalURL = %q", cfg.Server.ExternalURL)
	}
	if cfg.CLI.APIURL != "http://127.0.0.1:8080" {
		t.Fatalf("CLI.APIURL = %q, want WEB3_API_URL override", cfg.CLI.APIURL)
	}
}

func TestConfig_LoadsSecurityAgentFromConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[server]
external_url = "http://localhost:18080"

[security.agent]
review_mode = "warn"
allow_review_clear = false
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Security.Agent.ReviewMode != "warn" {
		t.Fatalf("ReviewMode = %q, want warn", cfg.Security.Agent.ReviewMode)
	}
	if cfg.Security.Agent.AllowReviewClear {
		t.Fatal("AllowReviewClear = true, want false")
	}
}

func TestConfig_DefaultSecurityAgentAlwaysReviewsAndAllowsReviewClear(t *testing.T) {
	cfg := config.LoadFromEnv()

	if cfg.Security.Agent.ReviewMode != "always" {
		t.Fatalf("ReviewMode = %q, want always", cfg.Security.Agent.ReviewMode)
	}
	if cfg.Security.Agent.AllowReviewClear {
		t.Fatal("AllowReviewClear = true, want false (secure default)")
	}
}

func TestConfig_ValidateRequiresJWTSecret(t *testing.T) {
	cfg := &config.Config{
		SSM: config.SSMConfig{RootKey: "test-ssm-root-key-that-is-long-enough"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Workdir: config.WorkdirConfig{Path: "./dist"},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "WEB3_JWT_SECRET") {
		t.Fatalf("Validate error = %v, want missing WEB3_JWT_SECRET", err)
	}
}

func TestConfig_ValidateAllowsEncryptedFileSSMFallbackWithoutAWS(t *testing.T) {
	cfg := &config.Config{
		SSM: config.SSMConfig{RootKey: "test-ssm-root-key-that-is-long-enough"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: "./dist"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with WEB3_SSM_KEY fallback: %v", err)
	}
}

func TestConfig_ValidateRequiresSSMRootKeyWhenAWSKMSIsNotConfigured(t *testing.T) {
	cfg := &config.Config{
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: "./dist"},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "WEB3_SSM_KEY") {
		t.Fatalf("Validate error = %v, want missing WEB3_SSM_KEY", err)
	}
}

func TestConfig_ValidateRejectsPartialAWSKMSConfig(t *testing.T) {
	cfg := &config.Config{
		AWS: config.AWSConfig{
			Region:   "us-east-1",
			KMSKeyID: "key",
		},
		SSM: config.SSMConfig{RootKey: "test-ssm-root-key-that-is-long-enough"},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: "./dist"},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "incomplete AWS KMS") {
		t.Fatalf("Validate error = %v, want incomplete AWS KMS", err)
	}
}

func TestConfig_ValidateAcceptsCompleteAWSKMSWithoutSSMRootKey(t *testing.T) {
	cfg := &config.Config{
		AWS: config.AWSConfig{
			Region: "us-east-1", KMSKeyID: "key", AccessKeyID: "access", SecretAccessKey: "secret",
		},
		WebAuthn: config.WebAuthnConfig{
			RPID: "localhost", RPOrigin: "http://localhost:8080",
		},
		Auth:    config.AuthConfig{JWTSecret: "test-jwt-secret-that-is-long-enough"},
		Workdir: config.WorkdirConfig{Path: "./dist"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with complete AWS KMS: %v", err)
	}
}

func TestConfigVerifyChainRPCsRejectsMismatchedChainID(t *testing.T) {
	cfg := &config.Config{Chains: []config.ChainConfig{
		{Name: "bsc", ChainID: 56, RPCURL: "https://rpc.example/bsc"},
	}}

	err := cfg.VerifyChainRPCs(t.Context(), func(ctx context.Context, rpcURL string, chainID int) error {
		if chainID != 56 {
			t.Fatalf("chainID = %d, want 56", chainID)
		}
		return fmt.Errorf("rpc chain id 8453 does not match configured chain id 56")
	})
	if err == nil || !strings.Contains(err.Error(), "bsc") || strings.Contains(err.Error(), "rpc.example") {
		t.Fatalf("VerifyChainRPCs error = %v, want chain name without URL leak", err)
	}
}

func TestCanonicalToken_KnownSymbol_ReturnsAddress(t *testing.T) {
	cfg := &config.Config{Chains: []config.ChainConfig{{Name: "ethereum"}}}
	tok, ok := cfg.CanonicalToken("ethereum", "USDC")
	if !ok {
		t.Fatal("expected USDC to be found on ethereum")
	}
	if tok.Address != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Errorf("wrong address: %s", tok.Address)
	}
	if tok.Decimals != 6 {
		t.Errorf("wrong decimals: %d", tok.Decimals)
	}
}

func TestCanonicalToken_UnknownSymbol_ReturnsFalse(t *testing.T) {
	cfg := &config.Config{Chains: []config.ChainConfig{{Name: "ethereum"}}}
	_, ok := cfg.CanonicalToken("ethereum", "UNKNOWN")
	if ok {
		t.Error("expected false for unknown symbol")
	}
}

func TestCanonicalToken_CaseInsensitiveSymbol(t *testing.T) {
	cfg := &config.Config{Chains: []config.ChainConfig{{Name: "ethereum"}}}
	_, ok := cfg.CanonicalToken("ethereum", "usdc")
	if !ok {
		t.Error("expected symbol lookup to be case-insensitive")
	}
}

func TestCanonicalToken_RichDefaultsForSupportedChains(t *testing.T) {
	cfg := &config.Config{Chains: []config.ChainConfig{
		{Name: "ethereum"},
		{Name: "base"},
		{Name: "polygon"},
		{Name: "bsc"},
	}}
	tests := []struct {
		chain    string
		symbol   string
		address  string
		decimals int
	}{
		{chain: "ethereum", symbol: "USDT", address: "0xdAC17F958D2ee523a2206206994597C13D831ec7", decimals: 6},
		{chain: "ethereum", symbol: "DAI", address: "0x6B175474E89094C44Da98b954EedeAC495271d0F", decimals: 18},
		{chain: "ethereum", symbol: "WBTC", address: "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", decimals: 8},
		{chain: "base", symbol: "USDT", address: "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2", decimals: 6},
		{chain: "base", symbol: "cbBTC", address: "0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf", decimals: 8},
		{chain: "base", symbol: "EURC", address: "0x60a3e35cc302bfa44cb288bc5a4f316fdb1adb42", decimals: 6},
		{chain: "polygon", symbol: "USDT", address: "0xc2132D05D31c914a87C6611C10748AEb04B58e8F", decimals: 6},
		{chain: "polygon", symbol: "DAI", address: "0x8f3Cf7ad23Cd3CaDbD9735AFf958023239c6A063", decimals: 18},
		{chain: "polygon", symbol: "WBTC", address: "0x1BFD67037B42Cf73acF2047067bd4F2C47D9BfD6", decimals: 8},
		{chain: "bsc", symbol: "USDT", address: "0x55d398326f99059fF775485246999027B3197955", decimals: 18},
		{chain: "bsc", symbol: "BTCB", address: "0x7130d2A12B9BCbFAe4f2634d864A1Ee1Ce3Ead9c", decimals: 18},
		{chain: "bsc", symbol: "DAI", address: "0x1AF3F329e8BE154074D8769D1FFa4eE058B1DBc3", decimals: 18},
	}

	for _, tt := range tests {
		t.Run(tt.chain+"/"+tt.symbol, func(t *testing.T) {
			token, ok := cfg.CanonicalToken(tt.chain, tt.symbol)
			if !ok {
				t.Fatalf("expected %s on %s", tt.symbol, tt.chain)
			}
			if token.Address != tt.address {
				t.Fatalf("address = %s, want %s", token.Address, tt.address)
			}
			if token.Decimals != tt.decimals {
				t.Fatalf("decimals = %d, want %d", token.Decimals, tt.decimals)
			}
		})
	}
}
