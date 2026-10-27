# Shared staging validation plan (Protocol A)

**Status:** Plan only — does **not** claim multi-replica or cloud staging was executed.  
**Prerequisite gate:** P7.1 remediation complete → schedule **P7.2 Shared Staging Validation** (separate milestone).

## 1. Ownership

| Role | Responsibility |
|---|---|
| Release owner | Artifact ID, deploy, migrate, rollback authority |
| Platform/infra | PG, Kafka, network, secrets |
| Eng on-call | Crash/recovery drills, fence drills |
| CTO | SLO approval, production go/no-go (later) |

If **no shared staging environment exists**, mark staging **BLOCKED** — do not relabel local Docker as cloud staging.

## 2. Environment requirements

- PostgreSQL with service schemas / search_path per service
- Kafka cluster with **dedicated topics** for chaos tests (not production topics)
- Five services: sale, admission, inventory, order, payment-simulator
- Observability: Prometheus, Grafana, Tempo, (Loki optional)
- Load generator host (or CI runner) for `tools/bench`
- Secrets via staging secret store (not default dev JWT in real staging)

## 3. Deployment baseline

1. Build/revision pin (git SHA + image digest).
2. Confirm **every** admission replica: `GET /v1/meta` → `grant_protocol=A`.
3. `GATEFLOW_ADMISSION_GRANT_PROTOCOL` unset or `A` on all replicas.
4. Run migrations; record versions.
5. Smoke: healthz all services.

## 4. Test sequence (reuse P7.0 checklist)

1. VS1-equivalent happy path + idempotent checkout  
2. Fence → grant fail-closed → unfence → grant OK  
3. Flash capacity N  
4. Auth: no-token 401; buyer isolation  
5. Crash recovery on **isolated Kafka topics** (order exclusive ports)  
6. Perf ramp c=1,5,10,20 with **classified** checkout outcomes  
7. Outbox lag / oldest age under load  
8. Trace: one checkout visible in Tempo  
9. Artifact rollback drill  

## 5. Provisional SLOs (starting points)

See `GRANT-LATENCY-SLO.md`.

| Signal | Target |
|---|---|
| Checkout p95 @ c10 | &lt; 200 ms |
| Grant-only p95 @ c10/c20 | &lt; 50 ms |
| Unexpected HTTP 5xx | &lt; 1% **and** unclassified = 0 |
| Oversell / budget overflow | 0 |
| Fence | fail closed |
| Outbox oldest age steady | &lt; 120 s |

## 6. Hard stops

Oversell, budget overflow, fence bypass, durable business-effect loss → **stop**, no production review.

## 7. Evidence pack

- Staging execution report  
- Bench JSON with `checkout_outcomes`  
- Crash logs + topic names  
- Rollback record  
- Meta screenshots / curls for protocol A on all replicas  
