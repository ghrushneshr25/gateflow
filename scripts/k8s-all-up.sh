#!/usr/bin/env bash
# ============================================================================
# Gateflow — ONE SCRIPT to bring up everything on local Kubernetes (kind)
# and prove it works. Lab / portfolio only — not production or cloud staging.
#
# Steps:
#   1) tools (kind, helm → ./.tools)
#   2) kind cluster
#   3) build + load images
#   4) Helm: Gateflow app (ns gateflow)
#   5) Helm: observability (ns gateflow-obs)
#   6) smoke: Protocol A, VS1, fence, flash
#   7) port-forwards for Grafana + APIs (until Ctrl-C)
#
# Usage:
#   ./scripts/k8s-all-up.sh
#   GATEFLOW_SKIP_OBS=1 ./scripts/k8s-all-up.sh    # apps only
#   GATEFLOW_SKIP_SMOKE=1 ./scripts/k8s-all-up.sh  # skip tests
#   GATEFLOW_SMOKE_FLASH=0 ./scripts/k8s-all-up.sh # skip flash bench
#
# Tear down:
#   ./scripts/k8s-all-down.sh
# ============================================================================
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/.tools:$PATH"

CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
NS="${GATEFLOW_K8S_NAMESPACE:-gateflow}"
NS_OBS="${GATEFLOW_OBS_NAMESPACE:-gateflow-obs}"
RELEASE="${GATEFLOW_HELM_RELEASE:-gateflow}"

log() { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }

log "0) prerequisites note"
echo "Need: Docker running, kubectl, go, curl, python3"
echo "kind/helm install into .tools if missing"

log "1) tools"
"$ROOT/scripts/k8s-tools-setup.sh"
export PATH="$ROOT/.tools:$PATH"

log "2) kind cluster"
"$ROOT/scripts/k8s-cluster-up.sh"

log "3) build + load images (first run is slow)"
"$ROOT/scripts/k8s-build-images.sh"

log "4) Gateflow app deploy (namespace $NS, Protocol A)"
"$ROOT/scripts/k8s-app-up.sh"

if [[ "${GATEFLOW_SKIP_OBS:-0}" != "1" ]]; then
  log "5) observability (namespace $NS_OBS)"
  "$ROOT/scripts/k8s-obs-up.sh"
else
  log "5) observability SKIPPED (GATEFLOW_SKIP_OBS=1)"
fi

if [[ "${GATEFLOW_SKIP_SMOKE:-0}" != "1" ]]; then
  log "6) smoke tests"
  "$ROOT/scripts/k8s-smoke.sh"
else
  log "6) smoke SKIPPED"
fi

log "7) access — port-forwards (Ctrl-C to stop)"
PF=()
cleanup() {
  echo
  echo "stopping port-forwards..."
  for p in "${PF[@]:-}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup EXIT INT TERM

kubectl config use-context "kind-$CLUSTER" >/dev/null

# APIs
kubectl -n "$NS" port-forward "svc/${RELEASE}-sale" 18081:8080 >/dev/null 2>&1 & PF+=($!)
kubectl -n "$NS" port-forward "svc/${RELEASE}-admission" 18082:8080 >/dev/null 2>&1 & PF+=($!)
kubectl -n "$NS" port-forward "svc/${RELEASE}-inventory" 18083:8080 >/dev/null 2>&1 & PF+=($!)
kubectl -n "$NS" port-forward "svc/${RELEASE}-order" 18084:8080 >/dev/null 2>&1 & PF+=($!)
kubectl -n "$NS" port-forward "svc/${RELEASE}-payment-simulator" 18085:8080 >/dev/null 2>&1 & PF+=($!)
# UIs
kubectl -n "$NS" port-forward "svc/${RELEASE}-buyer-web" 8088:80 >/dev/null 2>&1 & PF+=($!)
kubectl -n "$NS" port-forward "svc/${RELEASE}-operator-console" 8089:80 >/dev/null 2>&1 & PF+=($!)

if [[ "${GATEFLOW_SKIP_OBS:-0}" != "1" ]]; then
  kubectl -n "$NS_OBS" port-forward svc/grafana 3000:3000 >/dev/null 2>&1 & PF+=($!)
  kubectl -n "$NS_OBS" port-forward svc/prometheus 9090:9090 >/dev/null 2>&1 & PF+=($!)
  kubectl -n "$NS_OBS" port-forward svc/tempo 3200:3200 >/dev/null 2>&1 & PF+=($!)
  kubectl -n "$NS_OBS" port-forward svc/otel-collector 4318:4318 >/dev/null 2>&1 & PF+=($!)
fi

sleep 2

cat <<EOF

┌─────────────────────────────────────────────────────────────┐
│  Gateflow local K8s is UP (lab / portfolio — not prod)     │
├─────────────────────────────────────────────────────────────┤
│  APIs (Protocol A)                                          │
│    Sale        http://127.0.0.1:18081/healthz               │
│    Admission   http://127.0.0.1:18082/v1/meta               │
│    Inventory   http://127.0.0.1:18083/healthz               │
│    Order       http://127.0.0.1:18084/healthz               │
│    Payment     http://127.0.0.1:18085/healthz               │
│                                                             │
│  UIs                                                        │
│    Buyer Web          http://127.0.0.1:8088                 │
│    Operator Console   http://127.0.0.1:8089                 │
│                                                             │
│  Observability (ns $NS_OBS)                                 │
│    Grafana     http://127.0.0.1:3000  (admin/admin)         │
│    Prometheus  http://127.0.0.1:9090                        │
│    Tempo       http://127.0.0.1:3200/ready                  │
│    OTLP HTTP   localhost:4318                               │
│                                                             │
│  Namespaces:  $NS  (app)   |   $NS_OBS  (obs)             │
│  Status:      kubectl --context kind-$CLUSTER get pods -A   │
│  Tear down:   ./scripts/k8s-all-down.sh                     │
│                                                             │
│  Port-forwards running — press Ctrl-C to stop them.         │
└─────────────────────────────────────────────────────────────┘
EOF

# keep alive
while true; do sleep 3600; done
