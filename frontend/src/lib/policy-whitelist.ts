// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

const ETHEREUM_ADDRESS_PATTERN = /^0x[0-9a-fA-F]{40}$/;
const ADDRESS_LIKE_PATTERN = /0x[0-9a-fA-F]{40}/g;

export type RecipientWhitelistParseResult = {
  addresses: string[];
  invalidLines: number[];
};

export type RecipientWhitelistInvalidReason = "invalid_address" | "multiple_addresses";

export type RecipientWhitelistLineIssue = {
  line: number;
  value: string;
  reason: RecipientWhitelistInvalidReason;
};

export type RecipientWhitelistStatus = {
  valid: boolean;
  validCount: number;
  invalidLines: RecipientWhitelistLineIssue[];
};

export function parseRecipientWhitelist(value: string): RecipientWhitelistParseResult {
  const addresses: string[] = [];
  const invalidLines: number[] = [];
  const lines = value.replace(/\r\n?/g, "\n").split("\n");

  lines.forEach((line, index) => {
    const trimmed = line.trim();
    if (!trimmed) return;
    if (!ETHEREUM_ADDRESS_PATTERN.test(trimmed)) {
      invalidLines.push(index + 1);
      return;
    }
    addresses.push(trimmed.toLowerCase());
  });

  return { addresses, invalidLines };
}

export function recipientWhitelistStatus(value: string): RecipientWhitelistStatus {
  const parsed = parseRecipientWhitelist(value);
  const invalidLines: RecipientWhitelistLineIssue[] = [];
  const lines = value.replace(/\r\n?/g, "\n").split("\n");

  lines.forEach((line, index) => {
    const trimmed = line.trim();
    if (!trimmed || ETHEREUM_ADDRESS_PATTERN.test(trimmed)) return;
    const matches = trimmed.match(ADDRESS_LIKE_PATTERN) ?? [];
    invalidLines.push({
      line: index + 1,
      value: trimmed,
      reason: matches.length > 1 ? "multiple_addresses" : "invalid_address",
    });
  });

  return {
    valid: invalidLines.length === 0,
    validCount: parsed.addresses.length,
    invalidLines,
  };
}

export function normalizeRecipientWhitelistAddresses(addresses: string[]) {
  return addresses
    .map((address) => address.trim())
    .filter(Boolean)
    .map((address) => address.toLowerCase());
}
