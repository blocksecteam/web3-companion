// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/companion"
)

func TestMain(m *testing.M) {
	switch os.Getenv("COMPANION_TEST_HELPER") {
	case "delayed-ready":
		runCompanionHelper(parseHelperDelay())
		return
	case "mock-health":
		runMockHealthHelper()
		return
	case "term-aware":
		runCompanionHelper(0)
		return
	default:
		os.Exit(m.Run())
	}
}

func TestUserHomeAndSQLiteDBPathArePerUser(t *testing.T) {
	cfg := companion.Config{
		HomeRoot: t.TempDir(),
	}

	home := cfg.UserHome("user/with odd spaces")
	if !strings.HasPrefix(home, cfg.HomeRoot) {
		t.Fatalf("home %q must be under root %q", home, cfg.HomeRoot)
	}
	if strings.Contains(home, "/with odd spaces") || strings.Contains(home, " ") {
		t.Fatalf("home path should use sanitized user id, got %q", home)
	}
	if got, want := cfg.SQLiteDBPath("user/with odd spaces"), filepath.Join(home, "sessions.sqlite"); got != want {
		t.Fatalf("SQLiteDBPath() = %q, want %q", got, want)
	}
}

func TestWriteContainerCredentialUsesFixedFileAndDoesNotExposeSecretInEnv(t *testing.T) {
	cfg := companion.Config{
		HomeRoot:      t.TempDir(),
		RuntimePath:   "/usr/bin/bun",
		ScriptPath:    "/srv/companion-server.js",
		BackendRoot:   "/srv/backend",
		ListenHost:    "127.0.0.1",
		BackendAPIURL: "http://localhost:8080",
	}

	credential := companion.ContainerCredential{
		Version:      1,
		Client:       "container",
		CredentialID: "cc_1",
		Secret:       "secret-container-credential",
		APIURL:       "http://localhost:8080",
	}
	home, err := cfg.PrepareUserHome("user_1", companion.AuthHandoff{ContainerCredential: credential}, testRuntimeProvider())
	if err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".local", "web3", "container-credential.json"))
	if err != nil {
		t.Fatalf("read container credential: %v", err)
	}
	if !strings.Contains(string(data), `"credential_id":"cc_1"`) ||
		!strings.Contains(string(data), `"secret":"secret-container-credential"`) {
		t.Fatalf("container credential data = %q", string(data))
	}
	cmd := cfg.BuildCommand("user_1", 19001)
	for _, item := range cmd.Env {
		if strings.Contains(item, "secret-container-credential") {
			t.Fatalf("command env must not expose auth tokens: %#v", cmd.Env)
		}
	}
	if envContainsAny(cmd.Env, "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_EFFORT_LEVEL") {
		t.Fatalf("model/provider secrets and defaults should live in settings.json, not env: %#v", cmd.Env)
	}
}

func TestBuildCommandPassesSecurityAgentConfigFromToml(t *testing.T) {
	cfg := companion.Config{
		HomeRoot:      t.TempDir(),
		RuntimePath:   "/usr/bin/bun",
		ScriptPath:    "/srv/companion-server.js",
		BackendRoot:   "/srv/backend",
		ListenHost:    "127.0.0.1",
		BackendAPIURL: "http://localhost:8080",
		SecurityAgent: companion.SecurityAgentConfig{
			ReviewMode:       "warn",
			AllowReviewClear: false,
		},
	}

	cmd := cfg.BuildCommand("user_security_agent", 19002)

	for _, want := range []string{
		"WEB3_SECURITY_AGENT_CONFIG={\"review_mode\":\"warn\",\"allow_review_clear\":false}",
	} {
		if !envHas(cmd.Env, want) {
			t.Fatalf("command env missing %q: %#v", want, cmd.Env)
		}
	}
}

func TestBuildCommandDefaultsSecurityAgentConservatively(t *testing.T) {
	cfg := companion.Config{
		HomeRoot:      t.TempDir(),
		RuntimePath:   "/usr/bin/bun",
		ScriptPath:    "/srv/companion-server.js",
		BackendRoot:   "/srv/backend",
		ListenHost:    "127.0.0.1",
		BackendAPIURL: "http://localhost:8080",
	}

	cmd := cfg.BuildCommand("user_security_agent_defaults", 19003)

	if !envHas(cmd.Env, "WEB3_SECURITY_AGENT_CONFIG={\"review_mode\":\"always\",\"allow_review_clear\":false}") {
		t.Fatalf("command env missing conservative security agent defaults: %#v", cmd.Env)
	}
}

