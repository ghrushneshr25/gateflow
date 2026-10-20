# Phase 0 Report — Specification Consolidation

**Date:** 2026-10-20  
**Status:** Complete  
**Repo:** `github.com/ghrushneshr25/gateflow`

---

## 1. Existing repository state

| Item | State |
|---|---|
| Branch | `main` @ `3626425` (`chore: initialize Gateflow repository`) |
| Remote | `origin` → `https://github.com/ghrushneshr25/gateflow.git` |
| Working tree | Clean before Phase 0 edits |
| Existing files | `.gitignore`, `docs/PRD-v1.md`, `docs/PRD-UI` |
| Code | None (no services, apps, compose, or modules) |
| Issue fixed | `docs/` was gitignored — removed so specifications are versioned |

### Preserved work

- Full PRD content moved to `docs/product/PRD.md` and amended with UI requirements from `docs/PRD-UI`.
- Legacy files retained temporarily under `docs/` for reference; canonical paths are the structured tree.

---

## 2. Development tools and versions

| Tool | Version / notes |
|---|---|
| Go | `go1.25.5 darwin/arm64` |
| Node.js | `v25.2.1` |
| npm | `11.12.1` |
| pnpm / yarn | Not installed (use npm) |
| Docker | `29.5.3` |
| Docker Compose | `v5.1.4` |
| golangci-lint | Present (`~/go/bin/golangci-lint`) |
| make | `/usr/bin/make` |
| protoc | `/opt/homebrew/bin/protoc` |
| Host psql / redis-cli / kafka-topics | Not on host PATH (use containers) |
| Host | macOS arm64, 10 CPUs, 16 GB RAM |

---

## 3. Specification gaps and contradictions resolved

| Gap / contradiction | Resolution (documented default) |
|---|---|
| PRD delivery phases vs master prompt phases | **Master prompt wins for execution.** P0 spec → P1 foundation → P2 vertical slice → P3 concurrency → P4 failure/recovery → P5 polish → P6 perf → P7 K8s. |
| PRD open deps (exact APIs, budgets, Redis fence, events) | Resolved with defaults in `GF-SCS-001`; OpenAPI/JSON Schema files land in Phase 1–2 as contracts harden. |
| HTTP framework | **stdlib `net/http` + thin internal router helpers** (ADR-0004). Framework only if complexity justifies later. |
| Postgres topology | **One instance, five schemas, five roles** (ADR-0002). |
| Kafka local mode | **KRaft single-node** for Compose (ADR-0003). |
| Auth local | **Dev HMAC JWT adapter**; claims carry `sub`, `principal_type` (`buyer`\|`operator`\|`service`), scopes (ADR-0020). |
| Idempotency key scope | `principal_id + operation_type + resource_context + key`; retain 24h minimum. |
| Default purchase limit | 4 tickets / buyer / session. |
| Reservation lease | Default 5m, min 1m, max 15m; UTC. |
| Entitlement unbound TTL | 10 minutes; binding not governed by unbound TTL. |
| Payment outcome deadline | 3 minutes → Order enters reconciliation without extending reservation. |
| Frontend package manager | npm + lockfiles. |
| Shared UI package | Deferred until genuine reuse. |
| docs/ gitignore | Removed; docs are first-class artifacts. |

No remaining **material product** contradictions that block Phase 1 foundation.

### Implementation-critical contracts still to harden in Phase 1–2 (not blockers for scaffolding)

1. Final OpenAPI YAML per service endpoint set for vertical slice.
2. Exact Kafka topic names committed as JSON Schema.
3. Redis key layout and fence token format (defaults in SCS; code in Admission Phase 2–3).
4. Exact operator permission matrix (defaults in security doc).

---

## 4. Proposed first vertical slice (Phase 2)

**Name:** VS-1 Happy-path FIFO booking

**Scope:**

1. Sale: create offering + session + ticket pool; publish.
2. Buyer Web: list published offerings; open session.
3. Admission: FIFO join (idempotent); grant entitlement under budgets.
4. Order: checkout with idempotency key; bind entitlement; hold allowance.
5. Inventory: atomic reserve; confirm after payment.
6. Payment Simulator: deterministic success.
7. Order: finalize confirmed order.
8. Outbox: publish ≥1 domain event; consumer-safe idempotent record optional in VS-1.
9. Buyer Web: show confirmed order after refresh.

**Out of VS-1 (explicitly deferred):** lottery/slots/direct, late payment, compensation UI, Redis outage recovery, multi-instance budget soak, full operator recovery, Playwright suite (component tests minimum).

**Acceptance tests (must pass):**

| ID | Assertion |
|---|---|
| VS1-01 | Published offering visible to buyer; draft not visible |
| VS1-02 | Double join same buyer returns same queue entry / position |
| VS1-03 | Entitlement issued; unbound TTL set |
| VS1-04 | Checkout idempotent on same key+payload |
| VS1-05 | Conflicting idempotency key → stable conflict |
| VS1-06 | Reservation respects capacity (unit pool of 1 under 2 concurrent attempts → exactly one success) |
| VS1-07 | Payment success + inventory confirm → order `CONFIRMED` |
| VS1-08 | Confirmed order survives process restart / page refresh |
| VS1-09 | Outbox row committed with order confirmation transaction |
| VS1-10 | Zero inventory oversell in VS-1 concurrency test |

---

## 5. Files created or changed in Phase 0

| Path | Action |
|---|---|
| `.gitignore` | Stop ignoring `docs/` |
| `docs/product/PRD.md` | Canonical PRD + UI amendment |
| `docs/architecture/overview.md` | Architecture baseline |
| `docs/contracts/GF-SCS-001.md` | Service contract specification |
| `docs/adr/0001`–`0024` | Architecture decision records |
| `docs/security/overview.md` | Identity and auth baseline |
| `docs/testing/strategy.md` | Test strategy |
| `docs/benchmarks/methodology.md` | Benchmark methodology (no fake results) |
| `docs/runbooks/README.md` | Runbook index |
| `docs/execution/PHASE0.md` | This report |
| `docs/execution/PLAN.md` | Phased execution plan |
| `README.md` | Project entrypoint |

---

## 6. Phase 1 milestone — files and verification

### Files to create

- `go.work`, root `Makefile`, `.env.example`, `docker-compose.yml`
- `services/{sale,admission,inventory,order,payment-simulator}/go.mod` + minimal `cmd` + health
- `libs/go/{logging,config,httpx,telemetry,testid}/` as needed
- `apps/buyer-web`, `apps/operator-console` Vite+React+TS scaffolds
- `contracts/openapi/.gitkeep` (and stub envelopes)
- `deployments/docker/` Dockerfiles
- `.github/workflows/ci.yml`
- Service migration directories (empty or bootstrap schema only)

### Verification commands

```bash
make fmt
make lint          # or golangci-lint run ./...
make test
make build
docker compose config
docker compose up -d postgres redis kafka
# health checks once services exist
curl -sf http://localhost:8081/healthz
npm --prefix apps/buyer-web run typecheck
npm --prefix apps/operator-console run typecheck
npm --prefix apps/buyer-web test
npm --prefix apps/operator-console test
```

### Phase 1 acceptance criteria

1. `go work sync` succeeds; all five modules build.
2. Compose starts infra cleanly.
3. Each service binary starts and serves liveness.
4. Both frontends typecheck and build.
5. CI workflow file present and locally equivalent commands pass.
6. No domain fake-complete logic pretending full product readiness.
7. Docs remain consistent with ownership boundaries.

---

## 7. Gate decision

**Gate 0 (specification):** PASSED for foundation start.

Proceed to **Phase 1 — Repository foundation**.
