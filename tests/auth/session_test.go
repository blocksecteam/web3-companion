// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	authpkg "github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func TestAuthServiceIssuesJWTAndRefreshesWithRevocation(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))

	first, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:       userID,
		ClientType:   authpkg.ClientBrowser,
		Scope:        authpkg.ScopeBrowser,
		CredentialID: "cred_1",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if first.AccessToken == "" || first.RefreshToken == "" || first.TokenType != "Bearer" {
		t.Fatalf("incomplete token pair: %#v", first)
	}

	var session storage.AuthSession
	if err := db.Where("session_id = ?", first.SessionID).First(&session).Error; err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	if got := time.Until(session.ExpiresAt); got < 71*time.Hour || got > 73*time.Hour {
		t.Fatalf("refresh session ttl = %s, want about 72h", got)
	}

	r := gin.New()
	r.Use(authpkg.Middleware(db, authpkg.WithJWTSecret([]byte(authpkg.TestJWTSecret))))
	r.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id": authpkg.GetUserID(c.Request.Context()),
			"client":  authpkg.GetClientType(c.Request.Context()),
		})
	})
	w := testutil.DoRequest(r, http.MethodGet, "/api/v1/test", nil, map[string]string{
		"Authorization": "Bearer " + first.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("jwt auth = %d: %s", w.Code, w.Body.String())
	}
	var authResp map[string]interface{}
	testutil.ParseJSON(t, w, &authResp)
	if authResp["user_id"] != userID || authResp["client"] != authpkg.ClientBrowser {
		t.Fatalf("jwt middleware did not continue with expected context: %#v", authResp)
	}

	second, err := svc.Refresh(t.Context(), first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken {
		t.Fatalf("refresh should rotate both tokens: first=%#v second=%#v", first, second)
	}
	if _, err := svc.Refresh(t.Context(), first.RefreshToken); err == nil {
		t.Fatal("old refresh token should be revoked after rotation")
	}
}

func TestAuthServiceRevokesRefreshToken(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))

	issued, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := svc.RevokeRefreshToken(t.Context(), issued.RefreshToken); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	if _, err := svc.Refresh(t.Context(), issued.RefreshToken); err == nil {
		t.Fatal("revoked refresh token was accepted")
	}

	var session storage.AuthSession
	if err := db.Where("session_id = ?", issued.SessionID).First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.RevokedAt == nil {
		t.Fatal("session revoked_at was not set")
	}
}

func TestAuthServiceRefreshTokenCanOnlyBeRotatedOnceConcurrently(t *testing.T) {
	db := testutil.NewTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))

	first, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	var ready sync.WaitGroup
	var start sync.WaitGroup
	start.Add(1)
	var successes int32
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		ready.Add(1)
		go func() {
			ready.Done()
			start.Wait()
			_, refreshErr := svc.Refresh(t.Context(), first.RefreshToken)
			if refreshErr == nil {
				atomic.AddInt32(&successes, 1)
			}
			errs <- refreshErr
		}()
	}
	ready.Wait()
	start.Done()
	for i := 0; i < 20; i++ {
		<-errs
	}

	if successes != 1 {
		t.Fatalf("concurrent refresh successes = %d, want exactly 1", successes)
	}
}

func TestAuthServiceIssuesContainerAccessTokenWithoutRefreshToken(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)

	credential, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential: %v", err)
	}
	if credential.CredentialID == "" || credential.Secret == "" || credential.Scope != authpkg.ScopeCompanion {
		t.Fatalf("incomplete container credential: %#v", credential)
	}

	issued, err := svc.IssueContainerToken(t.Context(), credential.CredentialID, credential.Secret)
	if err != nil {
		t.Fatalf("IssueContainerToken: %v", err)
	}
	if issued.AccessToken == "" || issued.RefreshToken != "" || issued.TokenType != "Bearer" {
		t.Fatalf("container token response = %#v, want access-only bearer token", issued)
	}

	r := gin.New()
	r.Use(authpkg.Middleware(db, authpkg.WithJWTSecret(secret)))
	r.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"user_id": authpkg.GetUserID(c.Request.Context()),
			"client":  authpkg.GetClientType(c.Request.Context()),
			"scope":   authpkg.GetScope(c.Request.Context()),
		})
	})
	w := testutil.DoRequest(r, http.MethodGet, "/api/v1/test", nil, map[string]string{
		"Authorization": "Bearer " + issued.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("container jwt auth = %d: %s", w.Code, w.Body.String())
	}
	var authResp map[string]interface{}
	testutil.ParseJSON(t, w, &authResp)
	if authResp["user_id"] != userID || authResp["client"] != authpkg.ClientContainer || authResp["scope"] != authpkg.ScopeCompanion {
		t.Fatalf("container middleware context = %#v", authResp)
	}

	if _, err := svc.Refresh(t.Context(), credential.Secret); err == nil {
		t.Fatal("container credential secret must not be accepted as a refresh token")
	}
}

