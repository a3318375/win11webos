#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
cd "$ROOT"
umask 077
mkdir -p data logs tmp cache storage run
export TMPDIR="$ROOT/tmp" XDG_CACHE_HOME="$ROOT/cache"
export CLOUD_DATA_DIR="$ROOT/data" CLOUD_STATIC_DIR="$ROOT/www"
export CLOUD_API_ADDR="${CLOUD_API_ADDR:-127.0.0.1:9999}"
export CLOUD_LOCAL_ROOTS="${CLOUD_LOCAL_ROOTS:-$ROOT/storage}"
if [[ -f "$ROOT/config.sh" ]]; then source "$ROOT/config.sh"; fi
exec "$ROOT/bin/webos"