func TestPrepareUserHomeWritesClaudeSettings(t *testing.T) {
	cfg := companion.Config{
		HomeRoot: t.TempDir(),
	}
	home, err := cfg.PrepareUserHome("user_settings", companion.AuthHandoff{}, companion.LLMProviderConfig{
		Provider:  "openrouter",
		BaseURL:   "https://openrouter.ai/api",
		APIKey:    "settings-token",
		ModelName: "deepseek/deepseek-v4-pro",
	})
	if err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read claude settings: %v", err)
	}
	settings := string(data)
	for _, needle := range []string{
		`"model": "deepseek/deepseek-v4-pro"`,
		`"ANTHROPIC_BASE_URL": "https://openrouter.ai/api"`,
		`"ANTHROPIC_AUTH_TOKEN": "settings-token"`,
		`"CLAUDE_CODE_SUBAGENT_MODEL": "deepseek/deepseek-v4-pro"`,
		`"Bash(web3 *)"`,
	} {
		if !strings.Contains(settings, needle) {
			t.Fatalf("settings.json missing %q: %s", needle, settings)
		}
	}
	if strings.Contains(settings, "dist/web3") {
		t.Fatalf("settings.json should only allow web3 from PATH: %s", settings)
	}

	data, err = os.ReadFile(filepath.Join(home, ".claude", ".claude.json"))
	if err != nil {
		t.Fatalf("read claude home config: %v", err)
	}
	homeConfig := string(data)
	for _, needle := range []string{
		`"hasCompletedOnboarding": true`,
		filepath.Join(home, "workdir"),
		`"Bash(web3 *)"`,
	} {
		if !strings.Contains(homeConfig, needle) {
			t.Fatalf(".claude.json missing %q: %s", needle, homeConfig)
		}
	}
	if strings.Contains(homeConfig, "dist/web3") {
		t.Fatalf(".claude.json should only allow web3 from PATH: %s", homeConfig)
	}
}

func TestPrepareUserHomePreservesExistingClaudeSettingsAuth(t *testing.T) {
	cfg := companion.Config{
		HomeRoot: t.TempDir(),
	}
	home := cfg.UserHome("user_existing_settings")
	writeFileForTest(t, filepath.Join(home, ".claude", "settings.json"), `{"env":{"ANTHROPIC_AUTH_TOKEN":"kept-token"}}`+"\n", 0o600)

	if _, err := cfg.PrepareUserHome("user_existing_settings", companion.AuthHandoff{}, companion.LLMProviderConfig{
		Provider:  "openrouter",
		BaseURL:   "https://openrouter.ai/api",
		ModelName: "deepseek/deepseek-v4-pro",
	}); err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	settings := string(data)
	for _, needle := range []string{
		`"ANTHROPIC_AUTH_TOKEN": "kept-token"`,
		`"ANTHROPIC_BASE_URL": "https://openrouter.ai/api"`,
	} {
		if !strings.Contains(settings, needle) {
			t.Fatalf("settings.json missing %q: %s", needle, settings)
		}
	}
}

func TestPrepareUserHomeBackfillsAuthWhenExistingClaudeSettingsHaveProviderOnly(t *testing.T) {
	cfg := companion.Config{
		HomeRoot: t.TempDir(),
	}
	home := cfg.UserHome("user_provider_without_auth")
	writeFileForTest(t, filepath.Join(home, ".claude", "settings.json"), `{
  "model": "deepseek-v4-pro",
  "env": {
    "ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic",
    "ANTHROPIC_MODEL": "deepseek-v4-pro"
  }
}`+"\n", 0o600)

	if _, err := cfg.PrepareUserHome("user_provider_without_auth", companion.AuthHandoff{}, companion.LLMProviderConfig{
		Provider:  "openrouter",
		BaseURL:   "https://openrouter.ai/api",
		APIKey:    "backfilled-token",
		ModelName: "deepseek/deepseek-v4-pro",
	}); err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}
	settings, err := cfg.ReadUserClaudeSettings("user_provider_without_auth")
	if err != nil {
		t.Fatalf("ReadUserClaudeSettings: %v", err)
	}
	if settings.Env["ANTHROPIC_AUTH_TOKEN"] != "backfilled-token" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want backfilled token; env=%#v", settings.Env["ANTHROPIC_AUTH_TOKEN"], settings.Env)
	}
}

