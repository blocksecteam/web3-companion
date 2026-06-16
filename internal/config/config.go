// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package config

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type canonicalTokenLookup func(chain, symbol string) (CanonicalToken, bool)
type ChainRPCVerifier func(ctx context.Context, rpcURL string, chainID int) error

var canonicalTokenProvider canonicalTokenLookup

func SetCanonicalTokenProvider(provider canonicalTokenLookup) {
	canonicalTokenProvider = provider
}

type Config struct {
	Server    ServerConfig    `toml:"server"`
	DB        DBConfig        `toml:"db"`
	WebAuthn  WebAuthnConfig  `toml:"-"`
	AWS       AWSConfig       `toml:"-"`
	SSM       SSMConfig       `toml:"-"`
	Auth      AuthConfig      `toml:"-"`
	CLI       CLIConfig       `toml:"-"`
	LLM       LLMConfig       `toml:"-"`
	Providers ProvidersConfig `toml:"-"`
	CORS      CORSConfig      `toml:"-"`
	Security  SecurityConfig  `toml:"security"`
	Chains    []ChainConfig   `toml:"chains"`
	Workdir   WorkdirConfig   `toml:"-"`
	Log       LogConfig       `toml:"log"`
	SetupMode bool            `toml:"-"`
}

type ServerConfig struct {
	Host         string `toml:"host"`
	Port         int    `toml:"port"`
	ReadTimeout  int    `toml:"read_timeout_seconds"`
	WriteTimeout int    `toml:"write_timeout_seconds"`
	ExternalURL  string `toml:"external_url"`
}

type DBConfig struct {
	Driver string `toml:"driver"`
	DSN    string `toml:"dsn"`
}

type AWSConfig struct {
	Region          string `toml:"region"`
	KMSKeyID        string `toml:"kms_key_id"`
	AccessKeyID     string `toml:"access_key_id"`
	SecretAccessKey string `toml:"secret_access_key"`
}

type SSMConfig struct {
	RootKey string
}

type WebAuthnConfig struct {
	RPID                       string `toml:"rp_id"`
	RPName                     string `toml:"rp_name"`
	RPOrigin                   string `toml:"rp_origin"`
	ChallengeTimeoutSeconds    int    `toml:"challenge_timeout_seconds"`
	RegistrationTimeoutSeconds int    `toml:"registration_timeout_seconds"`
}

type AuthConfig struct {
	JWTSecret string `toml:"jwt_secret"`
}

type CLIConfig struct {
	APIURL        string
	WalletAddress string
}

type LLMConfig struct {
	Provider  string
	BaseURL   string
	ModelName string
	APIKey    string
}

type ProvidersConfig struct {
	PortalsAPIKey    string
	MetaSleuthAPIKey string
}

type CORSConfig struct {
	AllowedOrigins []string
}

type SecurityConfig struct {
	Agent SecurityAgentConfig `toml:"agent"`
}

type SecurityAgentConfig struct {
	ReviewMode       string `toml:"review_mode"`
	AllowReviewClear bool   `toml:"allow_review_clear"`
}

type CanonicalToken struct {
	Address  string `toml:"address"  json:"address"`
	Decimals int    `toml:"decimals" json:"decimals"`
}

type ChainConfig struct {
	Name            string                    `toml:"name"`
	ChainID         int                       `toml:"chain_id"`
	RPCURL          string                    `toml:"rpc_url"`
	NativeSymbol    string                    `toml:"native_symbol"`
	CanonicalTokens map[string]CanonicalToken `toml:"-"`
}

type WorkdirConfig struct {
	Path string
}

type LogConfig struct {
	Level      string `toml:"level"`
	FilePath   string `toml:"file_path"`
	MaxSizeMB  int    `toml:"max_size_mb"`
	MaxBackups int    `toml:"max_backups"`
	MaxAgeDays int    `toml:"max_age_days"`
}

