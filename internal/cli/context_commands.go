// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"github.com/spf13/cobra"

	"strings"
	"time"
)

func ContextCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "context", Short: "Current user and wallet context"}

	cmd.AddCommand(

		&cobra.Command{Use: "status", Short: "Show current registration/onboarding status",
			Run: func(cmd *cobra.Command, args []string) {
				s, _ := LoadState()
				client := NewAPIClient()
				status := unavailableUserContext(s, "")
				if canQueryUserContext(s, client) {
					if data, err := client.Get("/users/context"); err == nil {
						if parsed, perr := ParseUserContext(data); perr == nil {
							status = parsed
							s.CheckedAt = time.Now()
							_ = SaveState(s)
						} else {
							status = unavailableUserContext(s, perr.Error())
						}
					} else {
						status = unavailableUserContext(s, err.Error())
					}
				}

				PrintJSONObject(map[string]interface{}{
					"email":            strOr(s.Email, ""),
					"auth":             status.Auth,
					"readiness":        status.Readiness,
					"blocking_reason":  status.BlockingReason,
					"frontend_actions": status.FrontendActions,
					"wallets":          status.Wallets,
				})
			}},
	)
	return cmd
}

func canQueryUserContext(s *State, c *APIClient) bool {
	if s != nil && (strings.TrimSpace(s.AccessToken) != "" || strings.TrimSpace(s.RefreshToken) != "") {
		return true
	}
	return c != nil && c.hasContainerCredential()
}

func strOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func AuthStatus(s *State) string {
	if s != nil && (strings.TrimSpace(s.AccessToken) != "" || strings.TrimSpace(s.RefreshToken) != "") {
		return "configured"
	}
	if s != nil && s.AuthClient == "container" {
		if s.Metadata != nil && strings.TrimSpace(s.Metadata["container_credential_id"]) != "" {
			return "configured (container)"
		}
	}
	return "not configured"
}
