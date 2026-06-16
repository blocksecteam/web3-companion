#!/usr/bin/env sh
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -eu

found=0

report() {
  echo "::error::$1" >&2
  found=1
}

echo "Checking tracked sensitive file names..."
tracked_sensitive="$(
  git ls-files \
    | grep -Ei '(^|/)(\.env|.*\.(pem|key|p12|pfx)|id_rsa|id_ed25519|credentials|keystore|wallet\.json|secret(s)?\.(json|toml|ya?ml|env))$' \
    | grep -Eiv '(^|/)(\.env\.example|.*\.example\.(toml|json|ya?ml|env)|example.*|.*_test\.(go|ts|tsx)|.*\.template)$' \
    || true
)"
if [ -n "$tracked_sensitive" ]; then
  echo "$tracked_sensitive" >&2
  report "Sensitive-looking files are tracked in git"
fi

echo "Checking EVM private key assignments..."
private_key_matches="$(
  git grep -nIE '(^|[^A-Za-z0-9_])(private[_-]?key|wallet[_-]?key|signer[_-]?key|deployer[_-]?key|secret[_-]?key)[A-Za-z0-9_.-]*["'\'']?[[:space:]]*[:=][[:space:]]*["'\'']?(0x)?[0-9a-fA-F]{64}(["'\'']|[[:space:]]|$)' -- \
    ':!**/*_test.go' \
    ':!**/*.test.ts' \
    ':!**/*.test.tsx' \
    ':!**/*.lock' \
    ':!docker/data*' \
    ':!docs/**' \
    || true
)"
if [ -n "$private_key_matches" ]; then
  echo "$private_key_matches" >&2
  report "Possible EVM private key assignment found"
fi

echo "Checking mnemonic or seed phrase assignments..."
mnemonic_matches="$(
  git grep -nIE '(^|[^A-Za-z0-9_])(mnemonic|seed[_-]?phrase|recovery[_-]?phrase)[A-Za-z0-9_.-]*["'\'']?[[:space:]]*[:=][[:space:]]*["'\''][a-z]+( [a-z]+){11,23}["'\'']' -- \
    ':!**/*_test.go' \
    ':!**/*.test.ts' \
    ':!**/*.test.tsx' \
    ':!**/*.lock' \
    ':!docker/data*' \
    ':!docs/**' \
    || true
)"
if [ -n "$mnemonic_matches" ]; then
  echo "$mnemonic_matches" >&2
  report "Possible mnemonic or seed phrase assignment found"
fi

echo "Checking Docker build ignore coverage..."
for ignore_file in ".dockerignore" "docker/Dockerfile.dockerignore"; do
  if [ ! -f "$ignore_file" ]; then
    report "$ignore_file is required because Docker builds use the repository root as context"
    continue
  fi
  for pattern in '.env' '.env.*' 'docker/.env' 'docker/.env.*' 'docker/config.toml' 'docker/data*' '*.pem' '*.key' '*.db' '.git' 'AGENTS.md' 'CLAUDE.md' 'memory' '.doc-preview'; do
    if ! grep -Fxq "$pattern" "$ignore_file"; then
      report "$ignore_file must ignore $pattern"
    fi
  done
done

if [ "$found" -ne 0 ]; then
  exit 1
fi

echo "Project-specific sensitive data checks passed."
