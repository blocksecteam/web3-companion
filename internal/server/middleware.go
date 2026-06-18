// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/blocksecteam/web3-companion/internal/config"
)

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' https://api.portals.fi https://x402.blocksec.ai https://aml.blocksec.com")
		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := requestLogPath(c.Request.URL)
		if path == "/health" {
			c.Next()
			return
		}

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		slog.LogAttrs(c.Request.Context(), level,
			"request",
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("ip", c.ClientIP()),
			slog.Int("size", c.Writer.Size()),
		)
	}
}

func requestLogPath(u *url.URL) string {
	if u == nil {
		return ""
	}
	path := u.Path
	if u.RawQuery == "" {
		return path
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return path + "?<invalid>"
	}
	for key := range values {
		if sensitiveQueryKey(key) {
			values[key] = []string{"<redacted>"}
		}
	}
	return path + "?" + values.Encode()
}

func sensitiveQueryKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "credential") {
		return true
	}
	switch key {
	case "challenge", "challenge_id", "api_key", "key", "code", "signature", "private_key":
		return true
	default:
		return false
	}
}

func browserCORS(cfg *config.Config) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	if cfg != nil {
		for _, origin := range cfg.CORS.AllowedOrigins {
			if origin = normalizeOrigin(origin); origin != "" {
				allowed[origin] = struct{}{}
			}
		}
		if origin := normalizeOrigin(cfg.Server.ExternalURL); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := normalizeOrigin(c.GetHeader("Origin"))
		if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Sec-WebSocket-Protocol")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			}
		}
		if c.Request.Method == "OPTIONS" {
			if origin != "" {
				if _, ok := allowed[origin]; ok {
					c.AbortWithStatus(204)
					return
				}
			}
			c.AbortWithStatus(403)
			return
		}
		c.Next()
	}
}

func BrowserCORSForTest(cfg *config.Config) gin.HandlerFunc {
	return browserCORS(cfg)
}

func normalizeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func setupLogging(cfg config.LogConfig, workdir string, console io.Writer) {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if console == nil {
		console = os.Stdout
	}
	handler := slog.Handler(slog.NewJSONHandler(console, opts))

	if fileWriter := rotatingLogWriter(cfg, workdir); fileWriter != nil {
		handler = multiHandler{
			handlers: []slog.Handler{
				handler,
				slog.NewJSONHandler(fileWriter, opts),
			},
		}
	}
	slog.SetDefault(slog.New(handler))
}

type multiHandler struct {
	handlers []slog.Handler
}

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var firstErr error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithAttrs(attrs))
	}
	return multiHandler{handlers: handlers}
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithGroup(name))
	}
	return multiHandler{handlers: handlers}
}

func rotatingLogWriter(cfg config.LogConfig, workdir string) io.Writer {
	logPath := strings.TrimSpace(cfg.FilePath)
	if logPath == "" {
		logPath = filepath.Join(strings.TrimSpace(workdir), "logs", "web3.log.jsonl")
	}
	if !filepath.IsAbs(logPath) {
		base := strings.TrimSpace(workdir)
		if base == "" {
			base = "."
		}
		logPath = filepath.Join(base, logPath)
	}
	maxSize := cfg.MaxSizeMB
	if maxSize <= 0 {
		maxSize = 100
	}
	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = 7
	}
	maxAge := cfg.MaxAgeDays
	if maxAge <= 0 {
		maxAge = 30
	}
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   true,
	}
}

type ipWindow struct {
	count    atomic.Int64
	resetAt  atomic.Int64
	windowNs int64
}

type rateLimiterState struct {
	mu      sync.Mutex
	windows map[string]*ipWindow
}

func (s *rateLimiterState) allow(ip string, maxRequests int64, windowNs int64) bool {
	s.mu.Lock()
	w, ok := s.windows[ip]
	if !ok {
		w = &ipWindow{windowNs: windowNs}
		w.resetAt.Store(time.Now().UnixNano() + windowNs)
		s.windows[ip] = w
	}
	s.mu.Unlock()

	now := time.Now().UnixNano()
	if now >= w.resetAt.Load() {
		w.count.Store(0)
		w.resetAt.Store(now + w.windowNs)
	}
	return w.count.Add(1) <= maxRequests
}

func RateLimiter(maxRequests int64, window time.Duration) gin.HandlerFunc {
	state := &rateLimiterState{windows: make(map[string]*ipWindow)}
	windowNs := window.Nanoseconds()

	go func() {
		for range time.Tick(5 * time.Minute) {
			now := time.Now().UnixNano()
			state.mu.Lock()
			for ip, w := range state.windows {
				if now >= w.resetAt.Load()+windowNs {
					delete(state.windows, ip)
				}
			}
			state.mu.Unlock()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !state.allow(ip, maxRequests, windowNs) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{"code": "rate_limited", "message": "Too many requests"},
			})
			return
		}
		c.Next()
	}
}
