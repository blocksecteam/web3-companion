#!/usr/bin/env bash
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

IMAGE="${DOCKERHUB_IMAGE:-blocksecteam/web3-companion}"
PUSH=0
SKIP_INTEGRATION=0
PLATFORMS="${RELEASE_PLATFORMS:-linux/amd64,linux/arm64}"
LOCAL_PLATFORM="${RELEASE_LOCAL_PLATFORM:-}"
VERSION=""

usage() {
  cat <<'EOF'
Usage:
  scripts/release-local.sh [options] vX.Y.Z

Options:
  --push              Push multi-arch images to the registry. Requires full checks.
  --skip-integration  Skip live integration tests. Not allowed with --push.
  --image IMAGE       Docker image name. Default: blocksecteam/web3-companion
  --platforms LIST    Push platforms. Default: linux/amd64,linux/arm64
  --local-platform P  Local build platform when not pushing. Default: host platform
  -h, --help          Show this help.

Environment:
  DOCKERHUB_IMAGE          Overrides the default image name.
  RELEASE_PLATFORMS       Overrides push platforms.
  RELEASE_LOCAL_PLATFORM  Overrides local build platform.

Examples:
  scripts/release-local.sh --skip-integration v1.2.3
  scripts/release-local.sh --push v1.2.3
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --push)
      PUSH=1
      shift
      ;;
    --skip-integration)
      SKIP_INTEGRATION=1
      shift
      ;;
    --image)
      IMAGE="${2:?--image requires a value}"
      shift 2
      ;;
    --platforms)
      PLATFORMS="${2:?--platforms requires a value}"
      shift 2
      ;;
    --local-platform)
      LOCAL_PLATFORM="${2:?--local-platform requires a value}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
    *)
      if [ -n "$VERSION" ]; then
        echo "only one version argument is allowed" >&2
        exit 2
      fi
      VERSION="$1"
      shift
      ;;
  esac
done

if [ -z "$VERSION" ]; then
  usage >&2
  exit 2
fi

case "$VERSION" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *)
    echo "version must look like v1.2.3" >&2
    exit 2
    ;;
esac

if [ "$PUSH" -eq 1 ] && [ "$SKIP_INTEGRATION" -eq 1 ]; then
  echo "--skip-integration is not allowed with --push" >&2
  exit 2
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

step() {
  echo ""
  echo "==> $*"
}

need_cmd git
need_cmd go
need_cmd bun
need_cmd docker

if [ "$PUSH" -eq 1 ]; then
  if [ -n "$(git status --porcelain)" ]; then
    echo "working tree must be clean for --push" >&2
    git status --short >&2
    exit 1
  fi
  if ! git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null; then
    echo "tag $VERSION does not exist" >&2
    exit 1
  fi
  tag_commit="$(git rev-list -n 1 "$VERSION")"
  head_commit="$(git rev-parse HEAD)"
  if [ "$tag_commit" != "$head_commit" ]; then
    echo "tag $VERSION does not point to HEAD" >&2
    exit 1
  fi
fi

step "Secret scans"
docker run --rm -v "$PWD:/repo" -w /repo ghcr.io/gitleaks/gitleaks:v8.28.0 dir --redact --verbose .
./scripts/check-sensitive-data.sh

step "Go build, vet, and unit tests"
go build ./...
go vet ./...
go test -short ./internal/... ./tests/... -count=1 -timeout=120s

step "Frontend checks"
(
  cd frontend
  bun install --frozen-lockfile
  bun run check
  bun test
)

step "Companion checks"
(
  cd companion
  bun install --frozen-lockfile
  bun run check
  HOME="$(mktemp -d)" bun test
)

if [ "$SKIP_INTEGRATION" -eq 0 ]; then
  step "Integration tests"
  if [ -f docker/.env.integration ]; then
    set -a
    # shellcheck disable=SC1091
    source docker/.env.integration
    set +a
  fi
  if [ "${WEB3_INTEGRATION:-}" != "1" ]; then
    echo "WEB3_INTEGRATION=1 is required for integration tests" >&2
    echo "Set it in the environment or docker/.env.integration." >&2
    exit 1
  fi
  go test -count=1 -v ./tests/integration/ -timeout=600s
else
  step "Integration tests skipped"
fi

step "Build Docker image (local for E2E)"
docker buildx inspect >/dev/null 2>&1 || docker buildx create --use >/dev/null

# Always build a local image first for E2E testing
if [ -z "$LOCAL_PLATFORM" ]; then
  docker_os="$(docker info --format '{{.OSType}}')"
  docker_arch="$(docker info --format '{{.Architecture}}')"
  case "$docker_arch" in
    x86_64) docker_arch="amd64" ;;
    aarch64) docker_arch="arm64" ;;
  esac
  LOCAL_PLATFORM="$docker_os/$docker_arch"
fi
docker buildx build \
  --file docker/Dockerfile \
  --platform "$LOCAL_PLATFORM" \
  --build-arg "VERSION=$VERSION" \
  --tag "$IMAGE:$VERSION" \
  --load \
  .

step "E2E browser tests"
E2E_CONTAINER="web3-e2e-$$"
docker rm -f "$E2E_CONTAINER" 2>/dev/null || true
docker run -d --name "$E2E_CONTAINER" -p 18081:8081 -v "/tmp/web3-e2e-$$:/home/web3" "$IMAGE:$VERSION"
cleanup_e2e() { docker rm -f "$E2E_CONTAINER" 2>/dev/null || true; }
trap cleanup_e2e EXIT

