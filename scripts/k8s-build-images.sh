#!/usr/bin/env bash
# Build Gateflow images and load into kind.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/.tools:$PATH"
CLUSTER="${GATEFLOW_KIND_CLUSTER:-gateflow}"
TAG="${GATEFLOW_IMAGE_TAG:-p8-local}"

command -v docker >/dev/null || { echo "docker required"; exit 1; }
command -v kind >/dev/null || { echo "run k8s-tools-setup.sh first"; exit 1; }
kind get clusters 2>/dev/null | grep -qx "$CLUSTER" || { echo "cluster missing — run k8s-cluster-up.sh"; exit 1; }

SERVICES=(sale admission inventory order payment-simulator)
for s in "${SERVICES[@]}"; do
  echo "build gateflow/$s:$TAG"
  docker build -f deployments/docker/Dockerfile.service \
    --build-arg "SERVICE=$s" \
    -t "gateflow/$s:$TAG" .
done

echo "build gateflow/migrate:$TAG"
docker build -f deployments/docker/Dockerfile.migrate -t "gateflow/migrate:$TAG" .

for app in buyer-web operator-console; do
  echo "build gateflow/$app:$TAG"
  docker build -f deployments/docker/Dockerfile.frontend \
    --build-arg "APP=$app" \
    -t "gateflow/$app:$TAG" . || echo "WARN: frontend $app build failed (optional)"
done

echo "loading images into kind $CLUSTER"
for img in "${SERVICES[@]}" migrate buyer-web operator-console; do
  if docker image inspect "gateflow/$img:$TAG" >/dev/null 2>&1; then
    kind load docker-image "gateflow/$img:$TAG" --name "$CLUSTER"
  fi
done
echo "IMAGES_OK tag=$TAG"
