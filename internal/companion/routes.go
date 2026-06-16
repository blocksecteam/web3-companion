// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/config"
	"github.com/blocksecteam/web3-companion/internal/settings"
	"github.com/blocksecteam/web3-companion/internal/storage"
)

func DefaultConfig(backendRoot, workdir string, cfgs ...*config.Config) Config {
	if workdir == "" {
		workdir = filepath.Join(backendRoot, "dist")
	}
	var cfg *config.Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg == nil {
		cfg = config.LoadOrEnv("")
	} else {
		copied := *cfg
		copied.ApplyEnv(config.ReadEnv())
		cfg = &copied
	}
	scriptPath, err := defaultScriptPath(backendRoot, workdir)
	if err != nil {
		slog.Warn("embedded companion script unavailable; falling back to source dist", "error", err)
	}
	backendAPIURL := containerLocalBackendAPIURL(cfg)

	return Config{
		HomeRoot:             filepath.Join(workdir, "companion", "users"),
		RuntimePath:          defaultRuntimePath,
		ScriptPath:           scriptPath,
		ClaudeExecutablePath: defaultClaudeExecutablePath,
		BackendRoot:          backendRoot,
		ListenHost:           defaultListenHost,
		BackendAPIURL:        backendAPIURL,
		SecurityAgent: SecurityAgentConfig{
			ReviewMode:       cfg.Security.Agent.ReviewMode,
			AllowReviewClear: cfg.Security.Agent.AllowReviewClear,
		},
		StartupTimeout:   defaultStartupTimeout,
		ShutdownTimeout:  defaultShutdownTimeout,
		IdleTTL:          defaultIdleTTL,
		IdleScanInterval: defaultIdleScanInterval,
		MaxProcesses:     defaultMaxProcesses,
		MaxMessageBytes:  defaultMaxMessageBytes,
		MaxPromptChars:   defaultMaxPromptChars,
	}
}

func defaultScriptPath(backendRoot, workdir string) (string, error) {
	embeddedPath, err := EnsureEmbeddedScript(workdir)
	if err == nil {
		return embeddedPath, nil
	}
	return filepath.Join(backendRoot, "companion", "dist", "companion-server.js"), err
}

