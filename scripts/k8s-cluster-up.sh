#!/usr/bin/env bash
# Create kind cluster for Gateflow (idempotent).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$ROOT/.tools:$PATH"
CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"

command -v kind >/dev/null || { echo "run ./scripts/k8s-tools-setup.sh first"; exit 1; }
command -v docker >/dev/null || { echo "docker required"; exit 1; }
docker info >/dev/null 2>&1 || { echo "docker daemon not running"; exit 1; }

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "kind cluster '$CLUSTER' already exists"
else
  echo "creating kind cluster '$CLUSTER'"
  kind create cluster --name "$CLUSTER"
fi
kubectl cluster-info --context "kind-$CLUSTER"
kubectl config use-context "kind-$CLUSTER"
echo "context: kind-$CLUSTER"