func TestPrepareUserHomeOverwritesExistingProviderSettingsWithResolvedRuntimeProvider(t *testing.T) {
	cfg := companion.Config{
		HomeRoot: t.TempDir(),
	}
	if err := cfg.WriteUserClaudeSettings("user_existing_provider", companion.LLMProviderConfig{
		Provider: "openrouter",
		BaseURL:  "https://openrouter.ai/api",
		APIKey:   "openrouter-token",
	}); err != nil {
		t.Fatalf("WriteUserClaudeSettings: %v", err)
	}

	home, err := cfg.PrepareUserHome("user_existing_provider", companion.AuthHandoff{}, companion.LLMProviderConfig{
		Provider:  "deepseek",
		APIKey:    "deepseek-token",
		ModelName: "deepseek-v4-pro",
	})
	if err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	settings := string(data)
	for _, needle := range []string{
		`"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic"`,
		`"ANTHROPIC_AUTH_TOKEN": "deepseek-token"`,
		`"ANTHROPIC_MODEL": "deepseek-v4-pro"`,
	} {
		if !strings.Contains(settings, needle) {
			t.Fatalf("settings.json missing %q: %s", needle, settings)
		}
	}
	if strings.Contains(settings, "openrouter-token") {
		t.Fatalf("PrepareUserHome kept stale provider auth: %s", settings)
	}
}