func containerLocalBackendAPIURL(cfg *config.Config) string {
	port := cfg.Server.Port
	if port <= 0 {
		port = 8080
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

type RouteOption func(*routeConfig)

type LLMProviderResolver func(context.Context) (LLMProviderConfig, error)

type routeConfig struct {
	tokenService       *auth.TokenService
	providerResolver   LLMProviderResolver
	authOptions        []auth.MiddlewareOption
	webSocketTicketTTL time.Duration
}

func WithTokenService(tokenService *auth.TokenService) RouteOption {
	return func(cfg *routeConfig) { cfg.tokenService = tokenService }
}

func WithJWTSecret(secret string) RouteOption {
	return func(cfg *routeConfig) { cfg.authOptions = append(cfg.authOptions, auth.WithJWTSecretString(secret)) }
}

func WithRuntimeProviderResolver(resolver LLMProviderResolver) RouteOption {
	return func(cfg *routeConfig) { cfg.providerResolver = resolver }
}

func RegisterRoutes(r *gin.Engine, db *gorm.DB, manager *Manager, opts ...RouteOption) {
	cfg := routeConfig{webSocketTicketTTL: time.Minute}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.tokenService == nil {
		panic("companion.RegisterRoutes: WithTokenService is required")
	}
	api := r.Group("/api/companion")
	api.Use(auth.Middleware(db, cfg.authOptions...))
	api.Use(auth.RequireClient(auth.ClientBrowser), auth.RequireScope(auth.ScopeBrowser))
	api.POST("/ws-ticket", webSocketTicketHandler(db, cfg.webSocketTicketTTL))
	api.GET("/sessions", forwardHandler(db, manager, cfg.tokenService, cfg.providerResolver, "/api/companion"))
	api.GET("/sessions/:id/messages", forwardHandler(db, manager, cfg.tokenService, cfg.providerResolver, "/api/companion"))
	api.DELETE("/sessions/:id", forwardHandler(db, manager, cfg.tokenService, cfg.providerResolver, "/api/companion"))
	api.PUT("/sessions/:id/name", sessionRenameHandler(db))
	api.GET("/session-names", sessionNamesHandler(db))

	ws := r.Group("/ws")
	ws.Use(webSocketTicketMiddleware(db))
	ws.GET("/chat", forwardHandler(db, manager, cfg.tokenService, cfg.providerResolver, ""))
}

const (
	maxSessionIDLength      = 128
	maxSessionNameBytes     = 200
	maxSessionNamesReturned = 1000
)

// validateSessionID trims and bounds a client-supplied session id, rejecting
// blank, overlong, or control-character values so they cannot be used to
// inflate stored keys or smuggle unexpected characters into LIKE queries.
func validateSessionID(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxSessionIDLength {
		return "", false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return id, true
}

// validateSessionName trims and bounds the display name. An empty result is
// valid and signals a clear; ok is false only when the name exceeds the byte cap.
func validateSessionName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if len(name) > maxSessionNameBytes {
		return "", false
	}
	return name, true
}

func sessionNamesHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := auth.GetUserID(c.Request.Context())
		prefix := fmt.Sprintf("session_name:%s:", userID)
		var rows []storage.AppSetting
		if err := db.WithContext(c.Request.Context()).
			Where("key LIKE ?", prefix+"%").
			Limit(maxSessionNamesReturned).
			Find(&rows).Error; err != nil {
			slog.Error("companion session names load failed", "user_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "session_names_failed", "message": "Failed to load session names"}})
			return
		}
		names := make(map[string]string, len(rows))
		for _, row := range rows {
			sessionID := strings.TrimPrefix(row.Key, prefix)
			if sessionID != "" {
				names[sessionID] = row.Value
			}
		}
		c.JSON(http.StatusOK, gin.H{"names": names})
	}
}

func sessionRenameHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, ok := validateSessionID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "invalid session id"}})
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": err.Error()}})
			return
		}
		name, ok := validateSessionName(req.Name)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "session name too long"}})
			return
		}
		userID := auth.GetUserID(c.Request.Context())
		key := fmt.Sprintf("session_name:%s:%s", userID, sessionID)
		if name == "" {
			if err := db.WithContext(c.Request.Context()).Where("key = ?", key).Delete(&storage.AppSetting{}).Error; err != nil {
				slog.Error("companion session name clear failed", "user_id", userID, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "session_rename_failed", "message": "Failed to clear session name"}})
				return
			}
			c.JSON(http.StatusOK, gin.H{"status": "cleared"})
			return
		}
		if err := db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
		}).Create(&storage.AppSetting{Key: key, Value: name, UpdatedAt: time.Now().UTC()}).Error; err != nil {
			slog.Error("companion session name save failed", "user_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "session_rename_failed", "message": "Failed to save session name"}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "name": name})
	}
}

type webSocketTicketRequest struct {
	SessionID string `json:"session_id"`
}

func webSocketTicketHandler(db *gorm.DB, ttl time.Duration) gin.HandlerFunc {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return func(c *gin.Context) {
		var req webSocketTicketRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "session_id is required"}})
			return
		}
		chatSessionID := strings.TrimSpace(req.SessionID)
		if chatSessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request", "message": "session_id is required"}})
			return
		}
		ticket, err := randomWebSocketTicket()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "websocket_ticket_failed", "message": "Failed to create websocket ticket"}})
			return
		}
		now := time.Now()
		expiresAt := now.Add(ttl)
		row := storage.WebSocketTicket{
			TicketHash:    hashWebSocketTicket(ticket),
			UserID:        auth.GetUserID(c.Request.Context()),
			AuthSessionID: auth.GetSessionID(c.Request.Context()),
			ChatSessionID: chatSessionID,
			CreatedAt:     now,
			ExpiresAt:     expiresAt,
		}
		if err := db.WithContext(c.Request.Context()).Create(&row).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "websocket_ticket_failed", "message": "Failed to store websocket ticket"}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expires_at": expiresAt.Unix()})
	}
}

type webSocketTicketClaims struct {
	UserID        string
	AuthSessionID string
	ChatSessionID string
}

