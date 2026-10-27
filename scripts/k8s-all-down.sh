#!/usr/bin/env bash
# Tear down Gateflow K8s lab pieces (order: obs → app → optional cluster).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$ROOT/.tools:$PATH"

echo "1) observability"
"$ROOT/scripts/k8s-obs-down.sh" || true

echo "2) app helm release (PVC kept by default)"
"$ROOT/scripts/k8s-demo-down.sh" || true

if [[ "${GATEFLOW_K8S_DELETE_CLUSTER:-0}" == "1" ]]; then
  echo "3) kind cluster delete"
  kind delete cluster --name "${GATEFLOW_KIND_CLUSTER:-gateflow}" || true
else
  echo "3) kind cluster retained (GATEFLOW_K8S_DELETE_CLUSTER=1 to remove)"
fi

echo "ALL_DOWN_DONE"
