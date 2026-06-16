// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/blocksecteam/web3-companion/internal/config"
)

type APIClient struct {
	BaseURL             string
	AccessToken         string
	RefreshToken        string
	SessionID           string
	TokenExpiresAt      int64
	ContainerCredential *ContainerCredential
	HTTP                *http.Client
}

func NewAPIClient() *APIClient {
	accessToken, refreshToken, sessionID := "", "", ""
	var expiresAt int64
	if state, err := LoadState(); err == nil {
		accessToken = strings.TrimSpace(state.AccessToken)
		refreshToken = strings.TrimSpace(state.RefreshToken)
		sessionID = strings.TrimSpace(state.SessionID)
		expiresAt = state.TokenExpiresAt
	}
	containerCredential, _ := LoadContainerCredential()
	c := NewPublicAPIClient()
	c.AccessToken = accessToken
	c.RefreshToken = refreshToken
	c.SessionID = sessionID
	c.TokenExpiresAt = expiresAt
	c.ContainerCredential = containerCredential
	if containerCredential != nil {
		c.RefreshToken = ""
		if !hasConfiguredAPIURL() && strings.TrimSpace(containerCredential.APIURL) != "" {
			c.BaseURL = strings.TrimSpace(containerCredential.APIURL)
		}
	}
	return c
}

func NewPublicAPIClient() *APIClient {
	return &APIClient{
		BaseURL: configuredBaseURL(),
		HTTP:    &http.Client{Timeout: 120 * time.Second},
	}
}

func configuredBaseURL() string {
	cfg, err := config.Load("")
	if err == nil {
		if strings.TrimSpace(cfg.CLI.APIURL) != "" {
			return strings.TrimSpace(cfg.CLI.APIURL)
		}
		if strings.TrimSpace(cfg.Server.ExternalURL) != "" {
			return strings.TrimSpace(cfg.Server.ExternalURL)
		}
	}
	return "http://localhost:8080"
}

func hasConfiguredAPIURL() bool {
	return strings.TrimSpace(os.Getenv("WEB3_API_URL")) != ""
}

func (c *APIClient) Get(path string) ([]byte, error) {
	return c.do("GET", path, nil)
}

func (c *APIClient) Post(path string, body interface{}) ([]byte, error) {
	return c.do("POST", path, body)
}

func (c *APIClient) Put(path string, body interface{}) ([]byte, error) {
	return c.do("PUT", path, body)
}

func (c *APIClient) Delete(path string) ([]byte, error) {
	return c.do("DELETE", path, nil)
}

func (c *APIClient) RawPost(path string, body interface{}) ([]byte, error) {
	return c.doRaw("POST", path, body)
}

func (c *APIClient) do(method, path string, body interface{}) ([]byte, error) {
	return c.doRaw(method, "/api/v1"+path, body)
}

func (c *APIClient) doRaw(method, path string, body interface{}) ([]byte, error) {
	return c.doRawAttempt(method, path, body, true)
}

func (c *APIClient) doRawAttempt(method, path string, body interface{}, allowRefresh bool) ([]byte, error) {
	path = sanitizePath(path)
	if path == "" {
		return nil, fmt.Errorf("invalid path")
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.AccessToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AccessToken))
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		if allowRefresh && resp.StatusCode == http.StatusUnauthorized && c.canRefresh(path) {
			if err := c.refreshAccessToken(); err != nil {
				return data, fmt.Errorf("API error (status %d): %s; refresh failed: %w", resp.StatusCode, string(data), err)
			}
			return c.doRawAttempt(method, path, body, false)
		}
		if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			return data, fmt.Errorf("API error (status %d): received HTML response from %s; check WEB3_API_URL points to the web3 API server", resp.StatusCode, c.BaseURL)
		}
		return data, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *APIClient) canRefresh(path string) bool {
	if c.hasContainerCredential() {
		return !isBrowserOnlyPath(path)
	}
	return strings.TrimSpace(c.RefreshToken) != ""
}

func isBrowserOnlyPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/api/v1/user/")
}

func (c *APIClient) hasContainerCredential() bool {
	return c.ContainerCredential != nil &&
		strings.TrimSpace(c.ContainerCredential.CredentialID) != "" &&
		strings.TrimSpace(c.ContainerCredential.Secret) != ""
}

func RequireContainerClient(client *APIClient) error {
	if client == nil {
		client = NewAPIClient()
	}
	if client.hasContainerCredential() {
		return nil
	}
	return fmt.Errorf("web3 CLI write commands are companion-internal and require a container credential")
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	SessionID    string `json:"session_id"`
}