func TestRevokedContainerCredentialCannotMintToken(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))

	credential, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential: %v", err)
	}
	if err := svc.RevokeContainerCredential(t.Context(), credential.CredentialID); err != nil {
		t.Fatalf("RevokeContainerCredential: %v", err)
	}

	if _, err := svc.IssueContainerToken(t.Context(), credential.CredentialID, credential.Secret); err == nil {
		t.Fatal("IssueContainerToken accepted a revoked credential")
	}
}

func TestCreatingContainerCredentialRevokesPreviousCredentialForUserScope(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))

	first, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential first: %v", err)
	}
	second, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential second: %v", err)
	}
	if first.CredentialID == second.CredentialID || first.Secret == second.Secret {
		t.Fatalf("replacement credential reused secret material: first=%#v second=%#v", first, second)
	}

	if _, err := svc.IssueContainerToken(t.Context(), first.CredentialID, first.Secret); err == nil {
		t.Fatal("old container credential still minted an access token after replacement")
	}
	if _, err := svc.IssueContainerToken(t.Context(), second.CredentialID, second.Secret); err != nil {
		t.Fatalf("replacement credential did not mint token: %v", err)
	}
}

func TestValidateContainerCredentialChecksUserAndScopeWithoutMintingSession(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))
	credential, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential: %v", err)
	}

	if err := svc.ValidateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion, credential.CredentialID, credential.Secret); err != nil {
		t.Fatalf("ValidateContainerCredential: %v", err)
	}
	if err := svc.ValidateContainerCredential(t.Context(), "other_user", authpkg.ScopeCompanion, credential.CredentialID, credential.Secret); err == nil {
		t.Fatal("ValidateContainerCredential accepted credential for wrong user")
	}
	if err := svc.ValidateContainerCredential(t.Context(), userID, authpkg.ScopeBrowser, credential.CredentialID, credential.Secret); err == nil {
		t.Fatal("ValidateContainerCredential accepted credential for wrong scope")
	}

	var sessions int64
	if err := db.Model(&storage.AuthSession{}).Where("credential_id = ?", credential.CredentialID).Count(&sessions).Error; err != nil {
		t.Fatalf("count auth sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("ValidateContainerCredential minted %d auth sessions, want 0", sessions)
	}
}

func TestRefreshDoesNotSlideSessionExpiry(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))

	first, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	var before storage.AuthSession
	if err := db.Where("session_id = ?", first.SessionID).First(&before).Error; err != nil {
		t.Fatalf("load session before refresh: %v", err)
	}
	if _, err := svc.Refresh(t.Context(), first.RefreshToken); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	var after storage.AuthSession
	if err := db.Where("session_id = ?", first.SessionID).First(&after).Error; err != nil {
		t.Fatalf("load session after refresh: %v", err)
	}
	if !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Fatalf("refresh slid expiry from %s to %s", before.ExpiresAt, after.ExpiresAt)
	}
	if after.LastUsedAt == nil {
		t.Fatal("refresh should record last_used_at")
	}
}

func TestRefreshAccessTokenDoesNotOutliveRefreshSession(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))
	first, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	sessionExpiresAt := time.Now().Add(30 * time.Second)
	if err := db.Model(&storage.AuthSession{}).
		Where("session_id = ?", first.SessionID).
		Update("expires_at", sessionExpiresAt).Error; err != nil {
		t.Fatalf("shorten session expiry: %v", err)
	}

	refreshed, err := svc.Refresh(t.Context(), first.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if time.Unix(refreshed.ExpiresAt, 0).After(sessionExpiresAt.Add(time.Second)) {
		t.Fatalf("access token expires at %s after session expiry %s", time.Unix(refreshed.ExpiresAt, 0), sessionExpiresAt)
	}
}

