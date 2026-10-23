#!/usr/bin/env bash
set -euo pipefail
SOURCE=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TARGET=${1:-/opt/webos}
PORT=${2:-9999}
if [[ ! "$PORT" =~ ^[0-9]{1,5}$ ]] || ((10#$PORT < 1 || 10#$PORT > 65535)); then
  echo 'Port must be between 1 and 65535.' >&2
  exit 1
fi
mkdir -p "$TARGET"
TARGET=$(cd -- "$TARGET" && pwd)
if [[ "$TARGET" == / || "$TARGET" == "$SOURCE" ]]; then
  echo 'Choose a dedicated installation directory different from the extracted package.' >&2
  exit 1
fi
if [[ -f "$TARGET/run/webos.pid" ]] && kill -0 "$(cat "$TARGET/run/webos.pid")" 2>/dev/null; then
  echo 'Stop the existing installation before updating.' >&2
  exit 1
fi
mkdir -p "$TARGET"/{bin,www,data,logs,tmp,cache,storage,run}
cp "$SOURCE/bin/webos" "$TARGET/bin/webos"
cp -R "$SOURCE/www/." "$TARGET/www/"
cp "$SOURCE/run.sh" "$SOURCE/webos.sh" "$TARGET/"
chmod +x "$TARGET/bin/webos" "$TARGET/run.sh" "$TARGET/webos.sh"
chmod 700 "$TARGET/data" "$TARGET/logs" "$TARGET/tmp" "$TARGET/run"
if [[ ! -f "$TARGET/config.sh" ]]; then
  printf '%s\n' '# Optional overrides; local access only until login is implemented.' '# export CLOUD_LOCAL_ROOTS="/data:/st"' > "$TARGET/config.sh"
fi
if [[ $# -ge 2 ]] || ! grep -q '^export CLOUD_API_ADDR=' "$TARGET/config.sh"; then
  printf 'export CLOUD_API_ADDR="127.0.0.1:%s"\n' "$PORT" >> "$TARGET/config.sh"
fi
bash "$TARGET/webos.sh" start
printf 'Installed to %s\nDefault URL: http://localhost:%s (see config.sh for overrides)\n' "$TARGET" "$PORT"
