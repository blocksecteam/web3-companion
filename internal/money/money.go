// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package money

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const precision uint = 256

type Amount struct {
	value *big.Float
	rat   *big.Rat
}

func FromFloat64(v float64) (Amount, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Amount{}, fmt.Errorf("invalid money amount %v", v)
	}
	return Parse(strconv.FormatFloat(v, 'g', 15, 64))
}

func MustParse(raw string) Amount {
	amount, err := Parse(raw)
	if err != nil {
		panic(err)
	}
	return amount
}

func Parse(raw string) (Amount, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Amount{}, fmt.Errorf("empty money amount")
	}
	rat, ok := new(big.Rat).SetString(raw)
	if !ok {
		return Amount{}, fmt.Errorf("invalid money amount %q", raw)
	}
	return newAmountFromRat(rat)
}

func Sum(values ...Amount) Amount {
	total := new(big.Rat)
	for _, value := range values {
		total.Add(total, value.normalizedRat())
	}
	amount, _ := newAmountFromRat(total)
	return amount
}

func Multiply(a, b Amount) Amount {
	product := new(big.Rat).Mul(a.normalizedRat(), b.normalizedRat())
	amount, _ := newAmountFromRat(product)
	return amount
}

func Subtract(a, b Amount) Amount {
	diff := new(big.Rat).Sub(a.normalizedRat(), b.normalizedRat())
	amount, _ := newAmountFromRat(diff)
	return amount
}

func newAmountFromRat(rat *big.Rat) (Amount, error) {
	if rat == nil {
		rat = new(big.Rat)
	}
	canonical := canonicalRatString(rat)
	f, _, err := big.ParseFloat(canonical, 10, precision, big.ToNearestEven)
	if err != nil {
		return Amount{}, err
	}
	return Amount{value: f, rat: new(big.Rat).Set(rat)}, nil
}

func (a Amount) GreaterThan(other Amount) bool {
	return a.normalizedRat().Cmp(other.normalizedRat()) > 0
}

func (a Amount) Sign() int {
	return a.normalizedRat().Sign()
}

func (a Amount) Float64() (float64, error) {
	return strconv.ParseFloat(a.String(), 64)
}

func (a Amount) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

func (a *Amount) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		raw = strings.TrimSpace(string(data))
	}
	amount, err := Parse(raw)
	if err != nil {
		return err
	}
	*a = amount
	return nil
}

func (a *Amount) UnmarshalText(data []byte) error {
	amount, err := Parse(string(data))
	if err != nil {
		return err
	}
	*a = amount
	return nil
}

func (a Amount) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a Amount) Value() (driver.Value, error) {
	return a.String(), nil
}

func (a *Amount) Scan(value interface{}) error {
	switch v := value.(type) {
	case nil:
		*a = Amount{}
		return nil
	case string:
		amount, err := Parse(v)
		if err != nil {
			return err
		}
		*a = amount
		return nil
	case []byte:
		amount, err := Parse(string(v))
		if err != nil {
			return err
		}
		*a = amount
		return nil
	default:
		return fmt.Errorf("cannot scan money amount from %T", value)
	}
}

func (a Amount) String() string {
	if a.rat == nil && a.value == nil {
		return "0"
	}
	out := canonicalRatString(a.normalizedRat())
	if strings.Contains(out, ".") {
		out = strings.TrimRight(out, "0")
		out = strings.TrimRight(out, ".")
	}
	if out == "-0" || out == "" {
		return "0"
	}
	return out
}

func (a Amount) normalizedRat() *big.Rat {
	if a.rat != nil {
		return new(big.Rat).Set(a.rat)
	}
	if a.value == nil {
		return new(big.Rat)
	}
	rat, ok := new(big.Rat).SetString(a.value.Text('f', -1))
	if !ok {
		return new(big.Rat)
	}
	return rat
}

func canonicalRatString(rat *big.Rat) string {
	if rat == nil || rat.Sign() == 0 {
		return "0"
	}
	sign := ""
	num := new(big.Int).Set(rat.Num())
	den := new(big.Int).Set(rat.Denom())
	if num.Sign() < 0 {
		sign = "-"
		num.Abs(num)
	}
	scale, terminating := decimalScale(den)
	if !terminating {
		out := rat.FloatString(36)
		out = strings.TrimRight(out, "0")
		out = strings.TrimRight(out, ".")
		if out == "-0" || out == "" {
			return "0"
		}
		return out
	}
	scaled := new(big.Int).Mul(num, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
	scaled.Quo(scaled, den)
	out := scaled.String()
	if scale == 0 {
		return sign + out
	}
	if len(out) <= scale {
		out = strings.Repeat("0", scale-len(out)+1) + out
	}
	intPart := out[:len(out)-scale]
	fracPart := strings.TrimRight(out[len(out)-scale:], "0")
	if fracPart == "" {
		return sign + intPart
	}
	return sign + intPart + "." + fracPart
}

func decimalScale(den *big.Int) (int, bool) {
	remaining := new(big.Int).Set(den)
	two := big.NewInt(2)
	five := big.NewInt(5)
	zero := big.NewInt(0)
	twos := 0
	for new(big.Int).Mod(remaining, two).Cmp(zero) == 0 {
		remaining.Div(remaining, two)
		twos++
	}
	fives := 0
	for new(big.Int).Mod(remaining, five).Cmp(zero) == 0 {
		remaining.Div(remaining, five)
		fives++
	}
	if remaining.Cmp(big.NewInt(1)) != 0 {
		return 0, false
	}
	if twos > fives {
		return twos, true
	}
	return fives, true
}
