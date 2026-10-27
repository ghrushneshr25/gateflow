#!/bin/sh
set -eu
: "${DATABASE_URL:?DATABASE_URL required}"
# psql connection from URL-ish env; prefer PG* vars
export PGHOST="${PGHOST:-postgres}"
export PGPORT="${PGPORT:-5432}"
export PGUSER="${PGUSER:-gateflow}"
export PGPASSWORD="${PGPASSWORD:-gateflow}"
export PGDATABASE="${PGDATABASE:-gateflow}"

echo "waiting for postgres ${PGHOST}:${PGPORT}..."
i=0
while [ "$i" -lt 60 ]; do
  if pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" >/dev/null 2>&1; then
    break
  fi
  i=$((i + 1))
  sleep 2
done
pg_isready -h "$PGHOST" -p "$PGPORT" -U "$PGUSER"

echo "init schemas/roles (idempotent)"
psql -v ON_ERROR_STOP=1 -f /migrate/init.sql

apply() {
  schema="$1"
  dir="$2"
  echo "==> migrations schema=${schema}"
  for f in $(ls "$dir"/*.up.sql 2>/dev/null | sort); do
    echo "  $(basename "$f")"
    {
      echo "SET search_path TO ${schema};"
      cat "$f"
    } | psql -v ON_ERROR_STOP=1
  done
}

apply sale /migrate/sale
apply admission /migrate/admission
apply inventory /migrate/inventory
apply '"order"' /migrate/order
apply payment /migrate/payment
echo MIGRATIONS_OK
