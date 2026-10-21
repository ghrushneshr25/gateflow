# Phase 5.4.1 Report — RED Metrics, Dashboards, and Alerts

## Verdict: **COMPLETE**

## HEAD / branch

- HEAD: `dad01e5` (Phase 4 commit; P5.1.0–P5.4.1 uncommitted)
- Branch: `main` ahead of `origin/main` by 12
- No commit / no push this milestone

## Architecture

```
Apps expose GET /metrics (Prometheus text)
  → Prometheus scrape job gateflow-services
       host.docker.internal:8081–8085 (local host processes)
  → rule_files: deployments/docker/prometheus/rules/gateflow-alerts.yml
  → Grafana dashboard uid=gateflow-ops (provisioned)

OTLP collector :8889 remains scraped for collector health only
(no duplicate application RED series from OTLP metrics in this milestone)
```

## Metrics inventory

| Metric | Type | Labels (bounded) | Unit | Source |
|---|---|---|---|---|
| `gateflow_http_requests_total` | counter | service, method, route, status_class | requests | `metrics.Middleware` via `httpx.WrapHandler` |
| `gateflow_http_request_duration_seconds` | histogram | same | seconds | same; buckets 5ms–10s |
| `gateflow_admission_grants_total` | counter | outcome=ok\|fenced\|budget\|not_found\|error | grants | admission grant handler |
| `gateflow_admission_coordination_fenced` | gauge | — | 0/1 | fence/unfence/get coordination |
| `gateflow_admission_coordination_epoch` | gauge | — | epoch | same |
| `gateflow_order_reconciliation_required` | gauge | — | orders | order 15s refresh from PG `ListByStatus` |
| `gateflow_order_payment_pending` | gauge | — | orders | same |
| `gateflow_order_reconcile_total` | counter | result | advances | reconcile handler |
| `gateflow_outbox_unpublished` | gauge | — | rows | order 15s refresh `OutboxBacklog` |
| `gateflow_outbox_oldest_age_seconds` | gauge | — | seconds | same |
| `gateflow_outbox_quarantined` | gauge | — | rows | `CountQuarantined` |
| `gateflow_outbox_publish_total` | counter | result=ok\|fail | publishes | outbox publisher |
| `gateflow_payment_outcomes_total` | counter | outcome | payments | payment create |
| `gateflow_compensation_total` | counter | outcome | comps | compensation create/execute |

### Cardinality controls

- Route labels use templates (`/v1/orders/{id}`), never raw ULIDs/IDs.
- `status_class` is 2xx|3xx|4xx|5xx only.
- No buyer_id, order_id, correlation_id, token, or free-form error labels.
- Ops gauges refreshed every **15s** (not tight DB loops).

### Query note

Shared `libs/go/metrics` registers package-level collectors in every process, so non-owner services export zero gauges. Prefer scrape label filters:

```promql
gateflow_outbox_quarantined{service="order"}
max(gateflow_order_reconciliation_required{service="order"})
```

## Prometheus

- Config: `deployments/docker/prometheus/prometheus.yml`
- Rules: `deployments/docker/prometheus/rules/gateflow-alerts.yml`
- Promtool unit tests: `deployments/docker/prometheus/tests/gateflow-alerts_test.yml`
- Compose: rules volume + `extra_hosts: host.docker.internal:host-gateway`

### Live targets (this milestone)

| Target | Health |
|---|---|
| sale :8081 | **up** |
| admission :8082 | **up** |
| inventory :8083 | **up** |
| order :8084 | **up** |
| payment-simulator :8085 | **up** |
| otel-collector :8889 | **up** |

### Live PromQL (post-VS1)

- Request rate by service: 5 series non-zero
- p95 latency by service: ~0.005s
- 5xx error rate: empty (no 5xx)
- `gateflow_admission_grants_total{outcome="ok"}` = 1 after VS1
- `gateflow_payment_outcomes_total{outcome="succeeded"}` = 1 after VS1

## Dashboard

- Path: `deployments/docker/grafana/provisioning/dashboards/json/gateflow-ops.json`
- Provider: `dashboards.yaml` → folder Gateflow
- UID: `gateflow-ops` — **11 panels** after fresh Grafana recreate
- Refresh: 30s
- Panels: request rate, error ratio, p50/p95/p99, target health, outbox, recon, admission fence/grants, payment/compensation

## Alerts

| Alert | Expr | for | Severity | Runbook |
|---|---|---|---|---|
| GateflowOutboxOldestAgeHigh | `gateflow_outbox_oldest_age_seconds > 120` | 2m | warning | outbox-quarantine-replay.md |
| GateflowOutboxQuarantineNonZero | `gateflow_outbox_quarantined > 0` | 1m | critical | outbox-quarantine-replay.md |
| GateflowReconciliationBacklogHigh | `gateflow_order_reconciliation_required > 5` | 5m | warning | reconciliation-backlog.md |
| GateflowAdmissionFenced | `..._fenced == 1` | 1m | warning | admission-coordination-fence.md |
| GateflowServiceTargetDown | `up{job="gateflow-services"} == 0` | 2m | critical | observability-local.md |

### Live fire / resolve

| Alert | Fire evidence | Resolve evidence |
|---|---|---|
| Outbox oldest age | Controlled locked unpublished row age ~900s → **firing** | DELETE test rows → gauges 0 → **inactive** |
| Outbox quarantine | INSERT quarantine=true → gauge 1 → **firing** | DELETE → gauge 0 → **inactive** |
| Recon backlog | Real PG backlog 22 → **firing** | Left firing (real backlog; no destructive status mass-update). Resolve path covered by promtool series drop. |
| promtool | `test rules` **SUCCESS** (fire + resolve quarantine) | |

Notification channels: **not configured** (rule evaluation + Prometheus/Grafana visibility only).

## Verification (this milestone)

| Check | Result |
|---|---|
| `make test` | **PASS** |
| `make build` | **PASS** |
| `make compose-config` | **PASS** |
| promtool check config / rules | **PASS** |
| promtool test rules | **PASS** |
| `./scripts/vs1_e2e.sh` | **VS1_E2E_PASS** |
| Prometheus targets | 5/5 app + collector **up** |
| Grafana Gateflow Ops | provisioned, 11 panels |
| Alert fire/resolve | outbox age + quarantine live; recon fire live; promtool resolve |
| crash-recovery A–E | **PASS** (A–E, 68s) |
| integration-race | **PASS** (inventory/order/admission) |

## Invariants preserved

- Payment success alone never confirms an order (metric is outcome only).
- PG remains authority for outbox/recon gauges.
- Redis not used for grant authority.
- `/metrics` is local-scrape only (host network isolation; no public exposure decision).

## Gaps → P5.4.2

- Kafka span links / deeper internal spans
- Optional Loki log shipping from app stdout
- Service-scoped metric registries (eliminate zero gauge series on non-owners)
- External alert notification channels
- Natural lease e2e residual (pre-existing)

## No commit / no push
