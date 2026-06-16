#!/usr/bin/env sh
# Copyright (c) 2026 BlockSec (https://blocksec.com).
# Licensed under the MIT License.

set -eu

: "${HOME:=/home/web3}"
: "${WEB3_WORKDIR:=$HOME}"
export HOME WEB3_WORKDIR

mkdir -p "$HOME" "$WEB3_WORKDIR"

if [ "$(id -u)" = "0" ]; then
  : "${WEB3_UID:=1000}"
  : "${WEB3_GID:=1000}"
  if ! echo "$WEB3_UID" | grep -Eq '^[0-9]+$' || ! echo "$WEB3_GID" | grep -Eq '^[0-9]+$'; then
    echo "WEB3_UID and WEB3_GID must be numeric" >&2
    exit 1
  fi
  export USER=web3
  export LOGNAME=web3
  chown -R "$WEB3_UID:$WEB3_GID" /opt/web3
  chown -R "$WEB3_UID:$WEB3_GID" "$HOME" "$WEB3_WORKDIR"
  cd /opt/web3
  exec gosu "$WEB3_UID:$WEB3_GID" env HOME="$HOME" WEB3_WORKDIR="$WEB3_WORKDIR" USER=web3 LOGNAME=web3 "$@"
fi

cd /opt/web3
exec "$@"