func consumeWebSocketTicket(ctx context.Context, db *gorm.DB, ticket, chatSessionID string) (webSocketTicketClaims, error) {
	ticket = strings.TrimSpace(ticket)
	chatSessionID = strings.TrimSpace(chatSessionID)
	if ticket == "" || chatSessionID == "" {
		return webSocketTicketClaims{}, fmt.Errorf("invalid_websocket_ticket")
	}
	now := time.Now()
	var row storage.WebSocketTicket
	err := db.WithContext(ctx).
		Where("ticket_hash = ? AND chat_session_id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at > ?", hashWebSocketTicket(ticket), chatSessionID, now).
		First(&row).Error
	if err != nil {
		return webSocketTicketClaims{}, fmt.Errorf("invalid_websocket_ticket")
	}
	var session storage.AuthSession
	if err := db.WithContext(ctx).
		Where("session_id = ? AND user_id = ? AND client_type = ? AND scope = ? AND revoked_at IS NULL AND expires_at > ?",
			row.AuthSessionID, row.UserID, auth.ClientBrowser, auth.ScopeBrowser, now).
		First(&session).Error; err != nil {
		return webSocketTicketClaims{}, fmt.Errorf("invalid_websocket_ticket")
	}
	result := db.WithContext(ctx).Model(&storage.WebSocketTicket{}).
		Where("ticket_hash = ? AND consumed_at IS NULL AND revoked_at IS NULL", row.TicketHash).
		Update("consumed_at", now)
	if result.Error != nil {
		return webSocketTicketClaims{}, result.Error
	}
	if result.RowsAffected != 1 {
		return webSocketTicketClaims{}, fmt.Errorf("invalid_websocket_ticket")
	}
	return webSocketTicketClaims{UserID: row.UserID, AuthSessionID: row.AuthSessionID, ChatSessionID: row.ChatSessionID}, nil
}

func browserSessionStillActive(ctx context.Context, db *gorm.DB, userID, sessionID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return false, nil
	}
	var count int64
	err := db.WithContext(ctx).Model(&storage.AuthSession{}).
		Where("session_id = ? AND user_id = ? AND client_type = ? AND scope = ? AND revoked_at IS NULL AND expires_at > ?",
			sessionID, userID, auth.ClientBrowser, auth.ScopeBrowser, time.Now()).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func cancelWebSocketWhenBrowserSessionEnds(ctx context.Context, cancel context.CancelFunc, db *gorm.DB, userID, sessionID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active, err := browserSessionStillActive(ctx, db, userID, sessionID)
			if err != nil {
				slog.Warn("companion websocket session recheck failed", "user_id", userID, "session_id", sessionID, "error", err)
				continue
			}
			if !active {
				slog.Info("companion websocket closing after browser session ended", "user_id", userID, "session_id", sessionID)
				cancel()
				return
			}
		}
	}
}

