// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package money_test

import (
	"encoding/json"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/money"
)

func TestFromFloat64CanonicalizesDecimal(t *testing.T) {
	amount, err := money.FromFloat64(0.1 + 0.2)
	if err != nil {
		t.Fatalf("FromFloat64: %v", err)
	}
	if got := amount.String(); got != "0.3" {
		t.Fatalf("canonical amount = %q, want 0.3", got)
	}
}

func TestSumDoesNotTripFloatRoundingAtLimit(t *testing.T) {
	sum := money.Sum(money.MustParse("0.1"), money.MustParse("0.2"))
	limit := money.MustParse("0.3")
	if sum.GreaterThan(limit) {
		t.Fatalf("%s should not be greater than %s", sum.String(), limit.String())
	}
}

func TestLimitExceededTreatsZeroLimitAsDisabled(t *testing.T) {
	exceeded := limitExceeded(money.MustParse("100"), money.Amount{})
	if exceeded {
		t.Fatal("zero limit should disable the limit")
	}
}

func TestProductUsesDecimalOperands(t *testing.T) {
	product := money.Multiply(money.MustParse("0.2"), money.MustParse("0.1"))
	if product.String() != "0.02" {
		t.Fatalf("product = %s, want 0.02", product.String())
	}
	got, err := product.Float64()
	if err != nil {
		t.Fatalf("Float64: %v", err)
	}
	if got != 0.02 {
		t.Fatalf("float64 product = %.20f, want 0.02", got)
	}
}

func TestSubtractUsesDecimalOperands(t *testing.T) {
	diff := money.Subtract(money.MustParse("0.3"), money.MustParse("0.1"))
	if diff.String() != "0.2" {
		t.Fatalf("diff = %s, want 0.2", diff.String())
	}
	got, err := diff.Float64()
	if err != nil {
		t.Fatalf("Float64: %v", err)
	}
	if got != 0.2 {
		t.Fatalf("float64 diff = %.20f, want 0.2", got)
	}
}

func limitExceeded(value, limit money.Amount) bool {
	return limit.Sign() > 0 && value.GreaterThan(limit)
}

func TestAmountJSONUsesCanonicalDecimalString(t *testing.T) {
	payload := struct {
		Amount money.Amount `json:"amount"`
	}{Amount: money.MustParse("0.3000")}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data) != `{"amount":"0.3"}` {
		t.Fatalf("json = %s, want canonical decimal string", data)
	}

	var decoded struct {
		Amount money.Amount `json:"amount"`
	}
	if err := json.Unmarshal([]byte(`{"amount":"0.10"}`), &decoded); err != nil {
		t.Fatalf("UnmarshalJSON string: %v", err)
	}
	if decoded.Amount.String() != "0.1" {
		t.Fatalf("decoded amount = %s, want 0.1", decoded.Amount.String())
	}
	if err := json.Unmarshal([]byte(`{"amount":0.10}`), &decoded); err != nil {
		t.Fatalf("UnmarshalJSON number: %v", err)
	}
	if decoded.Amount.String() != "0.1" {
		t.Fatalf("decoded numeric amount = %s, want 0.1", decoded.Amount.String())
	}
}

func TestAmountSQLRoundTripUsesCanonicalDecimalString(t *testing.T) {
	value, err := money.MustParse("0.3000").Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if value != "0.3" {
		t.Fatalf("driver value = %#v, want canonical string 0.3", value)
	}

	var scanned money.Amount
	if err := scanned.Scan([]byte("0.10")); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scanned.String() != "0.1" {
		t.Fatalf("scanned = %s, want 0.1", scanned.String())
	}
}
