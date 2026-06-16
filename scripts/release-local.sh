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

(
  cd tests/e2e
  bun install --frozen-lockfile 2>/dev/null || bun install
  E2E_BASE_URL=http://localhost:18081 bunx playwright install --with-deps chromium 2>/dev/null
  E2E_BASE_URL=http://localhost:18081 bunx playwright test
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
