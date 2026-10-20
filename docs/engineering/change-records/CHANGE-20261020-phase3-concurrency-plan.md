# CHANGE-20261020 — Phase 3 concurrency & invariants (decision + completion)

**Status:** Implementation plan (updated with verification results at end)  
**Phase:** 3  
**Date:** 2026-10-20

---

## 1. Baseline (executed before changes)

| Check | Result |
|---|---|
| `make test` | PASS (libs unit tests) |
| `go test -tags=integration ./services/inventory/tests` | PASS (20-way last-unit reserve) |
| Git | Uncommitted Phase 0–2 work on `main` @ `3626425`; preserved |

### VS-1 gaps vs Phase 3 needs

| Property | VS-1 state | Phase 3 gap |
|---|---|---|
| Inventory capacity under concurrency | FOR UPDATE + CHECK; test exists | Confirm↔expire race; expire worker; no-revive |
| Purchase limit under concurrency | SUM in tx **without** buyer/session lock | **Race:** concurrent CreateWithHold can overshoot |
| Idempotency conflict | Handler compares fields after lookup | **Race** on concurrent first insert; no durable request_hash |
| Reservation expiry | Confirm path can expire if past deadline | No bulk expire; no explicit expire API race tests |
| Admission budgets | None | Grants unlimited; no active-checkout budget |
| Multi-instance admission | N/A | Need durable counters (PG authority) |
| Lost-response recovery | Inventory op_id lookup exists | Order must resume in-progress; payment by op_id |
| Outbox dedupe | Insert only | Consumer processed-event table missing |
| Redis fence | Not implemented | Phase 3: **PG is budget authority**; Redis optional later (ADR-0019 remains; no contradiction) |

---

## 2. Invariants (non-negotiable)

1. `confirmed + active_reserved <= sellable_capacity`
2. `committed + held + requested <= max_per_buyer` (session scope)
3. Same idempotency key + same payload → same order; different payload → stable conflict
4. Expired reservation cannot become CONFIRMED/ACTIVE again
5. Timeout/ambiguity → lookup by operation_id / idempotency key; no new logical op
6. Payment success alone ≠ order CONFIRMED
7. Holds stay HELD while checkout ambiguous/in-progress
8. Admission budgets durable; failure of Redis (if unused) cannot zero them

---

## 3. Implementation sequence

### Stage A — Order: purchase-limit lock + idempotency hash
- `allowance_scopes` row locked `FOR UPDATE` per (buyer, session)
- `request_hash` on orders; conflict detection durable
- Concurrent create same key: unique constraint → return existing or conflict
- Tests: concurrent limit breach; idempotent replay; payload conflict

### Stage B — Inventory: expiry, confirm/expire race, release
- `Expire(id)` and `ExpireDue(now)` conditional transitions
- Confirm after expiry → ErrExpired, status EXPIRED, no revive
- Tests: concurrent confirm vs expire; expire then confirm fails; release idempotent

### Stage C — Admission: durable budgets + FIFO multi-instance safety
- PG tables: `budget_scopes` (global + per-session)
- Grant consumes grant token under row lock; bind consumes active-checkout slot
- Consume/fail releases active-checkout
- Join remains unique (session, buyer); sequence durable
- Tests: budget exhaustion; concurrent grants; concurrent binds

### Stage D — Recovery: lost response / in-progress resume
- Order checkout: if existing non-terminal order for key, return it (do not second-create)
- Inventory reserve already idempotent on operation_id (harden inside one tx)
- Payment already idempotent on operation_id
- Test: double reserve same op_id; double checkout same key mid-flight shape

**Non-goals this phase:** Kafka publisher, Redis hot-path fence protocol (Phase 4), latency optimization, new product features, K8s.

---

## 4. Transaction / lock strategy

| Resource | Serialization |
|---|---|
| Inventory pool | `SELECT … FOR UPDATE` on pool row |
| Reservation transition | `FOR UPDATE` on reservation row; status predicates |
| Allowance | `FOR UPDATE` on `allowance_scopes(buyer_id, session_id)` then sum holds |
| Idempotency | UNIQUE(buyer_id, idempotency_key) + request_hash compare |
| Admission budget | `FOR UPDATE` on `budget_scopes(scope_key)` |
| Queue entry / entitlement | Existing `FOR UPDATE` on queue/entitlement rows |

---

## 5. ADR impact

- **No ADR supersession required.** ADR-0013/14/15/18/19 remain.
- Clarification recorded here: Phase 3 implements **PostgreSQL-authoritative** admission budgets so multi-instance correctness holds without Redis. ADR-0019 Redis fencing remains the plan when Redis is introduced as a hot-path coordinator (Phase 4).

---

## 6. Verification gates (must execute)

```bash
make test
go test -race ./libs/go/...
go test -race ./services/inventory/internal/store/...  # if unit-able
go test -tags=integration -count=1 ./services/inventory/tests/...
go test -tags=integration -count=1 ./services/order/tests/...
go test -tags=integration -count=1 ./services/admission/tests/...
./scripts/migrate.sh
./scripts/vs1_e2e.sh   # regression
```

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| Advisory lock hash collision | Prefer real scope row PK lock |
| Expire worker double-decrement | Conditional status update only from ACTIVE |
| Budget token refill | No blind reset; fixed capacity for VS/Phase3 tests |
| Order fail releases hold while payment unknown | Fail only on definitive entitlement/reserve failure paths |

---

## 8. Completion evidence

### Files changed (primary)

- `services/order/migrations/000003_phase3.*`, `000004_outbox_dedupe.*`
- `services/order/internal/store/store.go`, `internal/httpserver/server.go`, `tests/concurrency_test.go`
- `services/inventory/internal/store/store.go`, `internal/httpserver/server.go`, `tests/expiry_race_test.go`
- `services/admission/migrations/000003_phase3_budgets.*`
- `services/admission/internal/store/store.go`, `internal/httpserver/server.go`, `tests/budget_test.go`
- `docs/execution/PHASE3.md`

### Verification (actual)

| Command | Result |
|---|---|
| `go test -race -tags=integration -count=1 ./services/inventory/tests/` | PASS |
| `go test -race -tags=integration -count=1 ./services/order/tests/` | PASS |
| `go test -race -tags=integration -count=1 ./services/admission/tests/` | PASS |
| `go test -race ./libs/go/...` | PASS |
| VS-1 E2E checkout → CONFIRMED | PASS |

### Known limitations

See `docs/execution/PHASE3.md` § Limitations.
