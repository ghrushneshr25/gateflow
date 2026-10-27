#!/usr/bin/env bash
# Install Gateflow observability into namespace gateflow-obs (separate from app ns).
# Optional — not required for VS1/fence/checkout.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$ROOT/.tools:$PATH"
CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
NS_OBS="${GATEFLOW_OBS_NAMESPACE:-gateflow-obs}"

command -v helm >/dev/null || { echo "helm missing"; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl missing"; exit 1; }

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  kubectl config use-context "kind-$CLUSTER" >/dev/null
else
  echo "kind cluster $CLUSTER not found — run ./scripts/k8s-demo.sh first (or create cluster)"
  exit 1
fi

echo "Installing gateflow-obs into namespace $NS_OBS"
helm upgrade --install gateflow-obs "$ROOT/deployments/helm/gateflow-obs" \
  --namespace "$NS_OBS" --create-namespace \
  --set "namespace=$NS_OBS" \
  --wait --timeout 5m

kubectl -n "$NS_OBS" get pods,svc
echo
echo "Port-forward Grafana:"
echo "  kubectl -n $NS_OBS port-forward svc/grafana 3000:3000"
echo "  open http://127.0.0.1:3000  (admin/admin lab defaults)"
echo "OTel (from host apps or port-forward):"
echo "  kubectl -n $NS_OBS port-forward svc/otel-collector 4318:4318"
echo "Uninstall: ./scripts/k8s-obs-down.sh"