func webSocketTicketMiddleware(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ticket := ticketFromWebSocketProtocol(c.GetHeader("Sec-WebSocket-Protocol"))
		if ticket == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "missing_websocket_ticket", "message": "WebSocket ticket required"}})
			return
		}
		claims, err := consumeWebSocketTicket(c.Request.Context(), db, ticket, c.Query("session"))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "invalid_websocket_ticket", "message": "Invalid or expired WebSocket ticket"}})
			return
		}
		ctx := auth.WithRequestIdentity(c.Request.Context(), claims.UserID, claims.AuthSessionID, auth.ClientBrowser, auth.ScopeBrowser, c.GetHeader("X-Request-ID"))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func forwardHandler(db *gorm.DB, manager *Manager, tokenService *auth.TokenService, providerResolver LLMProviderResolver, stripPrefix string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := auth.GetUserID(c.Request.Context())
		isWebSocket := isWebSocketRequest(c.Request)
		routeKind := "http"
		if isWebSocket {
			routeKind = "websocket"
		}
		browserWSProtocol := browserWebSocketProtocol(c.GetHeader("Sec-WebSocket-Protocol"))
		protocolKind := browserWebSocketProtocolKind(c.GetHeader("Sec-WebSocket-Protocol"))
		slog.Info("companion gateway request received",
			"user_id", userID,
			"route_kind", routeKind,
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"raw_query", c.Request.URL.RawQuery,
			"is_websocket", isWebSocket,
			"upgrade", c.GetHeader("Upgrade"),
			"connection", c.GetHeader("Connection"),
			"browser_ws_protocol", browserWSProtocol,
			"ws_protocol_kind", protocolKind,
		)
		credential := containerCredentialForRequest(c.Request.Context(), manager, tokenService, userID)
		if strings.TrimSpace(credential.CredentialID) == "" || strings.TrimSpace(credential.Secret) == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "container_credential_failed", "message": "Failed to issue container credentials"}})
			return
		}
		var userEmail string
		if db != nil {
			var u storage.User
			if err := db.Select("email").Where("id = ?", userID).First(&u).Error; err == nil {
				userEmail = u.Email
			}
		}
		handoff := AuthHandoff{
			Client: "container",
			APIURL: manager.cfg.BackendAPIURL,
			Email:  userEmail,
			ContainerCredential: ContainerCredential{
				Version:      credential.Version,
				Client:       credential.Client,
				CredentialID: credential.CredentialID,
				Secret:       credential.Secret,
				Scope:        credential.Scope,
				APIURL:       manager.cfg.BackendAPIURL,
				Email:        userEmail,
			},
		}
		provider, err := runtimeProviderForRequest(c.Request.Context(), providerResolver)
		if err != nil {
			slog.Error("companion provider resolution failed", "user_id", userID, "path", c.Request.URL.Path, "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "provider_runtime_failed", "message": err.Error()}})
			return
		}
		gateway, err := manager.GatewayForProvider(c.Request.Context(), userID, handoff, provider)
		if err != nil {
			slog.Error("companion gateway request failed before upstream", "user_id", userID, "path", c.Request.URL.Path, "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "companion_unavailable", "message": err.Error()}})
			return
		}
		if isWebSocket && db != nil {
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			go cancelWebSocketWhenBrowserSessionEnds(ctx, cancel, db, userID, auth.GetSessionID(ctx))
		}
		if stripPrefix != "" {
			originalDirector := gateway.Director
			gateway.Director = func(req *http.Request) {
				originalDirector(req)
				req.URL.Path = companionAPIPath(c, stripPrefix)
				req.Host = req.URL.Host
				req.Header.Del("Authorization")
				req.Header.Del("Sec-WebSocket-Protocol")
				slog.Info("companion gateway upstream request prepared",
					"user_id", userID,
					"route_kind", routeKind,
					"method", req.Method,
					"upstream_url", req.URL.String(),
					"has_upgrade", isWebSocketRequest(req),
				)
			}
		} else {
			originalDirector := gateway.Director
			gateway.Director = func(req *http.Request) {
				originalDirector(req)
				req.Header.Del("Authorization")
				req.Header.Del("Sec-WebSocket-Protocol")
				req.Host = req.URL.Host
				slog.Info("companion gateway upstream request prepared",
					"user_id", userID,
					"route_kind", routeKind,
					"method", req.Method,
					"upstream_url", req.URL.String(),
					"has_upgrade", isWebSocketRequest(req),
				)
			}
		}
		manager.BeginRequest(userID, isWebSocket)
		defer manager.EndRequest(userID, isWebSocket)
		gateway.Transport = directTransport()
		originalModifyResponse := gateway.ModifyResponse
		gateway.ModifyResponse = func(res *http.Response) error {
			if originalModifyResponse != nil {
				if err := originalModifyResponse(res); err != nil {
					return err
				}
			}
			slog.Info("companion gateway upstream response received",
				"user_id", userID,
				"route_kind", routeKind,
				"method", res.Request.Method,
				"upstream_url", res.Request.URL.String(),
				"status", res.StatusCode,
				"upgrade", res.Header.Get("Upgrade"),
				"connection", res.Header.Get("Connection"),
			)
			if browserWSProtocol != "" && res.StatusCode == http.StatusSwitchingProtocols {
				res.Header.Set("Sec-WebSocket-Protocol", browserWSProtocol)
			}
			return nil
		}
		gateway.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
			slog.Error("companion upstream gateway failed",
				"user_id", userID,
				"route_kind", routeKind,
				"method", req.Method,
				"path", req.URL.Path,
				"upstream_url", req.URL.String(),
				"is_websocket", isWebSocketRequest(req),
				"error", err,
			)
			http.Error(w, "companion gateway failed", http.StatusBadGateway)
		}
		gateway.ServeHTTP(c.Writer, c.Request)
	}
}