func (c *APIClient) refreshAccessToken() error {
	if c.hasContainerCredential() {
		return c.mintContainerAccessToken()
	}
	refreshToken := strings.TrimSpace(c.RefreshToken)
	if refreshToken == "" {
		return fmt.Errorf("refresh token unavailable")
	}
	body, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	if err != nil {
		return fmt.Errorf("marshal refresh request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("refresh failed (status %d): %s", resp.StatusCode, string(data))
	}
	var refreshed refreshResponse
	if err := json.Unmarshal(data, &refreshed); err != nil {
		return fmt.Errorf("parse refresh response: %w", err)
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" || strings.TrimSpace(refreshed.RefreshToken) == "" {
		return fmt.Errorf("refresh response missing token pair")
	}
	c.AccessToken = strings.TrimSpace(refreshed.AccessToken)
	c.RefreshToken = strings.TrimSpace(refreshed.RefreshToken)
	c.SessionID = strings.TrimSpace(refreshed.SessionID)
	c.TokenExpiresAt = refreshed.ExpiresAt
	if state, err := LoadState(); err == nil {
		state.AccessToken = c.AccessToken
		state.RefreshToken = c.RefreshToken
		state.SessionID = c.SessionID
		state.TokenExpiresAt = c.TokenExpiresAt
		if state.AuthClient == "" {
			state.AuthClient = "cli"
		}
		_ = SaveState(state)
	}
	return nil
}

type containerTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresAt   int64  `json:"expires_at"`
	SessionID   string `json:"session_id"`
	Client      string `json:"client"`
	Scope       string `json:"scope"`
}

func (c *APIClient) mintContainerAccessToken() error {
	credential := c.ContainerCredential
	if credential == nil {
		return fmt.Errorf("container credential unavailable")
	}
	body, err := json.Marshal(map[string]string{
		"credential_id": strings.TrimSpace(credential.CredentialID),
		"secret":        strings.TrimSpace(credential.Secret),
	})
	if err != nil {
		return fmt.Errorf("marshal container token request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/auth/container-token", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create container token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("container token request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read container token response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("container token failed (status %d): %s", resp.StatusCode, string(data))
	}
	var minted containerTokenResponse
	if err := json.Unmarshal(data, &minted); err != nil {
		return fmt.Errorf("parse container token response: %w", err)
	}
	if strings.TrimSpace(minted.AccessToken) == "" {
		return fmt.Errorf("container token response missing access_token")
	}
	c.AccessToken = strings.TrimSpace(minted.AccessToken)
	c.RefreshToken = ""
	c.SessionID = strings.TrimSpace(minted.SessionID)
	c.TokenExpiresAt = minted.ExpiresAt
	if state, err := LoadState(); err == nil {
		state.AccessToken = c.AccessToken
		state.RefreshToken = ""
		state.SessionID = c.SessionID
		state.TokenExpiresAt = c.TokenExpiresAt
		state.AuthClient = "container"
		if state.Metadata == nil {
			state.Metadata = make(map[string]string)
		}
		state.Metadata["container_credential_id"] = strings.TrimSpace(credential.CredentialID)
		if credential.APIURL != "" {
			state.Metadata["api_url"] = strings.TrimSpace(credential.APIURL)
		}
		_ = SaveState(state)
	}
	return nil
}

func sanitizePath(path string) string {
	for _, c := range path {
		if c == '\n' || c == '\r' || c == ' ' {
			return ""
		}
	}
	if strings.Contains(path, "..") {
		return ""
	}
	if path != "/" && strings.HasSuffix(path, "/") {
		return strings.TrimRight(path, "/")
	}
	return path
}

func PrintJSON(data []byte) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		PrintJSONObject(map[string]string{"output": string(data)})
		return
	}
	PrintJSONObject(v)
}

func PrintJSONObject(v interface{}) {
	PrintJSONObjectTo(os.Stdout, v)
}

func PrintJSONObjectTo(w io.Writer, v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(w, string(b))
}

func PrintErrorJSON(err error, data []byte) {
	body := strings.TrimSpace(string(data))
	payload := map[string]interface{}{
		"error": map[string]interface{}{
			"message": err.Error(),
		},
	}
	if body != "" {
		var parsed interface{}
		if json.Unmarshal([]byte(body), &parsed) == nil {
			payload["response"] = parsed
		} else {
			payload["response"] = body
		}
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Fprintln(os.Stderr, string(b))
}