func Load(path string) (*Config, error) {
	cfg := defaultConfig()
	if path == "" {
		path = os.Getenv("WEB3_CONFIG_PATH")
	}
	if path == "" {
		cfg.applyEnvOverrides()
		return cfg, nil
	}
	md, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("config load: %w", err)
	}
	cfg.applyConfigCompatibility(&md)
	if md.IsDefined("companion") {
		return nil, fmt.Errorf("config load: companion section has been removed; companion runtime settings are source constants")
	}
	cfg.applyEnvOverrides()
	return cfg, nil
}

func LoadFromEnv() *Config {
	cfg := defaultConfig()
	cfg.applyEnvOverrides()
	return cfg
}

func (c *Config) applyConfigCompatibility(md *toml.MetaData) {
}

func LoadOrEnv(path string) *Config {
	cfg, err := Load(path)
	if err == nil {
		return cfg
	}
	return LoadFromEnv()
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30,
			WriteTimeout: 30,
			ExternalURL:  "http://localhost:8080",
		},
		DB: DBConfig{
			Driver: "sqlite",
			DSN:    "web3.db",
		},
		WebAuthn: WebAuthnConfig{
			RPName:                     "The Web3 Companion",
			ChallengeTimeoutSeconds:    120,
			RegistrationTimeoutSeconds: 300,
		},
		Workdir: WorkdirConfig{
			Path: defaultWorkdirPath(),
		},
		CLI: CLIConfig{
			APIURL: "http://localhost:8080",
		},
		Log: LogConfig{
			Level:      "info",
			MaxSizeMB:  100,
			MaxBackups: 7,
			MaxAgeDays: 30,
		},
		Security: SecurityConfig{
			Agent: SecurityAgentConfig{
				ReviewMode:       "always",
				AllowReviewClear: false,
			},
		},
	}
}

func (c *Config) applyEnvOverrides() {
	env := ReadEnv()
	c.ApplyEnv(env)
}

type Env struct {
	values map[string]string
}

func ReadEnv() Env {
	return Env{values: map[string]string{
		"AWS_REGION":                        os.Getenv("AWS_REGION"),
		"AWS_KMS_KEY_ID":                    os.Getenv("AWS_KMS_KEY_ID"),
		"AWS_ACCESS_KEY_ID":                 os.Getenv("AWS_ACCESS_KEY_ID"),
		"AWS_SECRET_ACCESS_KEY":             os.Getenv("AWS_SECRET_ACCESS_KEY"),
		"WEB3_SSM_KEY":                      os.Getenv("WEB3_SSM_KEY"),
		"WEB3_JWT_SECRET":                   os.Getenv("WEB3_JWT_SECRET"),
		"WEB3_EXTERNAL_URL":                 os.Getenv("WEB3_EXTERNAL_URL"),
		"WEB3_API_URL":                      os.Getenv("WEB3_API_URL"),
		"WEB3_WALLET_ADDRESS":               os.Getenv("WEB3_WALLET_ADDRESS"),
		"WEB3_RP_ID":                        os.Getenv("WEB3_RP_ID"),
		"WEB3_RP_NAME":                      os.Getenv("WEB3_RP_NAME"),
		"WEB3_RP_ORIGIN":                    os.Getenv("WEB3_RP_ORIGIN"),
		"WEB3_CHALLENGE_TIMEOUT_SECONDS":    os.Getenv("WEB3_CHALLENGE_TIMEOUT_SECONDS"),
		"WEB3_REGISTRATION_TIMEOUT_SECONDS": os.Getenv("WEB3_REGISTRATION_TIMEOUT_SECONDS"),
		"WEB3_WORKDIR":                      os.Getenv("WEB3_WORKDIR"),
		"WEB3_LLM_PROVIDER":                 os.Getenv("WEB3_LLM_PROVIDER"),
		"WEB3_LLM_BASE_URL":                 os.Getenv("WEB3_LLM_BASE_URL"),
		"WEB3_LLM_MODEL":                    os.Getenv("WEB3_LLM_MODEL"),
		"WEB3_LLM_API_KEY":                  os.Getenv("WEB3_LLM_API_KEY"),
		"WEB3_RPC_ETHEREUM_URL":             os.Getenv("WEB3_RPC_ETHEREUM_URL"),
		"WEB3_RPC_BASE_URL":                 os.Getenv("WEB3_RPC_BASE_URL"),
		"WEB3_RPC_POLYGON_URL":              os.Getenv("WEB3_RPC_POLYGON_URL"),
		"WEB3_RPC_BSC_URL":                  os.Getenv("WEB3_RPC_BSC_URL"),
		"PORTALS_API_KEY":                   os.Getenv("PORTALS_API_KEY"),
		"METASLEUTH_API_KEY":                os.Getenv("METASLEUTH_API_KEY"),
		"WEB3_ALLOWED_ORIGINS":              os.Getenv("WEB3_ALLOWED_ORIGINS"),
	}}
}

