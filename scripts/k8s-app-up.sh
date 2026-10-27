#!/usr/bin/env bash
# Helm install/upgrade Gateflow app stack (namespace gateflow). Protocol A.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/.tools:$PATH"

CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
NS="${GATEFLOW_K8S_NAMESPACE:-gateflow}"
RELEASE="${GATEFLOW_HELM_RELEASE:-gateflow}"
TAG="${GATEFLOW_IMAGE_TAG:-p8-local}"

command -v helm >/dev/null || { echo "run k8s-tools-setup.sh first"; exit 1; }
kubectl config use-context "kind-$CLUSTER" >/dev/null

echo "helm lint"
helm lint "$ROOT/deployments/helm/gateflow"

echo "helm upgrade --install $RELEASE (Protocol A)"
helm upgrade --install "$RELEASE" "$ROOT/deployments/helm/gateflow" \
  --namespace "$NS" --create-namespace \
  -f "$ROOT/deployments/helm/gateflow/values-local.yaml" \
  --set "imageTag=$TAG" \
  --set "imagePullPolicy=Never" \
  --set "admission.grantProtocol=A" \
  --wait=false \
  --timeout 10m

echo "wait postgres"
kubectl -n "$NS" rollout status "statefulset/${RELEASE}-postgres" --timeout=180s

echo "wait migrate"
MIG_JOB=$(kubectl -n "$NS" get jobs -l "app.kubernetes.io/component=migrate" \
  --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null || true)
if [[ -z "${MIG_JOB:-}" ]]; then
  MIG_JOB=$(kubectl -n "$NS" get jobs -o name 2>/dev/null | grep migrate | tail -1 | sed 's|job.batch/||')
fi
[[ -n "${MIG_JOB:-}" ]] || { echo "migrate job not found"; exit 1; }
kubectl -n "$NS" wait --for=condition=complete "job/$MIG_JOB" --timeout=300s
kubectl -n "$NS" logs "job/$MIG_JOB" --tail=20 || true

echo "wait kafka + backends + frontends"
kubectl -n "$NS" rollout status "deployment/${RELEASE}-kafka" --timeout=180s || true
for c in sale admission inventory order payment-simulator; do
  kubectl -n "$NS" rollout status "deployment/${RELEASE}-$c" --timeout=240s
done
for c in buyer-web operator-console; do
  kubectl -n "$NS" rollout status "deployment/${RELEASE}-$c" --timeout=120s 2>/dev/null || echo "WARN frontend $c not ready"
done

kubectl -n "$NS" get pods -o wide
echo "APP_UP_OK ns=$NS protocol=A"
