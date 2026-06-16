// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion

import (
	"encoding/json"
	"fmt"

	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/config"
)

type Config struct {
	HomeRoot             string
	RuntimePath          string
	ScriptPath           string
	ClaudeExecutablePath string
	BackendRoot          string
	ListenHost           string
	BackendAPIURL        string
	SecurityAgent        SecurityAgentConfig
	StartupTimeout       time.Duration
	ShutdownTimeout      time.Duration
	IdleTTL              time.Duration
	IdleScanInterval     time.Duration
	MaxProcesses         int
	MaxMessageBytes      int
	MaxPromptChars       int
}

type SecurityAgentConfig struct {
	ReviewMode       string `json:"review_mode"`
	AllowReviewClear bool   `json:"allow_review_clear"`
}

const (
	defaultRuntimePath          = "/usr/local/bin/bun"
	defaultClaudeExecutablePath = ""
	defaultListenHost           = "127.0.0.1"
	defaultStartupTimeout       = 5 * time.Second
	defaultShutdownTimeout      = 5 * time.Second
	defaultIdleTTL              = 30 * time.Minute
	defaultIdleScanInterval     = time.Minute
	defaultMaxProcesses         = 16
	defaultMaxMessageBytes      = 64 * 1024
	defaultMaxPromptChars       = 32 * 1024
)

type LLMProviderConfig struct {
	Provider  string
	BaseURL   string
	APIKey    string
	ModelName string
}

type Command struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

type AuthHandoff struct {
	Version             int                 `json:"version"`
	Client              string              `json:"client"`
	APIURL              string              `json:"api_url,omitempty"`
	Email               string              `json:"email,omitempty"`
	ContainerCredential ContainerCredential `json:"container_credential,omitempty"`
}

type ContainerCredential struct {
	Version      int    `json:"version"`
	Client       string `json:"client"`
	CredentialID string `json:"credential_id"`
	Secret       string `json:"secret"`
	Scope        string `json:"scope,omitempty"`
	APIURL       string `json:"api_url,omitempty"`
	Email        string `json:"email,omitempty"`
}

type commandPaths struct {
	RuntimePath          string
	ScriptPath           string
	ClaudeExecutablePath string
	Web3Path             string
	BackendRoot          string
}

var safeUserIDPattern = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

