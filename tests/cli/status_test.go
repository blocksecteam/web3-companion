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

func TestContextStatusDoesNotPrintUserID(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	stateDir := filepath.Join(homeDir, ".local", "web3")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := cli.State{
		Version: 1,
		Email:   "user@example.test",
		UserID:  "user_secret",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		cmd := cli.ContextCmd()
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute status: %v", err)
		}
	})

	if strings.Contains(output, "User ID") || strings.Contains(output, "user_secret") {
		t.Fatalf("context status leaked user id:\n%s", output)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("context status must output JSON, got %q: %v", output, err)
	}
	if parsed["email"] != "user@example.test" {
		t.Fatalf("email = %v, want user@example.test", parsed["email"])
	}
}

func TestContextStatusInitializesAPIClientWhenCommandBuiltDirectly(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("WEB3_API_URL", "http://127.0.0.1:1")
	stateDir := filepath.Join(homeDir, ".local", "web3")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := cli.State{
		Version:      1,
		Email:        "user@example.test",
		AccessToken:  "access.jwt",
		RefreshToken: "refresh-token",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		cmd := cli.ContextCmd()
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute status: %v", err)
		}
	})

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("context status must output JSON, got %q: %v", output, err)
	}
	if parsed["auth"] != "configured" {
		t.Fatalf("auth = %v, want configured", parsed["auth"])
	}
	if parsed["readiness"] != "status_unavailable" {
		t.Fatalf("readiness = %v, want status_unavailable", parsed["readiness"])
	}
}

func TestContextStatusUsesUserContextEndpoint(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/context" {
			t.Fatalf("context status should use /api/v1/users/context, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer access.jwt" {
			t.Fatalf("Authorization = %q, want Bearer access.jwt", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"auth":"container",
			"readiness":"pending_delegation",
			"blocking_reason":"delegation transaction is pending",
			"frontend_actions":["wait_for_delegation"],
			"wallets":[{"wallet_id":"main","address":"0xabc","chains":[{"chain":"ethereum","delegation_status":"pending_delegation","delegation_hash":"0xaaa"}]}]
		}`))
	}))
	t.Cleanup(server.Close)
	if err := cli.SaveState(&cli.State{
		Version:     1,
		Email:       "user@example.test",
		AccessToken: "access.jwt",
		AuthClient:  "container",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB3_API_URL", server.URL)

	output := captureStdout(t, func() {
		cmd := cli.ContextCmd()
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute status: %v", err)
		}
	})

	var parsed struct {
		Email          string `json:"email"`
		Auth           string `json:"auth"`
		Readiness      string `json:"readiness"`
		BlockingReason string `json:"blocking_reason"`
		Wallets        []struct {
			WalletID string `json:"wallet_id"`
			Address  string `json:"address"`
			Chains   []struct {
				Chain            string `json:"chain"`
				DelegationStatus string `json:"delegation_status"`
			} `json:"chains"`
		} `json:"wallets"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("context status must output JSON, got %q: %v", output, err)
	}
	if parsed.Email != "user@example.test" || parsed.Auth != "container" || parsed.Readiness != "pending_delegation" {
		t.Fatalf("unexpected auth status: %+v", parsed)
	}
	if parsed.BlockingReason != "delegation transaction is pending" {
		t.Fatalf("blocking_reason = %q", parsed.BlockingReason)
	}
	if len(parsed.Wallets) != 1 || parsed.Wallets[0].WalletID != "main" || len(parsed.Wallets[0].Chains) != 1 || parsed.Wallets[0].Chains[0].DelegationStatus != "pending_delegation" {
		t.Fatalf("unexpected wallets: %+v", parsed.Wallets)
	}
}

func TestPortfolioWalletLookupUsesUserContextEndpoint(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer access.jwt" {
			t.Fatalf("Authorization = %q, want Bearer access.jwt", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/users/context":
			_, _ = w.Write([]byte(`{
				"auth":"container",
				"readiness":"ready",
				"wallets":[{"wallet_id":"main","address":"0xabc","chains":[]}]
			}`))
		case "/api/v1/portfolio/base/0xabc":
			_, _ = w.Write([]byte(`{"total_usd":"0"}`))
		case "/api/v1/user/wallets":
			t.Fatal("portfolio --wallet must not call browser-only /api/v1/user/wallets")
		default:
			t.Fatalf("unexpected request path=%s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	if err := cli.SaveState(&cli.State{
		Version:     1,
		AccessToken: "access.jwt",
		AuthClient:  "container",
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEB3_API_URL", server.URL)

	output := captureStdout(t, func() {
		cmd := cli.PortfolioCmd()
		cmd.SetArgs([]string{"--wallet", "main", "--chain", "base"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute portfolio: %v", err)
		}
	})

	if strings.Contains(output, "error") {
		t.Fatalf("portfolio output contains error: %s", output)
	}
	if len(requests) != 2 || requests[0] != "/api/v1/users/context" || requests[1] != "/api/v1/portfolio/base/0xabc" {
		t.Fatalf("requests = %#v, want user context then portfolio", requests)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