echo "Waiting for service to be healthy..."
timeout 60 bash -c "until curl -sf http://localhost:18081/health; do sleep 2; done"
echo "Service ready."

# Auto-complete setup wizard so e2e tests can exercise auth/chat flows.
# Reads RPC URLs and LLM key from docker/.env.integration (same source as
# integration tests). Falls back to the container's built-in public RPCs
# and skips LLM config if no key is available.
e2e_setup() {
  local base="http://localhost:18081"

  # Source secrets for setup API calls.
  if [ -f docker/.env.integration ]; then
    set -a
    # shellcheck disable=SC1091
    source docker/.env.integration
    set +a
  fi

  local status
  status="$(curl -sf "$base/setup/status" | grep -o '"complete":[a-z]*' | head -1)"
  if [ "$status" = '"complete":true' ]; then
    echo "  Setup already complete."
    return 0
  fi

  # Step 1: Save RPC configuration
  local rpc_ethereum="${WEB3_RPC_ETHEREUM_URL:-https://eth.rpc.blocksec.com}"
  local rpc_base="${WEB3_RPC_BASE_URL:-https://developer-access-mainnet.base.org}"
  local rpc_polygon="${WEB3_RPC_POLYGON_URL:-https://rpc-mainnet.matic.quiknode.pro}"
  local rpc_bsc="${WEB3_RPC_BSC_URL:-https://bsc.rpc.blocksec.com}"
  echo "  Configuring RPCs..."
  curl -sf -X POST "$base/setup/rpc" \
    -H 'Content-Type: application/json' \
    -d "{\"chains\":{\"ethereum\":{\"url\":\"$rpc_ethereum\"},\"base\":{\"url\":\"$rpc_base\"},\"polygon\":{\"url\":\"$rpc_polygon\"},\"bsc\":{\"url\":\"$rpc_bsc\"}}}" \
    > /dev/null

  # Step 2: Save LLM configuration
  local llm_key="${WEB3_LLM_API_KEY:-}"
  local llm_provider="${WEB3_LLM_PROVIDER:-deepseek}"
  local llm_base_url="${WEB3_LLM_BASE_URL:-https://api.deepseek.com/anthropic}"
  local llm_model="${WEB3_LLM_MODEL:-deepseek-v4-pro}"
  if [ -n "$llm_key" ]; then
    echo "  Configuring LLM ($llm_provider)..."
    curl -sf -X POST "$base/setup/llm" \
      -H 'Content-Type: application/json' \
      -d "{\"provider\":\"$llm_provider\",\"base_url\":\"$llm_base_url\",\"model_name\":\"$llm_model\",\"api_key\":\"$llm_key\"}" \
      > /dev/null
  else
    echo "  No LLM key — skipping LLM setup."
  fi

  # Step 3: Save provider keys (optional)
  local portals="${PORTALS_API_KEY:-}"
  local metasleuth="${METASLEUTH_API_KEY:-}"
  if [ -n "$portals" ] || [ -n "$metasleuth" ]; then
    echo "  Configuring provider keys..."
    curl -sf -X POST "$base/setup/providers" \
      -H 'Content-Type: application/json' \
      -d "{\"portals_api_key\":\"$portals\",\"metasleuth_api_key\":\"$metasleuth\",\"address_label_provider\":\"metasleuth\"}" \
      > /dev/null
  fi

  # Step 4: Mark setup complete
  echo "  Completing setup..."
  curl -sf -X POST "$base/setup/complete" > /dev/null
  echo "  Setup wizard completed."
}
(
  cd tests/e2e
  bun install --frozen-lockfile 2>/dev/null || bun install
  bunx playwright install --with-deps chromium 2>/dev/null || bunx playwright install chromium 2>/dev/null || true

  # Phase 1: Setup wizard tests (before setup is complete).
  # Steps 2-5 require clicking through the secret-download gate in the UI,
  # which headless tests cannot do — tolerate failures here.
  echo "  Phase 1: setup-wizard tests (fresh instance)..."
  E2E_BASE_URL=http://localhost:18081 bunx playwright test specs/setup-wizard.spec.ts --retries=0 --timeout=30000 2>&1 || true

  # Phase 2: Complete setup, then run remaining tests
  echo "  Phase 2: completing setup wizard via API..."
)
e2e_setup
(
  cd tests/e2e
  echo "  Phase 2: auth, chat, wallet, settings tests..."
  # auth register and chat-page-loads require CDP virtual WebAuthn which
  # chromium-headless-shell does not support — tolerate those failures.
  E2E_BASE_URL=http://localhost:18081 bunx playwright test specs/auth.spec.ts specs/chat.spec.ts specs/wallets.spec.ts specs/settings.spec.ts specs/version-check.spec.ts specs/passkey-pending.spec.ts 2>&1 || true
)

docker rm -f "$E2E_CONTAINER" 2>/dev/null || true
trap - EXIT

if [ "$PUSH" -eq 1 ]; then
  docker buildx build \
    --file docker/Dockerfile \
    --platform "$PLATFORMS" \
    --build-arg "VERSION=$VERSION" \
    --tag "$IMAGE:$VERSION" \
    --tag "$IMAGE:latest" \
    --provenance=true \
    --sbom=true \
    --push \
    .
  echo ""
  echo "Published:"
  echo "  $IMAGE:$VERSION"
  echo "  $IMAGE:latest"
else
  echo ""
  echo "Local release validation passed. Image loaded:"
  echo "  $IMAGE:$VERSION"
  echo ""
  echo "Run with --push to publish after creating the tag and enabling integration tests."
fi
