// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"encoding/json"
	"fmt"

	"net/url"
	"os"
	"strings"
	"time"
)

func WithChallengeID(body map[string]interface{}, challengeID string) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	if strings.TrimSpace(challengeID) != "" {
		out["challenge_id"] = strings.TrimSpace(challengeID)
	}
	return out
}

func AppendChallengeQuery(path, challengeID string) string {
	challengeID = strings.TrimSpace(challengeID)
	if challengeID == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "challenge_id=" + url.QueryEscape(challengeID)
}

type SignPollOptions struct {
	Wait         bool
	PollInterval time.Duration
	Timeout      time.Duration
}

func ResolveSignedResponse(signResp []byte, opts SignPollOptions) ([]byte, bool, error) {
	if responseHasSignedStatus(signResp) {
		return signResp, true, nil
	}
	challengeID := extractField(signResp, "challenge_id")
	if challengeID == "" || !opts.Wait {
		return signResp, false, nil
	}
	if signURL := extractField(signResp, "sign_url"); signURL != "" {
		PrintJSONObjectTo(os.Stderr, map[string]string{"status": "waiting_for_webauthn", "sign_url": signURL})
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return signResp, false, fmt.Errorf("timed out waiting for signing challenge %s", challengeID)
		}
		pollResp := mustGet("/ssm/sign/" + url.PathEscape(challengeID))
		if responseHasSignedStatus(pollResp) {
			return pollResp, true, nil
		}
		status := extractField(pollResp, "status")
		if status != "" && status != "pending" && status != "pending_user_auth" && status != "signing" {
			return pollResp, false, fmt.Errorf("signing challenge %s ended with status %s", challengeID, status)
		}
		time.Sleep(interval)
	}
}

func responseHasSignedStatus(data []byte) bool {
	return strings.TrimSpace(extractField(data, "status")) == "signed" &&
		strings.TrimSpace(extractField(data, "transaction_id")) != "" &&
		strings.TrimSpace(extractField(data, "tx_hash")) != ""
}

func BuildSecurityCheckParams(transactionID string) map[string]interface{} {
	return map[string]interface{}{
		"transaction_id": transactionID,
	}
}

func BuildSecurityOverrideParams(transactionID, reason string, challengeID ...string) map[string]interface{} {
	params := map[string]interface{}{
		"transaction_id": strings.TrimSpace(transactionID),
		"reason":         strings.TrimSpace(reason),
	}
	if len(challengeID) > 0 && strings.TrimSpace(challengeID[0]) != "" {
		params["challenge_id"] = strings.TrimSpace(challengeID[0])
	}
	return params
}

// StripSimulationTrace removes simulation.trace from a security check JSON response
// to keep CLI output compact. Other simulation fields are preserved.
func StripSimulationTrace(data []byte) []byte {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return data
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return data
	}
	if sim, ok := obj["simulation"].(map[string]interface{}); ok {
		delete(sim, "trace")
	}
	result, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return data
	}
	return result
}

// ExtractSimulationTrace returns only the simulation.trace field from a security
// check JSON response, wrapped as {"trace": ...}.
func ExtractSimulationTrace(data []byte) []byte {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return data
	}
	obj, ok := v.(map[string]interface{})
	if !ok {
		return data
	}
	var trace interface{}
	if sim, ok := obj["simulation"].(map[string]interface{}); ok {
		trace = sim["trace"]
	}
	result, err := json.MarshalIndent(map[string]interface{}{"trace": trace}, "", "  ")
	if err != nil {
		return data
	}
	return result
}
