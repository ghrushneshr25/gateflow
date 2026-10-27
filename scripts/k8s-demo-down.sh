#!/usr/bin/env bash
# Uninstall Gateflow Helm release from kind. Does NOT delete PVC/data by default.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$ROOT/.tools:$PATH"

CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
NS="${GATEFLOW_K8S_NAMESPACE:-gateflow}"
RELEASE="${GATEFLOW_HELM_RELEASE:-gateflow}"

command -v helm >/dev/null || { echo "helm not found"; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl not found"; exit 1; }

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  kubectl config use-context "kind-$CLUSTER" >/dev/null 2>&1 || true
fi

echo "helm uninstall $RELEASE -n $NS (PVC retained)"
helm uninstall "$RELEASE" -n "$NS" 2>/dev/null || echo "release not found (ok)"

if [[ "${GATEFLOW_K8S_DELETE_PVC:-0}" == "1" ]]; then
  echo "GATEFLOW_K8S_DELETE_PVC=1 — deleting PVCs in $NS"
  kubectl -n "$NS" delete pvc --all --wait=false 2>/dev/null || true
else
  echo "PVCs left in place. To wipe data: GATEFLOW_K8S_DELETE_PVC=1 $0"
fi

if [[ "${GATEFLOW_K8S_DELETE_CLUSTER:-0}" == "1" ]]; then
  echo "GATEFLOW_K8S_DELETE_CLUSTER=1 — kind delete cluster $CLUSTER"
  kind delete cluster --name "$CLUSTER"
else
  echo "kind cluster '$CLUSTER' retained. To remove: GATEFLOW_K8S_DELETE_CLUSTER=1 $0"
fi
