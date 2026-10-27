#!/usr/bin/env bash
# Gateflow local lab demo — clone → proof in one script (portfolio / interview).
# Does not claim cloud staging or production readiness.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export DATABASE_URL="${DATABASE_URL:-postgres://gateflow:gateflow@localhost:5432/gateflow?sslmode=disable}"
export ORDER_DATABASE_URL="${ORDER_DATABASE_URL:-$DATABASE_URL}"
export GATEFLOW_DEV_JWT_SECRET="${GATEFLOW_DEV_JWT_SECRET:-gateflow-dev-secret-change-me}"
export ENVIRONMENT="${ENVIRONMENT:-local}"
export KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:9092}"
export KAFKA_ENABLED="${KAFKA_ENABLED:-true}"
export GATEFLOW_ADMISSION_GRANT_PROTOCOL="${GATEFLOW_ADMISSION_GRANT_PROTOCOL:-A}"

LOG_DIR="${GATEFLOW_DEMO_LOG_DIR:-$ROOT/.demo-logs}"
mkdir -p "$LOG_DIR" bin

need() { command -v "$1" >/dev/null || { echo "missing dependency: $1" >&2; exit 1; }; }
need docker
need go
need curl
need python3

health() {
  curl -sf --max-time 2 "http://127.0.0.1:$1/healthz" >/dev/null 2>&1
}

wait_health() {
  local port=$1 name=$2
  for _ in $(seq 1 60); do
    health "$port" && return 0
    sleep 0.5
  done
  echo "timeout waiting for $name on :$port" >&2
  tail -n 40 "$LOG_DIR/${name}.log" 2>/dev/null || true
  exit 1
}

start_svc() {
  local name=$1 port=$2
  if health "$port"; then
    echo "  · $name already up :$port"
    return 0
  fi
  # Free stale listener if any
  if lsof -tiTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "  · stopping stale process on :$port"
    lsof -tiTCP:"$port" -sTCP:LISTEN | xargs kill -9 2>/dev/null || true
    sleep 0.5
  fi
  local extra=()
  case "$name" in
    admission) extra+=(GATEFLOW_ADMISSION_GRANT_PROTOCOL="$GATEFLOW_ADMISSION_GRANT_PROTOCOL") ;;
    order) extra+=(ORDER_DATABASE_URL="$ORDER_DATABASE_URL" KAFKA_BROKERS="$KAFKA_BROKERS" KAFKA_ENABLED="$KAFKA_ENABLED") ;;
  esac
  env DATABASE_URL="$DATABASE_URL" GATEFLOW_DEV_JWT_SECRET="$GATEFLOW_DEV_JWT_SECRET" \
    ENVIRONMENT="$ENVIRONMENT" HTTP_ADDR=":$port" "${extra[@]}" \
    "$ROOT/bin/$name" >"$LOG_DIR/${name}.log" 2>&1 &
  echo $! >"$LOG_DIR/${name}.pid"
  wait_health "$port" "$name"
  echo "  · $name ready :$port"
}

echo "== Gateflow demo (local lab) =="
echo "Root: $ROOT"
echo

echo "1) Data plane (postgres, redis, kafka)"
docker compose up -d postgres redis kafka >/dev/null
for i in $(seq 1 40); do
  docker compose exec -T postgres pg_isready -U gateflow -d gateflow >/dev/null 2>&1 && break
  sleep 0.5
done
echo "   ok"

echo "2) Build services"
make build >/dev/null
echo "   ok → bin/{sale,admission,inventory,order,payment-simulator}"

echo "3) Migrations"
./scripts/migrate.sh >/dev/null
echo "   ok"

echo "4) Start services"
start_svc sale 8081
start_svc admission 8082
start_svc inventory 8083
start_svc order 8084
start_svc payment-simulator 8085