func runtimeProviderForRequest(ctx context.Context, resolver LLMProviderResolver) (LLMProviderConfig, error) {
	if resolver == nil {
		return LLMProviderConfig{Provider: settings.DefaultProvider, BaseURL: settings.DefaultBaseURL, ModelName: settings.DefaultModel}, nil
	}
	return resolver(ctx)
}

func containerCredentialForRequest(ctx context.Context, manager *Manager, tokenService *auth.TokenService, userID string) ContainerCredential {
	if manager != nil && tokenService != nil {
		if existing, err := manager.cfg.ReadContainerCredential(userID); err == nil {
			if err := tokenService.ValidateContainerCredential(ctx, userID, auth.ScopeCompanion, existing.CredentialID, existing.Secret); err == nil {
				return existing
			}
		}
	}
	if tokenService == nil {
		return ContainerCredential{}
	}
	issued, err := tokenService.CreateContainerCredential(ctx, userID, auth.ScopeCompanion)
	if err != nil {
		return ContainerCredential{}
	}
	return ContainerCredential{
		Version:      issued.Version,
		Client:       issued.Client,
		CredentialID: issued.CredentialID,
		Secret:       issued.Secret,
		Scope:        issued.Scope,
	}
}

func randomWebSocketTicket() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashWebSocketTicket(ticket string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ticket)))
	return hex.EncodeToString(sum[:])
}

func ticketFromWebSocketProtocol(header string) string {
	parts := strings.Split(header, ",")
	for index, part := range parts {
		if strings.TrimSpace(part) == "web3-ws-ticket" && index+1 < len(parts) {
			return strings.TrimSpace(parts[index+1])
		}
	}
	return ""
}

func browserWebSocketProtocol(header string) string {
	for _, part := range strings.Split(header, ",") {
		switch strings.TrimSpace(part) {
		case "web3-ws-ticket":
			return "web3-ws-ticket"
		case "web3-bearer":
			return "web3-bearer"
		}
	}
	return ""
}

func browserWebSocketProtocolKind(header string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "web3-ws-ticket":
			return "ticket"
		case part == "web3-bearer":
			return "bearer"
		}
	}
	return ""
}

func isWebSocketRequest(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

func directTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	transport := base.Clone()
	transport.Proxy = nil
	return transport
}

func joinPaths(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	aslash := a[len(a)-1] == '/'
	bslash := b[0] == '/'
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func companionAPIPath(c *gin.Context, stripPrefix string) string {
	path := c.Param("path")
	if path == "" && stripPrefix != "" {
		path = strings.TrimPrefix(c.Request.URL.Path, stripPrefix)
	}
	return joinPaths("/api", path)
}

type WebSocketTicketClaimsForTest struct {
	UserID        string
	AuthSessionID string
	ChatSessionID string
}

func BrowserWebSocketProtocolForTest(header string) string {
	return browserWebSocketProtocol(header)
}

func ConsumeWebSocketTicketForTest(ctx context.Context, db *gorm.DB, ticket, chatSessionID string) (WebSocketTicketClaimsForTest, error) {
	claims, err := consumeWebSocketTicket(ctx, db, ticket, chatSessionID)
	if err != nil {
		return WebSocketTicketClaimsForTest{}, err
	}
	return WebSocketTicketClaimsForTest{
		UserID:        claims.UserID,
		AuthSessionID: claims.AuthSessionID,
		ChatSessionID: claims.ChatSessionID,
	}, nil
}

func HashWebSocketTicketForTest(ticket string) string {
	return hashWebSocketTicket(ticket)
}

func WebSocketTicketMiddlewareForTest(db *gorm.DB) gin.HandlerFunc {
	return webSocketTicketMiddleware(db)
}

func BrowserSessionStillActiveForTest(ctx context.Context, db *gorm.DB, userID, sessionID string) (bool, error) {
	return browserSessionStillActive(ctx, db, userID, sessionID)
}

const (
	MaxSessionIDLengthForTest  = maxSessionIDLength
	MaxSessionNameBytesForTest = maxSessionNameBytes
)

func ValidateSessionIDForTest(id string) (string, bool) {
	return validateSessionID(id)
}

func ValidateSessionNameForTest(name string) (string, bool) {
	return validateSessionName(name)
}
