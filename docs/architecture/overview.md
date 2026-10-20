# Gateflow Architecture Overview

**Document ID:** GF-ARCH-001  
**Status:** Accepted baseline (Phase 0)  
**Related:** GF-PRD-001, GF-SCS-001, ADRs 0001–0024

---

## 1. Purpose

Gateflow is a high-demand booking and admission orchestration platform. The architecture prioritizes business correctness under concurrency and failure over raw throughput claims.

This document defines service boundaries, data ownership, communication patterns, and local deployment topology. Detailed contracts live in `docs/contracts/GF-SCS-001.md`.

---

## 2. System context

```
┌─────────────────┐     ┌──────────────────────┐
│   Buyer Web     │     │  Operator Console    │
│  (React/TS)     │     │    (React/TS)        │
└────────┬────────┘     └──────────┬───────────┘
         │ REST/JSON               │ REST/JSON
         └────────────┬────────────┘
                      │
    ┌─────────────────┼─────────────────┐
    │                 │                 │
┌───▼───┐  ┌─────────▼──┐  ┌───────────▼──┐
│ Sale  │  │ Admission  │  │   Inventory  │
└───┬───┘  └─────┬──────┘  └──────┬───────┘
    │            │                │
    │      ┌─────▼──────┐  ┌──────▼───────┐
    │      │   Order    │──│   Payment    │
    │      │            │  │  Simulator   │
    │      └─────┬──────┘  └──────────────┘
    │            │
    └─────┬──────┴──────────┐
          │                 │
   ┌──────▼──────┐   ┌──────▼──────┐
   │ PostgreSQL  │   │    Kafka    │
   │ (schemas)   │   │   (events)  │
   └─────────────┘   └─────────────┘
          ▲
   ┌──────┴──────┐
   │    Redis    │  (Admission coordination only)
   └─────────────┘
```

Observability: OpenTelemetry → Prometheus, Grafana, Loki, Tempo.

---

## 3. Service ownership

| Service | Owns (authoritative) | Does not own |
|---|---|---|
| **Sale** | Offerings, categories, sessions, ticket-type/section metadata, prices, sale windows, publication state, purchase-policy config/versions, admission config references | Live inventory, queues, entitlements, orders, payments |
| **Admission** | Queue membership, durable sequence, strategies, grants, entitlement lifecycle, global/per-session rate limits, active-checkout budgets, grant recovery/fencing state | Inventory capacity, orders, payments |
| **Inventory** | Sellable pools, capacity, reservations, expiry, confirmation, release, inventory operation IDs | Queues, entitlements, purchase limits, payments |
| **Order** | Checkout orchestration, orders, order transitions, order idempotency, purchase-allowance holds/commits, policy snapshots, reconciliation, cancellation orchestration | Inventory capacity truth, payment attempt truth |
| **Payment Simulator** | Payment attempts, simulated outcomes, delayed/duplicate callbacks, compensation state and history | Orders, inventory, entitlements |

**Hard rule:** No service writes another service's tables. No distributed DB transactions. Coordination uses synchronous APIs + asynchronous events.

---

## 4. Frontend applications

| App | Role |
|---|---|
| `apps/buyer-web` | Discovery, queue, entitlement, checkout, order history |
| `apps/operator-console` | Configuration, inspection, authorized recovery |

Backend authorization is mandatory. Frontend route guards are not security controls. Queue position, inventory, payment, and order state are server-authoritative. Initial status updates use polling.

---

## 5. Persistence and messaging

| Store | Role |
|---|---|
| **PostgreSQL** | Authoritative durable business state. One instance initially; service-owned schemas and least-privilege roles. |
| **Kafka** | Asynchronous domain events. At-least-once delivery. Business exactly-once via outbox + idempotent consumers. |
| **Redis** | Admission hot-path rate/concurrency coordination and queue projections. Not inventory/order truth. Recovery requires fencing before resume. |

Transactional outbox: state change + outbox row commit atomically in the owning service DB transaction.

---

## 6. Core invariants (non-negotiable)

