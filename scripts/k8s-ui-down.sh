#!/usr/bin/env bash
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PFDIR="$ROOT/.demo-logs/k8s-pf"
if [[ -f "$PFDIR/pids" ]]; then
  while read -r pid; do
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  done < "$PFDIR/pids"
  rm -f "$PFDIR/pids"
fi
pkill -f 'kubectl.*port-forward.*gateflow' 2>/dev/null || true
echo "UI port-forwards stopped"
