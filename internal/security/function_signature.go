// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultSourcifySignatureBaseURL = "https://api.4byte.sourcify.dev"

type FunctionSignatureResolver interface {
	ResolveFunctions(ctx context.Context, selectors []string) (map[string]string, error)
}

type SourcifyFunctionSignatureResolver struct {
	baseURL string
	client  *http.Client
}

func NewSourcifyFunctionSignatureResolver(baseURL string, client *http.Client) *SourcifyFunctionSignatureResolver {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultSourcifySignatureBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &SourcifyFunctionSignatureResolver{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

func (r *SourcifyFunctionSignatureResolver) ResolveFunctions(ctx context.Context, selectors []string) (map[string]string, error) {
	normalized := normalizeFunctionSelectors(selectors)
	if len(normalized) == 0 {
		return nil, nil
	}
	reqURL, err := url.Parse(r.baseURL + "/signature-database/v1/lookup")
	if err != nil {
		return nil, fmt.Errorf("parse 4byte lookup url: %w", err)
	}
	query := reqURL.Query()
	query.Set("function", strings.Join(normalized, ","))
	reqURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create 4byte lookup request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("4byte lookup request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("4byte lookup status %d", resp.StatusCode)
	}
	var parsed struct {
		OK     bool `json:"ok"`
		Result struct {
			Function map[string][]struct {
				Name                string `json:"name"`
				Filtered            bool   `json:"filtered"`
				HasVerifiedContract bool   `json:"hasVerifiedContract"`
			} `json:"function"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse 4byte lookup response: %w", err)
	}
	out := make(map[string]string, len(parsed.Result.Function))
	for selector, matches := range parsed.Result.Function {
		if len(matches) == 0 {
			continue
		}
		if name := preferredFunctionSignature(matches); name != "" {
			out[strings.ToLower(selector)] = name
		}
	}
	return out, nil
}

func preferredFunctionSignature(matches []struct {
	Name                string `json:"name"`
	Filtered            bool   `json:"filtered"`
	HasVerifiedContract bool   `json:"hasVerifiedContract"`
}) string {
	for _, match := range matches {
		if strings.TrimSpace(match.Name) != "" && match.HasVerifiedContract && !match.Filtered {
			return strings.TrimSpace(match.Name)
		}
	}
	for _, match := range matches {
		if strings.TrimSpace(match.Name) != "" && !match.Filtered {
			return strings.TrimSpace(match.Name)
		}
	}
	for _, match := range matches {
		if strings.TrimSpace(match.Name) != "" {
			return strings.TrimSpace(match.Name)
		}
	}
	return ""
}

func normalizeFunctionSelectors(selectors []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		selector = strings.ToLower(strings.TrimSpace(selector))
		if len(selector) != 10 || !strings.HasPrefix(selector, "0x") || seen[selector] {
			continue
		}
		valid := true
		for _, ch := range selector[2:] {
			if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		seen[selector] = true
		out = append(out, selector)
	}
	return out
}
