// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package settings

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	ProviderDeepSeek = "deepseek"

	DefaultProvider = ProviderDeepSeek
	DefaultBaseURL  = "https://api.deepseek.com/anthropic"
	DefaultModel    = "deepseek-v4-pro"
)

type LLMConfig struct {
	BaseURL string
}

type LLMService struct {
	cfg    LLMConfig
	client *http.Client
}

type RuntimeProvider struct {
	Provider  string
	BaseURL   string
	APIKey    string
	ModelName string
}

func NewLLMService(_ interface{}, _ interface{}, cfg LLMConfig) *LLMService {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return &LLMService{cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
}

func NormalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return DefaultProvider
	}
	return provider
}

func NormalizeRuntime(runtime RuntimeProvider) RuntimeProvider {
	runtime.Provider = NormalizeProvider(runtime.Provider)
	runtime.BaseURL = strings.TrimRight(strings.TrimSpace(runtime.BaseURL), "/")
	if runtime.BaseURL == "" {
		runtime.BaseURL = strings.TrimRight(svcDefaultBaseURL(), "/")
	}
	runtime.ModelName = strings.TrimSpace(runtime.ModelName)
	if runtime.ModelName == "" {
		runtime.ModelName = DefaultModel
	}
	runtime.APIKey = strings.TrimSpace(runtime.APIKey)
	return runtime
}

func (s *LLMService) VerifyRuntimeProvider(ctx context.Context, runtime RuntimeProvider) error {
	runtime = NormalizeRuntime(runtime)
	if runtime.APIKey == "" {
		return fmt.Errorf("provider API key is empty")
	}
	client := anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(runtime.BaseURL),
		option.WithAuthToken(runtime.APIKey),
		option.WithRequestTimeout(10*time.Second),
		option.WithHTTPClient(s.client),
	)
	_, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(runtime.ModelName),
		MaxTokens: 10,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("hello")),
		},
	})
	return err
}

func KeyHint(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func svcDefaultBaseURL() string {
	return DefaultBaseURL
}
