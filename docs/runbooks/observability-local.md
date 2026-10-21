# Runbook: Local observability stack

## Start

```bash
# Data plane
docker compose up -d postgres kafka

# Observability profile
docker compose --profile obs up -d

# Apps (example order) with OTLP to host collector
export GATEFLOW_OTEL_ENABLED=1
export GATEFLOW_OTEL_ENDPOINT=localhost:4318
export GATEFLOW_DEV_JWT_SECRET=gateflow-dev-secret-change-me
export ENVIRONMENT=local
# ... start bin/order etc.
```

## Endpoints

| Component | URL |
|---|---|
| OTLP HTTP (apps → collector) | `http://localhost:4318` |
| OTLP gRPC | `localhost:4317` |
| Tempo UI/API | `http://localhost:3200` |
| Prometheus | `http://localhost:9090` |
| Grafana | `http://localhost:3000` (admin/admin; anonymous viewer on) |
| Loki | `http://localhost:3100` |
| App metrics (each service) | `http://127.0.0.1:8081–8085/metrics` |

## RED metrics and scrape (P5.4.1)

1. Start apps on host ports 8081–8085 with DB/Kafka as usual.
2. `docker compose --profile obs up -d` (Prometheus scrapes `host.docker.internal:808x`).
3. Prometheus → Status → Targets: job `gateflow-services` all **UP**.
4. Useful queries (filter by scrape `service` label for ops gauges):

```promql
sum by (service) (rate(gateflow_http_requests_total[1m]))
sum by (service) (rate(gateflow_http_requests_total{status_class="5xx"}[5m]))
  / clamp_min(sum by (service) (rate(gateflow_http_requests_total[5m])), 1e-9)
histogram_quantile(0.95, sum by (le, service) (rate(gateflow_http_request_duration_seconds_bucket[5m])))
gateflow_outbox_unpublished{service="order"}
gateflow_outbox_oldest_age_seconds{service="order"}
gateflow_outbox_quarantined{service="order"}
gateflow_order_reconciliation_required{service="order"}
gateflow_admission_coordination_fenced{service="admission"}
```

5. Grafana → Dashboards → **Gateflow Ops** (`/d/gateflow-ops`).
6. Alerts: Prometheus → Alerts (rules in `deployments/docker/prometheus/rules/gateflow-alerts.yml`).

### Validate after fresh startup

```bash
curl -sf http://127.0.0.1:8084/metrics | grep gateflow_http
curl -sf 'http://localhost:9090/api/v1/targets' | grep -c '"health":"up"'
# expect gateflow-services targets up after ~30s
```

### Alert → runbook map

| Alert | Runbook |
|---|---|
| GateflowOutboxOldestAgeHigh / QuarantineNonZero | [outbox-quarantine-replay.md](./outbox-quarantine-replay.md) |
| GateflowReconciliationBacklogHigh | [reconciliation-backlog.md](./reconciliation-backlog.md) |
| GateflowAdmissionFenced | [admission-coordination-fence.md](./admission-coordination-fence.md) |
| GateflowServiceTargetDown | this runbook |

## Correlation

- Header: `X-Correlation-ID` (ULID). Invalid values replaced.
- Logs: JSON field `correlation_id` when using `httpx.Logger(ctx, log)`.
- Traces: W3C `traceparent`; log fields `trace_id` / `span_id` when span active.

## Prove a checkout trace

1. Start obs profile + services with `GATEFLOW_OTEL_ENABLED=1`.
2. Run a buyer checkout (or `./scripts/vs1_e2e.sh`).
3. Note response `X-Correlation-ID`.
4. Grafana → Explore → Tempo: search recent traces by service name `order` / `admission` / …
5. Confirm parent/child HTTP spans across services.

## Collector down

Apps continue booking. Spans may drop; correctness unchanged.

## Stop

```bash
docker compose --profile obs down
```
