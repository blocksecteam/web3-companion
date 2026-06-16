#!/usr/bin/env sh
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

cd "$script_dir"

: "${WEB3_PORT:=8081}"
: "${WEB3_DATA_DIR:=./data-${WEB3_PORT}}"
: "${COMPOSE_PROJECT_NAME:=web3-companion-${WEB3_PORT}}"
: "${WEB3_UID:=$(id -u)}"
: "${WEB3_GID:=$(id -g)}"
export WEB3_PORT WEB3_DATA_DIR COMPOSE_PROJECT_NAME WEB3_UID WEB3_GID

mkdir -p "$WEB3_DATA_DIR"

docker compose -f compose.yml up --build --force-recreate --remove-orphans "$@"
