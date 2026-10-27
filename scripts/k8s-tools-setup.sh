#!/usr/bin/env bash
# Install kind + helm into ./.tools (no sudo, not system-wide).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mkdir -p .tools
export PATH="$ROOT/.tools:$PATH"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

KIND_VER="${KIND_VERSION:-v0.27.0}"
HELM_VER="${HELM_VERSION:-v3.17.3}"

if ! command -v kind >/dev/null 2>&1 || [[ "${FORCE_TOOLS:-0}" == "1" ]]; then
  echo "downloading kind $KIND_VER ($OS-$ARCH)"
  curl -fsSL -o .tools/kind "https://kind.sigs.k8s.io/dl/${KIND_VER}/kind-${OS}-${ARCH}"
  chmod +x .tools/kind
fi

if ! command -v helm >/dev/null 2>&1 || [[ "${FORCE_TOOLS:-0}" == "1" ]]; then
  echo "downloading helm $HELM_VER ($OS-$ARCH)"
  TMP=$(mktemp -d)
  curl -fsSL "https://get.helm.sh/helm-${HELM_VER}-${OS}-${ARCH}.tar.gz" | tar -xz -C "$TMP"
  mv "$TMP/${OS}-${ARCH}/helm" .tools/helm
  chmod +x .tools/helm
  rm -rf "$TMP"
fi

command -v docker >/dev/null || { echo "ERROR: docker required"; exit 1; }
command -v kubectl >/dev/null || { echo "ERROR: kubectl required on PATH"; exit 1; }
command -v go >/dev/null || { echo "ERROR: go required on PATH"; exit 1; }

echo "OK kind=$($ROOT/.tools/kind version 2>/dev/null | head -1)"
echo "OK helm=$($ROOT/.tools/helm version --short 2>/dev/null)"
echo "OK kubectl=$(kubectl version --client -o yaml 2>/dev/null | head -3 | tr '\n' ' ')"
echo "Add to PATH for this shell:"
echo "  export PATH=\"$ROOT/.tools:\$PATH\""
