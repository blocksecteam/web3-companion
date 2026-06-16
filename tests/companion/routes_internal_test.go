// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package companion_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/companion"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestBrowserWebSocketProtocolSelectsBearer(t *testing.T) {
	got := companion.BrowserWebSocketProtocolForTest("web3-bearer,eyJhbGciOiJIUzI1NiJ9")

	if got != "web3-bearer" {
		t.Fatalf("browserWebSocketProtocol() = %q, want web3-bearer", got)
	}
}

func TestBrowserWebSocketProtocolIgnoresUnknownProtocol(t *testing.T) {
	got := companion.BrowserWebSocketProtocolForTest("web3-unknown,secret")

	if got != "" {
		t.Fatalf("browserWebSocketProtocol() = %q, want empty protocol", got)
	}
}

func TestCompanionRoutesRejectContainerTokenForBrowserGateway(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(auth.TestJWTSecret)
	tokenService := auth.NewTokenService(db, secret)
	containerToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientContainer,
		Scope:      auth.ScopeCompanion,
	})
	if err != nil {
		t.Fatalf("issue container token: %v", err)
	}

	r := gin.New()
	companion.RegisterRoutes(r, db, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), companion.WithTokenService(tokenService), companion.WithJWTSecret(string(secret)))

	w := testutil.DoRequest(r, http.MethodGet, "/api/companion/sessions", nil, map[string]string{
		"Authorization": "Bearer " + containerToken.AccessToken,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("container token reached browser companion route: %d %s", w.Code, w.Body.String())
	}
}

