// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

export const MAX_NAME_UTF8_BYTES = 50;
export const MAX_DECIMAL_INPUT_VALUE = 1_000_000_000;

const DECIMAL_PATTERN = /^(?:0|[1-9]\d*)(?:\.\d+)?$/;
const INTEGER_PATTERN = /^(?:0|[1-9]\d*)$/;

export function utf8ByteLength(value: string) {
  return new TextEncoder().encode(value).length;
}

export function validateNameInput(value: string, maxBytes = MAX_NAME_UTF8_BYTES) {
  const bytes = utf8ByteLength(value);
  if (bytes > maxBytes) return { valid: false as const, bytes, maxBytes };
  return { valid: true as const, bytes, maxBytes };
}

export function validateDecimalInput(value: string, max = MAX_DECIMAL_INPUT_VALUE) {
  const text = value.trim();
  if (!text) return { valid: true as const };
  if (!DECIMAL_PATTERN.test(text)) return { valid: false as const, max };
  const parsed = Number(text);
  if (!Number.isFinite(parsed) || parsed > max) return { valid: false as const, max };
  return { valid: true as const };
}

export function validateIntegerInput(value: string, max = MAX_DECIMAL_INPUT_VALUE) {
  const text = value.trim();
  if (!text) return { valid: true as const };
  if (!INTEGER_PATTERN.test(text)) return { valid: false as const, max };
  const parsed = Number(text);
  if (!Number.isSafeInteger(parsed) || parsed > max) return { valid: false as const, max };
  return { valid: true as const };
}