func TestPrepareUserHomeDoesNotOverwriteClaudeHomeConfig(t *testing.T) {
	cfg := companion.Config{
		HomeRoot: t.TempDir(),
	}
	home := cfg.UserHome("user_existing_claude")
	custom := `{"projects":{},"hasCompletedOnboarding":false}` + "\n"
	writeFileForTest(t, filepath.Join(home, ".claude", ".claude.json"), custom, 0o600)

	if _, err := cfg.PrepareUserHome("user_existing_claude", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", ".claude.json"))
	if err != nil {
		t.Fatalf("read claude home config: %v", err)
	}
	if string(data) != custom {
		t.Fatalf("PrepareUserHome overwrote existing .claude.json: %s", data)
	}
}

func TestBuildCommandUsesDirectContainerHomeAndWorkdir(t *testing.T) {
	homeRoot := t.TempDir()
	stageRoot := t.TempDir()
	cfg := companion.Config{
		HomeRoot:             homeRoot,
		RuntimePath:          filepath.Join(stageRoot, "bin", "bun"),
		ScriptPath:           filepath.Join(stageRoot, "bin", "companion-server.js"),
		ClaudeExecutablePath: filepath.Join(stageRoot, "bin", "claude"),
		BackendRoot:          filepath.Join(stageRoot, "backend"),
		ListenHost:           "127.0.0.1",
		BackendAPIURL:        "http://localhost:8080",
	}
	if err := os.MkdirAll(filepath.Join(stageRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.BackendRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PrepareUserHome("user_2", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("PrepareUserHome: %v", err)
	}

	cmd := cfg.BuildCommand("user_2", 19002)
	if cmd.Name != cfg.RuntimePath {
		t.Fatalf("command name = %q, want direct runtime", cmd.Name)
	}
	if len(cmd.Args) != 1 || cmd.Args[0] != cfg.ScriptPath {
		t.Fatalf("command args = %#v, want direct companion script", cmd.Args)
	}
	if cmd.Dir != filepath.Join(cfg.UserHome("user_2"), "workdir") {
		t.Fatalf("command dir = %q, want per-user workdir", cmd.Dir)
	}
	for _, want := range []string{
		"HOME=" + cfg.UserHome("user_2"),
		"TMPDIR=" + filepath.Join(cfg.UserHome("user_2"), "tmp"),
		"COMPANION_SESSION_STORE=sqlite:file=" + cfg.SQLiteDBPath("user_2"),
		"WEB3_BACKEND_ROOT=" + cfg.BackendRoot,
		"WEB3_REPO_SKILLS_DIR=" + filepath.Join(cfg.BackendRoot, "skills"),
		"WEB3_AGENT_WORKDIR=" + filepath.Join(cfg.UserHome("user_2"), "workdir"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(cfg.UserHome("user_2"), ".claude"),
		"CLAUDE_CODE_EXECUTABLE=" + cfg.ClaudeExecutablePath,
	} {
		if !envHas(cmd.Env, want) {
			t.Fatalf("command env missing %q: %#v", want, cmd.Env)
		}
	}
	if envContainsAny(cmd.Env, "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "WEB3_SSM_KEY", "AWS_SECRET_ACCESS_KEY", "WEB3_JWT_SECRET", "WEB3_RPC_ETHEREUM_URL") {
		t.Fatalf("command env leaks forbidden secrets: %#v", cmd.Env)
	}
}

func TestEnsureEmbeddedScriptWritesCompanionServerBundle(t *testing.T) {
	workdir := t.TempDir()
	path, err := companion.EnsureEmbeddedScript(workdir)
	if err != nil {
		t.Fatalf("EnsureEmbeddedScript: %v", err)
	}
	if path != filepath.Join(workdir, "companion", "server", "companion-server.js") {
		t.Fatalf("embedded script path = %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat embedded script: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("embedded script should not be empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read embedded script: %v", err)
	}
	if !strings.Contains(string(data), "Web3 Companion server listening") {
		t.Fatalf("embedded script does not look like companion bundle")
	}
}

func TestDirectStartUsesPersistentHomeForSessionStore(t *testing.T) {
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    writeRuntimeShim(t, "term-aware", nil),
		ScriptPath:     writePlaceholderScript(t),
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	persistentHome := cfg.UserHome("user_sessions")
	writeFileForTest(t, filepath.Join(persistentHome, "sessions.sqlite"), "history\n", 0o600)

	manager := companion.NewManager(cfg)
	t.Cleanup(func() { shutdownManager(t, manager) })

	if _, err := manager.Ensure(t.Context(), "user_sessions", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	logPath := filepath.Join(persistentHome, "companion.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read companion log: %v", err)
	}
	if !strings.Contains(string(data), "helper session store sqlite:file="+filepath.Join(persistentHome, "sessions.sqlite")) {
		t.Fatalf("companion did not receive persistent session store; log=%s", data)
	}
}

func TestCompanionPreflightReportsMissingScript(t *testing.T) {
	runtimePath := filepath.Join(t.TempDir(), "bun")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := companion.Config{
		HomeRoot:    t.TempDir(),
		RuntimePath: runtimePath,
		ScriptPath:  filepath.Join(t.TempDir(), "missing-companion-server.js"),
		BackendRoot: t.TempDir(),
	}
	manager := companion.NewManager(cfg)

	_, err := manager.Ensure(t.Context(), "user_3", companion.AuthHandoff{}, testRuntimeProvider())
	if err == nil {
		t.Fatal("Ensure returned nil error")
	}
	if !strings.Contains(err.Error(), "companion script unavailable") {
		t.Fatalf("error = %q, want missing script detail", err)
	}
}

func TestEnsureWaitsForReadyAndCapturesChildOutput(t *testing.T) {
	runtimePath := writeRuntimeShim(t, "delayed-ready", map[string]string{
		"COMPANION_TEST_READY_DELAY": "250ms",
	})
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { shutdownManager(t, manager) })

	startedAt := time.Now()
	proc, err := manager.Ensure(t.Context(), "user_ready", companion.AuthHandoff{}, testRuntimeProvider())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed < 200*time.Millisecond {
		t.Fatalf("Ensure returned before companion was ready: elapsed=%s", elapsed)
	}
	conn, err := net.DialTimeout("tcp", proc.BaseURL.Host, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("companion was not reachable after Ensure returned: %v", err)
	}
	_ = conn.Close()

	logPath := filepath.Join(cfg.UserHome("user_ready"), "companion.log")
	if !waitForFileContains(t, logPath, "helper listening") {
		t.Fatalf("companion log %s did not contain helper output", logPath)
	}
}

func TestEnsureRejectsMockHealthRuntime(t *testing.T) {
	runtimePath := writeRuntimeShim(t, "mock-health", nil)
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 500 * time.Millisecond,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	_, err := manager.Ensure(t.Context(), "user_mock_health", companion.AuthHandoff{}, testRuntimeProvider())
	if err == nil {
		t.Fatal("Ensure accepted mock health runtime")
	}
	if !strings.Contains(err.Error(), "companion readiness") && !strings.Contains(err.Error(), "sdk") {
		t.Fatalf("Ensure error = %q, want readiness/sdk detail", err)
	}
}

func TestStartProcessUsesPortZeroAndReadyFileNonce(t *testing.T) {
	runtimePath := writeRuntimeShim(t, "term-aware", nil)
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { shutdownManager(t, manager) })

	proc, err := manager.Ensure(t.Context(), "user_port_zero", companion.AuthHandoff{}, testRuntimeProvider())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if proc.Port == 0 {
		t.Fatal("process should expose the actual bound port after ready file is read")
	}
	logPath := filepath.Join(cfg.UserHome("user_port_zero"), "companion.log")
	if !waitForFileContains(t, logPath, "helper requested port 0") {
		t.Fatalf("companion log %s did not show child received COMPANION_PORT=0", logPath)
	}
	if !waitForFileContains(t, logPath, "helper ready nonce") {
		t.Fatalf("companion log %s did not show child received startup nonce", logPath)
	}
}

func TestEnsureWritesChildOutputOnlyToCompanionLog(t *testing.T) {
	runtimePath := writeRuntimeShim(t, "delayed-ready", map[string]string{
		"COMPANION_TEST_READY_DELAY": "250ms",
	})
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { shutdownManager(t, manager) })

	stdout, stderr := captureProcessStreams(t)
	if _, err := manager.Ensure(t.Context(), "user_quiet_logs", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	shutdownManager(t, manager)

	logPath := filepath.Join(cfg.UserHome("user_quiet_logs"), "companion.log")
	if !waitForFileContains(t, logPath, "helper listening") {
		t.Fatalf("companion log %s did not contain helper output", logPath)
	}
	output := stdout() + stderr()
	if strings.Contains(output, "helper starting") || strings.Contains(output, "helper listening") {
		t.Fatalf("server streams should not include companion helper output, got %q", output)
	}
}

func TestEnsureRefreshesContainerCredentialWhenReusingProcess(t *testing.T) {
	runtimePath := writeRuntimeShim(t, "term-aware", nil)
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { shutdownManager(t, manager) })

	first, err := manager.Ensure(t.Context(), "user_refresh", companion.AuthHandoff{
		ContainerCredential: companion.ContainerCredential{
			Version:      1,
			Client:       "container",
			CredentialID: "cc_old",
			Secret:       "old-secret",
			APIURL:       "http://localhost:8080",
		},
	}, testRuntimeProvider())
	if err != nil {
		t.Fatalf("initial Ensure: %v", err)
	}
	second, err := manager.Ensure(t.Context(), "user_refresh", companion.AuthHandoff{
		ContainerCredential: companion.ContainerCredential{
			Version:      1,
			Client:       "container",
			CredentialID: "cc_new",
			Secret:       "new-secret",
			APIURL:       "http://localhost:8080",
		},
	}, companion.LLMProviderConfig{Provider: "openrouter", APIKey: "runtime-token-new", ModelName: "deepseek/deepseek-v4-pro"})
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if second != first {
		t.Fatalf("Ensure should reuse the running process, got %p then %p", first, second)
	}

	credentialPath := filepath.Join(cfg.UserHome("user_refresh"), ".local", "web3", "container-credential.json")
	data, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatalf("read refreshed container credential: %v", err)
	}
	credential := string(data)
	for _, needle := range []string{
		`"credential_id":"cc_new"`,
		`"secret":"new-secret"`,
	} {
		if !strings.Contains(credential, needle) {
			t.Fatalf("container credential was not refreshed with %q: %s", needle, credential)
		}
	}
	if strings.Contains(credential, "old-secret") {
		t.Fatalf("container credential still contains old secret: %s", credential)
	}
	settings, err := cfg.ReadUserClaudeSettings("user_refresh")
	if err != nil {
		t.Fatalf("read refreshed provider settings: %v", err)
	}
	if settings.Env["ANTHROPIC_AUTH_TOKEN"] != "runtime-token-new" {
		t.Fatalf("reused process did not refresh runtime provider settings: %#v", settings.Env)
	}
}

func TestStopUserTerminatesCompanionProcess(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "stop.marker")
	runtimePath := writeRuntimeShim(t, "term-aware", map[string]string{
		"COMPANION_TEST_TERM_MARKER": markerPath,
	})
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     writePlaceholderScript(t),
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	if _, err := manager.Ensure(t.Context(), "user_stop", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := manager.StopUser(t.Context(), "user_stop"); err != nil {
		t.Fatalf("StopUser: %v", err)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read stop marker: %v", err)
	}
	if string(data) != "terminated\n" {
		t.Fatalf("stop marker = %q, want terminated", data)
	}
}

func TestMaxProcessesEvictsLeastRecentlyUsedIdleProcess(t *testing.T) {
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    writeRuntimeShim(t, "term-aware", nil),
		ScriptPath:     writePlaceholderScript(t),
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
		MaxProcesses:   1,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	first, err := manager.Ensure(t.Context(), "user_lru_old", companion.AuthHandoff{}, testRuntimeProvider())
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	second, err := manager.Ensure(t.Context(), "user_lru_new", companion.AuthHandoff{}, testRuntimeProvider())
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if first == second {
		t.Fatal("expected a distinct process for the second user")
	}
	select {
	case <-first.DoneForTest():
	case <-time.After(2 * time.Second):
		t.Fatal("least recently used process was not evicted")
	}
	if second.IsDoneForTest() {
		t.Fatal("new process was unexpectedly terminated")
	}
}

func TestConcurrentEnsureWaitsForSameReadyProcess(t *testing.T) {
	runtimePath := writeRuntimeShim(t, "delayed-ready", map[string]string{
		"COMPANION_TEST_READY_DELAY": "250ms",
	})
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	t.Cleanup(func() { shutdownManager(t, manager) })

	type result struct {
		proc    *companion.Process
		err     error
		elapsed time.Duration
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			startedAt := time.Now()
			proc, err := manager.Ensure(t.Context(), "user_concurrent", companion.AuthHandoff{}, testRuntimeProvider())
			results <- result{proc: proc, err: err, elapsed: time.Since(startedAt)}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var got []result
	for item := range results {
		got = append(got, item)
	}
	if len(got) != 2 {
		t.Fatalf("got %d Ensure results, want 2", len(got))
	}
	for _, item := range got {
		if item.err != nil {
			t.Fatalf("Ensure: %v", item.err)
		}
		if item.elapsed < 200*time.Millisecond {
			t.Fatalf("concurrent Ensure returned before readiness: elapsed=%s", item.elapsed)
		}
	}
	if got[0].proc != got[1].proc {
		t.Fatalf("concurrent Ensure should reuse one process, got %p and %p", got[0].proc, got[1].proc)
	}

	logPath := filepath.Join(cfg.UserHome("user_concurrent"), "companion.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read companion log: %v", err)
	}
	if count := strings.Count(string(data), "helper listening"); count != 1 {
		t.Fatalf("started %d helper processes, want 1; log=%q", count, string(data))
	}
}

func TestShutdownTerminatesCompanionProcessesGracefully(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "term.marker")
	runtimePath := writeRuntimeShim(t, "term-aware", map[string]string{
		"COMPANION_TEST_TERM_MARKER": markerPath,
	})
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 2 * time.Second,
	}
	manager := companion.NewManager(cfg)
	if _, err := manager.Ensure(t.Context(), "user_term", companion.AuthHandoff{}, testRuntimeProvider()); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read SIGTERM marker: %v", err)
	}
	if string(data) != "terminated\n" {
		t.Fatalf("SIGTERM marker = %q, want terminated", string(data))
	}
}

