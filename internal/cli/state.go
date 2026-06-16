// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type State struct {
	Version        int               `json:"version"`
	Email          string            `json:"email,omitempty"`
	UserID         string            `json:"user_id,omitempty"`
	SessionID      string            `json:"session_id,omitempty"`
	AccessToken    string            `json:"access_token,omitempty"`
	RefreshToken   string            `json:"refresh_token,omitempty"`
	AuthClient     string            `json:"auth_client,omitempty"`
	TokenExpiresAt int64             `json:"token_expires_at,omitempty"`
	TempToken      string            `json:"temp_token,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	UpdatedAt      time.Time         `json:"updated_at"`
	CheckedAt      time.Time         `json:"checked_at"`
}

func statePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "web3", "state.json")
}

func StatePath() string {
	return statePath()
}

func containerCredentialPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "web3", "container-credential.json")
}

type ContainerCredential struct {
	Version      int    `json:"version"`
	Client       string `json:"client"`
	CredentialID string `json:"credential_id"`
	Secret       string `json:"secret"`
	Scope        string `json:"scope,omitempty"`
	APIURL       string `json:"api_url,omitempty"`
	Email        string `json:"email,omitempty"`
}

func LoadState() (*State, error) {
	path := statePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return importContainerCredential(&State{Version: 1})
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if s.Metadata == nil {
		s.Metadata = make(map[string]string)
	}
	return importContainerCredential(&s)
}

func SaveState(s *State) error {
	s.UpdatedAt = time.Now()
	s.Version = 1
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func importContainerCredential(s *State) (*State, error) {
	if credential, err := LoadContainerCredential(); err == nil && credential != nil {
		s.AuthClient = "container"
		s.RefreshToken = ""
		if s.Metadata == nil {
			s.Metadata = make(map[string]string)
		}
		s.Metadata["container_credential_id"] = credential.CredentialID
		if credential.APIURL != "" {
			s.Metadata["api_url"] = strings.TrimSpace(credential.APIURL)
		}
		if credential.Email != "" {
			s.Email = strings.TrimSpace(credential.Email)
		}
		if err := SaveState(s); err != nil {
			return nil, fmt.Errorf("save container credential state: %w", err)
		}
		return s, nil
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func LoadContainerCredential() (*ContainerCredential, error) {
	data, err := os.ReadFile(containerCredentialPath())
	if err != nil {
		return nil, err
	}
	var credential ContainerCredential
	if err := json.Unmarshal(data, &credential); err != nil {
		return nil, fmt.Errorf("parse container credential: %w", err)
	}
	credential.CredentialID = strings.TrimSpace(credential.CredentialID)
	credential.Secret = strings.TrimSpace(credential.Secret)
	credential.Client = strings.TrimSpace(credential.Client)
	credential.Scope = strings.TrimSpace(credential.Scope)
	credential.APIURL = strings.TrimSpace(credential.APIURL)
	credential.Email = strings.TrimSpace(credential.Email)
	if credential.CredentialID == "" || credential.Secret == "" {
		return nil, fmt.Errorf("container credential missing credential_id or secret")
	}
	if credential.Client == "" {
		credential.Client = "container"
	}
	return &credential, nil
}

const ReadinessUnavailable = "status_unavailable"

type UserContext struct {
	Auth            string              `json:"auth"`
	Readiness       string              `json:"readiness"`
	BlockingReason  string              `json:"blocking_reason,omitempty"`
	FrontendActions []string            `json:"frontend_actions,omitempty"`
	Wallets         []UserContextWallet `json:"wallets,omitempty"`
}

type ChainDelegation struct {
	Chain            string `json:"chain"`
	DelegationStatus string `json:"delegation_status"`
	DelegationHash   string `json:"delegation_hash,omitempty"`
}

type UserContextWallet struct {
	WalletID string            `json:"wallet_id"`
	Address  string            `json:"address"`
	Chains   []ChainDelegation `json:"chains"`
}

func ParseUserContext(data []byte) (UserContext, error) {
	var status UserContext
	if err := json.Unmarshal(data, &status); err != nil {
		return UserContext{}, err
	}
	if status.Auth == "" {
		status.Auth = "unknown"
	}
	if status.Readiness == "" {
		status.Readiness = ReadinessUnavailable
	}
	if status.Wallets == nil {
		status.Wallets = []UserContextWallet{}
	}
	return status, nil
}

func unavailableUserContext(s *State, reason string) UserContext {
	return UserContext{
		Auth:           AuthStatus(s),
		Readiness:      ReadinessUnavailable,
		BlockingReason: strings.TrimSpace(reason),
		Wallets:        []UserContextWallet{},
	}
}
