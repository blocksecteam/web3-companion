// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/blocksecteam/web3-companion/internal/cli"
)

func TestAppendChallengeQuery(t *testing.T) {
	got := cli.AppendChallengeQuery("/ssm/sign", "ch_abc+123")
	want := "/ssm/sign?challenge_id=ch_abc%2B123"
	if got != want {
		t.Fatalf("appendChallengeQuery() = %q, want %q", got, want)
	}
}

func TestWithChallengeID(t *testing.T) {
	body := map[string]interface{}{"wallet_id": "main"}
	got := cli.WithChallengeID(body, "ch_123")
	want := map[string]interface{}{"wallet_id": "main", "challenge_id": "ch_123"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withChallengeID() = %#v, want %#v", got, want)
	}
	if _, ok := body["challenge_id"]; ok {
		t.Fatalf("withChallengeID mutated original body: %#v", body)
	}
}

func TestSSMCmdHasSignStatus(t *testing.T) {
	cmd := cli.SSMCmd()
	if _, _, err := cmd.Find([]string{"sign-status", "ch_123"}); err != nil {
		t.Fatalf("missing ssm sign-status command: %v", err)
	}
}

func TestSSMCmdHasDelegate(t *testing.T) {
	cmd := cli.SSMCmd()
	sub, _, err := cmd.Find([]string{"delegate"})
	if err != nil {
		t.Fatalf("missing ssm delegate command: %v", err)
	}
	for _, flag := range []string{"wallet", "chain", "challenge-id"} {
		if sub.Flags().Lookup(flag) == nil {
			t.Fatalf("ssm delegate missing --%s flag", flag)
		}
	}
	// Wallet + chain must be marked required so cobra errors at parse time
	// rather than sending an empty JSON body to the backend. Flag naming
	// matches every other write command (portfolio/swap/bridge/chain
	// transfer all use --wallet) so agents don't have to guess.
	for _, flag := range []string{"wallet", "chain"} {
		if !contains(sub.Flag(flag).Annotations[cobra.BashCompOneRequiredFlag], "true") {
			t.Fatalf("ssm delegate --%s should be required", flag)
		}
	}
	challengeUsage := strings.ToLower(sub.Flags().Lookup("challenge-id").Usage)
	if challengeUsage == "" || !strings.Contains(challengeUsage, "approved") || !strings.Contains(challengeUsage, "resume") {
		t.Fatalf("ssm delegate --challenge-id help should explain approved challenge resume, got %q", sub.Flags().Lookup("challenge-id").Usage)
	}
}

func TestSSMCmdHasDelegateEstimate(t *testing.T) {
	cmd := cli.SSMCmd()
	sub, _, err := cmd.Find([]string{"delegate-estimate"})
	if err != nil {
		t.Fatalf("missing ssm delegate-estimate command: %v", err)
	}
	for _, flag := range []string{"wallet", "chain"} {
		if sub.Flags().Lookup(flag) == nil {
			t.Fatalf("ssm delegate-estimate missing --%s flag", flag)
		}
		if !contains(sub.Flag(flag).Annotations[cobra.BashCompOneRequiredFlag], "true") {
			t.Fatalf("ssm delegate-estimate --%s should be required", flag)
		}
	}
}

func contains(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

func TestSSMSignCmdHasWaitFlag(t *testing.T) {
	cmd := cli.SSMCmd()
	sub, _, err := cmd.Find([]string{"sign"})
	if err != nil {
		t.Fatalf("missing ssm sign command: %v", err)
	}
	if sub.Flags().Lookup("wait") == nil {
		t.Fatal("ssm sign missing --wait flag")
	}
	if sub.Flags().Lookup("poll-interval") == nil {
		t.Fatal("ssm sign missing --poll-interval flag")
	}
	if sub.Flags().Lookup("timeout") == nil {
		t.Fatal("ssm sign missing --timeout flag")
	}
	if sub.Flags().Lookup("challenge-id") == nil {
		t.Fatal("ssm sign missing --challenge-id flag")
	}
}

func TestResolveSignedResponseReturnsChallengeForManualApproval(t *testing.T) {
	raw := []byte(`{"status":"pending","challenge_id":"ch_manual","sign_url":"https://example.test/confirm/ch_manual"}`)
	resolved, done, err := cli.ResolveSignedResponse(raw, cli.SignPollOptions{})
	if err != nil {
		t.Fatalf("resolve response: %v", err)
	}
	if done {
		t.Fatalf("manual challenge should not be treated as signed: %s", resolved)
	}
	if string(resolved) != string(raw) {
		t.Fatalf("manual challenge response changed: %s", resolved)
	}
}
