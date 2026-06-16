// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestClientReportsHTML404AsWrongAPIURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html><title>404</title></html>"))
	}))
	t.Cleanup(server.Close)

	c := &cli.APIClient{BaseURL: server.URL, HTTP: server.Client()}
	_, err := c.Get("/chain/supported")
	if err == nil {
		t.Fatal("Get returned nil error")
	}
	for _, want := range []string{"received HTML response", "check WEB3_API_URL"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestNewPublicUsesConfigExternalURLWhenEnvMissing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[server]
external_url = "http://localhost:8081"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB3_API_URL", "")
	t.Setenv("WEB3_CONFIG_PATH", configPath)

	c := cli.NewPublicAPIClient()
	if c.BaseURL != "http://localhost:8081" {
		t.Fatalf("BaseURL = %q, want config external_url", c.BaseURL)
	}
}

func TestClientUsesAccessToken(t *testing.T) {
	c := &cli.APIClient{
		BaseURL: "http://example.test",
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Authorization"); got != "Bearer access.jwt" {
				t.Fatalf("Authorization = %q, want Bearer access.jwt", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		})},
		AccessToken: "access.jwt",
	}

	if _, err := c.Get("/chain/supported"); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestClientRefreshesAccessTokenOnUnauthorizedAndPersistsState(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := cli.SaveState(&cli.State{
		AccessToken:    "expired.jwt",
		RefreshToken:   "refresh-old",
		SessionID:      "sess_old",
		TokenExpiresAt: 1,
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.URL.Path == "/api/v1/users/context" && r.Header.Get("Authorization") == "Bearer expired.jwt":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"invalid_token"}}`))
		case r.URL.Path == "/auth/refresh":
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("refresh request must not send expired authorization header")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"access_token":"fresh.jwt",
				"refresh_token":"refresh-new",
				"token_type":"Bearer",
				"expires_at":1893456000,
				"session_id":"sess_old"
			}`))
		case r.URL.Path == "/api/v1/users/context" && r.Header.Get("Authorization") == "Bearer fresh.jwt":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"wallets":[]}`))
		default:
			t.Fatalf("unexpected request path=%s auth=%s", r.URL.Path, r.Header.Get("Authorization"))
		}
	}))
	t.Cleanup(server.Close)

	c := cli.NewAPIClient()
	c.BaseURL = server.URL
	c.HTTP = server.Client()
	data, err := c.Get("/users/context")
	if err != nil {
		t.Fatalf("Get after refresh: %v\n%s", err, string(data))
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if string(data) != `{"wallets":[]}` {
		t.Fatalf("data = %s", string(data))
	}
	state, err := cli.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.AccessToken != "fresh.jwt" || state.RefreshToken != "refresh-new" || state.TokenExpiresAt != 1893456000 {
		t.Fatalf("state not refreshed: %#v", state)
	}
}

func TestClientUsesContainerCredentialInsteadOfRefreshToken(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stateDir := filepath.Join(homeDir, ".local", "web3")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	rawCredential, _ := json.Marshal(cli.ContainerCredential{
		Version:      1,
		Client:       "container",
		CredentialID: "cc_test",
		Secret:       "container-secret",
	})
	if err := os.WriteFile(filepath.Join(stateDir, "container-credential.json"), rawCredential, 0o600); err != nil {
		t.Fatalf("write container credential: %v", err)
	}
	if err := cli.SaveState(&cli.State{
		AccessToken:    "expired.jwt",
		RefreshToken:   "expired-refresh-token",
		SessionID:      "sess_old",
		TokenExpiresAt: 1,
		AuthClient:     "container",
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch {
		case r.URL.Path == "/api/v1/portfolio/base/0xabc" && r.Header.Get("Authorization") == "Bearer expired.jwt":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"invalid_token"}}`))
		case r.URL.Path == "/auth/container-token":
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("container-token request must not send expired authorization header")
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode container-token request: %v", err)
			}
			if body["credential_id"] != "cc_test" || body["secret"] != "container-secret" {
				t.Fatalf("container-token body = %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"access_token":"container-fresh.jwt",
				"token_type":"Bearer",
				"expires_at":1893456000,
				"client":"container",
				"scope":"companion"
			}`))
		case r.URL.Path == "/api/v1/portfolio/base/0xabc" && r.Header.Get("Authorization") == "Bearer container-fresh.jwt":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total_usd":"0"}`))
		case r.URL.Path == "/auth/refresh":
			t.Fatal("container CLI must not call /auth/refresh")
		default:
			t.Fatalf("unexpected request path=%s auth=%s", r.URL.Path, r.Header.Get("Authorization"))
		}
	}))
	t.Cleanup(server.Close)

	c := cli.NewAPIClient()
	c.BaseURL = server.URL
	c.HTTP = server.Client()
	data, err := c.Get("/portfolio/base/0xabc")
	if err != nil {
		t.Fatalf("Get after container token mint: %v\n%s", err, string(data))
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	state, err := cli.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.AccessToken != "container-fresh.jwt" || state.RefreshToken != "" || state.TokenExpiresAt != 1893456000 {
		t.Fatalf("state after container token mint = %#v", state)
	}
}

func TestClientDoesNotMintContainerTokenForBrowserOnlyRoutes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stateDir := filepath.Join(homeDir, ".local", "web3")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	rawCredential, _ := json.Marshal(cli.ContainerCredential{
		Version:      1,
		Client:       "container",
		CredentialID: "cc_test",
		Secret:       "container-secret",
	})
	if err := os.WriteFile(filepath.Join(stateDir, "container-credential.json"), rawCredential, 0o600); err != nil {
		t.Fatalf("write container credential: %v", err)
	}
	if err := cli.SaveState(&cli.State{
		AccessToken:    "expired.jwt",
		RefreshToken:   "expired-refresh-token",
		SessionID:      "sess_old",
		TokenExpiresAt: 1,
		AuthClient:     "container",
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/api/v1/user/wallets":
			if r.Header.Get("Authorization") != "Bearer expired.jwt" {
				t.Fatalf("Authorization = %q, want expired token", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"invalid_token"}}`))
		case "/auth/container-token":
			t.Fatal("browser-only route must not mint a container token after 401")
		default:
			t.Fatalf("unexpected request path=%s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	c := cli.NewAPIClient()
	c.BaseURL = server.URL
	c.HTTP = server.Client()
	data, err := c.Get("/user/wallets")
	if err == nil {
		t.Fatalf("Get returned nil error with data %s", string(data))
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if !strings.Contains(err.Error(), "invalid_token") {
		t.Fatalf("error = %q, want invalid_token response", err)
	}
}

func TestNewAPIClientPrefersConfiguredURLOverStaleContainerCredentialURL(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("WEB3_API_URL", "http://127.0.0.1:8080")
	stateDir := filepath.Join(homeDir, ".local", "web3")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	rawCredential, _ := json.Marshal(cli.ContainerCredential{
		Version:      1,
		Client:       "container",
		CredentialID: "cc_test",
		Secret:       "container-secret",
		APIURL:       "http://127.0.0.1:8083",
	})
	if err := os.WriteFile(filepath.Join(stateDir, "container-credential.json"), rawCredential, 0o600); err != nil {
		t.Fatalf("write container credential: %v", err)
	}

	c := cli.NewAPIClient()

	if c.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("BaseURL = %q, want configured container-local URL", c.BaseURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
