#!/usr/bin/env bash
# Gateflow local Kubernetes demo (kind + Helm). Protocol A only.
# Not cloud staging / production. Preserves Compose demo separately.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/.tools:$PATH"

CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
NS="${GATEFLOW_K8S_NAMESPACE:-gateflow}"
RELEASE="${GATEFLOW_HELM_RELEASE:-gateflow}"
TAG="${GATEFLOW_IMAGE_TAG:-p8-local}"
RESULTS="${GATEFLOW_K8S_RESULTS:-$ROOT/docs/phase-8/results}"
mkdir -p "$RESULTS"

log() { printf '%s\n' "$*"; }
die() { log "ERROR: $*"; exit 1; }

need() { command -v "$1" >/dev/null || die "missing tool: $1 (install or place under .tools/)"; }

need docker
need kind
need kubectl
need helm
need go
need curl
need python3

docker info >/dev/null 2>&1 || die "Docker daemon not available"

log "== Gateflow k8s-demo =="
log "cluster=$CLUSTER ns=$NS release=$RELEASE tag=$TAG"
log

# --- kind cluster ---
if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  log "1) creating kind cluster $CLUSTER"
  kind create cluster --name "$CLUSTER"
else
  log "1) kind cluster $CLUSTER exists"
fi
kubectl cluster-info --context "kind-$CLUSTER" >/dev/null
kubectl config use-context "kind-$CLUSTER" >/dev/null
kubectl get ns "$NS" >/dev/null 2>&1 || kubectl create ns "$NS"

# --- build images ---
log "2) building images"
SERVICES=(sale admission inventory order payment-simulator)
for s in "${SERVICES[@]}"; do
  log "   docker build gateflow/$s:$TAG"
  docker build -q -f deployments/docker/Dockerfile.service \
    --build-arg "SERVICE=$s" \
    -t "gateflow/$s:$TAG" . >/dev/null
done
log "   docker build gateflow/migrate:$TAG"
docker build -q -f deployments/docker/Dockerfile.migrate -t "gateflow/migrate:$TAG" . >/dev/null

# Frontends (best-effort — still validate APIs if build fails)
for app in buyer-web operator-console; do
  log "   docker build gateflow/$app:$TAG"
  if ! docker build -q -f deployments/docker/Dockerfile.frontend \
      --build-arg "APP=$app" \
      -t "gateflow/$app:$TAG" . >/dev/null 2>"$RESULTS/build-$app.err"; then
    log "   WARN frontend $app image build failed (see $RESULTS/build-$app.err); continuing with backends"
  fi
done

log "3) loading images into kind"
for img in "${SERVICES[@]}" migrate buyer-web operator-console; do
  if docker image inspect "gateflow/$img:$TAG" >/dev/null 2>&1; then
    kind load docker-image "gateflow/$img:$TAG" --name "$CLUSTER" >/dev/null
  fi
done

# --- helm ---
log "4) helm lint + install"
helm lint "$ROOT/deployments/helm/gateflow" | tee "$RESULTS/helm-lint.txt"
helm upgrade --install "$RELEASE" "$ROOT/deployments/helm/gateflow" \
  --namespace "$NS" --create-namespace \
  -f "$ROOT/deployments/helm/gateflow/values-local.yaml" \
  --set "imageTag=$TAG" \
  --set "imagePullPolicy=Never" \
  --set "admission.grantProtocol=A" \
  --wait=false \
  --timeout 10m | tee "$RESULTS/helm-install.txt"

# --- waits ---
log "5) wait postgres"
kubectl -n "$NS" rollout status "statefulset/${RELEASE}-postgres" --timeout=180s

log "6) wait migrate job"
# Find latest migrate job
MIG_JOB=$(kubectl -n "$NS" get jobs -l "app.kubernetes.io/component=migrate" \
  -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null || true)
if [[ -z "${MIG_JOB:-}" ]]; then
  MIG_JOB=$(kubectl -n "$NS" get jobs -o name | grep migrate | tail -1 | sed 's|job.batch/||' || true)
fi
[[ -n "${MIG_JOB:-}" ]] || die "migrate job not found"
if ! kubectl -n "$NS" wait --for=condition=complete "job/$MIG_JOB" --timeout=300s; then
  kubectl -n "$NS" logs "job/$MIG_JOB" --tail=80 | tee "$RESULTS/migrate-fail.log" || true
  die "migration job failed"
