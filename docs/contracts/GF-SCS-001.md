# GF-SCS-001 — Gateflow Service Contract Specification

**Document ID:** GF-SCS-001  
**Status:** Accepted baseline (Phase 0); OpenAPI/JSON Schema artifacts evolve in Phase 1–2  
**Module prefix:** `github.com/ghrushneshr25/gateflow`  
**API version prefix:** `/v1`

---

## 1. Conventions

### 1.1 HTTP

- REST/JSON over HTTPS in deployment; HTTP allowed locally.
- All public and service APIs under `/v1/...`.
- Time fields: RFC 3339 UTC (`2026-10-20T15:04:05Z`).
- IDs: ULID preferred (lexicographically sortable); UUID acceptable if documented per resource.
- Pagination: `limit` (default 20, max 100) + `cursor` opaque token.
- Authentication: `Authorization: Bearer <token>`.
- Idempotency: header `Idempotency-Key` on state-changing buyer/operator commands where specified; body may also carry `operation_id` for service-to-service calls.
- Correlation: incoming `X-Correlation-ID` propagated; generated if absent. Returned in response headers and error bodies.

### 1.2 Error envelope

```json
{
  "error": {
    "code": "INVENTORY_CAPACITY_EXCEEDED",
    "message": "Requested quantity exceeds remaining sellable capacity",
    "correlation_id": "01J...",
    "retryable": false,
    "status": "failed",
    "details": {}
  }
}
```

| Field | Meaning |
|---|---|
| `code` | Stable machine-readable code (SCREAMING_SNAKE) |
| `message` | Human-readable; safe for operators/buyers |
| `correlation_id` | Request correlation |
| `retryable` | Whether a blind retry of the **same** logical op is appropriate |
| `status` | `failed` \| `in_progress` \| `ambiguous` \| `conflict` |
| `details` | Optional structured context (no secrets) |

**Ambiguous:** caller must not invent a new operation ID; must lookup/reconcile.

### 1.3 Health

| Path | Meaning |
|---|---|
| `GET /healthz` | Liveness — process up |
| `GET /readyz` | Readiness — dependencies required for serving are OK |

### 1.4 Service base ports (local)

| Service | Port |
|---|---|
| Sale | 8081 |
| Admission | 8082 |
| Inventory | 8083 |
| Order | 8084 |
| Payment Simulator | 8085 |

---

## 2. Identity claims (dev adapter)

JWT claims (minimum):

```json
{
  "sub": "buyer_01J...",
  "principal_type": "buyer",
  "scopes": ["buyer:read", "buyer:checkout"],
  "iss": "gateflow-dev",
  "aud": ["gateflow"],
  "exp": 0
}
```

| `principal_type` | Use |
|---|---|
| `buyer` | Buyer Web |
| `operator` | Operator Console; scopes gate actions |
| `service` | S2S; audience + scope restricted |

Operator scopes (initial): `ops:sale:write`, `ops:sale:publish`, `ops:admission:read`, `ops:inventory:read`, `ops:order:read`, `ops:order:recover`, `ops:payment:recover`, `ops:audit:read`.

---

## 3. Shared defaults

| Parameter | Default | Notes |
|---|---|---|
| Purchase limit | 4 / buyer / session | Sale policy; Order enforces |
| Entitlement unbound TTL | 10 minutes | Binding ignores unbound TTL |
| Reservation lease | 5 minutes | Min 1m, max 15m |
| Payment outcome deadline | 3 minutes | Then Order → reconciliation; lease not extended |
| Idempotency retention | ≥ 24 hours | Longer via business records |
| Request timeout (S2S) | 2s default, 5s max for orchestration steps | Context deadlines mandatory |
| Outbox poll interval | 500ms (local) | With jitter |

---

## 4. Sale Service

### 4.1 Ownership

Offerings, sessions, ticket-type metadata, prices, sale windows, publication, purchase-policy versions, admission config references.

### 4.2 States

**Offering:** `DRAFT` → `PUBLISHED` → `UNPUBLISHED` → `PUBLISHED` (re-publish). Terminal archive later if needed: `ARCHIVED`.

**Session:** `SCHEDULED` → `ON_SALE` → `SALE_CLOSED` → `COMPLETED` | `CANCELLED`.

