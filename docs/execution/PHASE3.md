# Phase 3 Report — Concurrency and Invariants

**Date:** 2026-10-20  
**Status:** Complete (verified)

---

## Baseline (before Phase 3 work)

| Command | Result |
|---|---|
| `make test` | PASS |
| `go test -tags=integration ./services/inventory/tests` | PASS (last-unit only) |

### Gaps closed

| Gap | Fix |
|---|---|
| Purchase-limit race (SUM without lock) | `allowance_scopes` row `FOR UPDATE` |
| Idempotency race / no request hash | `request_hash` + locked create path |
| No reservation expire API/worker path | `Expire` / `ExpireDue`; confirm↔expire race |
| No admission budgets | PG `budget_scopes` (global + session) |
| Join race | `ON CONFLICT` upsert |
| Lost-response dual checkout | Return existing order by idempotency key; no second orchestrate |
| Outbox consumer dedupe | `processed_events` (admission + order) |

---

## Implementation stages

### A — Order
- Migration `000003_phase3`
- Serialized allowance; durable request hash; concurrent create returns same order

### B — Inventory
- `Expire`, `ExpireDue`; reserve idempotency inside single tx
- APIs: `POST .../expire`, `POST .../expire-due`

### C — Admission
- Migration `000003_phase3_budgets`
- Grant tokens + active-checkout slots under row locks
- Caps update does **not** reset counters (no blind Redis-style zeroing)
- PG is budget authority (ADR-0019 Redis fence remains Phase 4)

### D — Recovery
- Checkout returns established order for any status on same key/hash
- Concurrent insert loser does not dual-orchestrate
- Inventory/payment already operation-id idempotent

---

## Verification (executed)

```text
go test -race -tags=integration -count=1 ./services/inventory/tests/   PASS
go test -race -tags=integration -count=1 ./services/order/tests/       PASS
go test -race -tags=integration -count=1 ./services/admission/tests/   PASS
go test -race ./libs/go/...                                            PASS
VS-1 E2E (manual timed script)                                         PASS status=CONFIRMED
```

### Concurrency scenarios covered

1. 20-way last-unit inventory reserve → exactly 1 success  
2. Confirm vs expire race past deadline → EXPIRED, counters consistent, no revive  
3. Idempotent reserve same operation_id ×15 → single reservation  
4. Release idempotent → no double-decrement  
5. 20-way purchase limit 4 → exactly 4 holds  
6. Idempotent order create same key → one hold  
7. Conflicting request_hash → ErrIdempotency  
8. Grant budget max=3 ×12 workers → exactly 3 grants  
9. Active checkout budget max=2 → third bind exhausted  
10. Idempotent join/grant → sequence stable; grant counted once  
11. Event processed_events first-wins dedupe  

---

## Invariants tested

- `confirmed + active_reserved <= capacity`  
- `held + committed <= max_per_buyer` under concurrency  
- Expired cannot confirm  
- Duplicate logical ops blocked  
- Payment success still requires inventory confirm for `CONFIRMED` (VS-1 path)  

---

## ADR / contract

- No ADR superseded  
- Clarified in change record: Phase 3 budgets are PostgreSQL-authoritative  
- GF-SCS-001 budget codes `ADM_BUDGET_EXHAUSTED` used (429)

---

## Limitations / Phase 4 follow-up

- Redis hot-path + fencing protocol not implemented (budgets work multi-instance via PG)  
- No background expire worker process (API `expire-due` only)  
- Order does not auto-resume partial orchestration after crash mid-flight (returns in-progress state; full resume is Phase 4)  
- Kafka publisher still not shipping outbox rows  
- Rate windows are capacity counters, not time-sliding windows  

---

## Next gate

Phase 4 — lost-response full resume, Redis fence when introduced, Kafka replay, late payment, compensation.