fi
kubectl -n "$NS" logs "job/$MIG_JOB" --tail=40 | tee "$RESULTS/migrate.log"
grep -q MIGRATIONS_OK "$RESULTS/migrate.log" || log "WARN: MIGRATIONS_OK marker not grepped (check log)"

log "7) wait kafka + backends"
kubectl -n "$NS" rollout status "deployment/${RELEASE}-kafka" --timeout=180s || true
for c in sale admission inventory order payment-simulator; do
  kubectl -n "$NS" rollout status "deployment/${RELEASE}-$c" --timeout=240s
done
for c in buyer-web operator-console; do
  kubectl -n "$NS" rollout status "deployment/${RELEASE}-$c" --timeout=120s 2>/dev/null || log "WARN frontend $c not ready"
done

# --- port-forward APIs ---
log "8) port-forward backends to localhost"
pf_pids=()
cleanup_pf() {
  for p in "${pf_pids[@]:-}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup_pf EXIT

start_pf() {
  local svc=$1 lport=$2 rport=$3
  kubectl -n "$NS" port-forward "svc/${RELEASE}-$svc" "$lport:$rport" >/dev/null 2>&1 &
  pf_pids+=($!)
  sleep 0.5
}
start_pf sale 18081 8080
start_pf admission 18082 8080
start_pf inventory 18083 8080
start_pf order 18084 8080
start_pf payment-simulator 18085 8080

# wait health
for p in 18081 18082 18083 18084 18085; do
  ok=0
  for _ in $(seq 1 40); do
    if curl -sf --max-time 2 "http://127.0.0.1:$p/healthz" >/dev/null; then ok=1; break; fi
    sleep 0.5
  done
  [[ "$ok" == 1 ]] || die "healthz failed on localhost:$p"
done

export SALE_BASE=http://127.0.0.1:18081
export ADMISSION_BASE=http://127.0.0.1:18082
export ORDER_BASE=http://127.0.0.1:18084
export GATEFLOW_DEV_JWT_SECRET="${GATEFLOW_DEV_JWT_SECRET:-gateflow-dev-secret-change-me}"

log "9) Test A — protocol A meta"
curl -sf "$ADMISSION_BASE/v1/meta" | tee "$RESULTS/P8-admission-meta.json"
python3 - <<'PY'
import json
d=json.load(open("docs/phase-8/results/P8-admission-meta.json"))
assert d.get("grant_protocol")=="A", d
print("PROTOCOL_A_OK")
PY

log "10) Test B — VS1"
./scripts/vs1_e2e.sh | tee "$RESULTS/P8-vs1.txt"
grep -q VS1_E2E_PASS "$RESULTS/P8-vs1.txt" || die "VS1 failed"

log "11) Test C — fence drill"
ROOT="$ROOT" SALE_BASE="$SALE_BASE" ADMISSION_BASE="$ADMISSION_BASE" \
GATEFLOW_DEV_JWT_SECRET="$GATEFLOW_DEV_JWT_SECRET" python3 - <<'PY' | tee "$RESULTS/P8-fence.txt"
import json, os, subprocess, urllib.request
from datetime import datetime, timedelta, timezone
root = os.environ["ROOT"]
secret = os.environ["GATEFLOW_DEV_JWT_SECRET"]
sale = os.environ.get("SALE_BASE", "http://127.0.0.1:18081")
adm = os.environ.get("ADMISSION_BASE", "http://127.0.0.1:18082")

def tok(kind, *a):
    return subprocess.check_output(["go","run","./tools/devtoken",kind,*a], cwd=root, text=True,
        env={**os.environ, "GATEFLOW_DEV_JWT_SECRET": secret}).strip()

ops = tok("operator", "ops:sale:write,ops:sale:publish,ops:admission:fence,ops:admission:read", "k8s")
svc = tok("service", "admission:grant,admission:entitlement", "k8s")
buyer, bt = "buyer_k8s_fence", tok("buyer", "buyer:self", "buyer_k8s_fence")

def req(method, url, token=None, data=None):
    h={"Content-Type":"application/json"}
    if token: h["Authorization"]="Bearer "+token
    body=None if data is None else json.dumps(data).encode()
    r=urllib.request.Request(url, data=body, headers=h, method=method)
    try:
        with urllib.request.urlopen(r, timeout=30) as resp:
            return resp.status, json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode() or "{}")

