// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package cli

import (
	"encoding/json"
	"testing"
)

func TestStripSimulationTrace(t *testing.T) {
	input := []byte(`{
		"transaction_id": "txi_1",
		"verdict": "pass",
		"simulation": {
			"provider": "rpc",
			"balanceChanges": [{"address": "0xabc", "delta": "-1000"}],
			"trace": {"calls": [{"type": "CALL", "to": "0xdef"}]}
		}
	}`)

	got := StripSimulationTrace(input)

	var result map[string]interface{}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	sim, ok := result["simulation"].(map[string]interface{})
	if !ok {
		t.Fatal("simulation field missing")
	}
	if _, hasTrace := sim["trace"]; hasTrace {
		t.Error("simulation.trace should have been stripped")
	}
	if sim["provider"] != "rpc" {
		t.Errorf("simulation.provider should be preserved, got %v", sim["provider"])
	}
	if sim["balanceChanges"] == nil {
		t.Error("simulation.balanceChanges should be preserved")
	}
	if result["verdict"] != "pass" {
		t.Errorf("other fields should be preserved, verdict=%v", result["verdict"])
	}
}

func TestStripSimulationTrace_NoSimulation(t *testing.T) {
	input := []byte(`{"transaction_id": "txi_2", "verdict": "fail"}`)
	got := StripSimulationTrace(input)
	var result map[string]interface{}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if result["verdict"] != "fail" {
		t.Errorf("unexpected change, verdict=%v", result["verdict"])
	}
}

func TestStripSimulationTrace_InvalidJSON(t *testing.T) {
	input := []byte(`not json`)
	got := StripSimulationTrace(input)
	if string(got) != string(input) {
		t.Error("invalid JSON should be returned unchanged")
	}
}

func TestExtractSimulationTrace(t *testing.T) {
	input := []byte(`{
		"transaction_id": "txi_3",
		"simulation": {
			"provider": "rpc",
			"trace": {"calls": [{"type": "CALL"}]}
		}
	}`)

	got := ExtractSimulationTrace(input)

	var result map[string]interface{}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	trace, ok := result["trace"]
	if !ok {
		t.Fatal("trace field missing from output")
	}
	if trace == nil {
		t.Error("trace should not be nil")
	}
	if _, hasVerdict := result["verdict"]; hasVerdict {
		t.Error("non-trace fields should not be in output")
	}
}

func TestExtractSimulationTrace_NoTrace(t *testing.T) {
	input := []byte(`{"transaction_id": "txi_4", "simulation": {"provider": "rpc"}}`)
	got := ExtractSimulationTrace(input)
	var result map[string]interface{}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, hasTrace := result["trace"]; !hasTrace {
		t.Error("trace key should be present (with null value) even when missing from input")
	}
	if result["trace"] != nil {
		t.Errorf("trace value should be nil when missing from input, got %v", result["trace"])
	}
}

func TestExtractSimulationTrace_InvalidJSON(t *testing.T) {
	input := []byte(`bad input`)
	got := ExtractSimulationTrace(input)
	if string(got) != string(input) {
		t.Error("invalid JSON should be returned unchanged")
	}
}