echo "5) Protocol check"
META=$(curl -sf http://127.0.0.1:8082/v1/meta)
echo "   $META"
echo "$META" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d.get("grant_protocol")=="A", d'

echo "6) Happy-path booking (VS1)"
./scripts/vs1_e2e.sh
echo

echo "7) Fence drill (fail-closed grants)"
export ROOT
python3 - <<'PY'
import json, os, subprocess, urllib.request
from datetime import datetime, timedelta, timezone

root = os.environ.get("ROOT") or "."
secret = os.environ["GATEFLOW_DEV_JWT_SECRET"]

def tok(kind, *a):
    return subprocess.check_output(
        ["go", "run", "./tools/devtoken", kind, *a],
        cwd=root, text=True, env={**os.environ, "GATEFLOW_DEV_JWT_SECRET": secret},
    ).strip()

ops = tok("operator", "ops:sale:write,ops:sale:publish,ops:admission:fence,ops:admission:read", "demo")
svc = tok("service", "admission:grant,admission:entitlement", "demo")
buyer = "buyer_demo_fence"
bt = tok("buyer", "buyer:self", buyer)

def req(method, url, token=None, data=None):
    h = {"Content-Type": "application/json"}
    if token:
        h["Authorization"] = "Bearer " + token
    body = None if data is None else json.dumps(data).encode()
    r = urllib.request.Request(url, data=body, headers=h, method=method)
    try:
        with urllib.request.urlopen(r, timeout=20) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode() or "{}")

now = datetime.now(timezone.utc)
_, off = req("POST", "http://127.0.0.1:8081/v1/offerings", ops, {"name": "demo-fence", "category": "concert"})
oid = off["id"]
_, sess = req("POST", f"http://127.0.0.1:8081/v1/offerings/{oid}/sessions", ops, {
    "starts_at": (now + timedelta(days=2)).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "ends_at": (now + timedelta(days=2, hours=2)).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "sale_opens_at": (now - timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "sale_closes_at": (now + timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "max_per_buyer": 4,
})
sid = sess["session"]["id"]
req("POST", f"http://127.0.0.1:8081/v1/sessions/{sid}/ticket-types", ops, {"name": "GA", "capacity": 3, "price_cents": 1000})
req("POST", f"http://127.0.0.1:8081/v1/offerings/{oid}/publish", ops, {})
req("POST", f"http://127.0.0.1:8082/v1/sessions/{sid}/queue/joins", bt, {})
st, c = req("POST", "http://127.0.0.1:8082/v1/admin/coordination/fence", ops, {"reason": "demo"})
assert st == 200 and c.get("mode") == "FENCED", (st, c)
st, g = req("POST", f"http://127.0.0.1:8082/v1/sessions/{sid}/grants", svc, {"buyer_id": buyer})
assert st == 503, (st, g)
st, _ = req("POST", "http://127.0.0.1:8082/v1/admin/coordination/unfence", ops, {})
assert st == 200
st, g = req("POST", f"http://127.0.0.1:8082/v1/sessions/{sid}/grants", svc, {"buyer_id": buyer})
assert st == 200 and "entitlement" in g, (st, g)
print("FENCE_DRILL_PASS")
PY
echo

if [[ "${GATEFLOW_DEMO_FLASH:-0}" == "1" ]] && [[ -x ./bin/bench ]]; then
  echo "8) Flash capacity (optional GATEFLOW_DEMO_FLASH=1)"
  docker exec gateflow-postgres-1 psql -U gateflow -d gateflow -c \
    "UPDATE admission.budget_scopes SET grants_issued=0, active_checkouts=0;" >/dev/null 2>&1 || true
  ./bin/bench -scenario D -c 10 -d 6s -warmup 0 -capacity 5 -out "$LOG_DIR/flash-D.json" || true
  echo "   see $LOG_DIR/flash-D.json"
else
  echo "8) Flash bench skipped (set GATEFLOW_DEMO_FLASH=1 after: go build -o bin/bench ./tools/bench)"
fi

echo
echo "== DEMO_OK =="
echo "Services left running on :8081–:8085 (logs: $LOG_DIR)."
echo "Stop: ./scripts/demo-down.sh   or   make demo-down"
echo "This is a local lab demo — not cloud staging or production certification."