func TestShutdownPreventsNewCompanionStarts(t *testing.T) {
	cfg := companion.Config{
		HomeRoot:    t.TempDir(),
		RuntimePath: writeRuntimeShim(t, "term-aware", nil),
		ScriptPath:  writePlaceholderScript(t),
		BackendRoot: t.TempDir(),
		ListenHost:  "127.0.0.1",
	}
	manager := companion.NewManager(cfg)
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	_, err := manager.Ensure(t.Context(), "user_after_shutdown", companion.AuthHandoff{}, testRuntimeProvider())
	if err == nil {
		t.Fatal("Ensure after Shutdown returned nil error")
	}
	if !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("Ensure error = %q, want shutting down", err)
	}
}

func TestShutdownStopsProcessThatIsStillStarting(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "starting-term.marker")
	runtimePath := writeRuntimeShim(t, "delayed-ready", map[string]string{
		"COMPANION_TEST_READY_DELAY": "5s",
		"COMPANION_TEST_TERM_MARKER": markerPath,
	})
	scriptPath := writePlaceholderScript(t)
	cfg := companion.Config{
		HomeRoot:       t.TempDir(),
		RuntimePath:    runtimePath,
		ScriptPath:     scriptPath,
		BackendRoot:    t.TempDir(),
		ListenHost:     "127.0.0.1",
		StartupTimeout: 10 * time.Second,
	}
	manager := companion.NewManager(cfg)

	errCh := make(chan error, 1)
	go func() {
		_, err := manager.Ensure(context.Background(), "user_starting_shutdown", companion.AuthHandoff{}, testRuntimeProvider())
		errCh <- err
	}()
	logPath := filepath.Join(cfg.UserHome("user_starting_shutdown"), "companion.log")
	if !waitForFileContains(t, logPath, "helper starting") {
		t.Fatalf("companion log %s did not show helper startup", logPath)
	}

	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("Shutdown waited for startup timeout instead of terminating starting process: elapsed=%s", elapsed)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read SIGTERM marker: %v", err)
	}
	if string(data) != "terminated\n" {
		t.Fatalf("SIGTERM marker = %q, want terminated", string(data))
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Ensure returned nil error after Shutdown stopped starting process")
		}
	case <-time.After(time.Second):
		t.Fatal("Ensure did not return after Shutdown stopped starting process")
	}
}

