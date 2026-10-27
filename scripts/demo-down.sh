#!/usr/bin/env bash
# Stop Gateflow demo host processes started by scripts/demo.sh.
# Only terminates PIDs recorded under .demo-logs/ or listeners whose command
# clearly matches this repo's bin/{sale,admission,inventory,order,payment-simulator}.
# Does not kill unrelated apps on :8081–:8085.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="${GATEFLOW_DEMO_LOG_DIR:-$ROOT/.demo-logs}"
BINS='sale|admission|inventory|order|payment-simulator'

kill_pid() {
  local pid=$1 reason=$2
  if ! kill -0 "$pid" 2>/dev/null; then
    return 0
  fi
  echo "stopping pid=$pid ($reason)"
  kill "$pid" 2>/dev/null || true
  sleep 0.2
  if kill -0 "$pid" 2>/dev/null; then
    kill -9 "$pid" 2>/dev/null || true
  fi
}

# 1) Prefer PIDs written by demo.sh
if [[ -d "$LOG_DIR" ]]; then
  for f in "$LOG_DIR"/*.pid; do
    [[ -f "$f" ]] || continue
    pid=$(cat "$f" 2>/dev/null || true)
    if [[ -n "${pid:-}" ]]; then
      kill_pid "$pid" "demo pidfile $(basename "$f")"
    fi
    rm -f "$f"
  done
fi

# 2) Fallback: only listeners whose command line is this repo's service binary
if command -v lsof >/dev/null 2>&1; then
  for p in 8081 8082 8083 8084 8085; do
    while read -r pid; do
      [[ -n "$pid" ]] || continue
      cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
      # Match .../gateflow/bin/<service> or ./bin/<service>
      if echo "$cmd" | grep -Eq "(${ROOT}/bin/(${BINS})|/bin/(${BINS}))( |$)"; then
        kill_pid "$pid" "gateflow bin on :$p"
      else
        if [[ -n "$cmd" ]]; then
          echo "skip :$p pid=$pid (not a Gateflow demo binary): $cmd"
        fi
      fi
    done < <(lsof -tiTCP:"$p" -sTCP:LISTEN 2>/dev/null || true)
  done
fi

echo "Demo host processes cleaned (data plane docker compose left running)."
echo "  docker compose down   # optional full data-plane stop"
