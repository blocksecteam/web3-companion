#!/usr/bin/env sh
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -eu

ROOT_DIR="${1:-.}"
ASSETS_DIR="$ROOT_DIR/internal/frontend/assets"
INDEX_FILE="$ASSETS_DIR/index.html"

if [ ! -f "$INDEX_FILE" ]; then
  echo "missing embedded frontend asset: $INDEX_FILE" >&2
  exit 1
fi

if grep -R "/api/v1/settings/" "$ASSETS_DIR" >/dev/null 2>&1; then
  echo "embedded frontend assets contain legacy /api/v1/settings/ routes" >&2
  exit 1
fi

if ! grep -R "/api/v1/user/settings/" "$ASSETS_DIR" >/dev/null 2>&1; then
  echo "embedded frontend assets do not contain current /api/v1/user/settings/ routes" >&2
  exit 1
fi
