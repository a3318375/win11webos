#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
PIDFILE="$ROOT/run/webos.pid"
mkdir -p "$ROOT/run" "$ROOT/logs"
running() {
  [[ -f "$PIDFILE" ]] || return 1
  pid=$(cat "$PIDFILE")
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  [[ "$(readlink -f "/proc/$pid/exe" 2>/dev/null)" == "$ROOT/bin/webos" ]] && kill -0 "$pid" 2>/dev/null
}
case "${1:-status}" in
start)
  if running; then echo "Already running: $pid"; exit 0; fi
  # Keep only the current and previous run's log.
  if [[ -f "$ROOT/logs/server.log" ]]; then mv -f "$ROOT/logs/server.log" "$ROOT/logs/server.previous.log"; fi
  nohup bash "$ROOT/run.sh" > "$ROOT/logs/server.log" 2>&1 < /dev/null &
  echo $! > "$PIDFILE"
  sleep 1
  if running; then echo "Started: $pid"; else echo "Start failed; see $ROOT/logs/server.log" >&2; exit 1; fi
  ;;
stop)
  if running; then
    kill -TERM "$pid"
    for ((i=0;i<20;i++)); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
    if kill -0 "$pid" 2>/dev/null; then echo 'Still shutting down; PID retained.' >&2; exit 1; fi
  fi
  rm -f "$PIDFILE"
  echo Stopped
  ;;
status) if running; then echo "Running: $pid"; else echo Stopped; fi ;;
*) echo 'Usage: webos.sh start|stop|status' >&2; exit 1 ;;
esac