func TestCreateWebSocketTicketBindsSessionAndConsumesOnce(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(auth.TestJWTSecret)
	tokenService := auth.NewTokenService(db, secret)
	browserToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientBrowser,
		Scope:      auth.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue browser token: %v", err)
	}

	r := gin.New()
	companion.RegisterRoutes(r, db, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), companion.WithTokenService(tokenService), companion.WithJWTSecret(string(secret)))

	w := testutil.DoRequest(r, http.MethodPost, "/api/companion/ws-ticket", map[string]string{
		"session_id": "chat_1",
	}, map[string]string{
		"Authorization": "Bearer " + browserToken.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create ticket = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Ticket    string `json:"ticket"`
		ExpiresAt int64  `json:"expires_at"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Ticket == "" || resp.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("invalid websocket ticket response: %#v", resp)
	}

	claims, err := companion.ConsumeWebSocketTicketForTest(t.Context(), db, resp.Ticket, "chat_1")
	if err != nil {
		t.Fatalf("consume ticket first time: %v", err)
	}
	if claims.UserID != userID || claims.AuthSessionID != browserToken.SessionID || claims.ChatSessionID != "chat_1" {
		t.Fatalf("ticket claims = %#v, want user/session/chat binding", claims)
	}
	if _, err := companion.ConsumeWebSocketTicketForTest(t.Context(), db, resp.Ticket, "chat_1"); err == nil {
		t.Fatal("websocket ticket was reusable")
	}
}

func TestConsumeWebSocketTicketRejectsWrongChatSession(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	session := storage.AuthSession{
		SessionID:        "sess_ticket_wrong_chat",
		UserID:           userID,
		ClientType:       auth.ClientBrowser,
		Scope:            auth.ScopeBrowser,
		RefreshTokenHash: "hash_ticket_wrong_chat",
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	ticket := storage.WebSocketTicket{
		TicketHash:    companion.HashWebSocketTicketForTest("ticket-wrong-chat"),
		UserID:        userID,
		AuthSessionID: session.SessionID,
		ChatSessionID: "chat_expected",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(time.Minute),
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("create websocket ticket: %v", err)
	}

	if _, err := companion.ConsumeWebSocketTicketForTest(t.Context(), db, "ticket-wrong-chat", "chat_other"); err == nil {
		t.Fatal("ticket was accepted for the wrong chat session")
	}
}

func TestWebSocketTicketMiddlewareAuthenticatesTicketProtocol(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	session := storage.AuthSession{
		SessionID:        "sess_ws_ticket",
		UserID:           userID,
		ClientType:       auth.ClientBrowser,
		Scope:            auth.ScopeBrowser,
		RefreshTokenHash: "hash_ws_ticket",
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	ticket := storage.WebSocketTicket{
		TicketHash:    companion.HashWebSocketTicketForTest("ticket-protocol"),
		UserID:        userID,
		AuthSessionID: session.SessionID,
		ChatSessionID: "chat_ws",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(time.Minute),
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("create websocket ticket: %v", err)
	}

	r := gin.New()
	r.GET("/ws/chat", companion.WebSocketTicketMiddlewareForTest(db), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id":    auth.GetUserID(c.Request.Context()),
			"session_id": auth.GetSessionID(c.Request.Context()),
			"client":     auth.GetClientType(c.Request.Context()),
			"scope":      auth.GetScope(c.Request.Context()),
		})
	})
	w := testutil.DoRequest(r, http.MethodGet, "/ws/chat?session=chat_ws", nil, map[string]string{
		"Sec-WebSocket-Protocol": "web3-ws-ticket,ticket-protocol",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("ticket middleware = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	testutil.ParseJSON(t, w, &resp)
	if resp["user_id"] != userID || resp["session_id"] != session.SessionID || resp["client"] != auth.ClientBrowser || resp["scope"] != auth.ScopeBrowser {
		t.Fatalf("ticket middleware context = %#v", resp)
	}
}

func TestValidateSessionIDRejectsOverlongAndControlChars(t *testing.T) {
	if _, ok := companion.ValidateSessionIDForTest("  "); ok {
		t.Fatal("blank session id accepted")
	}
	if _, ok := companion.ValidateSessionIDForTest(strings.Repeat("a", companion.MaxSessionIDLengthForTest+1)); ok {
		t.Fatal("overlong session id accepted")
	}
	if _, ok := companion.ValidateSessionIDForTest("chat\x00id"); ok {
		t.Fatal("control character session id accepted")
	}
	got, ok := companion.ValidateSessionIDForTest("  chat_1  ")
	if !ok || got != "chat_1" {
		t.Fatalf("valid session id rejected: got=%q ok=%v", got, ok)
	}
}

func TestValidateSessionNameRejectsOverlong(t *testing.T) {
	if _, ok := companion.ValidateSessionNameForTest(strings.Repeat("x", companion.MaxSessionNameBytesForTest+1)); ok {
		t.Fatal("overlong session name accepted")
	}
	got, ok := companion.ValidateSessionNameForTest("  My Session  ")
	if !ok || got != "My Session" {
		t.Fatalf("valid session name rejected: got=%q ok=%v", got, ok)
	}
	if got, ok := companion.ValidateSessionNameForTest("   "); !ok || got != "" {
		t.Fatalf("blank name should normalize to empty clear: got=%q ok=%v", got, ok)
	}
}

func TestSessionRenameEndpointEnforcesLimits(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(auth.TestJWTSecret)
	tokenService := auth.NewTokenService(db, secret)
	browserToken, err := tokenService.Issue(t.Context(), auth.SessionRequest{
		UserID:     userID,
		ClientType: auth.ClientBrowser,
		Scope:      auth.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue browser token: %v", err)
	}
	r := gin.New()
	companion.RegisterRoutes(r, db, companion.NewManager(companion.Config{HomeRoot: t.TempDir()}), companion.WithTokenService(tokenService), companion.WithJWTSecret(string(secret)))
	authHeader := map[string]string{"Authorization": "Bearer " + browserToken.AccessToken}

	// Overlong name is rejected.
	w := testutil.DoRequest(r, http.MethodPut, "/api/companion/sessions/chat_1/name", map[string]string{
		"name": strings.Repeat("x", companion.MaxSessionNameBytesForTest+1),
	}, authHeader)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("overlong rename = %d: %s", w.Code, w.Body.String())
	}

	// Valid rename succeeds and persists.
	w = testutil.DoRequest(r, http.MethodPut, "/api/companion/sessions/chat_1/name", map[string]string{
		"name": "Renamed",
	}, authHeader)
	if w.Code != http.StatusOK {
		t.Fatalf("valid rename = %d: %s", w.Code, w.Body.String())
	}
	var row storage.AppSetting
	if err := db.Where("key = ?", "session_name:"+userID+":chat_1").First(&row).Error; err != nil {
		t.Fatalf("rename not persisted: %v", err)
	}
	if row.Value != "Renamed" {
		t.Fatalf("stored name = %q, want Renamed", row.Value)
	}

	// session-names returns the stored name.
	w = testutil.DoRequest(r, http.MethodGet, "/api/companion/session-names", nil, authHeader)
	if w.Code != http.StatusOK {
		t.Fatalf("session-names = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Names map[string]string `json:"names"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.Names["chat_1"] != "Renamed" {
		t.Fatalf("session-names = %#v, want chat_1=Renamed", resp.Names)
	}
}

func TestSessionStillActiveReflectsRevocation(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	session := storage.AuthSession{
		SessionID:        "sess_revoked_watch",
		UserID:           userID,
		ClientType:       auth.ClientBrowser,
		Scope:            auth.ScopeBrowser,
		RefreshTokenHash: "hash_revoked_watch",
		CreatedAt:        time.Now(),
		ExpiresAt:        time.Now().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	ok, err := companion.BrowserSessionStillActiveForTest(t.Context(), db, userID, session.SessionID)
	if err != nil || !ok {
		t.Fatalf("active session check = %v %v, want active", ok, err)
	}
	now := time.Now()
	if err := db.Model(&storage.AuthSession{}).Where("session_id = ?", session.SessionID).Update("revoked_at", now).Error; err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	ok, err = companion.BrowserSessionStillActiveForTest(t.Context(), db, userID, session.SessionID)
	if err != nil {
		t.Fatalf("revoked session check returned db error: %v", err)
	}
	if ok {
		t.Fatal("revoked session still reported active")
	}
}