func (e Env) Get(key string) string {
	return strings.TrimSpace(e.values[key])
}

func (c *Config) ApplyEnv(env Env) {
	if v := env.Get("AWS_REGION"); v != "" {
		c.AWS.Region = v
	}
	if v := env.Get("AWS_KMS_KEY_ID"); v != "" {
		c.AWS.KMSKeyID = v
	}
	if v := env.Get("AWS_ACCESS_KEY_ID"); v != "" {
		c.AWS.AccessKeyID = v
	}
	if v := env.Get("AWS_SECRET_ACCESS_KEY"); v != "" {
		c.AWS.SecretAccessKey = v
	}
	if v := env.Get("WEB3_SSM_KEY"); v != "" {
		c.SSM.RootKey = v
	}
	if v := env.Get("WEB3_JWT_SECRET"); v != "" {
		c.Auth.JWTSecret = v
	}
	if v := env.Get("WEB3_EXTERNAL_URL"); v != "" {
		c.Server.ExternalURL = v
	}
	if v := env.Get("WEB3_API_URL"); v != "" {
		c.CLI.APIURL = v
	} else if strings.TrimSpace(c.Server.ExternalURL) != "" {
		c.CLI.APIURL = strings.TrimSpace(c.Server.ExternalURL)
	}
	if v := env.Get("WEB3_WALLET_ADDRESS"); v != "" {
		c.CLI.WalletAddress = v
	}
	if v := env.Get("WEB3_RP_ID"); v != "" {
		c.WebAuthn.RPID = v
	}
	if v := env.Get("WEB3_RP_NAME"); v != "" {
		c.WebAuthn.RPName = v
	}
	if v := env.Get("WEB3_RP_ORIGIN"); v != "" {
		c.WebAuthn.RPOrigin = v
	}
	if v := env.Get("WEB3_CHALLENGE_TIMEOUT_SECONDS"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			c.WebAuthn.ChallengeTimeoutSeconds = seconds
		}
	}
	if v := env.Get("WEB3_REGISTRATION_TIMEOUT_SECONDS"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil {
			c.WebAuthn.RegistrationTimeoutSeconds = seconds
		}
	}

	if v := env.Get("WEB3_WORKDIR"); v != "" {
		c.Workdir.Path = v
	}
	if v := env.Get("WEB3_LLM_PROVIDER"); v != "" {
		c.LLM.Provider = v
	}
	if v := env.Get("WEB3_LLM_BASE_URL"); v != "" {
		c.LLM.BaseURL = v
	}
	if v := env.Get("WEB3_LLM_MODEL"); v != "" {
		c.LLM.ModelName = v
	}
	if v := env.Get("WEB3_LLM_API_KEY"); v != "" {
		c.LLM.APIKey = v
	}
	if v := env.Get("PORTALS_API_KEY"); v != "" {
		c.Providers.PortalsAPIKey = v
	}
	if v := env.Get("METASLEUTH_API_KEY"); v != "" {
		c.Providers.MetaSleuthAPIKey = v
	}
	if v := env.Get("WEB3_ALLOWED_ORIGINS"); v != "" {
		c.CORS.AllowedOrigins = splitCSV(v)
	}
	for i := range c.Chains {
		if v := env.Get(chainRPCEnvName(c.Chains[i].Name)); v != "" {
			c.Chains[i].RPCURL = v
		}
		c.Chains[i].RPCURL = expandEnvPlaceholders(c.Chains[i].RPCURL)
	}
	c.deriveWebAuthnDefaults()
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.DB.Driver) != "" && !strings.EqualFold(strings.TrimSpace(c.DB.Driver), "sqlite") {
		return fmt.Errorf("only sqlite database is supported")
	}
	awsValues := []string{c.AWS.Region, c.AWS.KMSKeyID, c.AWS.AccessKeyID, c.AWS.SecretAccessKey}
	awsSet := 0
	for _, value := range awsValues {
		if strings.TrimSpace(value) != "" {
			awsSet++
		}
	}
	if awsSet > 0 && awsSet < len(awsValues) {
		return fmt.Errorf("incomplete AWS KMS configuration: set AWS_REGION, AWS_KMS_KEY_ID, AWS_ACCESS_KEY_ID, and AWS_SECRET_ACCESS_KEY, or unset all AWS KMS variables")
	}
	if awsSet == 0 && strings.TrimSpace(c.SSM.RootKey) == "" {
		return fmt.Errorf("WEB3_SSM_KEY is required when AWS KMS is not configured")
	}
	if awsSet == 0 && len([]byte(strings.TrimSpace(c.SSM.RootKey))) < 32 {
		return fmt.Errorf("WEB3_SSM_KEY must be at least 32 bytes")
	}
	if strings.TrimSpace(c.Auth.JWTSecret) == "" {
		return fmt.Errorf("WEB3_JWT_SECRET is required")
	}
	if c.WebAuthn.RPID == "" {
		return fmt.Errorf("WEB3_RP_ID is required")
	}
	if c.WebAuthn.RPOrigin == "" {
		return fmt.Errorf("WEB3_RP_ORIGIN is required")
	}
	if c.Workdir.Path == "" {
		return fmt.Errorf("WEB3_WORKDIR is required")
	}

	for _, ch := range c.Chains {
		if missing := unresolvedEnvPlaceholders(ch.RPCURL); len(missing) > 0 {
			return fmt.Errorf("%s is required (unresolved placeholder in RPC URL for chain %q). Set it in docker/.env", missing[0], ch.Name)
		}
	}
	return nil
}

