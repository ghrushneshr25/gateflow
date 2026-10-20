#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
docker compose up -d postgres >/dev/null
for i in $(seq 1 40); do
  if docker compose exec -T postgres pg_isready -U gateflow -d gateflow >/dev/null 2>&1; then break; fi
  sleep 0.5
done

run_sql() {
  local schema="$1"
  local file="$2"
  echo "  $(basename "$file") @ $schema"
  # wrap file in search_path
  {
    echo "SET search_path TO ${schema};"
    cat "$file"
  } | docker compose exec -T postgres psql -U gateflow -d gateflow -v ON_ERROR_STOP=1
}

apply() {
  local service="$1"
  local schema="$2"
  echo "==> $service ($schema)"
  local dir="$ROOT/services/$service/migrations"
  for f in $(ls "$dir"/*.up.sql | sort); do
    run_sql "$schema" "$f"
  done
}

apply sale sale
apply admission admission
apply inventory inventory
apply order '"order"'
apply payment-simulator payment
echo MIGRATIONS_OK
