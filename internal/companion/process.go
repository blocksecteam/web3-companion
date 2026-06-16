// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Process struct {
	UserID           string
	Port             int
	BaseURL          *url.URL
	Cmd              *exec.Cmd
	LogFile          *os.File
	Handoff          AuthHandoff
	Provider         LLMProviderConfig
	Nonce            string
	done             chan struct{}
	exitMu           sync.Mutex
	exitErr          error
	stateMu          sync.Mutex
	lastAccessAt     time.Time
	activeHTTP       int
	activeWebSockets int
}

func (m *Manager) openLogFile(userID string) (*os.File, error) {
	return os.OpenFile(filepath.Join(m.cfg.UserHome(userID), "companion.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

type readyFilePayload struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	PID   int    `json:"pid"`
	Nonce string `json:"nonce"`
	SDK   string `json:"sdk"`
}

type healthPayload struct {
	OK    bool   `json:"ok"`
	SDK   string `json:"sdk"`
	Nonce string `json:"nonce"`
	Code  string `json:"code"`
}

func (m *Manager) waitReady(ctx context.Context, proc *Process, readyFile string) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, m.cfg.StartupTimeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		ready, readyErr := readReadyFile(readyFile)
		if readyErr == nil {
			if ready.Nonce != proc.Nonce {
				return fmt.Errorf("companion readiness nonce mismatch")
			}
			if ready.SDK != "claude-agent-sdk" {
				return fmt.Errorf("companion readiness rejected sdk %q", ready.SDK)
			}
			if ready.Port <= 0 {
				return fmt.Errorf("companion readiness missing bound port")
			}
			host := strings.TrimSpace(ready.Host)
			if host == "" {
				host = m.cfg.ListenHost
			}
			base, _ := url.Parse(fmt.Sprintf("http://%s:%d", host, ready.Port))
			proc.Port = ready.Port
			proc.BaseURL = base
		}
		if proc.Port > 0 {
			healthURL := proc.BaseURL.ResolveReference(&url.URL{Path: "/api/health"}).String()
			if err := companionHealthy(healthURL, proc.Nonce); err == nil {
				slog.Info("companion process ready", "user_id", proc.UserID, "pid", proc.Cmd.Process.Pid, "base_url", proc.BaseURL.String())
				return nil
			}
		}
		select {
		case <-proc.done:
			return fmt.Errorf("companion process exited before ready: %w", proc.err())
		case <-deadlineCtx.Done():
			if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("companion readiness timeout after %s", m.cfg.StartupTimeout)
			}
			return deadlineCtx.Err()
		case <-ticker.C:
		}
	}
}

func readReadyFile(path string) (readyFilePayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return readyFilePayload{}, err
	}
	var payload readyFilePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return readyFilePayload{}, err
	}
	return payload, nil
}

func companionHealthy(rawURL, nonce string) error {
	client := http.Client{Timeout: 250 * time.Millisecond}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("health status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return err
	}
	var payload healthPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if !payload.OK {
		return fmt.Errorf("health not ok: %s", payload.Code)
	}
	if payload.SDK != "claude-agent-sdk" {
		return fmt.Errorf("health rejected sdk %q", payload.SDK)
	}
	if strings.TrimSpace(nonce) != "" && payload.Nonce != nonce {
		return fmt.Errorf("health nonce mismatch")
	}
	return nil
}

func (m *Manager) terminateProcess(ctx context.Context, proc *Process) error {
	if proc == nil || proc.Cmd == nil || proc.Cmd.Process == nil {
		return nil
	}
	if proc.isDone() {
		return nil
	}
	slog.Info("terminating companion process", "user_id", proc.UserID, "pid", proc.Cmd.Process.Pid)
	if err := signalProcessGroup(proc.Cmd.Process.Pid, syscall.SIGTERM); err != nil && !isProcessAlreadyDone(err) {
		return err
	}

	shutdownCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		shutdownCtx, cancel = context.WithTimeout(ctx, m.cfg.ShutdownTimeout)
	}
	defer cancel()

	select {
	case <-proc.done:
		err := proc.err()
		if err != nil && !isTerminatedBySignal(err) {
			return err
		}
		return nil
	case <-shutdownCtx.Done():
		slog.Warn("companion graceful shutdown timed out; killing process", "user_id", proc.UserID, "pid", proc.Cmd.Process.Pid)
		if err := signalProcessGroup(proc.Cmd.Process.Pid, syscall.SIGKILL); err != nil && !isProcessAlreadyDone(err) {
			return err
		}
		select {
		case <-proc.done:
			return shutdownCtx.Err()
		case <-time.After(time.Second):
			return shutdownCtx.Err()
		}
	}
}

