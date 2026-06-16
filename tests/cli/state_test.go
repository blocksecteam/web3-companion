// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestStatePathUsesFixedLocalWeb3Path(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("WEB3_STATE_DIR", filepath.Join(t.TempDir(), "ignored"))

	want := filepath.Join(homeDir, ".local", "web3", "state.json")
	if got := cli.StatePath(); got != want {
		t.Fatalf("statePath() = %q, want %q", got, want)
	}
}

func TestLoadStateUsesContainerCredential(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	stateDir := filepath.Join(homeDir, ".local", "web3")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	rawCredential, _ := json.Marshal(cli.ContainerCredential{
		Version:      1,
		Client:       "container",
		CredentialID: "cc_live",
		Secret:       "container-secret",
		APIURL:       "http://localhost:8080",
	})
	if err := os.WriteFile(filepath.Join(stateDir, "container-credential.json"), rawCredential, 0o600); err != nil {
		t.Fatalf("write container credential: %v", err)
	}

	state, err := cli.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.AuthClient != "container" || state.Metadata["container_credential_id"] != "cc_live" {
		t.Fatalf("container credential state not imported: %#v", state)
	}
}

func TestLoadStateClearsRefreshTokenWhenContainerCredentialExists(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	if err := cli.SaveState(&cli.State{
		AccessToken:    "cached-access",
		RefreshToken:   "refresh-token",
		AuthClient:     "container",
		TokenExpiresAt: time.Now().Add(time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	stateDir := filepath.Join(homeDir, ".local", "web3")
	rawCredential, _ := json.Marshal(cli.ContainerCredential{
		Version:      1,
		Client:       "container",
		CredentialID: "cc_live",
		Secret:       "container-secret",
	})
	if err := os.WriteFile(filepath.Join(stateDir, "container-credential.json"), rawCredential, 0o600); err != nil {
		t.Fatalf("write container credential: %v", err)
	}

	state, err := cli.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.RefreshToken != "" {
		t.Fatalf("refresh token should be cleared in container credential mode: %#v", state)
	}
}