### 4.3 Key APIs (vertical slice minimum)

| Method | Path | Auth | Idempotent | Description |
|---|---|---|---|---|
| `POST` | `/v1/offerings` | operator `ops:sale:write` | key optional | Create offering |
| `GET` | `/v1/offerings/{id}` | buyer read or operator | — | Get offering |
| `GET` | `/v1/offerings` | buyer: published only | — | List |
| `POST` | `/v1/offerings/{id}/sessions` | operator write | key optional | Create session |
| `POST` | `/v1/sessions/{id}/ticket-types` | operator write | key optional | Configure ticket type + capacity intent |
| `POST` | `/v1/offerings/{id}/publish` | operator publish | key required | Publish |
| `POST` | `/v1/offerings/{id}/unpublish` | operator publish | key required | Unpublish |
| `GET` | `/v1/sessions/{id}/purchase-policy` | internal/order | — | Effective policy + version |

### 4.4 Publish semantics

- Publish requires ≥1 session with ≥1 ticket type and positive capacity configuration.
- On publish, Sale emits `sale.offering.published` and notifies Inventory of pool definitions via API or event (Inventory creates authoritative pools).
- Unpublish blocks new buyer-facing eligibility; does not delete orders/reservations.

### 4.5 Policy versions

Policy changes create a new `policy_version`. Active checkouts keep snapshotted version. Migration of holds is explicit and out of VS-1.

### 4.6 Errors (selected)

| Code | When |
|---|---|
| `SALE_VALIDATION_FAILED` | Invalid fields/windows |
| `SALE_INVALID_TRANSITION` | Illegal publish/state change |
| `SALE_NOT_FOUND` | Missing resource |
| `SALE_NOT_PUBLISHED` | Buyer access to draft |

---

## 5. Admission Service

### 5.1 Ownership

Queue entries, durable sequence, strategies, grants, entitlements, rate budgets, active-checkout budgets, coordination epoch/fence state.

### 5.2 Strategy interface (Go)

```go
type Strategy interface {
    Name() string
    // Evaluate decides whether the next grant may proceed for this entry
    // under durable state; shared budget checks remain outside strategies.
    Evaluate(ctx context.Context, in GrantInput) (GrantDecision, error)
}
```

Strategies: `fifo` (default), `lottery`, `scheduled_slot`, `direct`.  
Strategies **must not** bypass budget enforcement or entitlement lifecycle.

### 5.3 Queue entry states

`JOINED` → `ELIGIBLE` → `GRANTED` → `LEFT` | `EXPIRED` | `REMOVED`

### 5.4 Entitlement states

`ISSUED` → `BOUND` → `CONSUMED` | `EXPIRED` | `REVOKED`

- Bind: atomic to `(buyer_id, checkout_operation_id)`.
- Consumed after successful reservation established (Order drives).
- Ambiguous bind: remain bound; no reuse.

### 5.5 Budget model

| Budget | Unit | Reserve | Commit | Release |
|---|---|---|---|---|
| Global grant rate | grants / window | on grant attempt | on durable entitlement issue | on definitive grant failure |
| Per-session grant rate | grants / window | same | same | same |
| Global active checkout | concurrent checkouts | on entitlement bind start | on bind success | on checkout terminal or entitlement release policy |
| Per-session active checkout | concurrent checkouts | same | same | same |

Inventory capacity is **not** an admission budget.

### 5.6 Redis recovery protocol

1. Detect coordination uncertainty (error threshold, fence mismatch, or explicit outage).
2. **Fence:** stop new grants; set/advance `coordination_epoch`; refuse grants with stale epoch.
3. Preserve durable entitlements in PostgreSQL.
4. Reconcile outstanding durable grants vs Redis counters from PG truth.
5. Rebuild hot projections under new epoch.
6. Resume only when reconciliation marks epoch `ACTIVE` and stale workers cannot pass fence checks.
7. Never blind-reset counters to zero without reconciliation proof.