func TestMiddlewareRejectsExpiredJWTSession(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)
	issued, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := db.Model(&storage.AuthSession{}).
		Where("session_id = ?", issued.SessionID).
		Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}

	r := gin.New()
	r.Use(authpkg.Middleware(db, authpkg.WithJWTSecret(secret)))
	r.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	w := testutil.DoRequest(r, http.MethodGet, "/api/v1/test", nil, map[string]string{
		"Authorization": "Bearer " + issued.AccessToken,
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session auth = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestRequireScopeAndClientGateAuthenticatedRoutes(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)
	browser, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("issue browser token: %v", err)
	}
	container, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientContainer,
		Scope:      authpkg.ScopeCompanion,
	})
	if err != nil {
		t.Fatalf("issue container token: %v", err)
	}

	r := gin.New()
	r.Use(authpkg.Middleware(db, authpkg.WithJWTSecret(secret)))
	r.GET("/browser", authpkg.RequireScope(authpkg.ScopeBrowser), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"scope": authpkg.GetScope(c.Request.Context())})
	})
	r.POST("/companion", authpkg.RequireClient(authpkg.ClientContainer), authpkg.RequireScope(authpkg.ScopeCompanion), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"client": authpkg.GetClientType(c.Request.Context())})
	})

	w := testutil.DoRequest(r, http.MethodGet, "/browser", nil, map[string]string{
		"Authorization": "Bearer " + browser.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("browser route with browser token = %d: %s", w.Code, w.Body.String())
	}

	w = testutil.DoRequest(r, http.MethodPost, "/companion", nil, map[string]string{
		"Authorization": "Bearer " + browser.AccessToken,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("companion route with browser token = %d, want 403: %s", w.Code, w.Body.String())
	}

	w = testutil.DoRequest(r, http.MethodPost, "/companion", nil, map[string]string{
		"Authorization": "Bearer " + container.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("companion route with container token = %d: %s", w.Code, w.Body.String())
	}

	w = testutil.DoRequest(r, http.MethodGet, "/browser", nil, map[string]string{
		"Authorization": "Bearer " + container.AccessToken,
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("browser route with container token = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestAuthRefreshEndpointRotatesRefreshToken(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)

	first, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc))
	w := testutil.DoRequest(r, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": first.RefreshToken,
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh endpoint = %d: %s", w.Code, w.Body.String())
	}
	var refreshed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		SessionID    string `json:"session_id"`
	}
	testutil.ParseJSON(t, w, &refreshed)
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" || refreshed.SessionID != first.SessionID {
		t.Fatalf("incomplete refresh response: %#v", refreshed)
	}
	if refreshed.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	api := gin.New()
	api.Use(authpkg.Middleware(db, authpkg.WithJWTSecret(secret)))
	api.GET("/api/v1/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": authpkg.GetUserID(c.Request.Context())})
	})
	w = testutil.DoRequest(api, http.MethodGet, "/api/v1/test", nil, map[string]string{
		"Authorization": "Bearer " + refreshed.AccessToken,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("refreshed access token auth = %d: %s", w.Code, w.Body.String())
	}

	w = testutil.DoRequest(r, http.MethodPost, "/auth/refresh", map[string]string{
		"refresh_token": first.RefreshToken,
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh token reuse = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestAuthContainerTokenEndpointReturnsAccessTokenOnly(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)
	credential, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential: %v", err)
	}

	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc))
	w := testutil.DoRequest(r, http.MethodPost, "/auth/container-token", map[string]string{
		"credential_id": credential.CredentialID,
		"secret":        credential.Secret,
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("container-token endpoint = %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Client       string `json:"client"`
		Scope        string `json:"scope"`
	}
	testutil.ParseJSON(t, w, &resp)
	if resp.AccessToken == "" || resp.RefreshToken != "" || resp.TokenType != "Bearer" ||
		resp.Client != authpkg.ClientContainer || resp.Scope != authpkg.ScopeCompanion {
		t.Fatalf("container-token response = %#v", resp)
	}
}

func TestAuthContainerTokenEndpointRejectsInvalidCredential(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))
	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc))

	w := testutil.DoRequest(r, http.MethodPost, "/auth/container-token", map[string]string{
		"credential_id": "cc_missing",
		"secret":        "wrong-secret",
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("container-token invalid credential = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestAuthRefreshEndpointAcceptsHttpOnlyCookieForBrowserSessions(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)

	first, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc))
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "web3_refresh", Value: first.RefreshToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh endpoint = %d: %s", w.Code, w.Body.String())
	}
	var refreshed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		SessionID    string `json:"session_id"`
	}
	testutil.ParseJSON(t, w, &refreshed)
	if refreshed.AccessToken == "" || refreshed.RefreshToken != "" || refreshed.SessionID != first.SessionID {
		t.Fatalf("cookie refresh should return access token without JSON refresh token: %#v", refreshed)
	}
	var rotatedCookie *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "web3_refresh" {
			rotatedCookie = cookie
			break
		}
	}
	if rotatedCookie == nil || rotatedCookie.Value == "" || rotatedCookie.Value == first.RefreshToken || !rotatedCookie.HttpOnly {
		t.Fatalf("refresh cookie was not rotated securely: %#v", rotatedCookie)
	}
}