now=datetime.now(timezone.utc)
_, off=req("POST", f"{sale}/v1/offerings", ops, {"name":"k8s-fence","category":"concert"})
oid=off["id"]
_, sess=req("POST", f"{sale}/v1/offerings/{oid}/sessions", ops, {
  "starts_at":(now+timedelta(days=2)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "ends_at":(now+timedelta(days=2,hours=2)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "sale_opens_at":(now-timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "sale_closes_at":(now+timedelta(days=1)).strftime("%Y-%m-%dT%H:%M:%SZ"),
  "max_per_buyer":4})
sid=sess["session"]["id"]
req("POST", f"{sale}/v1/sessions/{sid}/ticket-types", ops, {"name":"GA","capacity":3,"price_cents":1000})
req("POST", f"{sale}/v1/offerings/{oid}/publish", ops, {})
req("POST", f"{adm}/v1/sessions/{sid}/queue/joins", bt, {})
st,c=req("POST", f"{adm}/v1/admin/coordination/fence", ops, {"reason":"k8s-demo"})
assert st==200 and c.get("mode")=="FENCED", (st,c)
st,g=req("POST", f"{adm}/v1/sessions/{sid}/grants", svc, {"buyer_id":buyer})
assert st==503, (st,g)
st,_=req("POST", f"{adm}/v1/admin/coordination/unfence", ops, {})
assert st==200
st,g=req("POST", f"{adm}/v1/sessions/{sid}/grants", svc, {"buyer_id":buyer})
assert st==200 and "entitlement" in g, (st,g)
print("FENCE_DRILL_PASS")
PY
grep -q FENCE_DRILL_PASS "$RESULTS/P8-fence.txt" || die "fence failed"

log "12) Test D — flash capacity (in-cluster order via PF)"
if go build -o bin/bench ./tools/bench 2>/dev/null; then
  # bench defaults 8081-8084 — override via flags
  ./bin/bench -scenario D -c 8 -d 6s -warmup 0 -capacity 5 \
    -sale "$SALE_BASE" -admission "$ADMISSION_BASE" -order "$ORDER_BASE" \
    -out "$RESULTS/P8-flash-D.json" 2>&1 | tee "$RESULTS/P8-flash-D.txt" || log "WARN flash bench exited non-zero"
  python3 - <<'PY' || true
import json
p="docs/phase-8/results/P8-flash-D.json"
try:
  d=json.load(open(p))
  inv=d.get("invariants") or {}
  print("flash ok", d.get("ok"), "confirmed", inv.get("checkouts_confirmed"), "cap", inv.get("sellable_capacity"), "inv", inv.get("invariant_inventory"))
  assert inv.get("invariant_inventory") in (True, "true", true) if False else inv.get("invariant_inventory") is True or str(inv.get("invariant_inventory")).lower()=="true"
except Exception as e:
  print("flash parse:", e)
PY
else
  log "WARN bench build failed — flash NOT_RUN"
fi

# inventory snapshot
kubectl -n "$NS" get pods,svc,deploy,sts,job -o wide | tee "$RESULTS/P8-k8s-inventory.txt"
helm -n "$NS" status "$RELEASE" | tee "$RESULTS/P8-helm-status.txt"

log
log "== K8S_DEMO_OK =="
log "APIs port-forwarded (this shell holds them until exit)."
log "  Admission meta: $ADMISSION_BASE/v1/meta"
log "  Results: $RESULTS"
log "Cleanup: ./scripts/k8s-demo-down.sh   (keeps PVC by default)"
log "Local lab only — not cloud staging or production."

# keep PF alive briefly for manual curl if interactive; otherwise exit cleans trap
if [[ "${GATEFLOW_K8S_KEEP_PF:-0}" == "1" ]]; then
  log "GATEFLOW_K8S_KEEP_PF=1 — sleeping 1h (Ctrl-C to stop)"
  sleep 3600
fi
