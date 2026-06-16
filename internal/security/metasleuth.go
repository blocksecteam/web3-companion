// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

const defaultMetaSleuthURL = "https://aml.blocksec.com/address-label/api/v3"

type MetaSleuthLabelProvider struct {
	baseURL    string
	apiKey     string
	apiKeyFunc func() string
	client     *http.Client
}

func NewMetaSleuthLabelProvider(baseURL, apiKey string, client *http.Client) *MetaSleuthLabelProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultMetaSleuthURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &MetaSleuthLabelProvider{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		client:  client,
	}
}

func NewMetaSleuthLabelProviderWithKeyFunc(baseURL string, keyFunc func() string, client *http.Client) *MetaSleuthLabelProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultMetaSleuthURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &MetaSleuthLabelProvider{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKeyFunc: keyFunc,
		client:     client,
	}
}

func (p *MetaSleuthLabelProvider) Labels(ctx context.Context, chainID int, addresses []string) ([]AddressLabel, error) {
	addresses = normalizeAddressList(addresses)
	if len(addresses) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]interface{}{
		"chain_id":  chainID,
		"addresses": addresses,
	})
	if err != nil {
		return nil, fmt.Errorf("metasleuth encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/batch-labels", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("metasleuth create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	key := p.apiKey
	if p.apiKeyFunc != nil {
		key = p.apiKeyFunc()
	}
	if key != "" {
		req.Header.Set("API-KEY", key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metasleuth request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("metasleuth read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("metasleuth status %d: %s", resp.StatusCode, string(body))
	}
	var parsed metaSleuthEnvelope
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("metasleuth parse response: %w", err)
	}
	if parsed.Code != 200000 {
		return nil, fmt.Errorf("metasleuth error %d: %s", parsed.Code, parsed.Message)
	}
	out := make([]AddressLabel, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		out = append(out, item.toAddressLabel())
	}
	return out, nil
}

func normalizeAddressList(addresses []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		normalized := strings.ToLower(strings.TrimSpace(addr))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

type metaSleuthEnvelope struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    []metaSleuthAddressRaw `json:"data"`
}

type metaSleuthAddressRaw struct {
	Address        string              `json:"address"`
	ChainID        int                 `json:"chain_id"`
	NameTag        string              `json:"name_tag"`
	MainEntity     string              `json:"main_entity"`
	CompEntity     string              `json:"comp_entity"`
	Attributes     []LabelValue        `json:"attributes"`
	MainEntityInfo metaSleuthEntityRaw `json:"main_entity_info"`
	CompEntityInfo metaSleuthEntityRaw `json:"comp_entity_info"`
}

type metaSleuthEntityRaw struct {
	Entity     string       `json:"entity"`
	Categories []LabelValue `json:"categories"`
	Attributes []LabelValue `json:"attributes"`
}

func (r metaSleuthAddressRaw) toAddressLabel() AddressLabel {
	label := AddressLabel{
		Address:    r.Address,
		ChainID:    r.ChainID,
		NameTag:    r.NameTag,
		MainEntity: primitives.FirstString(r.MainEntity, r.MainEntityInfo.Entity),
		CompEntity: primitives.FirstString(r.CompEntity, r.CompEntityInfo.Entity),
	}
	label.Categories = append(label.Categories, r.MainEntityInfo.Categories...)
	label.Categories = append(label.Categories, r.CompEntityInfo.Categories...)
	label.Attributes = append(label.Attributes, r.Attributes...)
	label.Attributes = append(label.Attributes, r.MainEntityInfo.Attributes...)
	label.Attributes = append(label.Attributes, r.CompEntityInfo.Attributes...)
	label.RiskReasons = HighRiskLabelReasons(label)
	if len(label.RiskReasons) > 0 {
		label.RiskLevel = "high"
	}
	return label
}
