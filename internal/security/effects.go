// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package security

import (
	"math/big"
	"strings"

	"github.com/blocksecteam/web3-companion/internal/security/primitives"
)

var nativeAssetAliases = map[string]bool{
	"native": true,
	"eth":    true,
	"bnb":    true,
	"0x0":    true,
	"0x0000000000000000000000000000000000000000": true,
	"0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee": true,
}

func ExtractTxEffect(req CheckRequest, data *SimulationData) TxEffect {
	effect := TxEffect{
		UserAddress: req.Owner,
		ChainID:     req.ChainID,
		Contract:    req.To,
		// Initialize as empty slices so JSON encodes as [] (verified empty)
		// rather than null (ambiguous: missing data vs no items).
		Spending:  []AssetEffect{},
		Receiving: []AssetEffect{},
	}
	if data == nil {
		return effect
	}

	for _, bc := range data.BalanceChanges {
		if !primitives.AddrEq(bc.Account, req.Owner) {
			continue
		}
		for _, asset := range bc.Assets {
			receiving, magnitudeAmount, magnitudeRaw := assetDirectionAndMagnitude(asset)
			item := AssetEffect{
				TokenAddress: asset.Address,
				TokenID:      asset.TokenID,
				Amount:       primitives.FirstString(magnitudeAmount, magnitudeRaw),
				USDValue:     asset.Value,
				Receiving:    receiving,
			}
			if receiving {
				effect.Receiving = append(effect.Receiving, item)
			} else {
				effect.Spending = append(effect.Spending, item)
			}
		}
	}

	effect.TotalSpendingUSD = sumUSD(effect.Spending)
	effect.TotalReceivingUSD = sumUSD(effect.Receiving)
	effect.GasUSD = gasUSD(data, req.Owner)
	effect.NetUSD = netUSD(effect)
	if req.TxType == "bridge" && len(effect.Receiving) == 0 {
		effect.NetUSD = nil
	}
	return effect
}

func assetDirectionAndMagnitude(asset BalanceChangeAsset) (receiving bool, amount string, raw string) {
	rawValue, rawOK := signedDecimal(asset.BaseUnits)
	amountValue, amountOK := signedDecimal(asset.Amount)
	receiving = true
	if rawOK {
		receiving = rawValue.Sign() >= 0
		raw = formatSignedMagnitude(rawValue)
	}
	if amountOK {
		if !rawOK {
			receiving = amountValue.Sign() >= 0
		}
		amount = formatSignedMagnitude(amountValue)
	}
	if !rawOK && !amountOK {
		amount = strings.TrimPrefix(strings.TrimSpace(asset.Amount), "-")
		raw = strings.TrimPrefix(strings.TrimSpace(asset.BaseUnits), "-")
	}
	return receiving, amount, raw
}

func signedDecimal(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, false
	}
	return r, true
}

func formatSignedMagnitude(value *big.Rat) string {
	if value == nil {
		return ""
	}
	return primitives.FormatRat(absRat(value))
}

func sumUSD(items []AssetEffect) *string {
	if len(items) == 0 {
		return stringPtr("0")
	}
	total := new(big.Rat)
	for _, item := range items {
		if strings.TrimSpace(item.USDValue) == "" {
			return nil
		}
		value, ok := primitives.DecimalRat(item.USDValue)
		if !ok {
			return nil
		}
		total.Add(total, absRat(value))
	}
	return stringPtr(primitives.FormatRat(total))
}

func gasUSD(data *SimulationData, owner string) *string {
	if data == nil {
		return nil
	}
	feeWei, ok := primitives.ParseInteger(data.TransactionFee)
	if !ok {
		return nil
	}
	if feeWei.Sign() == 0 {
		return stringPtr("0")
	}
	price := NativeUSDPerWei(data, owner)
	if price == nil {
		return nil
	}
	feeRat := new(big.Rat).SetInt(feeWei)
	feeRat.Mul(feeRat, price)
	return stringPtr(primitives.FormatRat(feeRat))
}

// NativeUSDPerWei derives the chain-native USD price per wei from the owner's
// balance change line. Exported because GasCostSanity (in the checks
// subpackage) needs it; it depends on SimulationData so it cannot live in
// the primitives subpackage without forming a dependency cycle.
func NativeUSDPerWei(data *SimulationData, owner string) *big.Rat {
	for _, bc := range data.BalanceChanges {
		if !primitives.AddrEq(bc.Account, owner) {
			continue
		}
		for _, asset := range bc.Assets {
			if !isNativeAsset(asset.Address) {
				continue
			}
			usd, ok := primitives.DecimalRat(asset.Value)
			if !ok {
				continue
			}
			rawRat, ok := signedDecimal(asset.BaseUnits)
			if !ok || rawRat.Sign() == 0 {
				continue
			}
			rawRat = absRat(rawRat)
			price := new(big.Rat).Set(usd)
			price.Quo(price, rawRat)
			return absRat(price)
		}
	}
	return nil
}

func netUSD(effect TxEffect) *string {
	if effect.TotalSpendingUSD == nil || effect.TotalReceivingUSD == nil {
		return nil
	}
	spend, ok := primitives.DecimalRat(*effect.TotalSpendingUSD)
	if !ok {
		return nil
	}
	receive, ok := primitives.DecimalRat(*effect.TotalReceivingUSD)
	if !ok {
		return nil
	}
	net := new(big.Rat).Sub(receive, spend)
	return stringPtr(primitives.FormatRat(net))
}

func absRat(v *big.Rat) *big.Rat {
	out := new(big.Rat).Set(v)
	if out.Sign() < 0 {
		out.Neg(out)
	}
	return out
}

func isNativeAsset(addr string) bool {
	return nativeAssetAliases[strings.ToLower(strings.TrimSpace(addr))]
}

func stringPtr(s string) *string {
	return &s
}
