// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package api

type RegisterResponse struct {
	Status    string `json:"status"`
	UserID    string `json:"user_id"`
	TempToken string `json:"temp_token,omitempty"`
}

type PasskeySetupResponse struct {
	Status      string      `json:"status"`
	ChallengeID string      `json:"challenge_id"`
	UserID      string      `json:"user_id"`
	PublicKey   interface{} `json:"public_key"`
}

type PasskeyVerifyResponse struct {
	Status       string `json:"status"`
	UserID       string `json:"user_id"`
	CredentialID string `json:"credential_id"`
	ResultStatus string `json:"result_status"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

type PasskeyResultResponse struct {
	Status       string `json:"status"`
	UserID       string `json:"user_id"`
	CredentialID string `json:"credential_id"`
	TokenPairResponse
}

type LoginOptionsResponse struct {
	Status      string      `json:"status"`
	ChallengeID string      `json:"challenge_id"`
	UserID      string      `json:"user_id"`
	PublicKey   interface{} `json:"public_key"`
}

type LoginVerifyResponse struct {
	Status       string `json:"status"`
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	CredentialID string `json:"credential_id"`
	TokenPairResponse
}

type TokenPairResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}
