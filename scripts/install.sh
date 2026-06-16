#!/usr/bin/env sh
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -eu

REPO="blocksecteam/web3-companion"
BRANCH="main"
BASE="https://raw.githubusercontent.com/${REPO}/${BRANCH}/docker"

echo "Downloading Web3 Companion Docker Compose file..."

curl -fsSL "${BASE}/compose.hub.yml" -o compose.yml

echo ""
echo "Done. File created:"
echo "  compose.yml        — Docker Compose service definition for the published image"
echo ""
echo "Next steps:"
echo "  1. Run: docker compose up -d"
echo "  2. Open: http://localhost:8081"
echo "  3. Complete the Setup Wizard in the browser"
echo ""
echo "Optional overrides:"
echo "  WEB3_PORT=9090 docker compose up -d"
echo "  WEB3_DATA_DIR=/path/to/web3-data docker compose up -d"
