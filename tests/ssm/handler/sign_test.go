// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/money"
	ssmhandler "github.com/blocksecteam/web3-companion/internal/ssm/handler"
)

func TestSignChallengePayloadIncludesTransactionAndSecuritySummary(t *testing.T) {
	payload, err := json.Marshal(map[string]interface{}{
		"payload": ssmhandler.SignChallengePayload{
			SignRequest: ssmhandler.SignRequest{
				TransactionID: "txi_123",
				WalletID:      "wallet_1",
				Chain:         "ethereum",
				To:            "0x1234567890123456789012345678901234567890",
				Value:         "123",
				Data:          "0xabcdef",
				GasLimit:      98765,
				AmountUSD:     mustMoneyAmount(t, "42.50"),
			},
			Reason:      "manual confirmation required",
			PayloadHash: "payload-hash-123",
			Security: map[string]interface{}{
				"verdict": "warn",
				"checks": []map[string]interface{}{
					{"rule": "usd_value_conservation", "status": "warn", "detail": "destination-chain receipt is not simulated"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var envelope struct {
		Payload map[string]interface{} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode payload envelope: %v", err)
	}
	for key, want := range map[string]interface{}{
		"transaction_id": "txi_123",
		"wallet_id":      "wallet_1",
		"chain":          "ethereum",
		"to":             "0x1234567890123456789012345678901234567890",
		"tx_type":        "",
		"payload_hash":   "payload-hash-123",
		"reason":         "manual confirmation required",
	} {
		if envelope.Payload[key] != want {
			t.Fatalf("payload[%s] = %#v, want %#v in %s", key, envelope.Payload[key], want, string(payload))
		}
	}
	if envelope.Payload["security"] == nil {
		t.Fatalf("payload missing security summary: %s", string(payload))
	}

	got, err := ssmhandler.SignRequestFromChallenge(string(payload))
	if err != nil {
		t.Fatalf("parse sign request: %v", err)
	}
	if got.TransactionID != "txi_123" {
		t.Fatalf("transaction_id = %q, want txi_123", got.TransactionID)
	}
}

func mustMoneyAmount(t *testing.T, raw string) money.Amount {
	t.Helper()
	amount, err := money.Parse(raw)
	if err != nil {
		t.Fatalf("parse money amount: %v", err)
	}
	return amount
}
