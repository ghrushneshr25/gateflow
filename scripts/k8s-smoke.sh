#!/usr/bin/env bash
# Smoke tests against Gateflow in kind via port-forward (Protocol A, VS1, fence, optional flash).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/.tools:$PATH"

CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
NS="${GATEFLOW_K8S_NAMESPACE:-gateflow}"
RELEASE="${GATEFLOW_HELM_RELEASE:-gateflow}"
RESULTS="${GATEFLOW_K8S_RESULTS:-$ROOT/docs/phase-8/results}"
mkdir -p "$RESULTS"

kubectl config use-context "kind-$CLUSTER" >/dev/null
export GATEFLOW_DEV_JWT_SECRET="${GATEFLOW_DEV_JWT_SECRET:-gateflow-dev-secret-change-me}"

PF_PIDS=()
cleanup() { for p in "${PF_PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

start_pf() {
  local svc=$1 lport=$2 rport=$3
  kubectl -n "$NS" port-forward "svc/${RELEASE}-$svc" "${lport}:${rport}" >/dev/null 2>&1 &
  PF_PIDS+=($!)
  sleep 0.4
}

echo "port-forward APIs 18081-18085"
start_pf sale 18081 8080
start_pf admission 18082 8080
start_pf inventory 18083 8080
start_pf order 18084 8080
start_pf payment-simulator 18085 8080

for p in 18081 18082 18083 18084 18085; do
  ok=0
  for _ in $(seq 1 40); do
    curl -sf --max-time 2 "http://127.0.0.1:$p/healthz" >/dev/null && { ok=1; break; }
    sleep 0.5
  done
  [[ "$ok" == 1 ]] || { echo "healthz fail :$p"; exit 1; }
done

export SALE_BASE=http://127.0.0.1:18081
export ADMISSION_BASE=http://127.0.0.1:18082
export ORDER_BASE=http://127.0.0.1:18084

echo "=== meta (Protocol A) ==="
curl -sf "$ADMISSION_BASE/v1/meta" | tee "$RESULTS/smoke-meta.json"
python3 -c 'import json;d=json.load(open("docs/phase-8/results/smoke-meta.json"));assert d.get("grant_protocol")=="A",d;print("PROTOCOL_A_OK")'

echo "=== VS1 ==="
./scripts/vs1_e2e.sh | tee "$RESULTS/smoke-vs1.txt"
grep -q VS1_E2E_PASS "$RESULTS/smoke-vs1.txt"

echo "=== fence ==="
ROOT="$ROOT" SALE_BASE="$SALE_BASE" ADMISSION_BASE="$ADMISSION_BASE" \
GATEFLOW_DEV_JWT_SECRET="$GATEFLOW_DEV_JWT_SECRET" python3 - <<'PY' | tee "$RESULTS/smoke-fence.txt"
import json, os, subprocess, urllib.request
from datetime import datetime, timedelta, timezone
root=os.environ["ROOT"]; secret=os.environ["GATEFLOW_DEV_JWT_SECRET"]
sale=os.environ["SALE_BASE"]; adm=os.environ["ADMISSION_BASE"]
def tok(kind,*a):
  return subprocess.check_output(["go","run","./tools/devtoken",kind,*a],cwd=root,text=True,env={**os.environ}).strip()
ops=tok("operator","ops:sale:write,ops:sale:publish,ops:admission:fence,ops:admission:read","smoke")
svc=tok("service","admission:grant,admission:entitlement","smoke")
buyer="buyer_smoke_fence"; bt=tok("buyer","buyer:self",buyer)
def req(method,url,token=None,data=None):
  h={"Content-Type":"application/json"}
  if token: h["Authorization"]="Bearer "+token
  body=None if data is None else json.dumps(data).encode()
  r=urllib.request.Request(url,data=body,headers=h,method=method)
  try:
    with urllib.request.urlopen(r,timeout=30) as resp: return resp.status, json.loads(resp.read())
  except urllib.error.HTTPError as e: return e.code, json.loads(e.read().decode() or "{}")
now=datetime.now(timezone.utc)
_,off=req("POST",f"{sale}/v1/offerings",ops,{"name":"smoke-fence","category":"concert"})
oid=off["id"]
_,sess=req("POST",f"{sale}/v1/offerings/{oid}/sessions",ops,{
  "starts_at":(now+timedelta(days=2)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "ends_at":(now+timedelta(days=2,hours=2)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "sale_opens_at":(now-timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "sale_closes_at":(now+timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "max_per_buyer":4})
sid=sess["session"]["id"]
req("POST",f"{sale}/v1/sessions/{sid}/ticket-types",ops,{"name":"GA","capacity":3,"price_cents":1000})
req("POST",f"{sale}/v1/offerings/{oid}/publish",ops,{})
req("POST",f"{adm}/v1/sessions/{sid}/queue/joins",bt,{})
st,c=req("POST",f"{adm}/v1/admin/coordination/fence",ops,{"reason":"smoke"})
assert st==200 and c.get("mode")=="FENCED",(st,c)
st,g=req("POST",f"{adm}/v1/sessions/{sid}/grants",svc,{"buyer_id":buyer})
assert st==503,(st,g)
st,_=req("POST",f"{adm}/v1/admin/coordination/unfence",ops,{})
assert st==200
st,g=req("POST",f"{adm}/v1/sessions/{sid}/grants",svc,{"buyer_id":buyer})
assert st==200 and "entitlement" in g,(st,g)
print("FENCE_DRILL_PASS")
PY
grep -q FENCE_DRILL_PASS "$RESULTS/smoke-fence.txt"

if [[ "${GATEFLOW_SMOKE_FLASH:-1}" == "1" ]]; then
  echo "=== flash ==="
  go build -o bin/bench ./tools/bench
  ./bin/bench -scenario D -c 8 -d 6s -warmup 0 -capacity 5 \
    -sale "$SALE_BASE" -admission "$ADMISSION_BASE" -order "$ORDER_BASE" \
    -out "$RESULTS/smoke-flash-D.json" 2>&1 | tee "$RESULTS/smoke-flash-D.txt" || true
fi

echo "SMOKE_OK"
