// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

func (h *Handler) simplifiedSimulationPayload(ctx context.Context, sim *SimulationResult) json.RawMessage {
	if sim == nil || sim.Data == nil {
		return nil
	}
	signatures := h.resolveTraceFunctionSignatures(ctx, sim.Data.Trace)
	return simplifiedSimulationPayloadRaw(sim, signatures)
}

func simplifiedSimulationPayloadRaw(sim *SimulationResult, signatures map[string]string) json.RawMessage {
	if sim == nil || sim.Data == nil {
		return nil
	}
	provider := primitives.FirstString(sim.Data.SimulationLabel, "rpc")
	raw, _ := json.Marshal(map[string]interface{}{
		"provider":       provider,
		"trace":          simulationCallView(sim.Data.Trace, signatures),
		"balanceChanges": sim.Data.BalanceChanges,
	})
	return raw
}

func (h *Handler) resolveTraceFunctionSignatures(ctx context.Context, trace *SimulationCall) map[string]string {
	if h.functionSignatureResolver == nil || trace == nil {
		return nil
	}
	selectors := traceFunctionSelectors(trace)
	if len(selectors) == 0 {
		return nil
	}
	resolved, err := h.functionSignatureResolver.ResolveFunctions(ctx, selectors)
	if err != nil {
		return nil
	}
	return resolved
}

func traceFunctionSelectors(trace *SimulationCall) []string {
	if trace == nil {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0)
	var walk func(*SimulationCall)
	walk = func(call *SimulationCall) {
		selector := strings.ToLower(strings.TrimSpace(call.Func))
		if len(selector) == 10 && strings.HasPrefix(selector, "0x") && !seen[selector] {
			seen[selector] = true
			out = append(out, selector)
		}
		for i := range call.Calls {
			walk(&call.Calls[i])
		}
	}
	walk(trace)
	return out
}

func simulationCallView(call *SimulationCall, signatures map[string]string) *SimulationCallView {
	if call == nil {
		return nil
	}
	view := &SimulationCallView{
		Type:      strings.TrimSpace(call.Type),
		From:      strings.TrimSpace(call.From),
		To:        strings.TrimSpace(call.To),
		FromLabel: nonEmptyTraceLabel(call.FromLabel),
		ToLabel:   nonEmptyTraceLabel(call.ToLabel),
		Value:     weiFloatValue(call.Value),
		Func:      resolvedFunctionSignature(call.Func, signatures),
	}
	if !call.Status {
		view.Error = strings.TrimSpace(call.Error)
	}
	for i := range call.Calls {
		if child := simulationCallView(&call.Calls[i], signatures); child != nil {
			view.Calls = append(view.Calls, *child)
		}
	}
	return view
}

func resolvedFunctionSignature(selector string, signatures map[string]string) string {
	selector = strings.ToLower(strings.TrimSpace(selector))
	if signature := strings.TrimSpace(signatures[selector]); signature != "" {
		return signature
	}
	return selector
}

func nonEmptyTraceLabel(label *TraceLabel) *TraceLabel {
	if label == nil {
		return nil
	}
	out := TraceLabel{
		Label:      strings.TrimSpace(label.Label),
		Attributes: strings.TrimSpace(label.Attributes),
	}
	if out.Label == "" && out.Attributes == "" {
		return nil
	}
	return &out
}

func weiFloatValue(value string) *float64 {
	wei, ok := primitives.ParseInteger(value)
	if !ok || wei.Sign() == 0 {
		return nil
	}
	rat := new(big.Rat).SetFrac(wei, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	f, _ := rat.Float64()
	return &f
}