func containsSequence(items []string, sequence []string) bool {
	return indexSequence(items, sequence) >= 0
}

func envContainsAny(env []string, needles ...string) bool {
	for _, item := range env {
		for _, needle := range needles {
			if needle != "" && strings.Contains(item, needle) {
				return true
			}
		}
	}
	return false
}

func envHas(env []string, value string) bool {
	for _, item := range env {
		if item == value {
			return true
		}
	}
	return false
}

func indexSequence(items []string, sequence []string) int {
	for i := 0; i+len(sequence) <= len(items); i++ {
		ok := true
		for j := range sequence {
			if items[i+j] != sequence[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func testRuntimeProvider() companion.LLMProviderConfig {
	return companion.LLMProviderConfig{
		Provider:  "openrouter",
		APIKey:    "runtime-token",
		ModelName: "deepseek/deepseek-v4-pro",
	}
}

func writeRuntimeShim(t *testing.T, helperMode string, env map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-shim")
	var builder strings.Builder
	builder.WriteString("#!/bin/sh\n")
	builder.WriteString("export COMPANION_TEST_HELPER=" + shellQuote(helperMode) + "\n")
	for key, value := range env {
		builder.WriteString("export " + key + "=" + shellQuote(value) + "\n")
	}
	builder.WriteString("exec " + shellQuote(os.Args[0]) + " \"$@\"\n")
	if err := os.WriteFile(path, []byte(builder.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writePlaceholderScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "companion-server.js")
	writeFileForTest(t, path, "// test companion script\n", 0o644)
	return path
}

func writeFileForTest(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shutdownManager(t *testing.T, manager *companion.Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown cleanup: %v", err)
	}
}

func waitForFileContains(t *testing.T, path, needle string) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), needle) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func captureProcessStreams(t *testing.T) (func() string, func() string) {
	t.Helper()
	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	})

	readPipe := func(reader *os.File, writer *os.File) func() string {
		var once sync.Once
		var output string
		return func() string {
			once.Do(func() {
				_ = writer.Close()
				data, err := io.ReadAll(reader)
				if err != nil {
					t.Fatalf("read captured stream: %v", err)
				}
				output = string(data)
				_ = reader.Close()
			})
			return output
		}
	}

	return readPipe(stdoutReader, stdoutWriter), readPipe(stderrReader, stderrWriter)
}

func parseHelperDelay() time.Duration {
	delay, err := time.ParseDuration(os.Getenv("COMPANION_TEST_READY_DELAY"))
	if err != nil {
		return 0
	}
	return delay
}

func runCompanionHelper(delay time.Duration) {
	if delay > 0 {
		fmt.Println("helper starting")
		setupSignalMarker(nil)
		time.Sleep(delay)
	}
	host := os.Getenv("COMPANION_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, os.Getenv("COMPANION_PORT")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper listen failed: %v\n", err)
		os.Exit(2)
	}
	if os.Getenv("COMPANION_PORT") == "0" {
		fmt.Println("helper requested port 0")
	}
	if nonce := os.Getenv("COMPANION_STARTUP_NONCE"); nonce != "" {
		fmt.Printf("helper ready nonce %s\n", nonce)
	}
	if sessionStore := os.Getenv("COMPANION_SESSION_STORE"); sessionStore != "" {
		fmt.Printf("helper session store %s\n", sessionStore)
	}
	if agentWorkdir := os.Getenv("WEB3_AGENT_WORKDIR"); agentWorkdir != "" {
		fmt.Printf("helper agent workdir %s\n", agentWorkdir)
	}
	if readyFile := os.Getenv("COMPANION_READY_FILE"); readyFile != "" {
		port := ln.Addr().(*net.TCPAddr).Port
		_ = os.MkdirAll(filepath.Dir(readyFile), 0o700)
		_ = os.WriteFile(readyFile, []byte(fmt.Sprintf(`{"host":%q,"port":%d,"pid":%d,"nonce":%q,"sdk":"claude-agent-sdk"}`+"\n", host, port, os.Getpid(), os.Getenv("COMPANION_STARTUP_NONCE"))), 0o600)
	}
	fmt.Println("helper listening")

	setupSignalMarker(ln)
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if strings.Contains(string(buf[:n]), "GET /api/health ") {
			body := fmt.Sprintf(`{"ok":true,"sdk":"claude-agent-sdk","nonce":%q}`, os.Getenv("COMPANION_STARTUP_NONCE"))
			_, _ = conn.Write([]byte(fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)))
		} else {
			_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"))
		}
		_ = conn.Close()
	}
}

