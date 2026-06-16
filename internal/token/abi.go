// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package token

import "strings"

// padHexAddress left-pads a hex-encoded address (with or without "0x" prefix,
// any case) into a 64-character lowercase 32-byte ABI word. If the input
// already exceeds 40 hex chars, the trailing 40 are used.
//
// Package-private helper shared by every encoder in this package
// (EncodeERC20Approve, EncodeERC20Transfer, EncodePermit2Approve, ...).
// Lives here rather than next to one encoder so adding a new encoder does
// not have to reach into an unrelated operation's file.
func padHexAddress(addr string) string {
	addr = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(addr)), "0x")
	if len(addr) > 40 {
		addr = addr[len(addr)-40:]
	}
	return strings.Repeat("0", 64-len(addr)) + addr
}
