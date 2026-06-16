#!/usr/bin/env bash
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FAILED=0

echo "=== Go build ==="
go build ./... || FAILED=1

echo ""
echo "=== Go vet ==="
go vet ./... || FAILED=1

echo ""
echo "=== Go test ==="
go test -short ./internal/... ./tests/... -count=1 -timeout=120s || FAILED=1

echo ""
echo "=== Frontend type check ==="
(cd frontend && bun install --frozen-lockfile --silent && bunx tsc --noEmit) || FAILED=1

echo ""
echo "=== Frontend test ==="
(cd frontend && bun test) || FAILED=1

echo ""
echo "=== Companion type check ==="
(cd companion && bun install --frozen-lockfile --silent && bunx tsc --noEmit) || FAILED=1

echo ""
echo "=== Companion test ==="
(cd companion && HOME="$(mktemp -d)" bun test) || FAILED=1

echo ""
echo "=== Sensitive data check ==="
docker run --rm -v "$PWD:/repo" -w /repo ghcr.io/gitleaks/gitleaks:v8.28.0 dir --redact --verbose . || FAILED=1
./scripts/check-sensitive-data.sh || FAILED=1

echo ""
if [ "$FAILED" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "SOME CHECKS FAILED"
  exit 1
fi
