// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import (
	"encoding/hex"
	"math/big"
)

// EncodeERC20Transfer encodes the standard ERC-20 transfer(recipient, amount)
// calldata (selector 0xa9059cbb + 32-byte padded recipient + 32-byte amount).
// A nil amount is treated as zero; negative amounts will panic via FillBytes.
func EncodeERC20Transfer(recipient string, amount *big.Int) string {
	paddedAmount := make([]byte, 32)
	if amount != nil {
		amount.FillBytes(paddedAmount)
	}
	return "0xa9059cbb" + padHexAddress(recipient) + hex.EncodeToString(paddedAmount)
}