func (c *Config) ValidateForSetup() error {
	if strings.TrimSpace(c.DB.Driver) != "" && !strings.EqualFold(strings.TrimSpace(c.DB.Driver), "sqlite") {
		return fmt.Errorf("only sqlite database is supported")
	}
	if c.Workdir.Path == "" {
		return fmt.Errorf("WEB3_WORKDIR is required")
	}
	return nil
}

func (c *Config) VerifyChainRPCs(ctx context.Context, verify ChainRPCVerifier) error {
	if c == nil || verify == nil {
		return nil
	}
	for _, ch := range c.Chains {
		name := strings.TrimSpace(ch.Name)
		rpcURL := strings.TrimSpace(ch.RPCURL)
		if name == "" || rpcURL == "" {
			continue
		}
		if err := verify(ctx, rpcURL, ch.ChainID); err != nil {
			return fmt.Errorf("RPC URL for chain %q failed chain-id verification: %w", name, err)
		}
	}
	return nil
}

func (c *Config) deriveWebAuthnDefaults() {
	origin, rpID := webAuthnDefaultsFromExternalURL(c.Server.ExternalURL)
	if strings.TrimSpace(c.WebAuthn.RPOrigin) == "" {
		c.WebAuthn.RPOrigin = origin
	}
	if strings.TrimSpace(c.WebAuthn.RPID) == "" {
		c.WebAuthn.RPID = rpID
	}
}