### 5.7 Key APIs

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/v1/sessions/{session_id}/queue/joins` | buyer | Idempotent join |
| `GET` | `/v1/sessions/{session_id}/queue/me` | buyer | Position/status |
| `POST` | `/v1/sessions/{session_id}/grants` | internal/worker | Attempt grant under strategy+budgets |
| `GET` | `/v1/entitlements/{id}` | buyer or internal | Lookup |
| `POST` | `/v1/entitlements/{id}/bind` | service order | Bind to checkout |
| `POST` | `/v1/entitlements/{id}/consume` | service order | Consume after reservation |
| `GET` | `/v1/admin/budgets` | operator read | Inspect budgets |
| `GET` | `/v1/admin/coordination` | operator read | Epoch/fence health |

Join idempotency scope: `(buyer_id, session_id, queue_scope)`.

### 5.8 Errors (selected)

| Code | Status |
|---|---|
| `ADM_QUEUE_CLOSED` | failed |
| `ADM_BUDGET_EXHAUSTED` | failed, retryable later |
| `ADM_ENTITLEMENT_NOT_FOUND` | failed |
| `ADM_ENTITLEMENT_EXPIRED` | failed |
| `ADM_ENTITLEMENT_BOUND_OTHER` | conflict |
| `ADM_COORDINATION_FENCED` | failed, retryable after recovery |
| `ADM_IN_PROGRESS` | in_progress |

### 5.9 Lottery / slots / direct (baseline)

- **Lottery:** seeded draw over eligible set; seed and algorithm version stored on grant for audit.
- **Scheduled slots:** grants only inside configured windows; still budget-bound.
- **Direct:** no waiting queue; still rate + active-checkout budgets.

---

## 6. Inventory Service

### 6.1 Ownership

Pools, capacity, reservations, expiry deadlines, confirmation, release, inventory operation IDs.

### 6.2 Invariant

```
confirmed_qty + active_reserved_qty <= sellable_capacity
```

Enforced with atomic SQL conditional updates and/or row locks + check constraints. **No read-then-write without serialization.**

### 6.3 Reservation states

`ACTIVE` → `CONFIRMED` | `EXPIRED` | `RELEASED`

- Expiry deadline set at durable creation (UTC).
- Confirmation vs expiry: single conditional transition wins.
- Expired cannot revive.
- Cleanup worker is not authoritative for eligibility; deadline is.

### 6.4 Key APIs

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/v1/pools` | service sale/ops | Create/update pool from sale config |
| `GET` | `/v1/pools/{id}` | operator/internal | Capacity snapshot |
| `POST` | `/v1/reservations` | service order | Reserve (`operation_id` required) |
| `GET` | `/v1/reservations/by-operation/{operation_id}` | service order | Lookup for reconcile |
| `GET` | `/v1/reservations/{id}` | service/ops | Get |
| `POST` | `/v1/reservations/{id}/confirm` | service order | Confirm if ACTIVE and not past deadline |
| `POST` | `/v1/reservations/{id}/release` | service order | Release ACTIVE |

### 6.5 Reserve request (conceptual)

```json
{
  "operation_id": "invop_...",
  "pool_id": "pool_...",
  "quantity": 2,
  "buyer_id": "buyer_...",
  "order_id": "ord_...",
  "lease_seconds": 300,
  "correlation_id": "..."
}
```

Idempotent on `operation_id`: same payload → same reservation; conflict payload → `INV_IDEMPOTENCY_CONFLICT`.

### 6.6 Errors (selected)

| Code | Meaning |
|---|---|
| `INV_CAPACITY_EXCEEDED` | Insufficient remaining capacity |
| `INV_RESERVATION_NOT_ACTIVE` | Confirm/release illegal |
| `INV_RESERVATION_EXPIRED` | Past deadline |
| `INV_IDEMPOTENCY_CONFLICT` | Key reuse conflict |
| `INV_NOT_FOUND` | Missing |
| `INV_AMBIGUOUS` | Caller must lookup |

---

## 7. Order Service

### 7.1 Ownership

Checkout/orders, transitions, idempotency records, allowance holds/commits, policy snapshots, reconciliation, cancellation orchestration.

### 7.2 Order states

```
CREATED
  → ENTITLEMENT_BOUND
  → RESERVATION_HELD
  → PAYMENT_PENDING
  → PAYMENT_SUCCEEDED
  → INVENTORY_CONFIRMING
  → CONFIRMED
  → CANCELLED
  → FAILED
  → RECONCILIATION_REQUIRED
```

