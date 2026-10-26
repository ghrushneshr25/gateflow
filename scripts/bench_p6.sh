#!/usr/bin/env bash
# P6.0 reproducible local baseline harness wrapper.
# Requires: postgres, kafka, five Go services on :8081–8085, GATEFLOW_DEV_JWT_SECRET.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export GATEFLOW_DEV_JWT_SECRET="${GATEFLOW_DEV_JWT_SECRET:-gateflow-dev-secret-change-me}"
export DATABASE_URL="${DATABASE_URL:-postgres://gateflow:gateflow@localhost:5432/gateflow?sslmode=disable}"

mkdir -p docs/phase-6/results
make build >/dev/null
(cd tools/bench && go build -o ../../bin/bench .)

# Optional: reset global admission budget so prior soaks do not exhaust grants.
if docker exec gateflow-postgres-1 psql -U gateflow -d gateflow -c \
  "UPDATE admission.budget_scopes SET grants_issued=0, active_checkouts=0, max_grants=1000000, max_active_checkouts=100000 WHERE scope_key='global';" \
  >/dev/null 2>&1; then
  echo "reset admission global budget"
fi

SCENARIO="${1:-all}"
OUT_DIR=docs/phase-6/results
case "$SCENARIO" in
  A|a) ./bin/bench -scenario A -c 5 -d 20s -warmup 0 -capacity 10000 -out "$OUT_DIR/A-baseline.json" ;;
  B|b) ./bin/bench -scenario B -c 10 -d 15s -warmup 0 -capacity 10000 -out "$OUT_DIR/B-concurrent.json" ;;
  C|c) ./bin/bench -scenario C -c 20 -d 8s -warmup 0 -capacity 50000 -out "$OUT_DIR/C-spike.json" ;;
  D|d) ./bin/bench -scenario D -c 20 -d 15s -warmup 0 -capacity 5 -out "$OUT_DIR/D-flash.json" ;;
  E|e) ./bin/bench -scenario E -c 8 -d 10s -warmup 0 -capacity 5000 -out "$OUT_DIR/E-async.json" ;;
  F|f) ./bin/bench -scenario F -c 2 -d 5s -warmup 0 -capacity 1000 -out "$OUT_DIR/F-recovery.json" ;;
  all)
    for s in A B C D E F; do
      docker exec gateflow-postgres-1 psql -U gateflow -d gateflow -c \
        "UPDATE admission.budget_scopes SET grants_issued=0, active_checkouts=0 WHERE scope_key='global';" >/dev/null 2>&1 || true
      "$0" "$s"
    done
    ;;
  *) echo "usage: $0 [A|B|C|D|E|F|all]"; exit 2 ;;
esac