func (c Config) withDefaults() Config {
	if c.HomeRoot == "" {
		c.HomeRoot = filepath.Join(defaultWorkdir(), "companion", "users")
	}
	if c.ListenHost == "" {
		c.ListenHost = defaultListenHost
	}
	if c.BackendAPIURL == "" {
		c.BackendAPIURL = "http://localhost:8080"
	}
	if c.StartupTimeout <= 0 {
		c.StartupTimeout = defaultStartupTimeout
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	if c.IdleTTL == 0 {
		c.IdleTTL = defaultIdleTTL
	}
	if c.IdleScanInterval == 0 {
		c.IdleScanInterval = defaultIdleScanInterval
	}
	if c.MaxProcesses == 0 {
		c.MaxProcesses = defaultMaxProcesses
	}
	if c.MaxMessageBytes == 0 {
		c.MaxMessageBytes = defaultMaxMessageBytes
	}
	if c.MaxPromptChars == 0 {
		c.MaxPromptChars = defaultMaxPromptChars
	}
	if strings.TrimSpace(c.SecurityAgent.ReviewMode) == "" {
		c.SecurityAgent.ReviewMode = "always"
		c.SecurityAgent.AllowReviewClear = false
	}
	return c
}

func defaultWorkdir() string {
	cfg := config.LoadFromEnv()
	if strings.TrimSpace(cfg.Workdir.Path) != "" {
		return strings.TrimSpace(cfg.Workdir.Path)
	}
	return filepath.Join(".", "dist")
}

func (c Config) UserHome(userID string) string {
	safe := safeUserIDPattern.ReplaceAllString(strings.TrimSpace(userID), "_")
	safe = strings.Trim(safe, "._-")
	if safe == "" {
		safe = "user"
	}
	homeRoot := c.HomeRoot
	if homeRoot == "" {
		homeRoot = filepath.Join(defaultWorkdir(), "companion", "users")
	}
	return filepath.Join(absPath(homeRoot), safe)
}

func (c Config) SQLiteDBPath(userID string) string {
	return filepath.Join(c.UserHome(userID), "sessions.sqlite")
}

func (c Config) PrepareUserHome(userID string, handoff AuthHandoff, provider LLMProviderConfig) (string, error) {
	home := c.UserHome(userID)
	for _, dir := range []string{
		home,
		filepath.Join(home, ".local", "web3"),
		filepath.Join(home, ".claude"),
		filepath.Join(home, "tmp"),
		filepath.Join(home, "workdir"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if credential := normalizeContainerCredential(handoff.ContainerCredential, c.BackendAPIURL, handoff.Email); credential.CredentialID != "" || credential.Secret != "" {
		data, err := json.Marshal(credential)
		if err != nil {
			return "", fmt.Errorf("marshal container credential: %w", err)
		}
		path := filepath.Join(home, ".local", "web3", "container-credential.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return "", fmt.Errorf("write container credential: %w", err)
		}
	}
	if err := c.WriteUserClaudeSettings(userID, provider); err != nil {
		return "", err
	}
	if err := ensureClaudeHomeConfig(filepath.Join(home, ".claude", ".claude.json")); err != nil {
		return "", err
	}
	return home, nil
}

func (c Config) ReadContainerCredential(userID string) (ContainerCredential, error) {
	path := filepath.Join(c.UserHome(userID), ".local", "web3", "container-credential.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ContainerCredential{}, err
	}
	var credential ContainerCredential
	if err := json.Unmarshal(data, &credential); err != nil {
		return ContainerCredential{}, fmt.Errorf("parse container credential: %w", err)
	}
	credential = normalizeContainerCredential(credential, c.BackendAPIURL, credential.Email)
	if credential.CredentialID == "" || credential.Secret == "" {
		return ContainerCredential{}, fmt.Errorf("container credential missing credential_id or secret")
	}
	return credential, nil
}

func (c Config) RemoveContainerCredential(userID string) error {
	path := filepath.Join(c.UserHome(userID), ".local", "web3", "container-credential.json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func normalizeContainerCredential(credential ContainerCredential, apiURL, email string) ContainerCredential {
	credential.CredentialID = strings.TrimSpace(credential.CredentialID)
	credential.Secret = strings.TrimSpace(credential.Secret)
	if credential.CredentialID == "" && credential.Secret == "" {
		return ContainerCredential{}
	}
	if credential.Version == 0 {
		credential.Version = 1
	}
	if strings.TrimSpace(credential.Client) == "" {
		credential.Client = "container"
	}
	if strings.TrimSpace(credential.APIURL) == "" {
		credential.APIURL = strings.TrimSpace(apiURL)
	}
	if strings.TrimSpace(credential.Email) == "" {
		credential.Email = strings.TrimSpace(email)
	}
	return credential
}

func (c Config) BuildCommand(userID string, port int) Command {
	return c.buildCommand(userID, port, nil)
}

func (c Config) BuildCommandWithStartup(userID string, port int, readyFile, nonce string) Command {
	extraEnv := []string{}
	if strings.TrimSpace(readyFile) != "" {
		extraEnv = append(extraEnv, "COMPANION_READY_FILE="+readyFile)
	}
	if strings.TrimSpace(nonce) != "" {
		extraEnv = append(extraEnv, "COMPANION_STARTUP_NONCE="+nonce)
	}
	if c.MaxMessageBytes > 0 {
		extraEnv = append(extraEnv, "COMPANION_MAX_MESSAGE_BYTES="+fmt.Sprintf("%d", c.MaxMessageBytes))
	}
	if c.MaxPromptChars > 0 {
		extraEnv = append(extraEnv, "COMPANION_MAX_PROMPT_CHARS="+fmt.Sprintf("%d", c.MaxPromptChars))
	}
	return c.buildCommand(userID, port, extraEnv)
}

func (c Config) buildCommand(userID string, port int, extraEnv []string) Command {
	c = c.withDefaults()
	home := c.UserHome(userID)
	paths := c.commandPaths()

	runtimeDir := filepath.Dir(paths.RuntimePath)
	if runtimeDir == "." {
		runtimeDir = "/usr/local/bin"
	}
	pathValue := joinPathEntries(runtimeDir, filepath.Dir(paths.Web3Path), "/usr/local/bin", "/usr/bin", "/bin")
	claudeExecutablePath := paths.ClaudeExecutablePath
	env := c.baseEnv(envPaths{
		home:         home,
		tmp:          filepath.Join(home, "tmp"),
		path:         pathValue,
		sessionStore: "sqlite:file=" + c.SQLiteDBPath(userID),
		backendRoot:  paths.BackendRoot,
		claudeDir:    filepath.Join(home, ".claude"),
		agentWorkdir: filepath.Join(home, "workdir"),
	}, port)
	if data, err := json.Marshal(c.SecurityAgent); err == nil {
		env = append(env, "WEB3_SECURITY_AGENT_CONFIG="+string(data))
	}
	env = append(env, extraEnv...)
	if claudeExecutablePath != "" {
		env = append(env, "CLAUDE_CODE_EXECUTABLE="+claudeExecutablePath)
	}
	if web3Path := containerWeb3Path(paths.Web3Path); web3Path != "" {
		env = append(env, "WEB3_CLI_PATH="+web3Path)
	}
	return Command{Name: paths.RuntimePath, Args: []string{paths.ScriptPath}, Env: env, Dir: filepath.Join(home, "workdir")}
}

func containerWeb3Path(hostPath string) string {
	if _, err := os.Stat("/usr/local/bin/web3"); err == nil {
		return "/usr/local/bin/web3"
	}
	if strings.TrimSpace(hostPath) != "" {
		return hostPath
	}
	return ""
}

func (c Config) commandPaths() commandPaths {
	runtimePath := c.hostRuntimePath()
	scriptPath := c.hostScriptPath()
	claudeExecutablePath := c.hostClaudeExecutablePath()
	web3Path := c.hostWeb3Path()
	backendRoot := c.hostBackendRoot()
	return commandPaths{
		RuntimePath:          runtimePath,
		ScriptPath:           scriptPath,
		ClaudeExecutablePath: claudeExecutablePath,
		Web3Path:             web3Path,
		BackendRoot:          backendRoot,
	}
}

type envPaths struct {
	home         string
	tmp          string
	path         string
	sessionStore string
	backendRoot  string
	claudeDir    string
	agentWorkdir string
}

func (c Config) baseEnv(paths envPaths, port int) []string {
	env := []string{
		"HOME=" + paths.home,
		"USER=agent",
		"LOGNAME=agent",
		"TMPDIR=" + paths.tmp,
		"PATH=" + paths.path,
		"COMPANION_HOST=" + c.ListenHost,
		"COMPANION_PORT=" + fmt.Sprintf("%d", port),
		"COMPANION_SESSION_STORE=" + paths.sessionStore,
		"WEB3_BACKEND_ROOT=" + paths.backendRoot,
		"WEB3_REPO_SKILLS_DIR=" + filepath.Join(paths.backendRoot, "skills"),
		"WEB3_API_URL=" + c.BackendAPIURL,
		"WEB3_AGENT_WORKDIR=" + paths.agentWorkdir,
		"CLAUDE_CONFIG_DIR=" + paths.claudeDir,
	}
	return env
}

func (c Config) hostRuntimePath() string {
	if c.RuntimePath == "" {
		return defaultRuntimePath
	}
	return absPath(c.RuntimePath)
}

func (c Config) hostScriptPath() string {
	if c.ScriptPath == "" {
		return absPath(filepath.Join("companion", "dist", "companion-server.js"))
	}
	return absPath(c.ScriptPath)
}

func (c Config) hostClaudeExecutablePath() string {
	if c.ClaudeExecutablePath != "" {
		return absPath(c.ClaudeExecutablePath)
	}
	if envPath := os.Getenv("CLAUDE_CODE_EXECUTABLE"); envPath != "" {
		return absPath(envPath)
	}
	return defaultClaudeExecutablePath
}

func (c Config) hostWeb3Path() string {
	if backendRoot := c.hostBackendRoot(); backendRoot != "" {
		return filepath.Join(backendRoot, "dist", "web3")
	}
	return ""
}

func (c Config) hostBackendRoot() string {
	if c.BackendRoot == "" {
		return ""
	}
	return absPath(c.BackendRoot)
}

func absPath(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			return abs
		}
	}
	return path
}

func joinPathEntries(entries ...string) string {
	seen := make(map[string]struct{}, len(entries))
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == "" || entry == "." {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}
	return strings.Join(out, string(filepath.ListSeparator))
}