1. **Inventory:** `confirmed + active_reserved <= sellable_capacity`
2. **Purchase limit:** `committed + held + requested <= effective_limit` (atomic hold in Order)
3. **Entitlement:** bound atomically to one buyer + one checkout/order; default unbound TTL 10 minutes
4. **Reservation lease:** default 5 min (min 1, max 15); UTC deadline; no revive after expiry
5. **Payment ≠ order confirmation:** order confirms only after inventory confirmation + required conditions
6. **Late payment success:** no reservation revive → reconciliation + idempotent compensation
7. **Idempotency:** stable operation IDs; conflicting key reuse rejected; retain ≥ 24h
8. **Uncertainty:** timeout ≠ failure; every ambiguous op is discoverable and recoverable

---

## 7. Communication patterns

### Synchronous (REST/JSON, `/v1/...`)

- Buyer/Operator → Sale, Admission, Order (and read paths on Inventory/Payment as needed)
- Order → Admission (bind/consume entitlement)
- Order → Inventory (reserve, confirm, release, lookup)
- Order → Payment Simulator (initiate, lookup, compensate)
- Operator recovery → Order / Payment / Admission authorized actions

### Asynchronous (Kafka events)

Event families: sale config, admission/entitlement, reservation lifecycle, order lifecycle, payment/compensation, reconciliation escalation.

Envelope fields: `event_id`, `event_type`, `schema_version`, `occurred_at`, `producer`, `correlation_id`, `causation_id`, `payload`.

Partition by stable business ID where per-entity ordering is required. No global ordering assumption.

---

## 8. Admission model

Strategies implement a common Go interface:

- FIFO (default)
- Lottery
- Scheduled slots
- Direct admission

Shared enforcement: global/per-session grant-rate limits, global/per-session active-checkout budgets, entitlement ownership, lifecycle validation.

Redis coordinates hot counters; PostgreSQL owns durable queue entries, grants, and entitlements. On Redis uncertainty: fence grants → preserve entitlements → reconcile → new epoch → resume only when safe.

---

## 9. Checkout orchestration (happy path)

1. Discover offering/session (Sale)
2. Join queue / receive entitlement (Admission)
3. Checkout with idempotency key (Order)
4. Snapshot policy + hold allowance (Order)
5. Bind entitlement (Admission)
6. Reserve inventory with stable op ID (Inventory)
7. Initiate payment (Payment Simulator)
8. On payment success: confirm reservation (Inventory)
9. Finalize order + commit allowance (Order)
10. Publish outbox events

Lost responses reconcile via the same operation identity.

---

## 10. Repository layout

```
gateflow/
├── go.work
├── services/{sale,admission,inventory,order,payment-simulator}/
├── apps/{buyer-web,operator-console}/
├── libs/go/                 # technical utilities only
├── contracts/{openapi,events,json-schema}/
├── deployments/docker/
├── docs/
└── scripts/
```

Go module prefix: `github.com/ghrushneshr25/gateflow`.

Shared Go libraries must not own domain state machines or business policies.

---

## 11. Local topology (Docker Compose)

- PostgreSQL 16
- Redis 7
- Kafka (KRaft mode preferred for local simplicity)
- OTel Collector, Prometheus, Grafana, Loki, Tempo
- Five Go service containers
- Two frontend dev/build containers (or host-run in early phases)

Kubernetes/Helm is Phase 7, after correctness gates pass.

---

## 12. Identity (initial)

- Development token adapter for local use
- Distinct buyer, operator, and service principals
- OIDC-compatible path reserved for later
- Payment callbacks authenticated and verified
- Sensitive operator actions audited (actor, action, target, timestamp, outcome, reason)

---

## 13. Observability principles

- Structured logs + distributed traces + service metrics
- Correlation/causation IDs on requests and events
- Liveness vs readiness health endpoints
- No high-cardinality metric labels (no buyer/order/entitlement IDs as labels)
- Critical failures must be actionable (metrics + durable workflow state + operator UI)

---

## 14. First vertical slice (Phase 2 target)

Narrow path with real persistence and contracts:

1. Operator creates/publishes one offering + session + pool
2. Buyer discovers offering
3. FIFO join → entitlement
4. Checkout → reserve → deterministic payment success → confirm
5. Display confirmed order
6. At least one durable domain event published via outbox

See `docs/execution/PHASE0.md` for acceptance tests and verification commands.

---

## 15. Decision index

See `docs/adr/` for ADRs 0001–0024 covering monorepo, Postgres topology, Kafka semantics, outbox, admission, inventory, order/payment, Redis recovery, auth, observability, CI, and performance methodology.
