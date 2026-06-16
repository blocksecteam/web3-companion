// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package primitives

import (
	"encoding/hex"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// SelectorApprove is the 4-byte function selector of ERC-20
// approve(address,uint256). Exported because checks outside this package
// match against it (e.g. SimulationNonEmpty's "is this just an approve?"
// heuristic). Other ERC selectors stay private to security/decode.go since
// only its own decoder cares about them.
const SelectorApprove = "0x095ea7b3"

const selectorSetApprovalForAll = "0xa22cb465"

// MethodID returns the 4-byte 0x-prefixed selector of the calldata, or "0x"
// for empty calldata, or the full data for inputs shorter than 4 bytes.
func MethodID(data []byte) string {
	if len(data) < 4 {
		if len(data) == 0 {
			return "0x"
		}
		return "0x" + hex.EncodeToString(data)
	}
	return "0x" + hex.EncodeToString(data[:4])
}

// MaxUint256 returns a fresh *big.Int equal to 2^256 - 1. Callers receive a
// new instance each call so the value is safe to mutate.
func MaxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}

// DecodeApprove decodes ERC-20 approve(address spender, uint256 amount).
// Returns the third value as false when the calldata does not match the
// approve selector or is too short.
func DecodeApprove(data []byte) (common.Address, *big.Int, bool) {
	if len(data) < 4+32+32 || MethodID(data) != SelectorApprove {
		return common.Address{}, nil, false
	}
	spender := common.BytesToAddress(data[4+12 : 4+32])
	amount := new(big.Int).SetBytes(data[4+32 : 4+64])
	return spender, amount, true
}

// DecodeSetApprovalForAll decodes ERC-721/1155
// setApprovalForAll(address operator, bool approved). Returns the third
// value as false when the calldata does not match the selector or is too
// short.
func DecodeSetApprovalForAll(data []byte) (common.Address, bool, bool) {
	if len(data) < 4+32+32 || MethodID(data) != selectorSetApprovalForAll {
		return common.Address{}, false, false
	}
	operator := common.BytesToAddress(data[4+12 : 4+32])
	approved := new(big.Int).SetBytes(data[4+32:4+64]).Sign() != 0
	return operator, approved, true
}