func (p *Process) setExitErr(err error) {
	p.exitMu.Lock()
	defer p.exitMu.Unlock()
	p.exitErr = err
}

func (p *Process) err() error {
	p.exitMu.Lock()
	defer p.exitMu.Unlock()
	return p.exitErr
}

func (p *Process) isDone() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Process) DoneForTest() <-chan struct{} {
	return p.done
}

func (p *Process) IsDoneForTest() bool {
	return p.isDone()
}

func (p *Process) touch() {
	p.stateMu.Lock()
	p.lastAccessAt = time.Now()
	p.stateMu.Unlock()
}

func (p *Process) isIdleFor(now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.activeHTTP == 0 && p.activeWebSockets == 0 && now.Sub(p.lastAccessAt) >= ttl
}

func (p *Process) isIdleNow() bool {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	return p.activeHTTP == 0 && p.activeWebSockets == 0
}

func (p *Process) lastAccessBefore(other *Process) bool {
	if other == nil {
		return true
	}
	p.stateMu.Lock()
	pLast := p.lastAccessAt
	p.stateMu.Unlock()
	other.stateMu.Lock()
	otherLast := other.lastAccessAt
	other.stateMu.Unlock()
	return pLast.Before(otherLast)
}

func signalProcessGroup(pid int, signal syscall.Signal) error {
	if err := syscall.Kill(-pid, signal); err == nil {
		return nil
	}
	return syscall.Kill(pid, signal)
}

func isTerminatedBySignal(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGTERM
}

func isProcessAlreadyDone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || strings.Contains(err.Error(), "process already finished") || strings.Contains(err.Error(), "no such process")
}

func (c Config) logPreflight(userID string, command Command) error {
	runtimePath := c.hostRuntimePath()
	if runtimePath == "" {
		runtimePath = command.Name
	}
	runtimeInfo, err := os.Stat(runtimePath)
	if err != nil {
		slog.Error("companion runtime preflight failed", "user_id", userID, "runtime", runtimePath, "error", err)
		return fmt.Errorf("companion runtime unavailable at %s: %w", runtimePath, err)
	}
	if runtimeInfo.Mode()&0o111 == 0 {
		slog.Error("companion runtime preflight failed", "user_id", userID, "runtime", runtimePath, "error", "not executable")
		return fmt.Errorf("companion runtime unavailable at %s: not executable", runtimePath)
	}

	scriptPath := c.hostScriptPath()
	if _, err := os.Stat(scriptPath); err != nil {
		slog.Error("companion script preflight failed", "user_id", userID, "script", scriptPath, "error", err)
		return fmt.Errorf("companion script unavailable at %s: %w", scriptPath, err)
	}

	if backendRoot := c.hostBackendRoot(); backendRoot != "" {
		if _, err := os.Stat(backendRoot); err != nil {
			slog.Error("companion backend root preflight failed", "user_id", userID, "backend_root", backendRoot, "error", err)
			return fmt.Errorf("companion backend root unavailable at %s: %w", backendRoot, err)
		}
	}
	return nil
}

func tailCompanionLog(home string, maxBytes int64) string {
	if maxBytes <= 0 {
		return ""
	}
	path := filepath.Join(home, "companion.log")
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - maxBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return ""
	}
	return string(data)
}

func displayPath(path string) string {
	if path == "" {
		return ""
	}
	return path
}

func sanitizeArgs(args []string) []string {
	safe := make([]string, len(args))
	copy(safe, args)
	for i := range safe {
		safe[i] = displayPath(safe[i])
		if strings.HasPrefix(safe[i], "ANTHROPIC_AUTH_TOKEN=") {
			safe[i] = "ANTHROPIC_AUTH_TOKEN=<redacted>"
		}
		if strings.HasPrefix(safe[i], "ANTHROPIC_API_KEY=") {
			safe[i] = "ANTHROPIC_API_KEY=<redacted>"
		}
	}
	return safe
}

func randomNonce() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