func runMockHealthHelper() {
	host := os.Getenv("COMPANION_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, os.Getenv("COMPANION_PORT")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "mock helper listen failed: %v\n", err)
		os.Exit(2)
	}
	if readyFile := os.Getenv("COMPANION_READY_FILE"); readyFile != "" {
		port := ln.Addr().(*net.TCPAddr).Port
		_ = os.MkdirAll(filepath.Dir(readyFile), 0o700)
		_ = os.WriteFile(readyFile, []byte(fmt.Sprintf(`{"host":%q,"port":%d,"pid":%d,"nonce":%q,"sdk":"mock"}`+"\n", host, port, os.Getpid(), os.Getenv("COMPANION_STARTUP_NONCE"))), 0o600)
	}
	setupSignalMarker(ln)
	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		if strings.Contains(string(buf[:n]), "GET /api/health ") {
			body := fmt.Sprintf(`{"ok":true,"sdk":"mock","nonce":%q}`, os.Getenv("COMPANION_STARTUP_NONCE"))
			_, _ = conn.Write([]byte(fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)))
		} else {
			_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"))
		}
		_ = conn.Close()
	}
}

func setupSignalMarker(listener net.Listener) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signals
		if marker := os.Getenv("COMPANION_TEST_TERM_MARKER"); marker != "" {
			_ = os.WriteFile(marker, []byte("terminated\n"), 0o600)
		}
		if listener != nil {
			_ = listener.Close()
		}
		os.Exit(0)
	}()
}
