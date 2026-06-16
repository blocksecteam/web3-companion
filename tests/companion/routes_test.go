// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/config"
)

func TestDefaultConfigDoesNotReadModelSecretsFromEnvFilesOrClaudeSettings(t *testing.T) {
	backendRoot := t.TempDir()
	workdir := filepath.Join(backendRoot, "dist")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-token")
	t.Setenv("ANTHROPIC_API_KEY", "env-api-key")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	writeFileForTest(t, filepath.Join(backendRoot, ".env.cli"), "ANTHROPIC_AUTH_TOKEN=cli-token\n", 0o600)
	writeFileForTest(t, filepath.Join(backendRoot, ".env"), "ANTHROPIC_API_KEY=env-file-key\n", 0o600)
	writeFileForTest(t, filepath.Join(workdir, "home", ".claude", "settings.json"), `{"env":{"ANTHROPIC_AUTH_TOKEN":"settings-token"}}`+"\n", 0o600)

	cfg := companion.DefaultConfig(backendRoot, workdir)
	cmd := cfg.BuildCommand("user_env_isolation", 19090)

	if envContainsAny(cmd.Env, "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "env-token", "env-api-key", "cli-token", "env-file-key", "settings-token") {
		t.Fatalf("companion env leaked model secrets: %#v", cmd.Env)
	}
}

func TestDefaultConfigFallsBackToEnvConfigWhenConfigFileIsInvalid(t *testing.T) {
	backendRoot := t.TempDir()
	badConfig := filepath.Join(backendRoot, "bad.toml")
	if err := os.WriteFile(badConfig, []byte("not = [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB3_CONFIG_PATH", badConfig)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("DefaultConfig panicked with invalid config file: %v", recovered)
		}
	}()
	cfg := companion.DefaultConfig(backendRoot, filepath.Join(backendRoot, "dist"))

	if cfg.BackendAPIURL != "http://127.0.0.1:8080" {
		t.Fatalf("BackendAPIURL = %q, want default container-local URL", cfg.BackendAPIURL)
	}
}

func TestDefaultConfigUsesServerExternalURL(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("WEB3_API_URL", "")

	cfg := companion.DefaultConfig(backendRoot, filepath.Join(backendRoot, "dist"), &config.Config{
		Server: config.ServerConfig{ExternalURL: "http://localhost:8081", Port: 8080},
	})

	if cfg.BackendAPIURL != "http://127.0.0.1:8080" {
		t.Fatalf("BackendAPIURL = %q, want container-local server URL", cfg.BackendAPIURL)
	}
}

func TestDefaultConfigUsesServerPortForContainerLocalBackendURL(t *testing.T) {
	backendRoot := t.TempDir()
	t.Setenv("WEB3_API_URL", "")

	cfg := companion.DefaultConfig(backendRoot, filepath.Join(backendRoot, "dist"), &config.Config{
		Server: config.ServerConfig{ExternalURL: "http://localhost:8083", Port: 19090},
	})

	if cfg.BackendAPIURL != "http://127.0.0.1:19090" {
		t.Fatalf("BackendAPIURL = %q, want server port URL", cfg.BackendAPIURL)
	}
}

func TestBuildCommandUsesDirectRuntimeAndAllowlistedEnvironment(t *testing.T) {
	homeRoot := t.TempDir()
	cfg := companion.Config{
		HomeRoot:             homeRoot,
		RuntimePath:          "/opt/web3/bin/bun",
		ScriptPath:           "/opt/web3/companion-server.js",
		ClaudeExecutablePath: "/opt/web3/bin/claude",
		BackendRoot:          "/opt/web3",
		ListenHost:           "127.0.0.1",
		BackendAPIURL:        "http://localhost:8080",
	}
	if _, err := cfg.PrepareUserHome("user_direct", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}

	cmd := cfg.BuildCommandWithStartup("user_direct", 0, filepath.Join(cfg.UserHome("user_direct"), "tmp", "ready.json"), "nonce")
	if cmd.Name != "/opt/web3/bin/bun" {
		t.Fatalf("command name = %q, want direct Bun runtime", cmd.Name)
	}
	if !slices.Equal(cmd.Args, []string{"/opt/web3/companion-server.js"}) {
		t.Fatalf("command args = %#v", cmd.Args)
	}
	if cmd.Dir != filepath.Join(cfg.UserHome("user_direct"), "workdir") {
		t.Fatalf("command dir = %q, want per-user workdir", cmd.Dir)
	}
	for _, want := range []string{
		"HOME=" + cfg.UserHome("user_direct"),
		"TMPDIR=" + filepath.Join(cfg.UserHome("user_direct"), "tmp"),
		"COMPANION_SESSION_STORE=sqlite:file=" + filepath.Join(cfg.UserHome("user_direct"), "sessions.sqlite"),
		"WEB3_AGENT_WORKDIR=" + filepath.Join(cfg.UserHome("user_direct"), "workdir"),
		"WEB3_REPO_SKILLS_DIR=/opt/web3/skills",
		"CLAUDE_CONFIG_DIR=" + filepath.Join(cfg.UserHome("user_direct"), ".claude"),
		"CLAUDE_CODE_EXECUTABLE=/opt/web3/bin/claude",
		"WEB3_CLI_PATH=/opt/web3/dist/web3",
	} {
		if !slices.Contains(cmd.Env, want) {
			t.Fatalf("command env missing %q: %#v", want, cmd.Env)
		}
	}
	if envContainsAny(cmd.Env, "WEB3_SSM_KEY", "AWS_SECRET_ACCESS_KEY", "WEB3_JWT_SECRET", "WEB3_LLM_API_KEY", "WEB3_RPC_ETHEREUM_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY") {
		t.Fatalf("command env leaked forbidden secrets: %#v", cmd.Env)
	}
}
