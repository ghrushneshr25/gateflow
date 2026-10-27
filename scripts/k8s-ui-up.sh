#!/usr/bin/env bash
# Start browser access port-forwards for Gateflow kind demo.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="$ROOT/.tools:$PATH"
kubectl config use-context kind-gateflow
"$ROOT/scripts/k8s-ui-down.sh" >/dev/null 2>&1 || true
PFDIR="$ROOT/.demo-logs/k8s-pf"
mkdir -p "$PFDIR"
: > "$PFDIR/pids"
start_pf() {
  local ns=$1 svc=$2 lport=$3 rport=$4
  nohup kubectl -n "$ns" port-forward "svc/$svc" "${lport}:${rport}" >"$PFDIR/${svc}-${lport}.log" 2>&1 &
  echo $! >> "$PFDIR/pids"
  echo "  http://127.0.0.1:${lport}  ($svc)"
}
echo "Port-forwards:"
start_pf gateflow gateflow-sale 8081 8080
start_pf gateflow gateflow-admission 8082 8080
start_pf gateflow gateflow-inventory 8083 8080
start_pf gateflow gateflow-order 8084 8080
start_pf gateflow gateflow-payment-simulator 8085 8080
start_pf gateflow gateflow-buyer-web 8088 80
start_pf gateflow gateflow-operator-console 8089 80
start_pf gateflow-obs grafana 3000 3000
start_pf gateflow-obs prometheus 9090 9090
start_pf gateflow-obs tempo 3200 3200
sleep 1
echo
echo "Open in browser (separate apps):"
echo "  BUYER  Web:        http://127.0.0.1:8088"
echo "  ADMIN  Console:    http://127.0.0.1:8089   ← Create event, publish, fence"
echo "  Grafana:           http://127.0.0.1:3000  (admin/admin)"
echo "  Prometheus:        http://127.0.0.1:9090"
echo "  APIs:              8081 sale · 8082 admission · 8084 order"
echo "E2E: Admin → Create event → publish · Buyer → sign in (email/Gmail) → book"
echo "Stop: ./scripts/k8s-ui-down.sh"
