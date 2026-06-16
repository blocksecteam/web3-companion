// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

type AddressLabelProvider interface {
	Labels(ctx context.Context, chainID int, addresses []string) ([]AddressLabel, error)
}

type AddressLabel struct {
	Address      string       `json:"address"`
	ChainID      int          `json:"chain_id"`
	NameTag      string       `json:"name_tag,omitempty"`
	MainEntity   string       `json:"main_entity,omitempty"`
	CompEntity   string       `json:"comp_entity,omitempty"`
	Categories   []LabelValue `json:"categories,omitempty"`
	Attributes   []LabelValue `json:"attributes,omitempty"`
	RiskLevel    string       `json:"risk_level,omitempty"`
	RiskReasons  []string     `json:"risk_reasons,omitempty"`
	ProviderData interface{}  `json:"provider_data,omitempty"`
}

type LabelValue struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

type noopAddressLabelProvider struct{}

func (noopAddressLabelProvider) Labels(ctx context.Context, chainID int, addresses []string) ([]AddressLabel, error) {
	return nil, nil
}

var ErrLabelCheckDisabled = fmt.Errorf("address label check disabled by user")
var ErrLabelPaymentFailed = fmt.Errorf("address label payment failed")

type DynamicLabelProvider struct {
	resolve func() AddressLabelProvider
}

func NewDynamicLabelProvider(resolve func() AddressLabelProvider) *DynamicLabelProvider {
	return &DynamicLabelProvider{resolve: resolve}
}

func (d *DynamicLabelProvider) Labels(ctx context.Context, chainID int, addresses []string) ([]AddressLabel, error) {
	if d == nil || d.resolve == nil {
		return nil, ErrLabelCheckDisabled
	}
	provider := d.resolve()
	if provider == nil {
		slog.Info("dynamic label provider resolved to nil (disabled)")
		return nil, ErrLabelCheckDisabled
	}
	slog.Info("dynamic label provider resolved", "type", fmt.Sprintf("%T", provider))
	return provider.Labels(ctx, chainID, addresses)
}

func CompactLabels(labels []AddressLabel) []map[string]string {
	out := make([]map[string]string, 0, len(labels))
	for _, label := range labels {
		compact := traceLabel(label)
		out = append(out, map[string]string{
			"address":    label.Address,
			"label":      compact.Label,
			"attributes": compact.Attributes,
		})
	}
	return out
}

// systemAddresses holds all-same-digit pattern addresses (0x0000...0000 through
// 0xffff...ffff) that must never trigger label risk checks.
var systemAddresses = func() map[string]struct{} {
	m := make(map[string]struct{}, 16)
	for _, c := range "0123456789abcdef" {
		m["0x"+strings.Repeat(string(c), 40)] = struct{}{}
	}
	return m
}()

// isSystemAddress returns true for precompile addresses (0x0~0xffff) and
// known pattern addresses so they are excluded from label risk checks.
func isSystemAddress(addr string) bool {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if _, ok := systemAddresses[addr]; ok {
		return true
	}
	h := strings.TrimPrefix(addr, "0x")
	return len(h) == 40 && h[:36] == "000000000000000000000000000000000000"
}

func CollectLabelAddresses(req CheckRequest, ops []Operation, fundFlow []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 2+len(ops)+len(fundFlow))
	add := func(addr string) {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" || !primitives.CommonAddress(addr) || seen[addr] || isSystemAddress(addr) {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}
	add(req.Owner)
	add(req.To)
	for _, op := range ops {
		add(op.To.Hex())
		if spender, _, ok := primitives.DecodeApprove(op.Data); ok {
			add(spender.Hex())
		}
	}
	for _, addr := range fundFlow {
		add(addr)
	}
	return out
}

func HighRiskLabelReasons(label AddressLabel) []string {
	reasons := make([]string, 0)
	for _, category := range label.Categories {
		if isHighRiskCategory(category) {
			reasons = append(reasons, fmt.Sprintf("category:%s", category.Name))
		}
	}
	for _, attr := range label.Attributes {
		if isHighRiskAttribute(attr) {
			reasons = append(reasons, fmt.Sprintf("attribute:%s", attr.Name))
		}
	}
	return reasons
}

func labelIndex(labels []AddressLabel) map[string]AddressLabel {
	out := make(map[string]AddressLabel, len(labels))
	for _, label := range labels {
		addr := strings.ToLower(strings.TrimSpace(label.Address))
		if addr != "" {
			out[addr] = label
		}
	}
	return out
}

func attachAddressLabelsToTrace(trace *SimulationCall, labels []AddressLabel) {
	if trace == nil || len(labels) == 0 {
		return
	}
	attachAddressLabelsToTraceWithIndex(trace, labelIndex(labels))
}

func attachAddressLabelsToTraceWithIndex(trace *SimulationCall, labels map[string]AddressLabel) {
	if trace == nil {
		return
	}
	if label, ok := labels[strings.ToLower(strings.TrimSpace(trace.From))]; ok {
		trace.FromLabel = traceLabel(label)
	}
	if label, ok := labels[strings.ToLower(strings.TrimSpace(trace.To))]; ok {
		trace.ToLabel = traceLabel(label)
	}
	for i := range trace.Calls {
		attachAddressLabelsToTraceWithIndex(&trace.Calls[i], labels)
	}
}

func traceLabel(label AddressLabel) *TraceLabel {
	return &TraceLabel{
		Label:      label.NameTag,
		Attributes: mergedLabelAttributes(label),
	}
}

func mergedLabelAttributes(label AddressLabel) string {
	values := make([]LabelValue, 0, len(label.Categories)+len(label.Attributes))
	values = append(values, label.Categories...)
	values = append(values, label.Attributes...)
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Code != values[j].Code {
			return values[i].Code < values[j].Code
		}
		return strings.ToLower(values[i].Name) < strings.ToLower(values[j].Name)
	})
	seen := make(map[string]bool)
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func isHighRiskCategory(label LabelValue) bool {
	switch label.Code {
	case 3003, 3006, 3017, 3019, 3032, 3042, 3049:
		return true
	}
	name := strings.ToUpper(strings.TrimSpace(label.Name))
	return strings.Contains(name, "SCAM") ||
		strings.Contains(name, "TERROR") ||
		strings.Contains(name, "RANSOM") ||
		strings.Contains(name, "MIXER") ||
		strings.Contains(name, "DARK")
}

func isHighRiskAttribute(label LabelValue) bool {
	switch label.Code {
	case 4001, 4002, 4006, 4011, 4012, 4015, 4016, 4017, 4018:
		return true
	}
	name := strings.ToUpper(strings.TrimSpace(label.Name))
	return strings.Contains(name, "SANCTION") ||
		strings.Contains(name, "SCAM") ||
		strings.Contains(name, "ATTACK") ||
		strings.Contains(name, "EXPLOIT") ||
		strings.Contains(name, "LAUNDER") ||
		strings.Contains(name, "MIX") ||
		strings.Contains(name, "BLOCK")
}
