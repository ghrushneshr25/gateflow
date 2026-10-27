#!/usr/bin/env bash
# Remove observability release only (does not touch gateflow app namespace).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$ROOT/.tools:$PATH"
NS_OBS="${GATEFLOW_OBS_NAMESPACE:-gateflow-obs}"
CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  kubectl config use-context "kind-$CLUSTER" >/dev/null 2>&1 || true
fi

echo "helm uninstall gateflow-obs -n $NS_OBS"
helm uninstall gateflow-obs -n "$NS_OBS" 2>/dev/null || echo "release not found"
if [[ "${GATEFLOW_OBS_DELETE_NS:-0}" == "1" ]]; then
  kubectl delete ns "$NS_OBS" --wait=false 2>/dev/null || true
else
  echo "Namespace $NS_OBS retained (empty). Delete with: GATEFLOW_OBS_DELETE_NS=1 $0"
fi
