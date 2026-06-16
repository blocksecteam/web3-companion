// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion

import (
	"encoding/json"
	"fmt"

	"os"
	"path/filepath"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/settings"
)

func (c Config) EnsureUserClaudeSettings(userID string) error {
	current, err := c.ReadUserClaudeSettings(userID)
	if err == nil && settingsHasProvider(current) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return c.WriteUserClaudeSettings(userID, LLMProviderConfig{Provider: settings.DefaultProvider, BaseURL: settings.DefaultBaseURL, ModelName: settings.DefaultModel})
}

func (c Config) WriteUserClaudeSettings(userID string, provider LLMProviderConfig) error {
	path := filepath.Join(c.UserHome(userID), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir claude settings dir: %w", err)
	}
	return writeClaudeSettings(path, c.modelSettings(provider))
}

func (c Config) ReadUserClaudeSettings(userID string) (claudeSettings, error) {
	path := filepath.Join(c.UserHome(userID), ".claude", "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return claudeSettings{}, err
	}
	var settings claudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return claudeSettings{}, err
	}
	return settings, nil
}

func settingsHasProvider(settings claudeSettings) bool {
	return strings.TrimSpace(settings.Model) != "" &&
		strings.TrimSpace(settings.Env["ANTHROPIC_BASE_URL"]) != "" &&
		strings.TrimSpace(settings.Env["ANTHROPIC_MODEL"]) != "" &&
		(strings.TrimSpace(settings.Env["ANTHROPIC_AUTH_TOKEN"]) != "" ||
			strings.TrimSpace(settings.Env["ANTHROPIC_API_KEY"]) != "")
}

func (c Config) modelSettings(provider LLMProviderConfig) claudeSettings {
	providerName := settings.NormalizeProvider(provider.Provider)
	model := strings.TrimSpace(provider.ModelName)
	if model == "" {
		model = settings.DefaultModel
	}
	baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if baseURL == "" {
		baseURL = settings.DefaultBaseURL
	}
	env := map[string]string{
		"ANTHROPIC_BASE_URL":             baseURL,
		"ANTHROPIC_MODEL":                model,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL": model,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  model,
		"CLAUDE_CODE_SUBAGENT_MODEL":     model,
		"CLAUDE_CODE_EFFORT_LEVEL":       "max",
	}
	authToken := strings.TrimSpace(provider.APIKey)
	if authToken != "" {
		env["ANTHROPIC_AUTH_TOKEN"] = authToken
	}
	_ = providerName
	return claudeSettings{
		Schema:      "https://json.schemastore.org/claude-code-settings.json",
		Model:       model,
		EffortLevel: "xhigh",
		Env:         env,
		Permissions: claudePermissions{
			Allow: web3ClaudeAllowRules(),
		},
	}
}

type claudeSettings struct {
	Schema          string            `json:"$schema"`
	Model           string            `json:"model"`
	EffortLevel     string            `json:"effortLevel"`
	Env             map[string]string `json:"env"`
	Permissions     claudePermissions `json:"permissions"`
	ModelOverrides  map[string]string `json:"modelOverrides,omitempty"`
	AvailableModels []string          `json:"availableModels,omitempty"`
}

type claudePermissions struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny,omitempty"`
}

type claudeHomeConfig struct {
	Projects               map[string]claudeProjectConfig `json:"projects"`
	HasCompletedOnboarding bool                           `json:"hasCompletedOnboarding"`
}

type claudeProjectConfig struct {
	AllowedTools                  []string `json:"allowedTools"`
	HasTrustDialogAccepted        bool     `json:"hasTrustDialogAccepted"`
	HasCompletedProjectOnboarding bool     `json:"hasCompletedProjectOnboarding"`
}

func writeClaudeSettings(path string, settings claudeSettings) error {
	preserveClaudeAuth(path, settings.Env)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude settings: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write claude settings: %w", err)
	}
	return nil
}

func preserveClaudeAuth(path string, env map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var existing claudeSettings
	if err := json.Unmarshal(data, &existing); err != nil {
		return
	}
	for _, key := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if strings.TrimSpace(env[key]) == "" && strings.TrimSpace(existing.Env[key]) != "" {
			env[key] = strings.TrimSpace(existing.Env[key])
		}
	}
}

func ensureClaudeHomeConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat claude home config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir claude home config dir: %w", err)
	}
	cfg := claudeHomeConfig{
		HasCompletedOnboarding: true,
		Projects: map[string]claudeProjectConfig{
			filepath.Join(filepath.Dir(filepath.Dir(path)), "workdir"): {
				AllowedTools:                  web3ClaudeAllowRules(),
				HasTrustDialogAccepted:        true,
				HasCompletedProjectOnboarding: true,
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal claude home config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write claude home config: %w", err)
	}
	return nil
}

func web3ClaudeAllowRules() []string {
	return []string{
		"Bash(web3)",
		"Bash(web3 *)",
	}
}