func TestAuthLogoutEndpointRevokesRefreshCookieAndClearsCookie(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)

	issued, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc))
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "web3_refresh", Value: issued.RefreshToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout endpoint = %d: %s", w.Code, w.Body.String())
	}

	if _, err := svc.Refresh(t.Context(), issued.RefreshToken); err == nil {
		t.Fatal("logout did not revoke refresh token")
	}
	var clearedCookie *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "web3_refresh" {
			clearedCookie = cookie
			break
		}
	}
	if clearedCookie == nil || clearedCookie.Value != "" || clearedCookie.MaxAge >= 0 || !clearedCookie.HttpOnly {
		t.Fatalf("logout did not clear refresh cookie securely: %#v", clearedCookie)
	}
}

func TestAuthLogoutEndpointRevokesCompanionCredentialAndStopsEnvironment(t *testing.T) {
	db := testutil.NewTestDB(t)
	userID, _ := testutil.CreateTestUser(t, db)
	secret := []byte(authpkg.TestJWTSecret)
	svc := authpkg.NewTokenService(db, secret)

	issued, err := svc.Issue(t.Context(), authpkg.SessionRequest{
		UserID:     userID,
		ClientType: authpkg.ClientBrowser,
		Scope:      authpkg.ScopeBrowser,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	credential, err := svc.CreateContainerCredential(t.Context(), userID, authpkg.ScopeCompanion)
	if err != nil {
		t.Fatalf("CreateContainerCredential: %v", err)
	}
	ticket := storage.WebSocketTicket{
		TicketHash:    "ws_ticket_logout_hash",
		UserID:        userID,
		AuthSessionID: issued.SessionID,
		ChatSessionID: "chat_logout",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(time.Minute),
	}
	if err := db.Create(&ticket).Error; err != nil {
		t.Fatalf("create websocket ticket: %v", err)
	}
	cleanup := &recordingCompanionCleanup{}

	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc), authpkg.WithCompanionEnvironment(cleanup))
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "web3_refresh", Value: issued.RefreshToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout endpoint = %d: %s", w.Code, w.Body.String())
	}

	if _, err := svc.IssueContainerToken(t.Context(), credential.CredentialID, credential.Secret); err == nil {
		t.Fatal("logout did not revoke companion container credential")
	}
	if cleanup.userID != userID || cleanup.calls != 1 {
		t.Fatalf("companion cleanup = user %q calls %d, want user %q once", cleanup.userID, cleanup.calls, userID)
	}
	var revokedTicket storage.WebSocketTicket
	if err := db.Where("ticket_hash = ?", ticket.TicketHash).First(&revokedTicket).Error; err != nil {
		t.Fatalf("load websocket ticket: %v", err)
	}
	if revokedTicket.RevokedAt == nil {
		t.Fatal("logout did not revoke outstanding websocket tickets")
	}
}

type recordingCompanionCleanup struct {
	userID string
	calls  int
}

func (r *recordingCompanionCleanup) StopUser(ctx context.Context, userID string) error {
	r.userID = userID
	r.calls++
	return nil
}

func TestAuthLogoutEndpointIsIdempotentWithoutRefreshToken(t *testing.T) {
	db := testutil.NewTestDB(t)
	svc := authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))
	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(svc))

	w := testutil.DoRequest(r, http.MethodPost, "/auth/logout", map[string]string{}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("logout without token = %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	testutil.ParseJSON(t, w, &resp)
	if resp["status"] != "logged_out" {
		t.Fatalf("logout response = %#v", resp)
	}
}
