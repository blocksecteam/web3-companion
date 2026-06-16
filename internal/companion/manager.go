// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion

import (
	"context"
	"errors"
	"fmt"

	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/blocksecteam/web3-companion/internal/settings"
)

type ApplyResult struct {
	Restarted bool
	Running   bool
}

type Manager struct {
	cfg        Config
	mu         sync.Mutex
	processes  map[string]*Process
	starting   map[string]*startState
	closing    bool
	reaperStop chan struct{}
	reaperOnce sync.Once
}

type startState struct {
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	proc   *Process
	err    error
}

var errManagerShuttingDown = errors.New("companion manager is shutting down")

func NewManager(cfg Config) *Manager {
	cfg = cfg.withDefaults()
	m := &Manager{cfg: cfg, processes: make(map[string]*Process), starting: make(map[string]*startState), reaperStop: make(chan struct{})}
	if cfg.IdleTTL > 0 && cfg.IdleScanInterval > 0 {
		go m.idleReaper()
	}
	return m
}

func (m *Manager) Ensure(ctx context.Context, userID string, handoff AuthHandoff, provider LLMProviderConfig) (*Process, error) {
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return nil, errManagerShuttingDown
	}
	if start := m.starting[userID]; start != nil {
		m.mu.Unlock()
		select {
		case <-start.done:
			return start.proc, start.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if proc := m.processes[userID]; proc != nil {
		if proc.isDone() {
			delete(m.processes, userID)
		} else {
			m.mu.Unlock()
			if _, err := m.cfg.PrepareUserHome(userID, handoff, provider); err != nil {
				return nil, err
			}
			proc.Handoff = handoff
			proc.Provider = provider
			proc.touch()
			slog.Debug("companion process reused", "user_id", userID, "port", proc.Port, "base_url", proc.BaseURL.String())
			return proc, nil
		}
	}
	if err := m.evictForCapacityLocked(ctx, userID); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	startCtx, cancel := context.WithCancel(context.Background())
	start := &startState{done: make(chan struct{}), ctx: startCtx, cancel: cancel}
	m.starting[userID] = start
	m.mu.Unlock()

	proc, err := m.startProcess(start.ctx, start, userID, handoff, provider)
	start.mu.Lock()
	start.proc = proc
	start.err = err
	start.mu.Unlock()
	m.mu.Lock()
	delete(m.starting, userID)
	close(start.done)
	m.mu.Unlock()
	return proc, err
}

func (m *Manager) startProcess(ctx context.Context, start *startState, userID string, handoff AuthHandoff, provider LLMProviderConfig) (*Process, error) {
	persistentHome, err := m.cfg.PrepareUserHome(userID, handoff, provider)
	if err != nil {
		slog.Error("companion home preparation failed", "user_id", userID, "error", err)
		return nil, err
	}
	home := persistentHome
	commandCfg := m.cfg
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	readyFile := filepath.Join(home, "tmp", "companion-ready-"+nonce+".json")
	port := 0
	command := commandCfg.BuildCommandWithStartup(userID, port, readyFile, nonce)
	if err := commandCfg.logPreflight(userID, command); err != nil {
		return nil, err
	}
	slog.Info("starting companion process",
		"user_id", userID,
		"port", port,
		"runtime", displayPath(m.cfg.RuntimePath),
		"script", displayPath(m.cfg.ScriptPath),
		"home", displayPath(home),
		"backend_root", displayPath(commandCfg.hostBackendRoot()),
		"command", command.Name,
		"args", sanitizeArgs(command.Args),
	)
	cmd := exec.Command(command.Name, command.Args...)
	cmd.Env = command.Env
	cmd.Dir = command.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logFile, err := m.openLogFile(userID)
	if err != nil {
		slog.Error("companion log open failed", "user_id", userID, "error", err)
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		slog.Error("companion process start failed",
			"user_id", userID,
			"command", command.Name,
			"args", sanitizeArgs(command.Args),
			"dir", command.Dir,
			"error", err,
		)
		return nil, err
	}
	base, _ := url.Parse(fmt.Sprintf("http://%s:%d", commandCfg.ListenHost, port))
	proc := &Process{UserID: userID, Port: port, BaseURL: base, Cmd: cmd, LogFile: logFile, Handoff: handoff, Provider: provider, Nonce: nonce, done: make(chan struct{}), lastAccessAt: time.Now()}
	if start != nil {
		start.mu.Lock()
		start.proc = proc
		start.mu.Unlock()
	}

	m.mu.Lock()
	m.processes[userID] = proc
	m.mu.Unlock()

	slog.Info("companion process started", "user_id", userID, "pid", cmd.Process.Pid, "base_url", base.String())

	go func() {
		err := cmd.Wait()
		proc.setExitErr(err)
		close(proc.done)
		_ = logFile.Close()
		if err != nil {
			slog.Error("companion process exited with error", "user_id", userID, "pid", cmd.Process.Pid, "error", err)
		} else {
			slog.Info("companion process exited", "user_id", userID, "pid", cmd.Process.Pid)
		}
		m.mu.Lock()
		if m.processes[userID] == proc {
			delete(m.processes, userID)
		}
		m.mu.Unlock()
	}()
	if err := m.waitReady(ctx, proc, readyFile); err != nil {
		m.mu.Lock()
		if m.processes[userID] == proc {
			delete(m.processes, userID)
		}
		m.mu.Unlock()
		slog.Error("companion readiness failed", "user_id", userID, "pid", cmd.Process.Pid, "log_tail", tailCompanionLog(persistentHome, 8192), "error", err)
		_ = m.terminateProcess(context.Background(), proc)
		return nil, err
	}
	return proc, nil
}

func (m *Manager) GatewayFor(ctx context.Context, userID string, handoff AuthHandoff) (*httputil.ReverseProxy, error) {
	return m.GatewayForProvider(ctx, userID, handoff, LLMProviderConfig{Provider: settings.DefaultProvider, BaseURL: settings.DefaultBaseURL, ModelName: settings.DefaultModel})
}

func (m *Manager) GatewayForProvider(ctx context.Context, userID string, handoff AuthHandoff, provider LLMProviderConfig) (*httputil.ReverseProxy, error) {
	proc, err := m.Ensure(ctx, userID, handoff, provider)
	if err != nil {
		slog.Error("companion gateway unavailable", "user_id", userID, "error", err)
		return nil, err
	}
	slog.Debug("companion gateway ready", "user_id", userID, "base_url", proc.BaseURL.String())
	return httputil.NewSingleHostReverseProxy(proc.BaseURL), nil
}

func (m *Manager) BeginRequest(userID string, websocket bool) {
	m.mu.Lock()
	proc := m.processes[userID]
	m.mu.Unlock()
	if proc == nil {
		return
	}
	proc.stateMu.Lock()
	proc.lastAccessAt = time.Now()
	if websocket {
		proc.activeWebSockets++
	} else {
		proc.activeHTTP++
	}
	proc.stateMu.Unlock()
}

func (m *Manager) EndRequest(userID string, websocket bool) {
	m.mu.Lock()
	proc := m.processes[userID]
	m.mu.Unlock()
	if proc == nil {
		return
	}
	proc.stateMu.Lock()
	proc.lastAccessAt = time.Now()
	if websocket {
		if proc.activeWebSockets > 0 {
			proc.activeWebSockets--
		}
	} else if proc.activeHTTP > 0 {
		proc.activeHTTP--
	}
	proc.stateMu.Unlock()
}

func (m *Manager) StopUser(ctx context.Context, userID string) error {
	m.mu.Lock()
	proc := m.processes[userID]
	if proc != nil {
		delete(m.processes, userID)
	}
	start := m.starting[userID]
	if start != nil {
		start.cancel()
	}
	m.mu.Unlock()
	if start != nil {
		select {
		case <-start.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if proc == nil {
		return nil
	}
	return m.terminateProcess(ctx, proc)
}

func (m *Manager) EndUserEnvironment(ctx context.Context, userID string) error {
	if err := m.StopUser(ctx, userID); err != nil {
		return err
	}
	return m.cfg.RemoveContainerCredential(userID)
}

func (m *Manager) ClientFor(ctx context.Context, userID string, handoff AuthHandoff) (*http.Client, *url.URL, error) {
	proc, err := m.Ensure(ctx, userID, handoff, LLMProviderConfig{Provider: settings.DefaultProvider, BaseURL: settings.DefaultBaseURL, ModelName: settings.DefaultModel})
	if err != nil {
		return nil, nil, err
	}
	return &http.Client{Timeout: 120 * time.Second}, proc.BaseURL, nil
}

func (m *Manager) ApplyUserLLMProvider(ctx context.Context, userID string, provider LLMProviderConfig) (ApplyResult, error) {
	if err := m.cfg.WriteUserClaudeSettings(userID, provider); err != nil {
		return ApplyResult{}, err
	}
	return m.RestartUser(ctx, userID, provider)
}

func (m *Manager) ApplyUserRuntimeProvider(ctx context.Context, userID string, provider settings.RuntimeProvider) (settings.LLMProviderApplyResult, error) {
	result, err := m.ApplyUserLLMProvider(ctx, userID, LLMProviderConfig{
		Provider:  provider.Provider,
		BaseURL:   provider.BaseURL,
		APIKey:    provider.APIKey,
		ModelName: provider.ModelName,
	})
	return settings.LLMProviderApplyResult{Running: result.Running, Restarted: result.Restarted}, err
}

func (m *Manager) UserClaudeSettings(userID string) (claudeSettings, error) {
	return m.cfg.ReadUserClaudeSettings(userID)
}

func (m *Manager) RestartUser(ctx context.Context, userID string, provider LLMProviderConfig) (ApplyResult, error) {
	m.mu.Lock()
	start := m.starting[userID]
	proc := m.processes[userID]
	handoff := AuthHandoff{}
	if start != nil {
		start.cancel()
	}
	m.mu.Unlock()

	if start != nil {
		select {
		case <-start.done:
			start.mu.Lock()
			if start.proc != nil {
				proc = start.proc
			}
			if proc != nil {
				handoff = proc.Handoff
			}
			start.mu.Unlock()
		case <-ctx.Done():
			return ApplyResult{}, ctx.Err()
		}
	}
	if proc == nil {
		return ApplyResult{Running: false, Restarted: false}, nil
	}
	handoff = proc.Handoff
	if err := m.terminateProcess(ctx, proc); err != nil {
		return ApplyResult{Running: true, Restarted: false}, err
	}
	m.mu.Lock()
	if m.processes[userID] == proc {
		delete(m.processes, userID)
	}
	m.mu.Unlock()
	if _, err := m.Ensure(ctx, userID, handoff, provider); err != nil {
		return ApplyResult{Running: true, Restarted: false}, err
	}
	return ApplyResult{Running: true, Restarted: true}, nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.reaperOnce.Do(func() {
		if m.reaperStop != nil {
			close(m.reaperStop)
		}
	})
	m.mu.Lock()
	m.closing = true
	starts := make([]*startState, 0, len(m.starting))
	for _, start := range m.starting {
		starts = append(starts, start)
	}
	processes := make([]*Process, 0, len(m.processes))
	for _, proc := range m.processes {
		processes = append(processes, proc)
	}
	m.mu.Unlock()

	var errs []error
	for _, start := range starts {
		start.cancel()
		select {
		case <-start.done:
			start.mu.Lock()
			if start.proc != nil {
				processes = append(processes, start.proc)
			}
			if start.err != nil && !errors.Is(start.err, context.Canceled) {
				errs = append(errs, start.err)
			}
			start.mu.Unlock()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, proc := range processes {
		if err := m.terminateProcess(ctx, proc); err != nil {
			errs = append(errs, fmt.Errorf("user %s pid %d: %w", proc.UserID, proc.Cmd.Process.Pid, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *Manager) idleReaper() {
	ticker := time.NewTicker(m.cfg.IdleScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evictIdle(context.Background())
		case <-m.reaperStop:
			return
		}
	}
}

func (m *Manager) evictIdle(ctx context.Context) {
	now := time.Now()
	var victims []*Process
	m.mu.Lock()
	for userID, proc := range m.processes {
		if proc.isDone() {
			delete(m.processes, userID)
			continue
		}
		if proc.isIdleFor(now, m.cfg.IdleTTL) {
			victims = append(victims, proc)
			delete(m.processes, userID)
		}
	}
	m.mu.Unlock()
	for _, proc := range victims {
		if err := m.terminateProcess(ctx, proc); err != nil {
			slog.Warn("companion idle eviction failed", "user_id", proc.UserID, "error", err)
		}
	}
}

func (m *Manager) evictForCapacityLocked(ctx context.Context, nextUserID string) error {
	if m.cfg.MaxProcesses <= 0 || len(m.processes) < m.cfg.MaxProcesses {
		return nil
	}
	var candidate *Process
	for userID, proc := range m.processes {
		if userID == nextUserID || proc.isDone() {
			continue
		}
		if !proc.isIdleNow() {
			continue
		}
		if candidate == nil || proc.lastAccessBefore(candidate) {
			candidate = proc
		}
	}
	if candidate == nil {
		return fmt.Errorf("companion_capacity_exceeded")
	}
	delete(m.processes, candidate.UserID)
	go func() {
		evictCtx, cancel := context.WithTimeout(context.Background(), m.cfg.ShutdownTimeout)
		defer cancel()
		if err := m.terminateProcess(evictCtx, candidate); err != nil {
			slog.Warn("companion capacity eviction failed", "user_id", candidate.UserID, "error", err)
		}
	}()
	return nil
}