func webAuthnDefaultsFromExternalURL(raw string) (string, string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ""
	}
	return parsed.Scheme + "://" + parsed.Host, parsed.Hostname()
}

func (c *Config) SSMKeysDir() string {
	if c.Workdir.Path == "" {
		return filepath.Join(defaultWorkdirPath(), "keys")
	}
	return filepath.Join(c.Workdir.Path, "keys")
}

func (c *Config) SettingsDir() string {
	if c.Workdir.Path == "" {
		return filepath.Join(defaultWorkdirPath(), "settings")
	}
	return filepath.Join(c.Workdir.Path, "settings")
}

func (c *Config) ActiveChains() []ChainConfig {
	var active []ChainConfig
	for _, ch := range c.Chains {
		if strings.TrimSpace(ch.RPCURL) != "" && !strings.HasPrefix(ch.RPCURL, "${") {
			active = append(active, ch)
		}
	}
	return active
}

func (c *Config) ChainByID(id int) *ChainConfig {
	for i := range c.Chains {
		if c.Chains[i].ChainID == id {
			return &c.Chains[i]
		}
	}
	return nil
}

func (c *Config) ChainByName(name string) *ChainConfig {
	lower := strings.ToLower(name)
	for i := range c.Chains {
		if strings.ToLower(c.Chains[i].Name) == lower {
			return &c.Chains[i]
		}
	}
	return nil
}

func (c *Config) CanonicalToken(chain, symbol string) (CanonicalToken, bool) {
	if canonicalTokenProvider != nil {
		if token, ok := canonicalTokenProvider(chain, symbol); ok {
			return token, true
		}
	}
	ch := c.ChainByName(chain)
	if ch == nil {
		return CanonicalToken{}, false
	}
	upper := strings.ToUpper(symbol)
	for k, v := range ch.CanonicalTokens {
		if strings.ToUpper(k) == upper {
			return v, true
		}
	}
	return CanonicalToken{}, false
}

func defaultWorkdirPath() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	return "."
}

func chainRPCEnvName(chainName string) string {
	name := strings.ToUpper(strings.TrimSpace(chainName))
	name = strings.NewReplacer("-", "_", " ", "_").Replace(name)
	if name == "" {
		return ""
	}
	return "WEB3_RPC_" + name + "_URL"
}

func (c *Config) ValidateChain(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("chain name is required")
	}
	if c.ChainByName(name) != nil {
		return nil
	}
	supported := make([]string, len(c.Chains))
	for i, ch := range c.Chains {
		supported[i] = ch.Name
	}
	return fmt.Errorf("unsupported chain %q — supported: %s", name, strings.Join(supported, ", "))
}

func (c *Config) GetSignChallengeTimeout() time.Duration {
	if c.WebAuthn.ChallengeTimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(c.WebAuthn.ChallengeTimeoutSeconds) * time.Second
}

func (c *Config) GetRegistrationTimeout() time.Duration {
	if c.WebAuthn.RegistrationTimeoutSeconds <= 0 {
		return 300 * time.Second
	}
	return time.Duration(c.WebAuthn.RegistrationTimeoutSeconds) * time.Second
}

var envPlaceholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandEnvPlaceholders(value string) string {
	return envPlaceholderPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if envValue, ok := os.LookupEnv(name); ok {
			return envValue
		}
		return match
	})
}

func unresolvedEnvPlaceholders(value string) []string {
	matches := envPlaceholderPattern.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return nil
	}
	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if _, ok := seen[match[1]]; ok {
			continue
		}
		seen[match[1]] = struct{}{}
		names = append(names, match[1])
	}
	return names
}