Notes:

- `PAYMENT_SUCCEEDED` is not terminal success.
- `CONFIRMED` requires inventory confirmation established.
- `RECONCILIATION_REQUIRED` for unknown payment, lost responses past deadline, late payment after expiry, etc.
- Terminal: `CONFIRMED`, `CANCELLED`, `FAILED` (with explicit recovery transitions only if defined).

### 7.3 Allowance

Atomic:

```
committed + held + requested <= effective_limit
```

- Hold on checkout start (policy snapshot).
- Commit on order confirm.
- Release hold on definitive failure/cancel when safe.
- Ambiguous: retain hold.

### 7.4 Key APIs

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/v1/checkouts` | buyer | Start/resume checkout (`Idempotency-Key`) |
| `GET` | `/v1/orders/{id}` | buyer owner or ops | Get order |
| `GET` | `/v1/orders` | buyer | History |
| `POST` | `/v1/orders/{id}/cancel` | buyer or ops recover | Cancel workflow |
| `POST` | `/v1/orders/{id}/reconcile` | ops recover / internal | Drive reconciliation |
| `GET` | `/v1/admin/reconciliation` | ops | Backlog |

### 7.5 Checkout orchestration steps

1. Validate auth buyer.
2. Create/get order by idempotency key.
3. Snapshot purchase policy from Sale.
4. Atomic allowance hold.
5. Bind entitlement (Admission).
6. Reserve inventory (`operation_id = order.inventory_operation_id`).
7. On ambiguous: lookup reservation by operation ID; do not new-op.
8. Start payment (`payment_operation_id` stable).
9. On payment success: confirm inventory; if expired → `RECONCILIATION_REQUIRED` + compensation request.
10. On inventory confirmed: order `CONFIRMED`, commit allowance, outbox events.
11. Payment outcome deadline 3m without result → `RECONCILIATION_REQUIRED` (lease not extended).

### 7.6 Errors (selected)

| Code | Meaning |
|---|---|
| `ORD_IDEMPOTENCY_CONFLICT` | Conflicting key reuse |
| `ORD_PURCHASE_LIMIT_EXCEEDED` | Allowance |
| `ORD_ENTITLEMENT_INVALID` | Missing/expired/wrong buyer |
| `ORD_IN_PROGRESS` | Workflow running |
| `ORD_RECONCILIATION_REQUIRED` | Needs reconcile |
| `ORD_INVALID_TRANSITION` | Illegal state change |

---

## 8. Payment Simulator

### 8.1 Ownership

Payment attempts, simulated outcomes, delayed/duplicate callbacks, compensation operations.

**Not a real payment processor.**

### 8.2 Payment attempt states

`CREATED` → `PENDING` → `SUCCEEDED` | `FAILED` | `UNKNOWN`

### 8.3 Compensation states

`CREATED` → `PENDING` → `SUCCEEDED` | `FAILED` | `UNKNOWN`

After first **definitive** compensation `FAILED`: escalate, expose in Operator Console, allow authorized idempotent manual retry. If `UNKNOWN`, query before retry. Never mark success without verified outcome.

### 8.4 Scenario controls (test)

Request field `scenario` (or header `X-Payment-Scenario`):

| Scenario | Behavior |
|---|---|
| `success` | Immediate success (default VS-1) |
| `failure` | Definitive failure |
| `delay_success` | Succeed after N ms / after callback |
| `duplicate_callback` | Emit callback twice |
| `compensation_fail` | Compensation definitive fail once |

### 8.5 Key APIs

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/v1/payments` | service order | Start attempt (`operation_id`) |
| `GET` | `/v1/payments/by-operation/{operation_id}` | service/ops | Lookup |
| `POST` | `/v1/payments/{id}/callbacks` | verified callback auth | Simulator/internal callback |
| `POST` | `/v1/compensations` | service order | Create/get compensation |
| `POST` | `/v1/compensations/{id}/retry` | ops recover | Manual retry |
| `GET` | `/v1/compensations/{id}` | ops | Inspect |

Callback authentication: shared HMAC or service JWT with `payment:callback` scope; reject unverified.

### 8.6 Late payment success contract

If payment → `SUCCEEDED` after reservation expired:

1. Do not revive reservation.
2. Keep payment `SUCCEEDED`.
3. Order → `RECONCILIATION_REQUIRED`.
4. Create/get same compensation operation.
5. Order stays unconfirmed unless valid inventory confirmation exists.
6. Visible to operators with correlation IDs.

---

## 9. Events

### 9.1 Envelope (JSON Schema committed under `contracts/json-schema/`)

```json
{
  "event_id": "evt_...",
  "event_type": "order.order.confirmed",
  "schema_version": 1,
  "occurred_at": "2026-10-20T15:04:05Z",
  "producer": "order",
  "correlation_id": "...",
  "causation_id": "...",
  "payload": {}
}
```

### 9.2 Topic naming

```
gateflow.<domain>.<entity>.<event>
```

Examples:

- `gateflow.sale.offering.published`
- `gateflow.admission.entitlement.issued`
- `gateflow.inventory.reservation.confirmed`
- `gateflow.order.order.confirmed`
- `gateflow.payment.payment.succeeded`
- `gateflow.payment.compensation.failed`

### 9.3 Delivery semantics

- Producer: owning service via transactional outbox.
- Kafka: at-least-once.
- Consumer: idempotent on `event_id` (and/or business op id).
- Partition key: primary entity id (e.g., `order_id`, `session_id`).
- No global order.
- DLQ topic per consumer group after N retries with backoff.
- Retention: local default 7 days (configurable).
- Replay: allowed; consumers must tolerate duplicates.

### 9.4 Compatibility

- Additive optional fields preferred within `schema_version`.
- Breaking changes require `schema_version` bump and coordinated deploy.
- CI validates schemas; unknown required field failures block.

---

## 10. Idempotency record model

Stored by owning service:

| Field | Description |
|---|---|
| `principal_id` | Authenticated actor |
| `operation_type` | e.g. `checkout.create` |
| `resource_context` | e.g. session/order scope |
| `idempotency_key` | Client key |
| `request_hash` | Hash of canonical payload |
| `state` | `started` \| `succeeded` \| `failed` |
| `response_ref` | Result pointer |
| `created_at` / `expires_at` | Retention |

Conflict: same key, different hash → `*_IDEMPOTENCY_CONFLICT` (HTTP 409).

---

## 11. Timeouts, retries, ambiguity

| Situation | Behavior |
|---|---|
| Client timeout before response | Treat as **ambiguous**; GET by operation/idempotency |
| S2S timeout | Bounded retries only for **idempotent** reads or same op id writes |
| Non-idempotent without op id | Forbidden |
| Kafka redelivery | Consumer dedupe |
| Payment unknown past 3m | Order reconciliation |

Retry-After may be set for budget exhaustion.

---

## 12. Observability contract

Required metric families (low cardinality labels: `service`, `operation`, `result`, `strategy`, `session_shard` if needed — **not** entity UUIDs):

- Admission grant latency, budget exhaust counts, fence state
- Reservation success/expire/fail
- Order transition counts, reconciliation backlog gauge
- Payment outcome counts, compensation fail gauge
- Kafka consumer lag
- Redis health / coordination epoch state
- DB pool in-use / wait

Logs: structured JSON; include correlation_id; never tokens/secrets/PAN.

---

## 13. Vertical-slice contract freeze (VS-1)

Locked for first E2E:

- Sale publish + list
- Admission FIFO join + entitlement issue + bind
- Inventory reserve/confirm/lookup
- Order checkout happy path
- Payment `success` scenario
- Event: `order.order.confirmed` (+ optional reservation confirmed)

All other endpoints may exist as stubs returning `501` only if explicitly labeled temporary; prefer omitting until implemented.

---

## 14. Open items tracked for later phases

| ID | Item | Phase |
|---|---|---|
| O-01 | Full OpenAPI YAML per service | P1–P2 |
| O-02 | Lottery algorithm ADR detail + tests | P3+ |
| O-03 | Capacity reduction policy edge cases | P3 |
| O-04 | Full operator permission matrix UI | P5 |
| O-05 | Helm/K8s | P7 |

---

## 15. Change control

Contract changes that break consumers require:

1. SCS update
2. Schema/OpenAPI bump
3. ADR if architectural
4. CI contract tests green
5. Coordinated service rollout notes in PR
