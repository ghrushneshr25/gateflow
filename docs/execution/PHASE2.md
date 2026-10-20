# Phase 2 Report — Vertical Slice VS-1

**Date:** 2026-10-20  
**Status:** Complete (backend E2E verified)

---

## Outcome

Happy-path FIFO booking works end-to-end against real PostgreSQL:

1. Create/publish offering + session + ticket type  
2. Inventory pool created on publish  
3. Idempotent queue join + entitlement grant  
4. Checkout with idempotency key → reserve → payment success → inventory confirm → order `CONFIRMED`  
5. Outbox row written in same transaction as confirmation  
6. Idempotent checkout replay returns same order  

## Verification (executed)

```text
./scripts/migrate.sh          → MIGRATIONS_OK
start all five binaries       → /healthz + /readyz OK on 8081–8085
./scripts/vs1_e2e.sh          → VS1_E2E_PASS order=ord_... status=CONFIRMED
```

| Acceptance | Result |
|---|---|
| VS1-01 published visible | PASS |
| VS1-02 double join idempotent | PASS (script rejoin) |
| VS1-03 entitlement issued | PASS |
| VS1-04 checkout idempotent | PASS |
| VS1-07 confirmed after payment | PASS |
| VS1-09 outbox on confirm | Implemented in `order` Confirm txn |
| VS1-06 concurrent last unit | **PASS** — `go test -tags=integration ./services/inventory/tests` (20 goroutines, capacity 1 → exactly 1 success) |
| VS1-05 conflicting key | Implemented in Order handler |
| VS1-08 refresh | Order GET durable in PG |
| VS1-10 zero oversell | **PASS** (invariant + concurrency test) |

## Files of note

- `services/{sale,admission,inventory,order,payment-simulator}/` domain + migrations  
- `scripts/migrate.sh`, `scripts/vs1_e2e.sh`  
- Buyer Web lists offerings/orders from live APIs  

## Limitations (explicit)

- Auth still header `X-Buyer-ID` (dev); JWT wiring next  
- Admission budgets / Redis fencing not yet enforced (Phase 3–4)  
- Kafka outbox publisher not yet shipping to Kafka (row durable only)  
- Operator console not wired to create offerings yet  
- Full Playwright E2E not yet  
- CORS not configured for browser cross-origin (may need proxy for UI)

## Next

Phase 3 — concurrency hardening, purchase-limit races, reservation expiry worker, multi-instance budgets.
